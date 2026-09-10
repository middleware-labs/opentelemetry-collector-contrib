// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// The lock guard exists to stop this receiver from joining a lock queue that
// the customer's own application queries then queue behind. These tests pin the
// three properties that make it work: it asks the right question, it reports
// what is locked, and a failure to ask is never silently treated as "nothing is
// locked".

func TestLockedRelationsQueryShape(t *testing.T) {
	q := lockedRelationsQuery()

	// Only ACCESS EXCLUSIVE conflicts with the ACCESS SHARE that catalog
	// introspection takes. Filtering on a weaker mode would skip tables we
	// could have read safely.
	assert.Contains(t, q, "AccessExclusiveLock")

	// pg_locks is cluster-wide and relation OIDs are only unique within a
	// database. Without the database predicate we would skip an unrelated
	// table that happens to share an OID with a locked relation elsewhere.
	assert.Contains(t, q, "current_database()")

	// Advisory, transaction and tuple locks cannot block catalog reads.
	assert.Contains(t, q, "locktype = 'relation'")
}

func TestLockedRelationsIncludesPendingRequests(t *testing.T) {
	// A pending (granted = false) ACCESS EXCLUSIVE request means a DDL
	// statement is already waiting. Introspecting that relation would put us
	// behind the waiter, which is the pile-up the guard exists to prevent, so
	// the query must NOT filter on granted.
	assert.NotContains(t, lockedRelationsQuery(), "granted")
}

func TestLockedRelationsReturnsLockedOIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT l.relation FROM pg_locks").
		WillReturnRows(sqlmock.NewRows([]string{"relation"}).
			AddRow(uint32(16385)).
			AddRow(uint32(16390)))

	locked, err := lockedRelations(context.Background(), WrapDBWithIgnore(db))
	require.NoError(t, err)

	assert.Len(t, locked, 2)
	assert.Contains(t, locked, uint32(16385))
	assert.Contains(t, locked, uint32(16390))
	assert.NotContains(t, locked, uint32(16386))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLockedRelationsEmptyWhenNothingLocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT l.relation FROM pg_locks").
		WillReturnRows(sqlmock.NewRows([]string{"relation"}))

	locked, err := lockedRelations(context.Background(), WrapDBWithIgnore(db))
	require.NoError(t, err)
	assert.Empty(t, locked)
}

func TestLockedRelationsErrorsRatherThanReportingNothingLocked(t *testing.T) {
	// The critical distinction. An empty set means "introspect everything",
	// which is precisely the unguarded behaviour that can stall a customer's
	// application. A query failure must therefore surface as an error so the
	// caller can decide, not degrade into an empty set.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT l.relation FROM pg_locks").
		WillReturnError(errors.New("permission denied for view pg_locks"))

	locked, err := lockedRelations(context.Background(), WrapDBWithIgnore(db))
	require.Error(t, err)
	assert.Nil(t, locked)
}

func TestIsLockTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"lock timeout by SQLSTATE", &pq.Error{Code: "55P03"}, true},
		{"statement timeout is not a lock timeout", &pq.Error{Code: "57014"}, false},
		{"unrelated server error", &pq.Error{Code: "42501"}, false},
		{
			// Errors are routinely annotated with fmt.Errorf on the way up, so
			// the check has to see through wrapping.
			"wrapped lock timeout",
			fmt.Errorf("collecting columns: %w", &pq.Error{Code: "55P03"}),
			true,
		},
		{
			// A pooler or proxy may hand back a plain error with no SQLSTATE.
			"message fallback when no SQLSTATE",
			errors.New("pq: canceling statement due to lock timeout"),
			true,
		},
		{"plain unrelated error", errors.New("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isLockTimeout(tt.err))
		})
	}
}

func TestIsStatementTimeout(t *testing.T) {
	assert.True(t, isStatementTimeout(&pq.Error{Code: "57014"}))
	assert.False(t, isStatementTimeout(&pq.Error{Code: "55P03"}))
	assert.False(t, isStatementTimeout(nil))
	assert.True(t, isStatementTimeout(
		fmt.Errorf("wrapped: %w", &pq.Error{Code: "57014"})))
	assert.True(t, isStatementTimeout(
		errors.New("pq: canceling statement due to statement timeout")))
}

func TestSQLStateOf(t *testing.T) {
	assert.Equal(t, "55P03", sqlStateOf(&pq.Error{Code: "55P03"}))
	assert.Equal(t, "55P03", sqlStateOf(fmt.Errorf("wrap: %w", &pq.Error{Code: "55P03"})))
	// A non-server error has no SQLSTATE, and must not be confused with one.
	assert.Equal(t, "", sqlStateOf(errors.New("dial tcp: connection refused")))
	assert.Equal(t, "", sqlStateOf(nil))
}

// TestSchemaCollectionSkipsLockedTable is the end-to-end proof of the guard:
// when pg_locks reports a table under ACCESS EXCLUSIVE lock, the collector must
// not introspect it.
//
// The assertion is on observable state, not on the mock's expectation list.
// sqlmock's ExpectationsWereMet only checks that queued expectations were used,
// and enrichTableDefinition deliberately swallows per-query errors (it warns and
// continues), so an unexpected query would be silently absorbed and prove
// nothing. Instead this asserts the table came back flagged and with no columns
// — which is only true if the per-table queries really were skipped.
func TestSchemaCollectionSkipsLockedTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	factory := &mockPostgreSQLClientFactory{db: db}

	cfg := createDefaultConfig().(*Config)
	cfg.SchemaCollection.Enabled = true
	cfg.SchemaCollection.CollectionInterval = time.Nanosecond // never throttle
	cfg.Databases = []string{"postgres"}

	settings := receivertest.NewNopSettings(metadata.Type)
	settings.Logger = zap.NewNop()

	scraper := newPostgreSQLScraper(settings, cfg, factory, newCache(1), newTTLCache[string](1, time.Second))

	const lockedOID = uint32(16385)

	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))
	setupCloudDetectorExpectations(mock)

	mock.ExpectQuery(`SELECT oid FROM pg_database`).
		WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(16384))
	mock.ExpectQuery(`SELECT pg_total_relation_size`).
		WillReturnRows(sqlmock.NewRows([]string{"pg_total_relation_size"}).AddRow(0))

	// The table is visible in the lock-free catalog scan...
	mock.ExpectQuery("SELECT.*pg_class.*pg_namespace").WillReturnRows(sqlmock.NewRows([]string{
		"oid", "schema", "table", "type", "hasoids", "tablespace", "desc", "owner", "xmin", "total_size",
	}).AddRow(lockedOID, "public", "locked_table", "r", false, 0, nil, "postgres", 100, 2048))

	// ...but pg_locks reports it as exclusively locked.
	mock.ExpectQuery("SELECT l.relation FROM pg_locks").
		WillReturnRows(sqlmock.NewRows([]string{"relation"}).AddRow(lockedOID))

	// The per-table queries are queued as *optional* answers that would succeed
	// if issued. This is deliberate and load-bearing: if they were absent, a
	// collector that ignored the lock would fail those queries for lack of a
	// mock answer, emit zero columns anyway, and the assertion below would pass
	// against broken code. By making success available, the only thing that can
	// keep the column count at zero is the skip actually happening.
	//
	// sqlmock matches expectations in order and does not require them to be
	// consumed unless ExpectationsWereMet is asked to check, so queuing these
	// is safe for the passing path.
	mock.MatchExpectationsInOrder(false)
	mock.ExpectQuery("SELECT.*pg_attribute").WithArgs(lockedOID).WillReturnRows(sqlmock.NewRows([]string{
		"attnum", "name", "type", "typeoid", "mod", "notnull", "hasdef", "def", "desc", "coll", "xmin",
	}).AddRow(1, "id", "integer", 23, -1, true, false, nil, nil, 0, 100))
	mock.ExpectQuery("SELECT.*pg_index").WithArgs(lockedOID).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "table", "primary", "unique", "valid", "exclusion", "type", "def", "partial", "xmin", "size",
	}))
	mock.ExpectQuery("SELECT.*pg_constraint").WithArgs(lockedOID).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "type", "table", "def", "deferrable", "deferred", "validated",
	}))
	mock.ExpectQuery("SELECT.*pg_stat_user_tables").WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{
		"relid", "live", "dead", "mod", "vac", "autovac", "ana", "autoana", "seq", "seq_read", "idx", "idx_fetch", "size", "total_size",
	}).AddRow(lockedOID, 100, 0, 0, nil, nil, nil, nil, 0, 0, 0, 0, 1024, 2048))

	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)
	// ExpectationsWereMet is deliberately NOT asserted here: the per-table
	// expectations above are meant to go unconsumed.

	// The table is still reported. A locked table must not look dropped: the
	// change tracker and downstream consumers need to see it still exists.
	require.Positive(t, logs.ResourceLogs().Len())

	locked := findTableRecord(t, logs, "locked_table")

	// Skipped, not introspected: no columns were read. With the skip removed
	// this is 1, because the mock would answer the pg_attribute query.
	count, ok := locked.Attributes().Get("table.column_count")
	require.True(t, ok, "table.column_count must be emitted")
	assert.Equal(t, int64(0), count.Int(),
		"a locked table must be emitted with no introspected columns")
}

// findTableRecord returns the emitted log record for the named table.
func findTableRecord(t *testing.T, logs plog.Logs, tableName string) plog.LogRecord {
	t.Helper()
	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		sls := logs.ResourceLogs().At(i).ScopeLogs()
		for j := 0; j < sls.Len(); j++ {
			recs := sls.At(j).LogRecords()
			for k := 0; k < recs.Len(); k++ {
				rec := recs.At(k)
				if name, ok := rec.Attributes().Get("table.name"); ok && name.Str() == tableName {
					return rec
				}
			}
		}
	}
	t.Fatalf("no emitted record found for table %q", tableName)
	return plog.NewLogRecord()
}

// TestSchemaCollectionEnrichesUnlockedTable is the control for the test above.
// Without it, the skip test would still pass if the collector had simply stopped
// introspecting tables altogether.
func TestSchemaCollectionEnrichesUnlockedTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	factory := &mockPostgreSQLClientFactory{db: db}

	cfg := createDefaultConfig().(*Config)
	cfg.SchemaCollection.Enabled = true
	cfg.SchemaCollection.CollectionInterval = time.Nanosecond
	cfg.Databases = []string{"postgres"}

	settings := receivertest.NewNopSettings(metadata.Type)
	settings.Logger = zap.NewNop()

	scraper := newPostgreSQLScraper(settings, cfg, factory, newCache(1), newTTLCache[string](1, time.Second))

	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))
	setupCloudDetectorExpectations(mock)
	// expectSchemaCollection queues the lock query returning no rows, followed
	// by the full per-table sequence — all of which must be consumed.
	expectSchemaCollection(mock, 16385, 100)

	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())

	// The mirror of the skip assertion: an unlocked table is introspected, so
	// its single mocked column is present.
	rec := findTableRecord(t, logs, "test_table")
	count, ok := rec.Attributes().Get("table.column_count")
	require.True(t, ok)
	assert.Equal(t, int64(1), count.Int(),
		"an unlocked table must be fully introspected")
}

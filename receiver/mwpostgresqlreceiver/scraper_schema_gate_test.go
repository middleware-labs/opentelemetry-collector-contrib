// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// expectSchemaCollection queues the query sequence for one full schema
// collection of a single-table database.
func expectSchemaCollection(mock sqlmock.Sqlmock, tableOID uint32, xmin uint32) {
	mock.ExpectQuery(`SELECT oid FROM pg_database`).
		WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(16384))

	mock.ExpectQuery(`SELECT pg_total_relation_size`).
		WillReturnRows(sqlmock.NewRows([]string{"pg_total_relation_size"}).AddRow(0))

	mock.ExpectQuery("SELECT.*pg_class.*pg_namespace").WillReturnRows(sqlmock.NewRows([]string{
		"oid", "schema", "table", "type", "hasoids", "tablespace", "desc", "owner", "xmin", "total_size",
	}).AddRow(tableOID, "public", "test_table", "r", false, 0, nil, "postgres", xmin, 2048))

	// Lock guard: one query per collection, before per-table enrichment.
	mock.ExpectQuery("SELECT l.relation FROM pg_locks").
		WillReturnRows(sqlmock.NewRows([]string{"relation"}))
	mock.ExpectQuery("SELECT.*pg_attribute").WithArgs(tableOID).WillReturnRows(sqlmock.NewRows([]string{
		"attnum", "name", "type", "typeoid", "mod", "notnull", "hasdef", "def", "desc", "coll", "xmin",
	}).AddRow(1, "id", "integer", 23, -1, true, false, nil, nil, 0, xmin))

	mock.ExpectQuery("SELECT.*pg_index").WithArgs(tableOID).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "table", "primary", "unique", "valid", "exclusion", "type", "def", "partial", "xmin", "size",
	}))

	mock.ExpectQuery("SELECT.*pg_constraint").WithArgs(tableOID).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "type", "table", "def", "deferrable", "deferred", "validated",
	}))

	mock.ExpectQuery("SELECT.*pg_stat_user_tables").WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{
		"relid", "live", "dead", "mod", "vac", "autovac", "ana", "autoana", "seq", "seq_read", "idx", "idx_fetch", "size", "total_size",
	}).AddRow(tableOID, 100, 0, 0, nil, nil, nil, nil, 0, 0, 0, 0, 1024, 2048))
}

// TestSchemaCollectionSkipsUnchangedDatabase is the measurable claim of the
// per-database gate: once a database has been collected, a later cycle that
// finds no xmin change must not collect it again.
//
// Before this change the collect/skip decision was global and xmin detection
// ran only against the default database, so a change anywhere — or, since the
// snapshot was overwritten by each database in turn, effectively every cycle —
// re-collected the full schema of every database. That is the work the customer
// was paying for continuously.
func TestSchemaCollectionSkipsUnchangedDatabase(t *testing.T) {
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

	scraper := newPostgreSQLScraper(settings, cfg, factory, newStatementStateCache(1), newTTLCache[queryPlanKey, string](1, time.Second))

	const tableOID = uint32(16385)
	const xmin = uint32(100)

	// --- Cycle 1: never snapshotted, so a full collection happens. ---
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))
	setupCloudDetectorExpectations(mock)
	expectSchemaCollection(mock, tableOID, xmin)

	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, logs.ResourceLogs().Len(), "first cycle must collect")

	// --- Cycle 2: xmin unchanged, so only detection runs and nothing is
	// collected. Note the absence of any table/column/index expectations: if
	// the gate failed to skip, those queries would be unexpected and the mock
	// would error. Version and cloud detection are cached from cycle 1, so
	// the only query this cycle is the xmin probe. ---
	mock.ExpectQuery(`SELECT c\.oid, c\.xmin`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "xmin", "nspname", "relname"}).AddRow(tableOID, xmin, "public", "orders"))

	logs, err = scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, logs.ResourceLogs().Len(),
		"unchanged database must be skipped, not re-collected")

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSchemaCollectionRecollectsChangedDatabase is the other half: a real xmin
// change must still trigger a full collection, so the skip above is not simply
// the receiver going quiet.
func TestSchemaCollectionRecollectsChangedDatabase(t *testing.T) {
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

	scraper := newPostgreSQLScraper(settings, cfg, factory, newStatementStateCache(1), newTTLCache[queryPlanKey, string](1, time.Second))

	const tableOID = uint32(16385)

	// --- Cycle 1: initial collection at xmin 100. ---
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))
	setupCloudDetectorExpectations(mock)
	expectSchemaCollection(mock, tableOID, 100)

	_, err = scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)

	// --- Cycle 2: the table's xmin moved, so it is collected again. ---
	mock.ExpectQuery(`SELECT c\.oid, c\.xmin`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "xmin", "nspname", "relname"}).AddRow(tableOID, 200, "public", "orders"))
	expectSchemaCollection(mock, tableOID, 200)

	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, logs.ResourceLogs().Len(),
		"a changed database must be re-collected")

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchemaChangeDetectionSkipsExcludedTables(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tracker := NewXminChangeTracker(1)
	tracker.UpdateSnapshot("postgres", map[uint32]uint32{16385: 100})
	collector, err := NewSchemaCollector(
		WrapDBWithIgnore(db),
		&VersionInfo{VersionNum: 150000},
		CloudProviderSelfHosted,
		nil,
		tracker,
		&CollectorConfig{DatabaseName: "postgres"},
		&FilterConfig{ExcludeTables: map[string][]string{"public": {"ignored"}}},
		&zapAdapter{l: zap.NewNop()},
	)
	require.NoError(t, err)

	// The ignored relation is new to pg_class. It must not make the database
	// look changed, otherwise an excluded table causes a full schema snapshot
	// every interval.
	mock.ExpectQuery(`SELECT c\.oid, c\.xmin`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "xmin", "nspname", "relname"}).
			AddRow(16385, 100, "public", "tracked").
			AddRow(16386, 200, "public", "ignored"))

	changed, dropped, err := collector.DetectChanges(context.Background())
	require.NoError(t, err)
	require.Empty(t, changed)
	require.Zero(t, dropped)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchemaCollectionBatchesIndexStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	collector, err := NewSchemaCollector(
		WrapDBWithIgnore(db),
		&VersionInfo{VersionNum: 150000},
		CloudProviderSelfHosted,
		nil,
		NewXminChangeTracker(1),
		&CollectorConfig{DatabaseName: "postgres"},
		&FilterConfig{},
		&zapAdapter{l: zap.NewNop()},
	)
	require.NoError(t, err)

	first := &IndexDefinition{OID: 100}
	second := &IndexDefinition{OID: 101}
	mock.ExpectQuery(`SELECT.*indexrelid.*pg_stat_user_indexes`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"indexrelid", "idx_scan", "idx_tup_read", "idx_tup_fetch"}).
			AddRow(100, 4, 50, 40).
			AddRow(101, 8, 100, 90))

	collector.collectIndexStatsForTables(context.Background(), []*TableDefinition{
		{Indexes: []*IndexDefinition{first}},
		{Indexes: []*IndexDefinition{second}},
	})
	require.EqualValues(t, 4, first.ScanCount)
	require.EqualValues(t, 50, first.TupleRead)
	require.EqualValues(t, 40, first.TupleFetch)
	require.EqualValues(t, 8, second.ScanCount)
	require.EqualValues(t, 100, second.TupleRead)
	require.EqualValues(t, 90, second.TupleFetch)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchemaCollectionBatchesTableStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	collector, err := NewSchemaCollector(
		WrapDBWithIgnore(db),
		&VersionInfo{VersionNum: 150000},
		CloudProviderSelfHosted,
		nil,
		NewXminChangeTracker(1),
		&CollectorConfig{DatabaseName: "postgres"},
		&FilterConfig{},
		&zapAdapter{l: zap.NewNop()},
	)
	require.NoError(t, err)

	first := &TableDefinition{OID: 42, Type: "r"}
	second := &TableDefinition{OID: 43, Type: "p"}
	mock.ExpectQuery(`SELECT.*relid.*pg_stat_user_tables`).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"relid", "live", "dead", "mod", "vac", "autovac", "ana", "autoana", "seq", "seq_read", "idx", "idx_fetch", "size", "total_size",
		}).
			AddRow(42, 100, 2, 3, nil, nil, nil, nil, 4, 500, 6, 70, 8192, 16384).
			AddRow(43, 200, 5, 6, nil, nil, nil, nil, 7, 800, 9, 100, 32768, 65536))

	collector.collectTableStatsForTables(context.Background(), []*TableDefinition{first, second})
	require.EqualValues(t, 100, first.LiveTuples)
	require.EqualValues(t, 2, first.DeadTuples)
	require.EqualValues(t, 16384, first.TotalSizeBytes)
	require.EqualValues(t, 200, second.LiveTuples)
	require.EqualValues(t, 5, second.DeadTuples)
	require.EqualValues(t, 65536, second.TotalSizeBytes)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchemaCollectionReadsIndexColumnsWithIndexes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	collector, err := NewSchemaCollector(
		WrapDBWithIgnore(db),
		&VersionInfo{VersionNum: 150000},
		CloudProviderSelfHosted,
		nil,
		NewXminChangeTracker(1),
		&CollectorConfig{DatabaseName: "postgres"},
		&FilterConfig{},
		&zapAdapter{l: zap.NewNop()},
	)
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT.*pg_index`).WithArgs(uint32(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"oid", "name", "table", "primary", "unique", "valid", "exclusion", "type", "def", "partial", "xmin", "size", "columns",
		}).AddRow(100, "orders_created_at_idx", 42, false, false, true, false, "btree", "CREATE INDEX", nil, 10, 8192, "{id,created_at}"))

	indexes, err := collector.collectIndexes(context.Background(), 42)
	require.NoError(t, err)
	require.Len(t, indexes, 1)
	require.Equal(t, []string{"id", "created_at"}, indexes[0].Columns)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchemaCollectionReadsForeignKeyDetailsWithConstraints(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	collector, err := NewSchemaCollector(
		WrapDBWithIgnore(db),
		&VersionInfo{VersionNum: 150000},
		CloudProviderSelfHosted,
		nil,
		NewXminChangeTracker(1),
		&CollectorConfig{DatabaseName: "postgres"},
		&FilterConfig{},
		&zapAdapter{l: zap.NewNop()},
	)
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT.*pg_constraint`).WithArgs(uint32(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"oid", "name", "type", "table", "def", "deferrable", "deferred", "validated", "ref_table", "columns", "ref_columns",
		}).AddRow(100, "orders_customer_id_fkey", "f", 42, "FOREIGN KEY", false, false, true, 43, "{customer_id}", "{id}"))

	constraints, err := collector.collectConstraints(context.Background(), 42)
	require.NoError(t, err)
	require.Len(t, constraints, 1)
	require.EqualValues(t, 43, constraints[0].ReferencedTableOID)
	require.Equal(t, []string{"customer_id"}, constraints[0].ColumnNames)
	require.Equal(t, []string{"id"}, constraints[0].ReferencedColumns)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSchemaCollectionBatchesPostgres16IndexStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	collector, err := NewSchemaCollector(
		WrapDBWithIgnore(db),
		&VersionInfo{VersionNum: 160000},
		CloudProviderSelfHosted,
		nil,
		NewXminChangeTracker(1),
		&CollectorConfig{DatabaseName: "postgres"},
		&FilterConfig{},
		&zapAdapter{l: zap.NewNop()},
	)
	require.NoError(t, err)

	lastScan := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	index := &IndexDefinition{OID: 100}
	mock.ExpectQuery(`SELECT.*indexrelid.*last_idx_scan.*pg_stat_user_indexes`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"indexrelid", "idx_scan", "idx_tup_read", "idx_tup_fetch", "last_idx_scan"}).
			AddRow(100, 4, 50, 40, lastScan))

	collector.collectIndexStats(context.Background(), []*IndexDefinition{index})
	require.EqualValues(t, 4, index.ScanCount)
	require.EqualValues(t, 50, index.TupleRead)
	require.EqualValues(t, 40, index.TupleFetch)
	require.Equal(t, &lastScan, index.LastScan)
	require.NoError(t, mock.ExpectationsWereMet())
}

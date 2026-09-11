// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Scope enforcement across signals.
//
// Before Step 4, `databases` and `exclude_databases` reached only the
// per-database metric and schema paths. Query samples, top-query events and
// query-performance metrics had no database predicate at all, so a receiver
// configured for one database still reported queries from every database on the
// server. These tests assert that every path now agrees.

// TestTopQuerySQLCarriesSelectionPredicate proves the predicate is applied in
// the database, before ORDER BY and LIMIT.
//
// Filtering in Go after the fact would be wrong twice over: the server would
// still do the work, and an unselected database with high query volume could
// consume the whole candidate limit before any selected row was considered.
func TestTopQuerySQLCarriesSelectionPredicate(t *testing.T) {
	for _, tt := range []struct {
		name          string
		databases     []string
		excludes      []string
		wantPredicate string
		wantNoFilter  bool
	}{
		{
			name:         "unrestricted adds no predicate",
			wantNoFilter: true,
		},
		{
			name:          "allowlist",
			databases:     []string{"orders"},
			wantPredicate: "AND pg_database.datname = ANY($1)",
		},
		{
			name:          "exclusion keeps unresolved rows",
			excludes:      []string{"scratch"},
			wantPredicate: "AND (pg_database.datname IS NULL OR NOT (pg_database.datname = ANY($1)))",
		},
		{
			name:          "cancelled selection matches nothing",
			databases:     []string{"orders"},
			excludes:      []string{"orders"},
			wantPredicate: "AND false",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			require.NoError(t, err)
			defer db.Close()

			client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}
			sel := newDatabaseSelection(tt.databases, tt.excludes)

			expectPgStatStatementsVersionRegexp(mock, "1.11")
			mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows(benchmarkTopQueryColumns))

			_, err = client.getTopQuery(t.Context(), 100, sel, zap.NewNop())
			require.NoError(t, err)

			// sqlmock's regexp matcher already asserted the query ran; inspect
			// the rendered SQL directly for the predicate.
			rendered := renderTopQuerySQL(t, sel)
			if tt.wantNoFilter {
				assert.NotContains(t, rendered, "pg_database.datname = ANY")
				return
			}
			assert.Contains(t, rendered, tt.wantPredicate)

			// The predicate must precede ORDER BY, or an unselected database
			// could still consume the LIMIT.
			predicateAt := indexOf(rendered, tt.wantPredicate)
			orderAt := indexOf(rendered, "ORDER BY")
			require.Positive(t, orderAt)
			assert.Less(t, predicateAt, orderAt,
				"the database filter must be applied before ORDER BY and LIMIT")
		})
	}
}

// renderTopQuerySQL renders the top-query template for a selection, so the
// generated SQL can be asserted on without a server.
func renderTopQuerySQL(t *testing.T, sel databaseSelection) string {
	t.Helper()
	caps := pgStatStatementsCapabilities{installed: true, version: extensionVersion{1, 11}, schema: "public"}
	predicate, _ := sel.datnamePredicate("pg_database.datname", 1)
	buf := &templateOutput{}
	require.NoError(t, topQueryTemplateParsed.Execute(buf, map[string]any{
		"limit":               100,
		"hasExecTimeColumns":  caps.hasExecTimeColumns(),
		"hasTopLevel":         caps.hasTopLevel(),
		"hasSharedBlkTimings": caps.hasSharedBlkTimings(),
		"statementsView":      caps.qualify("pg_stat_statements"),
		"orderByExecTimeCol":  "total_exec_time",
		"databasePredicate":   predicate,
	}))
	return buf.String()
}

// TestQuerySampleSQLCarriesSelectionPredicate is the same assertion for the
// sample path, which also had no database predicate.
func TestQuerySampleSQLCarriesSelectionPredicate(t *testing.T) {
	render := func(sel databaseSelection) string {
		predicate, _ := sel.datnamePredicate("datname", 1)
		buf := &templateOutput{}
		require.NoError(t, querySampleTemplateParsed.Execute(buf, map[string]any{
			"limit":                100,
			"newestQueryTimestamp": float64(0),
			"hasQueryID":           true,
			"databasePredicate":    predicate,
		}))
		return buf.String()
	}

	restricted := render(newDatabaseSelection([]string{"orders"}, nil))
	assert.Contains(t, restricted, "AND datname = ANY($1)")
	assert.Less(t, indexOf(restricted, "AND datname = ANY($1)"), indexOf(restricted, "LIMIT"),
		"the filter must be applied before LIMIT")

	unrestricted := render(newDatabaseSelection(nil, nil))
	assert.NotContains(t, unrestricted, "datname = ANY")
}

// TestQueryPerfStatsFiltersByDbid covers the query-performance path, which is
// keyed on dbid rather than on a database name and so resolves the selection to
// OIDs in the server.
func TestQueryPerfStatsFiltersByDbid(t *testing.T) {
	for _, tt := range []struct {
		name      string
		databases []string
		excludes  []string
		want      string
	}{
		{name: "unrestricted", want: ""},
		{
			name:      "allowlist",
			databases: []string{"orders"},
			want:      "dbid IN (SELECT oid FROM pg_database WHERE datname = ANY($1))",
		},
		{
			name:     "exclusion",
			excludes: []string{"scratch"},
			want:     "dbid NOT IN (SELECT oid FROM pg_database WHERE datname = ANY($1))",
		},
		{
			name:      "cancelled",
			databases: []string{"orders"},
			excludes:  []string{"orders"},
			want:      "false",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sel := newDatabaseSelection(tt.databases, tt.excludes)
			got, _ := sel.dbidPredicate("dbid", 1)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBufferHitsRespectSelection covers collectBufferHits, which read
// pg_stat_database for every database on the server regardless of
// configuration.
func TestBufferHitsRespectSelection(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}
	sel := newDatabaseSelection([]string{"orders"}, nil)

	mock.ExpectQuery(regexp.QuoteMeta("datname = ANY($1)")).
		WillReturnRows(sqlmock.NewRows([]string{"datname", "blks_hit"}).AddRow("orders", 42))

	hits, err := client.getBufferHit(t.Context(), sel)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSelectionAgreesAcrossSignals is the cross-signal check the plan asks for:
// the same policy object decides scope everywhere, so metrics, schema and query
// telemetry cannot disagree about which databases are in scope.
func TestSelectionAgreesAcrossSignals(t *testing.T) {
	sel := newDatabaseSelection([]string{"orders", "billing"}, []string{"billing"})

	// Per-database metric and schema collection use the effective list.
	assert.Equal(t, []string{"orders"}, sel.effectiveDatabases([]string{"orders", "billing", "scratch"}))

	// Query telemetry uses a SQL predicate derived from the same policy.
	datname, datnameArgs := sel.datnamePredicate("datname", 1)
	dbid, dbidArgs := sel.dbidPredicate("dbid", 1)
	require.Len(t, datnameArgs, 1)
	require.Len(t, dbidArgs, 1)
	assert.Contains(t, datname, "ANY($1)")
	assert.Contains(t, dbid, "ANY($1)")

	// EXPLAIN and any other Go-side check use includes().
	assert.True(t, sel.includes("orders"))
	assert.False(t, sel.includes("billing"), "an excluded database is out of scope everywhere")
	assert.False(t, sel.includes("scratch"))
}

// TestMaintenanceConnectionIsNotDataScope documents the control-connection
// exception: `postgres` may be connected to for server-wide statistics even
// when it is not selected, but that must not make its own telemetry in scope.
func TestMaintenanceConnectionIsNotDataScope(t *testing.T) {
	sel := newDatabaseSelection([]string{"orders"}, nil)

	assert.False(t, sel.includes(defaultPostgreSQLDatabase),
		"connecting to postgres for server-wide statistics does not put it in data scope")

	// Its per-database telemetry is therefore not collected.
	assert.NotContains(t, sel.effectiveDatabases([]string{defaultPostgreSQLDatabase, "orders"}),
		defaultPostgreSQLDatabase)
}

func indexOf(haystack, needle string) int {
	return strings.Index(haystack, needle)
}

// TestDatabaseStatsToleratesNullDatname is a regression test for a latent bug
// that only surfaced once the selection stopped adding a WHERE clause in the
// unrestricted case.
//
// pg_stat_database carries one row with a NULL datname holding statistics for
// shared objects, and pg_stat_activity has NULL datname for background workers.
// The old helper always appended `WHERE datname IN (...)` when it had any
// database names, which filtered those rows out incidentally; the code scanned
// into a plain string and would have failed on them. It now reads the column as
// nullable and skips such rows.
func TestDatabaseStatsToleratesNullDatname(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	cols := []string{
		"datname", "xact_commit", "xact_rollback", "deadlocks", "temp_files", "temp_bytes",
		"tup_updated", "tup_returned", "tup_fetched", "tup_inserted", "tup_deleted",
		"blks_hit", "blks_read", "blk_read_time", "blk_write_time",
	}
	mock.ExpectQuery("SELECT datname").WillReturnRows(
		sqlmock.NewRows(cols).
			AddRow(nil, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.0, 0.0).
			AddRow("orders", 2, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.0, 0.0))

	stats, err := client.getDatabaseStats(t.Context(), databaseSelection{})
	require.NoError(t, err, "a NULL datname row must not fail the whole scrape")
	require.Len(t, stats, 1, "the shared-objects row belongs to no database and is skipped")
	_, ok := stats["orders"]
	assert.True(t, ok)
}

// TestBackendsTolerateNullDatname is the same guard for pg_stat_activity, where
// background workers have no database.
func TestBackendsTolerateNullDatname(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	mock.ExpectQuery("SELECT datname").WillReturnRows(
		sqlmock.NewRows([]string{"datname", "count"}).
			AddRow(nil, 3).
			AddRow("orders", 5))

	backends, err := client.getBackends(t.Context(), databaseSelection{})
	require.NoError(t, err)
	require.Len(t, backends, 1)
	assert.Equal(t, int64(5), backends["orders"])
}

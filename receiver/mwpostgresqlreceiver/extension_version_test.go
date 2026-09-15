// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestParseExtensionVersionOrdersNumerically(t *testing.T) {
	// The whole point of parsing rather than comparing strings: "1.10" sorts
	// below "1.9" as text while being three releases newer, and
	// pg_stat_statements has shipped versions 1.4 through 1.13.
	v110, err := parseExtensionVersion("1.10")
	require.NoError(t, err)
	v19, err := parseExtensionVersion("1.9")
	require.NoError(t, err)

	assert.True(t, v110.atLeast(1, 9), "1.10 must compare as newer than 1.9")
	assert.False(t, v19.atLeast(1, 10), "1.9 must not compare as newer than 1.10")
}

func TestParseExtensionVersion(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    extensionVersion
		wantErr bool
	}{
		{in: "1.8", want: extensionVersion{1, 8}},
		{in: "1.11", want: extensionVersion{1, 11}},
		{in: "1.13", want: extensionVersion{1, 13}},
		{in: " 1.9 ", want: extensionVersion{1, 9}},
		{in: "2", want: extensionVersion{2, 0}},
		{in: "", wantErr: true},
		{in: "banana", wantErr: true},
	} {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseExtensionVersion(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtensionCapabilityGates(t *testing.T) {
	for _, tt := range []struct {
		version          string
		execTimeColumns  bool
		topLevel         bool
		sharedBlkTimings bool
		statsSince       bool
	}{
		// total_exec_time/total_plan_time arrive in 1.8.
		{version: "1.7"},
		{version: "1.8", execTimeColumns: true},
		// toplevel and pg_stat_statements_info arrive in 1.9.
		{version: "1.9", execTimeColumns: true, topLevel: true},
		// 1.10 must not be mistaken for older than 1.9.
		{version: "1.10", execTimeColumns: true, topLevel: true},
		// 1.11 renames the block timing columns and adds the per-entry
		// stats_since. They are stated separately rather than sharing one
		// expectation: they arrive in the same upgrade script today, but they
		// are independent capabilities and a later split should fail here
		// rather than pass silently.
		{version: "1.11", execTimeColumns: true, topLevel: true, sharedBlkTimings: true, statsSince: true},
		{version: "1.13", execTimeColumns: true, topLevel: true, sharedBlkTimings: true, statsSince: true},
	} {
		t.Run(tt.version, func(t *testing.T) {
			v, err := parseExtensionVersion(tt.version)
			require.NoError(t, err)
			caps := pgStatStatementsCapabilities{installed: true, version: v}

			assert.Equal(t, tt.execTimeColumns, caps.hasExecTimeColumns())
			assert.Equal(t, tt.topLevel, caps.hasTopLevel())
			assert.Equal(t, tt.sharedBlkTimings, caps.hasSharedBlkTimings())
			assert.Equal(t, tt.statsSince, caps.hasStatsSince())
		})
	}
}

func TestCapabilitiesQualifyUsesInstallSchema(t *testing.T) {
	// Installations that place the extension outside the search path - Supabase
	// uses "extensions" - are only reachable when the view is qualified.
	caps := pgStatStatementsCapabilities{installed: true, schema: "extensions"}
	assert.Equal(t, "extensions.pg_stat_statements", caps.qualify("pg_stat_statements"))

	// With no schema known the bare name is used, preserving search_path
	// resolution rather than producing invalid SQL.
	assert.Equal(t, "pg_stat_statements", pgStatStatementsCapabilities{}.qualify("pg_stat_statements"))
}

// TestGetTopQueryUsesExtensionVersionNotServerVersion is the regression gate
// for the live failure: extension 1.11, which CREATE EXTENSION installs on
// PostgreSQL 17, renamed blk_read_time and blk_write_time. Selecting the old
// names there fails the entire top-query path.
func TestGetTopQueryUsesExtensionVersionNotServerVersion(t *testing.T) {
	for _, tt := range []struct {
		name     string
		version  string
		expected string
	}{
		{name: "extension 1.9 keeps the original timing column names", version: "1.9", expected: expectedScrapeTopQuery},
		{name: "extension 1.11 selects the renamed timing columns", version: "1.11", expected: expectedScrapeTopQueryExtension111},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			defer db.Close()

			client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

			expectPgStatStatementsVersion(mock, tt.version)
			mock.ExpectQuery(tt.expected).
				WillReturnRows(sqlmock.NewRows([]string{"queryid"}))

			_, err = client.getTopQuery(t.Context(), 31, databaseSelection{}, zap.NewNop())
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestGetQueryStatsGatesTopLevelOnExtensionVersion covers the other half of the
// same defect. pg_upgrade does not update extension versions, so a PG14+ server
// can still be on pg_stat_statements 1.8, which has no toplevel column at all;
// gating on the server's major version selects a column that does not exist and
// fails the query-metrics path.
func TestGetQueryStatsGatesTopLevelOnExtensionVersion(t *testing.T) {
	for _, tt := range []struct {
		name        string
		version     string
		wantColumn  string
		hasTopLevel bool
	}{
		{name: "extension 1.8 on a modern server has no toplevel", version: "1.8", wantColumn: "false AS toplevel", hasTopLevel: false},
		{name: "extension 1.9 selects toplevel", version: "1.9", wantColumn: "toplevel AS toplevel", hasTopLevel: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			require.NoError(t, err)
			defer db.Close()

			client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

			expectPgStatStatementsVersionRegexp(mock, tt.version)
			mock.ExpectQuery(regexp.QuoteMeta(tt.wantColumn)).
				WillReturnRows(sqlmock.NewRows([]string{
					"queryid", "dbid", "userid", "toplevel", "calls", "total_exec_time",
				}).AddRow("42", 1, 2, true, 3, 4.0))

			stats, err := client.getQueryStats(t.Context(), databaseSelection{})
			require.NoError(t, err)
			require.Len(t, stats, 1)
			assert.Equal(t, tt.hasTopLevel, stats[0].key.hasTopLevel)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestStatementCapabilitiesReadOncePerConnection keeps the catalog lookup off
// the per-scrape path: it is cheap, but it would otherwise run on every scrape
// of every statement path.
func TestStatementCapabilitiesReadOncePerConnection(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	// Queued exactly once: a second lookup would fail ExpectationsWereMet.
	expectPgStatStatementsVersion(mock, "1.11")

	for range 3 {
		caps, err := client.statementCapabilities(t.Context())
		require.NoError(t, err)
		assert.True(t, caps.hasSharedBlkTimings())
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestStatementCapabilitiesExtensionAbsent covers a database where
// pg_stat_statements is simply not installed. That is not an error: the caller
// moves on to the next candidate database.
func TestStatementCapabilitiesExtensionAbsent(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	mock.ExpectQuery(pgStatStatementsVersionSQL).
		WillReturnRows(sqlmock.NewRows([]string{"extversion", "quote_ident"}))

	caps, err := client.statementCapabilities(t.Context())
	require.NoError(t, err)
	assert.False(t, caps.installed)
}

// TestGetTopQueryScansStatsSinceOnExtension111 checks that a real 1.11 row's
// stats_since survives the scan and reaches the delta logic.
//
// The rig this receiver is measured on runs pg_stat_statements 1.10, so a clean
// rig run never exercises this column at all. Only the PostgreSQL 17 container
// integration test and this unit test cover it, which is why the scan is
// asserted here rather than left to the SQL-shape test above — that one asserts
// the statement sent, not that the value comes back typed.
func TestGetTopQueryScansStatsSinceOnExtension111(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	statsSince := time.Date(2026, 9, 12, 10, 30, 0, 0, time.UTC)
	values := []driverValue{
		int64(5), "postgres", int64(1), int64(1), int64(1), int64(1), int64(1), int64(1),
		"select 1", int64(114514), "master", int64(5),
		5000.0, 5000.0, 1.0, 1.0,
		int64(16384), int64(10), true,
		statsSince,
	}

	expectPgStatStatementsVersion(mock, "1.11")
	mock.ExpectQuery(expectedScrapeTopQueryExtension111).
		WillReturnRows(sqlmock.NewRows(benchmarkTopQueryColumns).AddRow(values...))

	rows, err := client.getTopQuery(t.Context(), 31, databaseSelection{}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.True(t, rows[0].statsSince.Valid, "a 1.11 row must carry a non-NULL stats_since")
	assert.Equal(t, statsSince, rows[0].statsSince.Time.UTC())
	assert.Equal(t, statsSince, rows[0].snapshot().statsSince.UTC(),
		"the snapshot the delta cache compares must carry the scanned value")
}

// TestGetTopQueryTreatsAbsentStatsSinceAsUnavailable is the pre-1.11 half.
//
// Below 1.11 the template selects a literal NULL, which must read as the zero
// time. That is what tells the cache the signal is unavailable, so it falls
// back to the counter guards rather than treating a NULL as a timestamp.
func TestGetTopQueryTreatsAbsentStatsSinceAsUnavailable(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	values := []driverValue{
		int64(5), "postgres", int64(1), int64(1), int64(1), int64(1), int64(1), int64(1),
		"select 1", int64(114514), "master", int64(5),
		5000.0, 5000.0, 1.0, 1.0,
		int64(16384), int64(10), true,
		nil, // stats_since: the literal NULL the pre-1.11 template selects
	}

	expectPgStatStatementsVersion(mock, "1.9")
	mock.ExpectQuery(expectedScrapeTopQuery).
		WillReturnRows(sqlmock.NewRows(benchmarkTopQueryColumns).AddRow(values...))

	rows, err := client.getTopQuery(t.Context(), 31, databaseSelection{}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, rows, 1)

	assert.False(t, rows[0].statsSince.Valid)
	assert.True(t, rows[0].snapshot().statsSince.IsZero(),
		"an absent stats_since must read as the zero time, which is how the "+
			"cache recognizes the signal is unavailable")
}

// TestGetQueryTextsQualifiesStatementsView pins the schema qualification of the
// text-resolution pass.
//
// getQueryStats and the top-query template both qualify the view, but
// getQueryTexts named it bare. On an installation that keeps the extension out
// of search_path -- Supabase puts it in "extensions" -- that combination is
// quietly asymmetric: counters resolve, text never does, and every statement is
// reported without the query it stands for.
func TestGetQueryTextsQualifiesStatementsView(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	mock.ExpectQuery(regexp.QuoteMeta(pgStatStatementsVersionSQL)).
		WillReturnRows(sqlmock.NewRows([]string{"extversion", "quote_ident"}).AddRow("1.11", "extensions"))

	// The assertion is the expectation itself: only a FROM naming the
	// extension's own schema matches, so a bare view name fails here.
	mock.ExpectQuery(`FROM extensions\.pg_stat_statements AS s`).
		WillReturnRows(sqlmock.NewRows([]string{
			"queryid", "dbid", "userid", "toplevel", "raw_query", "query",
		}).AddRow("42", 1, 2, true, "SELECT 1", "SELECT 1"))

	texts, err := client.getQueryTexts(t.Context(), []queryStatsKey{{
		queryID: 42, database: 1, user: 2, topLevel: true, hasTopLevel: true,
	}})
	require.NoError(t, err)
	require.Len(t, texts, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestStatementCapabilitiesRetriesAfterFailure pins that a failed catalog read
// is not remembered.
//
// The failures this lookup can see are transient -- a scrape context deadline,
// a dropped connection -- not verdicts about the server. Caching one would
// outlive its cause by the life of the process, because the client pool holds
// a client indefinitely and never evicts the default database's, so a single
// unlucky first scrape would leave the statement paths dark until restart.
func TestStatementCapabilitiesRetriesAfterFailure(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	mock.ExpectQuery(pgStatStatementsVersionSQL).
		WillReturnError(errors.New("context deadline exceeded"))
	expectPgStatStatementsVersion(mock, "1.11")

	_, err = client.statementCapabilities(t.Context())
	require.Error(t, err)

	// The retry succeeds, and the success is cached: a third call queues no
	// further query, so an unmet-expectation check would catch a repeat read.
	for range 2 {
		caps, err := client.statementCapabilities(t.Context())
		require.NoError(t, err)
		assert.True(t, caps.hasSharedBlkTimings())
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetTopQuerySkipsUndecodableRow covers the per-row branch of the scan
// loop: a row whose value will not decode is dropped with a warning rather than
// failing the scrape, and the rows around it still arrive.
//
// The distinction matters because the two failure modes are handled opposite
// ways -- one row is survivable, a truncated result set is not -- and neither
// branch had a test.
func TestGetTopQuerySkipsUndecodableRow(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	good := func(queryID int64) []driverValue {
		return []driverValue{
			int64(1), "postgres", int64(0), int64(0), int64(0), int64(0),
			int64(0), int64(0), "SELECT 1", queryID, "master", int64(0),
			1.0, 1.0, 0.0, 0.0, int64(16384), int64(10), true, nil,
		}
	}
	// calls arrives as a string that is not a number: the destination is an
	// integer, so this row alone fails to scan.
	bad := good(2)
	bad[0] = "not-an-integer"

	expectPgStatStatementsVersionRegexp(mock, "1.9")
	mock.ExpectQuery(`FROM\s+public\.pg_stat_statements`).
		WillReturnRows(sqlmock.NewRows(benchmarkTopQueryColumns).
			AddRow(good(1)...).
			AddRow(bad...).
			AddRow(good(3)...))

	rows, err := client.getTopQuery(t.Context(), 10, databaseSelection{}, zap.NewNop())
	require.NoError(t, err, "one undecodable row must not fail the scrape")
	require.Len(t, rows, 2, "the undecodable row is skipped and its neighbours kept")
	assert.Equal(t, int64(1), rows[0].queryID.Int64)
	assert.Equal(t, int64(3), rows[1].queryID.Int64)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetTopQueryFailsOnRowError covers the other branch: an error that ends
// iteration part way through.
//
// sql.Rows.Next returns false for a failure exactly as it does for a completed
// result set, so without the Err check a driver or network failure mid-read is
// a silent short read -- a scrape that looks successful while reporting a
// fraction of the statements, which is worse than a scrape that fails.
func TestGetTopQueryFailsOnRowError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}

	rowValues := []driverValue{
		int64(1), "postgres", int64(0), int64(0), int64(0), int64(0),
		int64(0), int64(0), "SELECT 1", int64(1), "master", int64(0),
		1.0, 1.0, 0.0, 0.0, int64(16384), int64(10), true, nil,
	}

	expectPgStatStatementsVersionRegexp(mock, "1.9")
	mock.ExpectQuery(`FROM\s+public\.pg_stat_statements`).
		WillReturnRows(sqlmock.NewRows(benchmarkTopQueryColumns).
			AddRow(rowValues...).
			AddRow(rowValues...).
			RowError(1, errors.New("connection reset by peer")))

	rows, err := client.getTopQuery(t.Context(), 10, databaseSelection{}, zap.NewNop())
	require.Error(t, err, "a truncated result set must fail rather than report a short read")
	assert.ErrorContains(t, err, "failed iterating log rows")
	assert.Nil(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
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

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"regexp"
	"testing"

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
	}{
		// total_exec_time/total_plan_time arrive in 1.8.
		{version: "1.7"},
		{version: "1.8", execTimeColumns: true},
		// toplevel and pg_stat_statements_info arrive in 1.9.
		{version: "1.9", execTimeColumns: true, topLevel: true},
		// 1.10 must not be mistaken for older than 1.9.
		{version: "1.10", execTimeColumns: true, topLevel: true},
		// 1.11 renames the block timing columns.
		{version: "1.11", execTimeColumns: true, topLevel: true, sharedBlkTimings: true},
		{version: "1.13", execTimeColumns: true, topLevel: true, sharedBlkTimings: true},
	} {
		t.Run(tt.version, func(t *testing.T) {
			v, err := parseExtensionVersion(tt.version)
			require.NoError(t, err)
			caps := pgStatStatementsCapabilities{installed: true, version: v}

			assert.Equal(t, tt.execTimeColumns, caps.hasExecTimeColumns())
			assert.Equal(t, tt.topLevel, caps.hasTopLevel())
			assert.Equal(t, tt.sharedBlkTimings, caps.hasSharedBlkTimings())
			// stats_since lands with the rename, in the same upgrade script.
			assert.Equal(t, tt.sharedBlkTimings, caps.hasStatsSince())
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

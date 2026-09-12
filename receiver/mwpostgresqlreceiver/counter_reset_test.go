// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

func resetRows(ts any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"stats_reset"}).AddRow(ts)
}

// resetCaps is an extension new enough to have pg_stat_statements_info, in the
// default schema. Tests about reset detection itself use this so the version
// gate is satisfied and out of the way.
var resetCaps = pgStatStatementsCapabilities{
	installed: true,
	version:   extensionVersion{major: 1, minor: 9},
}

func TestResetDetectorFirstCheckEstablishesBaseline(t *testing.T) {
	// The first observation has no predecessor to invalidate. Reporting a reset
	// here would purge a cache that is already empty and, worse, would make
	// every process start look like a reset event.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT stats_reset FROM pg_stat_statements_info").
		WillReturnRows(resetRows(time.Now()))

	d := newResetDetector()
	assert.False(t, d.check(context.Background(), WrapDBWithIgnore(db), resetCaps))
}

func TestResetDetectorReportsChangedTimestamp(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	first := time.Date(2026, 9, 7, 11, 34, 18, 0, time.UTC)
	second := time.Date(2026, 9, 7, 11, 35, 29, 0, time.UTC)

	mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(first))
	mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(second))

	d := newResetDetector()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, d.check(context.Background(), wrapped, resetCaps), "baseline")
	assert.True(t, d.check(context.Background(), wrapped, resetCaps),
		"a moved stats_reset must be reported as a reset")
}

func TestResetDetectorSilentWhenUnchanged(t *testing.T) {
	// The overwhelmingly common case. A false positive here purges the delta
	// cache and emits a full interval of cumulative counters as if they were
	// one scrape's work, so this must be exact.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	ts := time.Date(2026, 9, 7, 11, 34, 18, 0, time.UTC)
	for range 3 {
		mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(ts))
	}

	d := newResetDetector()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, d.check(context.Background(), wrapped, resetCaps))
	assert.False(t, d.check(context.Background(), wrapped, resetCaps))
	assert.False(t, d.check(context.Background(), wrapped, resetCaps))
}

func TestResetDetectorReportsOnlyOncePerReset(t *testing.T) {
	// After a reset is reported the new timestamp becomes the baseline, so a
	// subsequent unchanged scrape is quiet. Otherwise every scrape after a
	// reset would keep purging the cache and no delta would ever be emitted
	// again.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	first := time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	second := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(first))
	mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(second))
	mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(second))

	d := newResetDetector()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, d.check(context.Background(), wrapped, resetCaps))
	require.True(t, d.check(context.Background(), wrapped, resetCaps))
	assert.False(t, d.check(context.Background(), wrapped, resetCaps),
		"the reset must be reported once, not on every following scrape")
}

func TestResetDetectorHandlesNullStatsReset(t *testing.T) {
	// stats_reset is NULL on a server whose statistics have never been reset.
	// That is a stable state and must not read as a change on every scrape.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(nil))
	mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(nil))

	d := newResetDetector()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, d.check(context.Background(), wrapped, resetCaps))
	assert.False(t, d.check(context.Background(), wrapped, resetCaps),
		"a persistently NULL stats_reset is not a reset")
}

func TestResetDetectorNullThenSetIsAReset(t *testing.T) {
	// Never-reset, then reset: the transition must be caught.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT stats_reset").WillReturnRows(resetRows(nil))
	mock.ExpectQuery("SELECT stats_reset").
		WillReturnRows(resetRows(time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)))

	d := newResetDetector()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, d.check(context.Background(), wrapped, resetCaps))
	assert.True(t, d.check(context.Background(), wrapped, resetCaps))
}

func TestResetDetectorStopsAskingWhenViewIsAbsent(t *testing.T) {
	// pg_stat_statements_info exists only from extension 1.9 (PostgreSQL 14).
	// Running against an older server is a supported configuration, not a
	// fault, so the detector must go quiet rather than error - and must stop
	// issuing a query every scrape that the server will never answer.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Only ONE query is queued. A second attempt would be unexpected.
	mock.ExpectQuery("SELECT stats_reset").
		WillReturnError(&pq.Error{Code: "42P01"}) // undefined_table

	d := newResetDetector()
	wrapped := WrapDBWithIgnore(db)

	assert.False(t, d.check(context.Background(), wrapped, resetCaps))
	assert.False(t, d.check(context.Background(), wrapped, resetCaps))
	assert.False(t, d.supported, "an absent view must disable further probing")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestResetDetectorKeepsTryingAfterTransientError(t *testing.T) {
	// A timeout or dropped connection must NOT permanently disable detection:
	// that would silently lose reset detection for the life of the process
	// after one blip.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT stats_reset").
		WillReturnError(errors.New("connection reset by peer"))
	mock.ExpectQuery("SELECT stats_reset").
		WillReturnRows(resetRows(time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)))
	mock.ExpectQuery("SELECT stats_reset").
		WillReturnRows(resetRows(time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)))

	d := newResetDetector()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, d.check(context.Background(), wrapped, resetCaps), "transient error")
	require.True(t, d.supported, "a transient error must not disable detection")
	require.False(t, d.check(context.Background(), wrapped, resetCaps), "baseline after recovery")
	assert.True(t, d.check(context.Background(), wrapped, resetCaps),
		"detection must still work after a transient failure")
}

// TestTopQueryPurgesCacheOnReset is the end-to-end half: detecting a reset is
// only useful if the stale deltas are actually discarded.
//
// Without the purge the cached pre-reset totals survive, and the next scrape
// differences the server's fresh (small) counters against them. Every delta
// goes negative, is clamped to zero, and the top-query report silently shows no
// activity at all until the counters climb back past their pre-reset values -
// which on a busy server can take hours.
func TestTopQueryPurgesCacheOnReset(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Databases = []string{}
	cfg.Events.DbServerTopQuery.Enabled = true

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	factory := mockSimpleClientFactory{db: db}

	settings := receivertest.NewNopSettings(metadata.Type)
	settings.Logger = zap.NewNop()

	// Column order matters: rows are scanned positionally, so the fixture
	// lists the projection in template order rather than ranging a map, whose
	// iteration order is random.
	cols := append([]string(nil), benchmarkTopQueryColumns...)
	// Typed values rather than a CSV string: stats_since is NULL here, and a
	// CSV cell cannot express NULL.
	vals := []driverValue{
		int64(5),   // calls
		"postgres", // datname
		int64(1),   // shared_blks_dirtied
		int64(1),   // shared_blks_hit
		int64(1),   // shared_blks_read
		int64(1),   // shared_blks_written
		int64(1),   // temp_blks_read
		int64(1),   // temp_blks_written
		"select * from pg_stat_activity where id = 32",
		int64(114514), // queryid
		"master",      // rolname
		int64(5),      // rows
		5000.0,        // total_exec_time
		5000.0,        // total_plan_time
		1.0,           // blk_read_time
		1.0,           // blk_write_time
		int64(16384),  // dbid
		int64(10),     // userid
		true,          // toplevel
		nil,           // stats_since, NULL below extension 1.11
	}

	// The identity the seeded counters live under: the tuple the row projects,
	// not the names it joins to.
	priorID := statementIdentity{queryID: 114514, dbID: 16384, userID: 10, topLevel: true}

	scraper := newPostgreSQLScraper(settings, cfg, factory, newStatementStateCache(30), newTTLCache[queryPlanKey, string](1, time.Second))

	// Seed the cache as a previous scrape would have.
	scraper.statements.lru.Add(priorID, statementSnapshot{
		counters: statementCounters{calls: 999999},
	})

	// Establish the reset baseline, then move it: this scrape sees a reset.
	scraper.resetDetector.lastReset = time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC)
	scraper.resetDetector.seen = true
	// Pin the instance as already seen and unchanged, so the reset path is what
	// is under test.
	scraper.instanceTracker.current = instanceIdentity{
		startTime: time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC),
	}
	scraper.instanceTracker.seen = true

	expectPgStatStatementsVersion(mock, "1.9")
	mock.ExpectQuery(expectedScrapeTopQuery).
		WillReturnRows(sqlmock.NewRows(cols).AddRow(vals...))
	// The instance check runs first and must report no change here, so that
	// this test exercises the reset path rather than passing because the
	// instance check happened to fire.
	mock.ExpectQuery("/* otel-collector-ignore */ SELECT pg_postmaster_start_time()").
		WillReturnRows(sqlmock.NewRows([]string{"pg_postmaster_start_time"}).
			AddRow(time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)))
	mock.ExpectQuery("/* otel-collector-ignore */ SELECT stats_reset FROM pg_stat_statements_info").
		WillReturnRows(resetRows(time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(expectedExplain).
		WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow("[]"))

	_, err = scraper.scrapeTopQuery(t.Context(), 31, 32, 33)
	require.NoError(t, err)

	// The pre-reset value must be gone. If it survived, the next scrape would
	// difference against 999999 and report nothing for hours.
	stale, exists := scraper.statements.lru.Get(priorID)
	assert.False(t, exists && stale.counters.calls == 999999,
		"cached pre-reset counters must be discarded when the server reports a reset")
}

// TestResetDetectorSkipsQueryBelowExtension19 pins the version gate.
//
// pg_stat_statements_info arrived with extension 1.9. Below that the view is
// absent by construction, so the query can only come back 42P01 -- a round trip
// per scrape whose answer is already known. Asking anyway also spent the single
// probe that distinguishes "this server cannot answer" from a transient error.
func TestResetDetectorSkipsQueryBelowExtension19(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// A 42P01 is queued for anyone who asks, which is what a real pre-1.9
	// server answers. A gated detector never sends the query, so the
	// expectation goes unconsumed; an ungated one consumes it and, because
	// 42P01 is the undefined-table state, latches supported=false forever.
	mock.ExpectQuery("SELECT stats_reset FROM pg_stat_statements_info").
		WillReturnError(&pq.Error{Code: "42P01"})

	d := newResetDetector()
	caps := pgStatStatementsCapabilities{installed: true, version: extensionVersion{major: 1, minor: 8}}

	assert.False(t, d.check(context.Background(), WrapDBWithIgnore(db), caps))

	// The query was not sent, so the queued expectation is still outstanding.
	assert.Error(t, mock.ExpectationsWereMet(), "the version gate must skip the query entirely")

	// And the gate is the version rather than a remembered failure: the probe
	// is unspent, so a connection whose extension is later updated still works.
	assert.True(t, d.supported, "a skipped query must not disable detection")
}

// TestResetDetectorQualifiesInfoView covers an extension installed outside
// search_path, where an unqualified name fails with 42P01 exactly as a missing
// view does -- and so would have disabled detection permanently on a server
// that can answer perfectly well once the name is qualified.
func TestResetDetectorQualifiesInfoView(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT stats_reset FROM extensions.pg_stat_statements_info").
		WillReturnRows(resetRows(time.Now()))

	d := newResetDetector()
	caps := pgStatStatementsCapabilities{
		installed: true,
		version:   extensionVersion{major: 1, minor: 9},
		schema:    "extensions",
	}

	assert.False(t, d.check(context.Background(), WrapDBWithIgnore(db), caps), "baseline")
	require.NoError(t, mock.ExpectationsWereMet())
}

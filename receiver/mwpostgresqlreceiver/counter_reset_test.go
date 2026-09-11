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
	assert.False(t, d.check(context.Background(), WrapDBWithIgnore(db)))
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

	require.False(t, d.check(context.Background(), wrapped), "baseline")
	assert.True(t, d.check(context.Background(), wrapped),
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

	require.False(t, d.check(context.Background(), wrapped))
	assert.False(t, d.check(context.Background(), wrapped))
	assert.False(t, d.check(context.Background(), wrapped))
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

	require.False(t, d.check(context.Background(), wrapped))
	require.True(t, d.check(context.Background(), wrapped))
	assert.False(t, d.check(context.Background(), wrapped),
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

	require.False(t, d.check(context.Background(), wrapped))
	assert.False(t, d.check(context.Background(), wrapped),
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

	require.False(t, d.check(context.Background(), wrapped))
	assert.True(t, d.check(context.Background(), wrapped))
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

	assert.False(t, d.check(context.Background(), wrapped))
	assert.False(t, d.check(context.Background(), wrapped))
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

	require.False(t, d.check(context.Background(), wrapped), "transient error")
	require.True(t, d.supported, "a transient error must not disable detection")
	require.False(t, d.check(context.Background(), wrapped), "baseline after recovery")
	assert.True(t, d.check(context.Background(), wrapped),
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

	queryid := "114514"
	row := map[string]string{
		"calls": "5", "datname": "postgres",
		"shared_blks_dirtied": "1", "shared_blks_hit": "1",
		"shared_blks_read": "1", "shared_blks_written": "1",
		"temp_blks_read": "1", "temp_blks_written": "1",
		"query": "select * from pg_stat_activity where id = 32", "queryid": queryid,
		"rolname": "master", "rows": "5",
		"total_exec_time": "5000", "total_plan_time": "5000",
		"blk_read_time": "1", "blk_write_time": "1",
	}
	cols := make([]string, 0, len(row))
	vals := ""
	for k, v := range row {
		cols = append(cols, k)
		vals += v + ","
	}

	scraper := newPostgreSQLScraper(settings, cfg, factory, newCache(30), newTTLCache[string](1, time.Second))

	// Seed the cache as a previous scrape would have, under the identity key.
	priorKey := topQueryDeltaKey(map[string]any{
		"db.namespace":                        "postgres",
		dbAttributePrefix + rolnameColumnName: "master",
	}, queryid)
	scraper.cache.Add(priorKey+callsColumnName, 999999)

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
		WillReturnRows(sqlmock.NewRows(cols).FromCSVString(vals[:len(vals)-1]))
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
	stale, exists := scraper.cache.Get(priorKey + callsColumnName)
	assert.False(t, exists && stale == 999999,
		"cached pre-reset counters must be discarded when the server reports a reset")
}

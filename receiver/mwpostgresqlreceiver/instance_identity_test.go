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

func startTimeRows(ts time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"pg_postmaster_start_time"}).AddRow(ts)
}

func TestInstanceTrackerFirstCheckEstablishesBaseline(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT pg_postmaster_start_time").
		WillReturnRows(startTimeRows(time.Now()))

	tr := newInstanceTracker()
	assert.False(t, tr.check(context.Background(), WrapDBWithIgnore(db)),
		"the first observation has no predecessor to invalidate")
}

func TestInstanceTrackerDetectsRestart(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	before := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	after := time.Date(2026, 9, 7, 14, 30, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT pg_postmaster_start_time").WillReturnRows(startTimeRows(before))
	mock.ExpectQuery("SELECT pg_postmaster_start_time").WillReturnRows(startTimeRows(after))

	tr := newInstanceTracker()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, tr.check(context.Background(), wrapped), "baseline")
	assert.True(t, tr.check(context.Background(), wrapped),
		"a restart moves the postmaster start time and must invalidate deltas")
}

func TestInstanceTrackerDetectsFailoverToOlderInstance(t *testing.T) {
	// The case that motivates checking identity at all.
	//
	// A promoted standby has been running independently, often for longer than
	// the primary it replaces, so its postmaster start time is EARLIER. Its
	// counters have also been accumulating independently and can be higher than
	// the primary's cached values - which means the delta comes out positive
	// and entirely plausible while describing a different machine.
	//
	// Neither a went-backwards check nor a stats_reset check can see this.
	// Nothing looks wrong. Only the identity differing reveals it.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	primary := time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC)
	promotedStandby := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC) // running for days

	mock.ExpectQuery("SELECT pg_postmaster_start_time").WillReturnRows(startTimeRows(primary))
	mock.ExpectQuery("SELECT pg_postmaster_start_time").WillReturnRows(startTimeRows(promotedStandby))

	tr := newInstanceTracker()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, tr.check(context.Background(), wrapped), "baseline on the primary")
	assert.True(t, tr.check(context.Background(), wrapped),
		"failover to a differently-started instance must be detected even though its start time is older")
}

func TestInstanceTrackerSilentWhenUnchanged(t *testing.T) {
	// The overwhelmingly common case. A false positive purges the delta cache
	// and blanks the top-query report for an interval, so this must be exact.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	ts := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	for range 3 {
		mock.ExpectQuery("SELECT pg_postmaster_start_time").WillReturnRows(startTimeRows(ts))
	}

	tr := newInstanceTracker()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, tr.check(context.Background(), wrapped))
	assert.False(t, tr.check(context.Background(), wrapped))
	assert.False(t, tr.check(context.Background(), wrapped))
}

func TestInstanceTrackerReportsOnlyOncePerChange(t *testing.T) {
	// After a change is reported the new value becomes the baseline. Otherwise
	// every scrape after a restart would keep purging and no delta would ever
	// be emitted again.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	before := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	after := time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT pg_postmaster_start_time").WillReturnRows(startTimeRows(before))
	mock.ExpectQuery("SELECT pg_postmaster_start_time").WillReturnRows(startTimeRows(after))
	mock.ExpectQuery("SELECT pg_postmaster_start_time").WillReturnRows(startTimeRows(after))

	tr := newInstanceTracker()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, tr.check(context.Background(), wrapped))
	require.True(t, tr.check(context.Background(), wrapped))
	assert.False(t, tr.check(context.Background(), wrapped),
		"the change must be reported once, not on every following scrape")
}

func TestInstanceTrackerStopsAskingWhenUnavailable(t *testing.T) {
	// Only a definitively absent or forbidden function disables probing.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Only ONE query queued: a second attempt would be unexpected.
	mock.ExpectQuery("SELECT pg_postmaster_start_time").
		WillReturnError(&pq.Error{Code: "42883"}) // undefined_function

	tr := newInstanceTracker()
	wrapped := WrapDBWithIgnore(db)

	assert.False(t, tr.check(context.Background(), wrapped))
	assert.False(t, tr.check(context.Background(), wrapped))
	assert.False(t, tr.supported)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestInstanceTrackerKeepsTryingAfterTransientError(t *testing.T) {
	// A blip must not silently disable failover detection for the life of the
	// process - that would reintroduce exactly the silent corruption this
	// exists to prevent.
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT pg_postmaster_start_time").
		WillReturnError(errors.New("connection reset by peer"))
	mock.ExpectQuery("SELECT pg_postmaster_start_time").
		WillReturnRows(startTimeRows(time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)))
	mock.ExpectQuery("SELECT pg_postmaster_start_time").
		WillReturnRows(startTimeRows(time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC)))

	tr := newInstanceTracker()
	wrapped := WrapDBWithIgnore(db)

	require.False(t, tr.check(context.Background(), wrapped), "transient error")
	require.True(t, tr.supported, "a transient error must not disable detection")
	require.False(t, tr.check(context.Background(), wrapped), "baseline after recovery")
	assert.True(t, tr.check(context.Background(), wrapped),
		"detection must still work after a transient failure")
}

func TestInstanceIdentityDoesNotRelyOnSystemIdentifier(t *testing.T) {
	// A regression guard on the design decision, not on behaviour.
	//
	// system_identifier is the obvious choice and it does not work: PostgreSQL
	// REQUIRES a physical standby to carry its primary's identifier, refusing
	// replication otherwise ("FATAL: database system identifier differs between
	// the primary and standby"), and promotion changes only the timeline. A
	// failover therefore shows the identical value before and after. The same
	// holds for Aurora, Azure zone-redundant HA and Cloud SQL HA, which all
	// fail over by promoting an existing standby.
	//
	// If someone later "improves" this by keying on system_identifier, the
	// check silently stops detecting the very case it was built for.
	q := instanceIdentityQuery()
	assert.Contains(t, q, "pg_postmaster_start_time")
	assert.NotContains(t, q, "system_identifier",
		"system_identifier cannot detect failover: a standby shares its primary's")
	assert.NotContains(t, q, "pg_control_system",
		"pg_control_system availability on managed platforms is unverified")
}

// TestTopQueryPurgesCacheOnInstanceChange is the end-to-end half: noticing the
// server changed is only useful if the stale deltas are actually discarded.
//
// This is the failover shape specifically. The cached counter is LARGER than
// what the new instance reports, so a went-backwards check would also have
// fired here - but the reverse case (a promoted standby reporting higher
// counters) produces a plausible positive delta that only the identity check
// can catch. The purge must be driven by the identity change, which is why the
// stats_reset query is deliberately not queued: if the implementation fell
// through to the reset check instead, that query would be unexpected.
func TestTopQueryPurgesCacheOnInstanceChange(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Databases = []string{}
	cfg.Events.DbServerTopQuery.Enabled = true

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

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

	scraper := newPostgreSQLScraper(settings, cfg, mockSimpleClientFactory{db: db},
		newCache(30), newTTLCache[string](1, time.Second))

	priorKey := topQueryDeltaKey(map[string]any{
		"db.namespace":                        "postgres",
		dbAttributePrefix + rolnameColumnName: "master",
	}, queryid)
	scraper.cache.Add(priorKey+callsColumnName, 999999)

	// Baseline: we were talking to an instance started at 09:00.
	scraper.instanceTracker.current = instanceIdentity{
		startTime: time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC),
	}
	scraper.instanceTracker.seen = true

	expectPgStatStatementsVersion(mock, "1.9")
	mock.ExpectQuery(expectedScrapeTopQuery).
		WillReturnRows(sqlmock.NewRows(cols).FromCSVString(vals[:len(vals)-1]))
	// A different instance answers now.
	mock.ExpectQuery("/* otel-collector-ignore */ SELECT pg_postmaster_start_time()").
		WillReturnRows(startTimeRows(time.Date(2026, 9, 7, 14, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(expectedExplain).
		WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow("[]"))

	_, err = scraper.scrapeTopQuery(t.Context(), 31, 32, 33)
	require.NoError(t, err)

	stale, exists := scraper.cache.Get(priorKey + callsColumnName)
	assert.False(t, exists && stale == 999999,
		"counters cached from a different instance must be discarded")
}

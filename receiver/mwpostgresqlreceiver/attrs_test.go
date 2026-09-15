// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

func TestAttrHelpersTolerateMissingAndMistypedKeys(t *testing.T) {
	attrs := map[string]any{
		"str":   "value",
		"i64":   int64(7),
		"f64":   float64(1.5),
		"wrong": []string{"not a scalar"},
		"nil":   nil,
	}

	require.Equal(t, "value", attrString(attrs, "str"))
	require.Equal(t, int64(7), attrInt64(attrs, "i64"))
	require.InDelta(t, 1.5, attrFloat64(attrs, "f64"), 0)

	// int64 is accepted as a float64 source, since numeric attributes are
	// stored as either depending on the converter applied upstream.
	require.InDelta(t, 7, attrFloat64(attrs, "i64"), 0)

	// Absent keys yield zero values rather than panicking.
	require.Equal(t, "", attrString(attrs, "absent"))
	require.Equal(t, int64(0), attrInt64(attrs, "absent"))
	require.InDelta(t, 0, attrFloat64(attrs, "absent"), 0)

	// Present-but-nil (an explicit NULL) behaves like absent.
	require.Equal(t, "", attrString(attrs, "nil"))
	require.Equal(t, int64(0), attrInt64(attrs, "nil"))
	require.InDelta(t, 0, attrFloat64(attrs, "nil"), 0)

	// Wrong types yield zero values rather than panicking.
	require.Equal(t, "", attrString(attrs, "wrong"))
	require.Equal(t, int64(0), attrInt64(attrs, "wrong"))
	require.InDelta(t, 0, attrFloat64(attrs, "wrong"), 0)
}

// fakeTopQueryClient implements just enough of the client interface to drive
// collectTopQuery with a controlled set of rows.
type fakeTopQueryClient struct {
	client
	rows []topQueryStatRow
}

func (f *fakeTopQueryClient) getTopQuery(context.Context, int64, databaseSelection, *zap.Logger) ([]topQueryStatRow, error) {
	return f.rows, nil
}

func (*fakeTopQueryClient) explainQuery(string, string, *zap.Logger) (string, error) {
	return "", nil
}

func (*fakeTopQueryClient) Close() error { return nil }

type fakeTopQueryClientFactory struct {
	rows []topQueryStatRow
}

func (f fakeTopQueryClientFactory) getClient(string) (client, error) {
	return &fakeTopQueryClient{rows: f.rows}, nil
}

func (fakeTopQueryClientFactory) close() error { return nil }

func newTestTopQueryScraper(t testing.TB) *postgreSQLScraper {
	t.Helper()

	cfg := createDefaultConfig().(*Config)
	cfg.Events.DbServerTopQuery.Enabled = true

	// One statement is one cache entry, so the cache only has to hold as many
	// statements as a test scrapes. Eviction can no longer take part of a
	// statement's baseline and leave the rest.
	return newTestTopQueryScraperWithConfig(t, cfg, newStatementStateCache(8))
}

func newTestTopQueryScraperWithConfig(t testing.TB, cfg *Config, statements *statementStateCache) *postgreSQLScraper {
	t.Helper()

	settings := receivertest.NewNopSettings(metadata.Type)
	settings.TelemetrySettings = component.TelemetrySettings{Logger: zap.NewNop()}

	return newPostgreSQLScraper(settings, cfg, mockSimpleClientFactory{}, statements, newTTLCache[queryPlanKey, string](10, time.Second))
}

type queryTextCacheClient struct {
	client
	stats     []queryStats
	textCalls int
}

func (c *queryTextCacheClient) getQueryStats(context.Context, databaseSelection) ([]queryStats, error) {
	return c.stats, nil
}

func (*queryTextCacheClient) getQueryStatsMax(context.Context) (int, error) { return 1, nil }

func (c *queryTextCacheClient) getQueryTexts(_ context.Context, keys []queryStatsKey) (map[queryStatsKey]string, error) {
	c.textCalls++
	return map[queryStatsKey]string{keys[0]: "SELECT cached_statement"}, nil
}

func TestQueryPerformanceStatsCachesQueryText(t *testing.T) {
	scraper := newTestTopQueryScraper(t)
	key := queryStatsKey{queryID: 42, database: 1, user: 2, topLevel: true, hasTopLevel: true}
	client := &queryTextCacheClient{stats: []queryStats{{key: key, queryID: "42", queryCount: 3, queryExecTime: 4}}}

	var errs errsMux
	scraper.collectQueryPerfStats(t.Context(), pcommon.NewTimestampFromTime(time.Now()), client, &errs)
	scraper.collectQueryPerfStats(t.Context(), pcommon.NewTimestampFromTime(time.Now()), client, &errs)

	require.Equal(t, 1, client.textCalls)
	require.NoError(t, errs.combine())
}

func completeTopQueryRow() topQueryStatRow {
	return topQueryStatRow{
		calls:             sql.NullInt64{Int64: 3, Valid: true},
		datname:           sql.NullString{String: "somedb", Valid: true},
		sharedBlksDirtied: sql.NullInt64{Int64: 5, Valid: true},
		sharedBlksHit:     sql.NullInt64{Int64: 6, Valid: true},
		sharedBlksRead:    sql.NullInt64{Int64: 7, Valid: true},
		sharedBlksWritten: sql.NullInt64{Int64: 8, Valid: true},
		tempBlksRead:      sql.NullInt64{Int64: 9, Valid: true},
		tempBlksWritten:   sql.NullInt64{Int64: 10, Valid: true},
		query:             sql.NullString{String: "select 1", Valid: true},
		queryID:           sql.NullInt64{Int64: 1, Valid: true},
		rolname:           sql.NullString{String: "someuser", Valid: true},
		rows:              sql.NullInt64{Int64: 4, Valid: true},
		totalExecTime:     sql.NullFloat64{Float64: 2500, Valid: true},
		totalPlanTime:     sql.NullFloat64{Float64: 1500, Valid: true},
		blkReadTime:       sql.NullFloat64{Float64: 500, Valid: true},
		blkWriteTime:      sql.NullFloat64{Float64: 250, Valid: true},
		dbid:              sql.NullInt64{Int64: 16384, Valid: true},
		userid:            sql.NullInt64{Int64: 10, Valid: true},
		toplevel:          sql.NullBool{Bool: true, Valid: true},
	}
}

// nullableTopQueryFields sets each nullable column of a row to NULL in turn.
// Named so a failing subtest says which column was NULL.
var nullableTopQueryFields = map[string]func(*topQueryStatRow){
	"calls":               func(r *topQueryStatRow) { r.calls = sql.NullInt64{} },
	"datname":             func(r *topQueryStatRow) { r.datname = sql.NullString{} },
	"shared_blks_dirtied": func(r *topQueryStatRow) { r.sharedBlksDirtied = sql.NullInt64{} },
	"shared_blks_hit":     func(r *topQueryStatRow) { r.sharedBlksHit = sql.NullInt64{} },
	"shared_blks_read":    func(r *topQueryStatRow) { r.sharedBlksRead = sql.NullInt64{} },
	"shared_blks_written": func(r *topQueryStatRow) { r.sharedBlksWritten = sql.NullInt64{} },
	"temp_blks_read":      func(r *topQueryStatRow) { r.tempBlksRead = sql.NullInt64{} },
	"temp_blks_written":   func(r *topQueryStatRow) { r.tempBlksWritten = sql.NullInt64{} },
	"query":               func(r *topQueryStatRow) { r.query = sql.NullString{} },
	"queryid":             func(r *topQueryStatRow) { r.queryID = sql.NullInt64{} },
	"rolname":             func(r *topQueryStatRow) { r.rolname = sql.NullString{} },
	"rows":                func(r *topQueryStatRow) { r.rows = sql.NullInt64{} },
	"total_exec_time":     func(r *topQueryStatRow) { r.totalExecTime = sql.NullFloat64{} },
	"total_plan_time":     func(r *topQueryStatRow) { r.totalPlanTime = sql.NullFloat64{} },
	"blk_read_time":       func(r *topQueryStatRow) { r.blkReadTime = sql.NullFloat64{} },
	"blk_write_time":      func(r *topQueryStatRow) { r.blkWriteTime = sql.NullFloat64{} },
	"dbid":                func(r *topQueryStatRow) { r.dbid = sql.NullInt64{} },
	"userid":              func(r *topQueryStatRow) { r.userid = sql.NullInt64{} },
	"toplevel":            func(r *topQueryStatRow) { r.toplevel = sql.NullBool{} },
}

// TestCollectTopQueryMissingDatabaseDoesNotPanic covers the crash that took down
// the whole agent process: pg_stat_statements rows outlive the databases they
// came from, so datname comes back NULL and db.namespace is absent from the row.
func TestCollectTopQueryMissingDatabaseDoesNotPanic(t *testing.T) {
	withoutDatabase := func() topQueryStatRow {
		row := completeTopQueryRow()
		row.datname = sql.NullString{}
		return row
	}

	scraper := newTestTopQueryScraper(t)

	// The first scrape only establishes a baseline; counters are cumulative, so
	// there is nothing to report until a second observation exists.
	require.NotPanics(t, func() {
		scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: []topQueryStatRow{withoutDatabase()}}, 30, 10, 10, &errsMux{}, zap.NewNop())
	})

	advanced := withoutDatabase()
	advanced.calls = sql.NullInt64{Int64: 4, Valid: true}
	advanced.totalExecTime = sql.NullFloat64{Float64: 3500, Valid: true}

	before := scraper.lb.Emit().LogRecordCount()
	require.NotPanics(t, func() {
		scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: []topQueryStatRow{advanced}}, 30, 10, 10, &errsMux{}, zap.NewNop())
	})
	after := scraper.lb.Emit().LogRecordCount()

	// The row is still reported rather than silently dropped: losing it would
	// hide top queries for exactly the database that is churning most.
	require.Equal(t, 1, after-before, "row with unresolvable database should still be emitted")
}

// TestCollectTopQueryEveryColumnNullDoesNotPanic sets each nullable column to
// NULL in turn and asserts the scrape survives.
//
// Typed rows make the original failure mode - an absent map key read as an
// untyped nil and then type-asserted - structurally impossible, since every
// field exists with a zero value whether or not the column was NULL. This keeps
// the guarantee under test rather than assuming the new representation is
// self-evidently safe.
func TestCollectTopQueryEveryColumnNullDoesNotPanic(t *testing.T) {
	for name, makeNull := range nullableTopQueryFields {
		t.Run("null_"+name, func(t *testing.T) {
			row := completeTopQueryRow()
			makeNull(&row)

			scraper := newTestTopQueryScraper(t)
			factory := fakeTopQueryClientFactory{rows: []topQueryStatRow{row}}

			require.NotPanics(t, func() {
				scraper.collectTopQuery(t.Context(), factory, 30, 10, 10, &errsMux{}, zap.NewNop())
			})
		})
	}
}

// TestCollectTopQueryAllColumnsNullDoesNotPanic is the degenerate case: a row
// where every nullable column is NULL at once.
func TestCollectTopQueryAllColumnsNullDoesNotPanic(t *testing.T) {
	var row topQueryStatRow

	scraper := newTestTopQueryScraper(t)
	require.NotPanics(t, func() {
		scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: []topQueryStatRow{row}}, 30, 10, 10, &errsMux{}, zap.NewNop())
	})
}

// TestCollectQuerySamplesEveryKeyMissingDoesNotPanic is the equivalent gate for
// the query-sample path. Those assertions are not reachable today because the
// client writes every key from a fixed list, but that is a property of the
// client, not of this code — this test keeps the scraper safe if that changes.
func TestCollectQuerySamplesEveryKeyMissingDoesNotPanic(t *testing.T) {
	baseRow := func() map[string]any {
		return map[string]any{
			dbAttributePrefix + querySampleColumnState:           "active",
			dbAttributePrefix + querySampleColumnPID:             int64(1450),
			dbAttributePrefix + querySampleColumnQueryStart:      "2025-02-12T16:37:54.843+08:00",
			dbAttributePrefix + querySampleColumnApplicationName: "receiver",
			dbAttributePrefix + querySampleColumnClientHostname:  "otel",
			dbAttributePrefix + querySampleColumnBackendType:     "client backend",
			dbAttributePrefix + querySampleColumnXactStart:       "",
			dbAttributePrefix + querySampleColumnStateChange:     "",
			dbAttributePrefix + querySampleColumnWaitEvent:       "",
			dbAttributePrefix + querySampleColumnWaitEventType:   "",
			dbAttributePrefix + querySampleColumnBackendXid:      int64(0),
			dbAttributePrefix + querySampleColumnQueryID:         "qid",
			dbAttributePrefix + querySampleColumnBlockingPids:    []any{},
			postgresqlTotalExecTimeAttributeName:                 float64(1.2),
			"event.type":                                         "query_sample",
			"db.query.comment":                                   "",
			"db.query.tables":                                    "pg_stat_activity",
			"db.namespace":                                       "postgres",
			"user.name":                                          "otelu",
			"db.query.text":                                      "select 1",
			"network.peer.address":                               "11.4.5.14",
			"network.peer.port":                                  int64(114514),
		}
	}

	cfg := createDefaultConfig().(*Config)
	cfg.Events.DbServerQuerySample.Enabled = true

	for key := range baseRow() {
		t.Run("missing_"+key, func(t *testing.T) {
			row := baseRow()
			delete(row, key)

			settings := receivertest.NewNopSettings(metadata.Type)
			settings.TelemetrySettings = component.TelemetrySettings{Logger: zap.NewNop()}
			scraper := newPostgreSQLScraper(settings, cfg, mockSimpleClientFactory{}, newStatementStateCache(10), newTTLCache[queryPlanKey, string](10, time.Second))

			require.NotPanics(t, func() {
				scraper.collectQuerySamples(t.Context(), &fakeQuerySamplesClient{rows: []map[string]any{row}}, 30, &errsMux{}, zap.NewNop())
			})
		})
	}
}

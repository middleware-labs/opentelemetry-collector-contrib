// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
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
	rows []map[string]any
}

func (f *fakeTopQueryClient) getTopQuery(context.Context, int64, databaseSelection, *zap.Logger) ([]map[string]any, error) {
	return f.rows, nil
}

func (*fakeTopQueryClient) explainQuery(string, string, *zap.Logger) (string, error) {
	return "", nil
}

func (*fakeTopQueryClient) Close() error { return nil }

type fakeTopQueryClientFactory struct {
	rows []map[string]any
}

func (f fakeTopQueryClientFactory) getClient(string) (client, error) {
	return &fakeTopQueryClient{rows: f.rows}, nil
}

func (fakeTopQueryClientFactory) close() error { return nil }

func newTestTopQueryScraper(t testing.TB) *postgreSQLScraper {
	t.Helper()

	cfg := createDefaultConfig().(*Config)
	cfg.Events.DbServerTopQuery.Enabled = true

	// One statement occupies topQueryCounterCount entries, so a cache smaller
	// than that evicts part of a statement's own baseline before the next
	// scrape reads it and every row looks like a first observation forever.
	return newTestTopQueryScraperWithConfig(t, cfg, newCache(int(topQueryCounterCount)*8))
}

func newTestTopQueryScraperWithConfig(t testing.TB, cfg *Config, cache *lru.Cache[string, float64]) *postgreSQLScraper {
	t.Helper()

	settings := receivertest.NewNopSettings(metadata.Type)
	settings.TelemetrySettings = component.TelemetrySettings{Logger: zap.NewNop()}

	return newPostgreSQLScraper(settings, cfg, mockSimpleClientFactory{}, cache, newTTLCache[string](10, time.Second))
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

func completeTopQueryRow() map[string]any {
	return map[string]any{
		"db.namespace":                                  "somedb",
		"db.query.text":                                 "select 1",
		"db.query.comment":                              "",
		dbAttributePrefix + "raw_query":                 "select 1",
		dbAttributePrefix + "rolname":                   "someuser",
		dbAttributePrefix + queryidColumnName:           "qid-1",
		dbAttributePrefix + callsColumnName:             int64(3),
		dbAttributePrefix + rowsColumnName:              int64(4),
		dbAttributePrefix + sharedBlksDirtiedColumnName: int64(5),
		dbAttributePrefix + sharedBlksHitColumnName:     int64(6),
		dbAttributePrefix + sharedBlksReadColumnName:    int64(7),
		dbAttributePrefix + sharedBlksWrittenColumnName: int64(8),
		dbAttributePrefix + tempBlksReadColumnName:      int64(9),
		dbAttributePrefix + tempBlksWrittenColumnName:   int64(10),
		dbAttributePrefix + totalExecTimeColumnName:     float64(2.5),
		dbAttributePrefix + totalPlanTimeColumnName:     float64(1.5),
		postgresqlBlkReadTimeAttributeName:              float64(0.5),
		postgresqlBlkWriteTimeAttributeName:             float64(0.25),
	}
}

// TestCollectTopQueryMissingDatabaseDoesNotPanic covers the crash that took down
// the whole agent process: pg_stat_statements rows outlive the databases they
// came from, so datname comes back NULL and db.namespace is absent from the row.
func TestCollectTopQueryMissingDatabaseDoesNotPanic(t *testing.T) {
	withoutDatabase := func() map[string]any {
		row := completeTopQueryRow()
		delete(row, "db.namespace")
		return row
	}

	scraper := newTestTopQueryScraper(t)

	// The first scrape only establishes a baseline; counters are cumulative, so
	// there is nothing to report until a second observation exists.
	require.NotPanics(t, func() {
		scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: []map[string]any{withoutDatabase()}}, 30, 10, 10, &errsMux{}, zap.NewNop())
	})

	advanced := withoutDatabase()
	advanced[dbAttributePrefix+callsColumnName] = int64(4)
	advanced[dbAttributePrefix+totalExecTimeColumnName] = float64(3.5)

	before := scraper.lb.Emit().LogRecordCount()
	require.NotPanics(t, func() {
		scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: []map[string]any{advanced}}, 30, 10, 10, &errsMux{}, zap.NewNop())
	})
	after := scraper.lb.Emit().LogRecordCount()

	// The row is still reported rather than silently dropped: losing it would
	// hide top queries for exactly the database that is churning most.
	require.Equal(t, 1, after-before, "row with unresolvable database should still be emitted")
}

// TestCollectTopQueryEveryKeyMissingDoesNotPanic removes each attribute in turn
// and asserts the scrape survives. This is the regression gate for the whole
// class of unguarded type assertions, not just the one that was observed
// crashing in production.
func TestCollectTopQueryEveryKeyMissingDoesNotPanic(t *testing.T) {
	for key := range completeTopQueryRow() {
		t.Run("missing_"+key, func(t *testing.T) {
			row := completeTopQueryRow()
			delete(row, key)

			scraper := newTestTopQueryScraper(t)
			factory := fakeTopQueryClientFactory{rows: []map[string]any{row}}

			require.NotPanics(t, func() {
				scraper.collectTopQuery(t.Context(), factory, 30, 10, 10, &errsMux{}, zap.NewNop())
			})
		})
	}
}

// TestCollectTopQueryNilValuesDoNotPanic covers the same keys being present but
// explicitly nil, which is what an untyped NULL looks like if it ever reaches
// the map with its key intact.
func TestCollectTopQueryNilValuesDoNotPanic(t *testing.T) {
	for key := range completeTopQueryRow() {
		t.Run("nil_"+key, func(t *testing.T) {
			row := completeTopQueryRow()
			row[key] = nil

			scraper := newTestTopQueryScraper(t)
			factory := fakeTopQueryClientFactory{rows: []map[string]any{row}}

			require.NotPanics(t, func() {
				scraper.collectTopQuery(t.Context(), factory, 30, 10, 10, &errsMux{}, zap.NewNop())
			})
		})
	}
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
			scraper := newPostgreSQLScraper(settings, cfg, mockSimpleClientFactory{}, newCache(10), newTTLCache[string](10, time.Second))

			require.NotPanics(t, func() {
				scraper.collectQuerySamples(t.Context(), &fakeQuerySamplesClient{rows: []map[string]any{row}}, 30, &errsMux{}, zap.NewNop())
			})
		})
	}
}

// pg_stat_statements identifies an entry by (userid, dbid, queryid, toplevel).
// These tests pin that the delta cache key distinguishes the same query text
// across roles and databases, which is what stops one row's counters being
// differenced against another's.

func TestTopQueryDeltaKeyDistinguishesRoles(t *testing.T) {
	// The reported defect: the same normalised query run by two roles is two
	// pg_stat_statements rows with independent counters. Keyed on queryid
	// alone they collide, and each scrape differences one role's cumulative
	// total against the other's.
	appRow := map[string]any{
		"db.namespace":                        "shop",
		dbAttributePrefix + rolnameColumnName: "app",
	}
	reportingRow := map[string]any{
		"db.namespace":                        "shop",
		dbAttributePrefix + rolnameColumnName: "reporting",
	}

	require.NotEqual(t,
		topQueryDeltaKey(appRow, "114514"),
		topQueryDeltaKey(reportingRow, "114514"),
		"same queryid under different roles must not share a cache key")
}

func TestTopQueryDeltaKeyDistinguishesDatabases(t *testing.T) {
	// pg_stat_statements is cluster-wide, so one queryid legitimately appears
	// once per database. This is the 52-database case.
	staging := map[string]any{
		"db.namespace":                        "shop_staging",
		dbAttributePrefix + rolnameColumnName: "app",
	}
	prod := map[string]any{
		"db.namespace":                        "shop_prod",
		dbAttributePrefix + rolnameColumnName: "app",
	}

	require.NotEqual(t,
		topQueryDeltaKey(staging, "114514"),
		topQueryDeltaKey(prod, "114514"),
		"same queryid in different databases must not share a cache key")
}

func TestTopQueryDeltaKeyIsStableAcrossScrapes(t *testing.T) {
	// The key has to be reproducible or every scrape misses the cache and
	// emits cumulative values as if they were deltas.
	row := map[string]any{
		"db.namespace":                        "shop",
		dbAttributePrefix + rolnameColumnName: "app",
	}
	require.Equal(t, topQueryDeltaKey(row, "1"), topQueryDeltaKey(row, "1"))
}

func TestTopQueryDeltaKeyToleratesMissingComponents(t *testing.T) {
	// datname comes back NULL for rows whose database has been dropped, and
	// the row scanner omits NULL columns entirely. A missing component must
	// still yield a usable, distinct key rather than panicking or collapsing
	// every such row onto one key.
	empty := topQueryDeltaKey(map[string]any{}, "114514")
	require.NotEmpty(t, empty)

	withDB := topQueryDeltaKey(map[string]any{"db.namespace": "shop"}, "114514")
	require.NotEqual(t, empty, withDB)
}

func TestTopQueryDeltaKeyIsUnambiguous(t *testing.T) {
	// A separator that can occur inside an identifier lets two different
	// identities collapse onto one key by shifting the boundary. PostgreSQL
	// identifiers may contain almost any character when quoted - including
	// "|", "-", ":" and whitespace - so the separator must be a byte that
	// cannot appear in a valid identifier at all.
	//
	// This probes candidate separators rather than assuming which one is in
	// use, so it fails for ANY printable choice, not only the one it was
	// written against.
	for _, candidate := range []string{"|", "-", ":", "/", ".", " ", "_", ",", "\t"} {
		split := topQueryDeltaKey(map[string]any{
			"db.namespace":                        "a" + candidate + "b",
			dbAttributePrefix + rolnameColumnName: "c",
		}, "1")
		shifted := topQueryDeltaKey(map[string]any{
			"db.namespace":                        "a",
			dbAttributePrefix + rolnameColumnName: "b" + candidate + "c",
		}, "1")
		require.NotEqual(t, split, shifted,
			"separator %q is ambiguous: an identifier containing it can shift the field boundary", candidate)
	}
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// countingClient records which query each scrape issued, so a test can assert
// on the SQL a configuration actually causes rather than only on the metrics it
// produces. Every method returns one plausible row so that a family that *is*
// collected still emits data points.
type countingClient struct {
	mu     sync.Mutex
	calls  map[string]int
	closes int
}

func newCountingClient() *countingClient {
	return &countingClient{calls: map[string]int{}}
}

func (c *countingClient) record(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[name]++
}

// names returns the sorted set of queries seen, which is what assertions in
// this file compare against.
func (c *countingClient) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.calls))
	for k := range c.calls {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *countingClient) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[name]
}

func (c *countingClient) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = map[string]int{}
}

func (c *countingClient) Close() error {
	c.mu.Lock()
	c.closes++
	c.mu.Unlock()
	return nil
}

func (c *countingClient) listDatabases(context.Context) ([]string, error) {
	c.record("listDatabases")
	return []string{"otel"}, nil
}

func (c *countingClient) getDatabaseStats(context.Context, databaseSelection) (map[databaseName]databaseStats, error) {
	c.record("getDatabaseStats")
	return map[databaseName]databaseStats{"otel": {transactionCommitted: 1, transactionRollback: 2, deadlocks: 3, tempFiles: 4, tempIo: 5, tupUpdated: 6, tupReturned: 7, tupFetched: 8, tupInserted: 9, tupDeleted: 10, blksHit: 11, blksRead: 12, blkReadTime: 13, blkWriteTime: 14}}, nil
}

func (c *countingClient) getDatabaseSize(context.Context, databaseSelection) (map[databaseName]int64, error) {
	c.record("getDatabaseSize")
	return map[databaseName]int64{"otel": 4096}, nil
}

func (c *countingClient) getBackends(context.Context, databaseSelection) (map[databaseName]int64, error) {
	c.record("getBackends")
	return map[databaseName]int64{"otel": 3}, nil
}

func (c *countingClient) getDatabaseTableMetrics(context.Context, string) (map[tableIdentifier]tableStats, error) {
	c.record("getDatabaseTableMetrics")
	return map[tableIdentifier]tableStats{
		"otel|public|t1": {database: "otel", schema: "public", table: "t1", live: 1, dead: 2, inserts: 3, upd: 4, del: 5, hotUpd: 6, seqScans: 7, size: 8, vacuumCount: 9, autovacuumCount: 10, analyzeCount: 11, autoanalyzeCount: 12, toastSize: 13},
		"otel|public|t2": {database: "otel", schema: "public", table: "t2", live: 1},
	}, nil
}

func (c *countingClient) getBlocksReadByTable(context.Context, string) (map[tableIdentifier]tableIOStats, error) {
	c.record("getBlocksReadByTable")
	return map[tableIdentifier]tableIOStats{
		"otel|public|t1": {database: "otel", schema: "public", table: "t1", heapRead: 1, heapHit: 2, idxRead: 3, idxHit: 4, toastRead: 5, toastHit: 6, tidxRead: 7, tidxHit: 8},
	}, nil
}

func (c *countingClient) getIndexStats(context.Context, string) (map[indexIdentifer]indexStat, error) {
	c.record("getIndexStats")
	return map[indexIdentifer]indexStat{
		"otel|public|t1|i1": {database: "otel", schema: "public", table: "t1", index: "i1", size: 1, scans: 2, tuplesRead: 3, blocksRead: 4, blocksHit: 5},
	}, nil
}

func (c *countingClient) getFunctionStats(context.Context, string) (map[functionIdentifer]functionStat, error) {
	c.record("getFunctionStats")
	return map[functionIdentifer]functionStat{
		"otel|public|f1": {database: "otel", schema: "public", function: "f1", calls: 7},
	}, nil
}

func (c *countingClient) getTableBloatStats(context.Context, string) (map[tableIdentifier]tableBloatStats, error) {
	c.record("getTableBloatStats")
	return map[tableIdentifier]tableBloatStats{
		"otel|public|t1": {database: "otel", schema: "public", table: "t1", bloat: 1.5},
	}, nil
}

func (c *countingClient) getIndexBloatStats(context.Context, string) (map[indexIdentifer]indexBloatStats, error) {
	c.record("getIndexBloatStats")
	return map[indexIdentifer]indexBloatStats{
		"otel|public|t1|i1": {database: "otel", schema: "public", table: "t1", indexName: "i1", bloat: 2.5},
	}, nil
}

func (c *countingClient) getDatabaseLocks(context.Context) ([]databaseLocks, error) {
	c.record("getDatabaseLocks")
	return []databaseLocks{{relation: "t1", mode: "AccessShareLock", lockType: "relation", locks: 2}}, nil
}

func (c *countingClient) getRowStats(context.Context) ([]RowStats, error) {
	c.record("getRowStats")
	return []RowStats{{relationName: "t1", rowsFetched: 1, rowsInserted: 2, rowsUpdated: 3, rowsDeleted: 4, liveRows: 5}}, nil
}

func (c *countingClient) getBufferHit(context.Context, databaseSelection) ([]BufferHit, error) {
	c.record("getBufferHit")
	return []BufferHit{{dbName: "otel", hits: 42}}, nil
}

func (c *countingClient) getQueryStats(context.Context, databaseSelection) ([]queryStats, error) {
	c.record("getQueryStats")
	return []queryStats{{key: queryStatsKey{queryID: 1}, queryID: "1", queryText: "SELECT 1", queryCount: 2, queryExecTime: 3}}, nil
}

func (c *countingClient) getQueryStatsMax(context.Context) (int, error) {
	c.record("getQueryStatsMax")
	return 5000, nil
}

func (c *countingClient) getQueryTexts(context.Context, []queryStatsKey) (map[queryStatsKey]string, error) {
	c.record("getQueryTexts")
	return map[queryStatsKey]string{}, nil
}

func (c *countingClient) getBGWriterStats(context.Context) (*bgStat, error) {
	c.record("getBGWriterStats")
	return &bgStat{checkpointsReq: 1, checkpointsScheduled: 2, checkpointWriteTime: 3, checkpointSyncTime: 4, bgWrites: 5, bufferBackendWrites: 6, bufferFsyncWrites: 7, bufferCheckpoints: 8, buffersAllocated: 9, maxWritten: 10}, nil
}

func (c *countingClient) getMaxConnections(context.Context) (int64, error) {
	c.record("getMaxConnections")
	return 100, nil
}

func (c *countingClient) getActiveConnections(context.Context) (int64, error) {
	c.record("getActiveConnections")
	return 3, nil
}

func (c *countingClient) getConnectionStats(context.Context, databaseSelection) (map[databaseName][]connectionStat, error) {
	c.record("getConnectionStats")
	return map[databaseName][]connectionStat{"otel": {{database: "otel", user: "otel", app: "otel", state: "active", count: 1}}}, nil
}

func (c *countingClient) getLatestWalAgeSeconds(context.Context) (int64, error) {
	c.record("getLatestWalAgeSeconds")
	return 11, nil
}

func (c *countingClient) getReplicationStats(context.Context) ([]replicationStats, error) {
	c.record("getReplicationStats")
	return []replicationStats{{clientAddr: "10.0.0.1", pendingBytes: 1, writeLagInt: 2, replayLagInt: 3, flushLagInt: 4}}, nil
}

func (c *countingClient) getWALStats(context.Context) (int64, int64, error) {
	c.record("getWALStats")
	return 10, 1024, nil
}

func (c *countingClient) getTransactionsStats(context.Context) (float64, float64, error) {
	c.record("getTransactionsStats")
	return 100, 500, nil
}

func (*countingClient) getVersion(context.Context) (string, error) { return "17.0", nil }

func (*countingClient) getVersionString(context.Context) (string, error) {
	return "PostgreSQL 17.0", nil
}

func (*countingClient) getQuerySamples(context.Context, int64, float64, databaseSelection, *zap.Logger) ([]map[string]any, float64, error) {
	panic("not used by metric scrapes")
}

func (*countingClient) getTopQuery(context.Context, int64, databaseSelection, *zap.Logger) ([]topQueryStatRow, error) {
	panic("not used by metric scrapes")
}

func (*countingClient) explainQuery(string, string, *zap.Logger) (string, error) {
	panic("not used by metric scrapes")
}

var _ client = (*countingClient)(nil)

type countingClientFactory struct {
	c        *countingClient
	requests []string
	mu       sync.Mutex
}

func (f *countingClientFactory) getClient(db string) (client, error) {
	f.mu.Lock()
	f.requests = append(f.requests, db)
	f.mu.Unlock()
	return f.c, nil
}

func (*countingClientFactory) close() error { return nil }

var _ postgreSQLClientFactory = (*countingClientFactory)(nil)

// scrapeWith runs one metric scrape against the counting client and returns the
// metrics plus the queries that scrape issued.
func scrapeWith(t *testing.T, mutate func(m *metadata.MetricsConfig)) (pmetric.Metrics, *countingClient, *countingClientFactory) {
	t.Helper()

	cfg := createDefaultConfig().(*Config)
	mutate(&cfg.Metrics)

	c := newCountingClient()
	factory := &countingClientFactory{c: c}
	scraper := newPostgreSQLScraper(receivertest.NewNopSettings(metadata.Type), cfg, factory, newCache(1), newTTLCache[string](1, time.Second))

	c.reset()
	m, err := scraper.scrape(t.Context())
	require.NoError(t, err)
	return m, c, factory
}

// enableAll turns on every metric the receiver declares, which is the
// configuration the gating must leave untouched.
func enableAll(m *metadata.MetricsConfig) {
	m.PostgresqlAnalyzed.Enabled = true
	m.PostgresqlAutoanalyzed.Enabled = true
	m.PostgresqlAutovacuumed.Enabled = true
	m.PostgresqlBackends.Enabled = true
	m.PostgresqlBgwriterBuffersAllocated.Enabled = true
	m.PostgresqlBgwriterBuffersWrites.Enabled = true
	m.PostgresqlBgwriterCheckpointCount.Enabled = true
	m.PostgresqlBgwriterDuration.Enabled = true
	m.PostgresqlBgwriterMaxwritten.Enabled = true
	m.PostgresqlBlkReadTime.Enabled = true
	m.PostgresqlBlkWriteTime.Enabled = true
	m.PostgresqlBlksHit.Enabled = true
	m.PostgresqlBlksRead.Enabled = true
	m.PostgresqlBlocksRead.Enabled = true
	m.PostgresqlBufferHit.Enabled = true
	m.PostgresqlCommits.Enabled = true
	m.PostgresqlConnectionCount.Enabled = true
	m.PostgresqlConnectionMax.Enabled = true
	m.PostgresqlDatabaseCount.Enabled = true
	m.PostgresqlDatabaseLocks.Enabled = true
	m.PostgresqlDbSize.Enabled = true
	m.PostgresqlDeadlocks.Enabled = true
	m.PostgresqlFunctionCalls.Enabled = true
	m.PostgresqlIndexBlocksRead.Enabled = true
	m.PostgresqlIndexBloat.Enabled = true
	m.PostgresqlIndexRowsRead.Enabled = true
	m.PostgresqlIndexScans.Enabled = true
	m.PostgresqlIndexSize.Enabled = true
	m.PostgresqlLiveRows.Enabled = true
	m.PostgresqlOperations.Enabled = true
	m.PostgresqlQueryCount.Enabled = true
	m.PostgresqlQueryTotalExecTime.Enabled = true
	m.PostgresqlReplicationDataDelay.Enabled = true
	m.PostgresqlRollbacks.Enabled = true
	m.PostgresqlRows.Enabled = true
	m.PostgresqlRowsDeleted.Enabled = true
	m.PostgresqlRowsFetched.Enabled = true
	m.PostgresqlRowsInserted.Enabled = true
	m.PostgresqlRowsUpdated.Enabled = true
	m.PostgresqlSequentialScans.Enabled = true
	m.PostgresqlTableBloat.Enabled = true
	m.PostgresqlTableCount.Enabled = true
	m.PostgresqlTableSize.Enabled = true
	m.PostgresqlTableVacuumCount.Enabled = true
	m.PostgresqlTempFiles.Enabled = true
	m.PostgresqlTempIo.Enabled = true
	m.PostgresqlToastBlocksHit.Enabled = true
	m.PostgresqlToastIndexBlocksRead.Enabled = true
	m.PostgresqlToastSize.Enabled = true
	m.PostgresqlTransactionsDurationMax.Enabled = true
	m.PostgresqlTransactionsDurationSum.Enabled = true
	m.PostgresqlTupDeleted.Enabled = true
	m.PostgresqlTupFetched.Enabled = true
	m.PostgresqlTupInserted.Enabled = true
	m.PostgresqlTupReturned.Enabled = true
	m.PostgresqlTupUpdated.Enabled = true
	m.PostgresqlWalAge.Enabled = true
	m.PostgresqlWalCount.Enabled = true
	m.PostgresqlWalDelay.Enabled = true
	m.PostgresqlWalLag.Enabled = true
	m.PostgresqlWalSize.Enabled = true
}

func disableAll(m *metadata.MetricsConfig) {
	enableAll(m)
	// Walk the same list back off. Writing it as enable-then-disable keeps a
	// single authoritative list of metric names in this file.
	*m = metadata.MetricsConfig{}
}

// metricNames returns the sorted metric names present in a scrape result.
func metricNames(m pmetric.Metrics) []string {
	var names []string
	rms := m.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		sms := rms.At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				names = append(names, ms.At(k).Name())
			}
		}
	}
	sort.Strings(names)
	return names
}

// TestCollectionPlanAllEnabledIssuesEveryQuery pins the all-enabled case: the
// gating must not remove a single query when every metric is on, because that
// configuration is the one whose output equivalence the golden-file tests in
// scraper_test.go assert.
func TestCollectionPlanAllEnabledIssuesEveryQuery(t *testing.T) {
	_, c, _ := scrapeWith(t, enableAll)

	assert.Equal(t, []string{
		"getBGWriterStats",
		"getBackends",
		"getBlocksReadByTable",
		"getBufferHit",
		"getConnectionStats",
		"getDatabaseLocks",
		"getDatabaseSize",
		"getDatabaseStats",
		"getDatabaseTableMetrics",
		"getFunctionStats",
		"getIndexBloatStats",
		"getIndexStats",
		"getLatestWalAgeSeconds",
		"getMaxConnections",
		"getQueryStats",
		"getQueryStatsMax",
		"getReplicationStats",
		"getRowStats",
		"getTableBloatStats",
		"getTransactionsStats",
		"getWALStats",
		"listDatabases",
	}, c.names())
}

// TestCollectionPlanAllDisabledIssuesNoQueries is the exit criterion: with no
// metric enabled, a scrape must reach the server only for database discovery
// and must not open a per-database connection at all.
func TestCollectionPlanAllDisabledIssuesNoQueries(t *testing.T) {
	m, c, factory := scrapeWith(t, disableAll)

	assert.Equal(t, []string{"listDatabases"}, c.names(),
		"a scrape with no metric enabled must issue no collection SQL")
	assert.Equal(t, []string{defaultPostgreSQLDatabase}, factory.requests,
		"no per-database client should be opened when nothing per-database is enabled")
	assert.Equal(t, 0, m.MetricCount())
}

// TestCollectionPlanDefaultsSkipDisabledFamilies covers the two families the
// plan calls out as pure waste at defaults.
func TestCollectionPlanDefaultsSkipDisabledFamilies(t *testing.T) {
	_, c, _ := scrapeWith(t, func(*metadata.MetricsConfig) {})

	assert.Equal(t, 0, c.count("getFunctionStats"),
		"postgresql.function.calls is disabled by default, so getFunctionStats must not run")
	assert.Equal(t, 0, c.count("getDatabaseLocks"),
		"postgresql.database.locks is disabled by default, so getDatabaseLocks must not run")

	// The families that *are* on by default still run; the default deployment is
	// not silently losing telemetry.
	assert.Equal(t, 1, c.count("getQueryStats"))
	assert.Equal(t, 1, c.count("getTableBloatStats"))
	assert.Equal(t, 1, c.count("getIndexBloatStats"))
}

func TestCollectionPlanMixedFamilies(t *testing.T) {
	tests := []struct {
		name string
		// enabled names the metrics turned on; everything else is off.
		enable func(m *metadata.MetricsConfig)
		// wantQueries is the exact set of queries the scrape may issue.
		wantQueries []string
		wantMetrics []string
	}{
		{
			// The shared-table case the plan warns about: table.count is the row
			// count of the table query, so enumeration must continue even though
			// no per-table metric is enabled.
			name: "table count alone keeps enumeration",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlTableCount.Enabled = true
			},
			wantQueries: []string{"getDatabaseTableMetrics", "listDatabases"},
			wantMetrics: []string{"postgresql.table.count"},
		},
		{
			// blocks_read is the only consumer of the block-read query, and it
			// needs the table enumeration to join against.
			name: "blocks read keeps both table queries",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlBlocksRead.Enabled = true
			},
			wantQueries: []string{"getBlocksReadByTable", "getDatabaseTableMetrics", "listDatabases"},
			wantMetrics: []string{"postgresql.blocks_read"},
		},
		{
			name: "one index metric keeps the index query and nothing else",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlIndexScans.Enabled = true
			},
			wantQueries: []string{"getIndexStats", "listDatabases"},
			wantMetrics: []string{"postgresql.index.scans"},
		},
		{
			name: "function calls alone",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlFunctionCalls.Enabled = true
			},
			wantQueries: []string{"getFunctionStats", "listDatabases"},
			wantMetrics: []string{"postgresql.function.calls"},
		},
		{
			name: "database locks alone",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlDatabaseLocks.Enabled = true
			},
			wantQueries: []string{"getDatabaseLocks", "listDatabases"},
			wantMetrics: []string{"postgresql.database.locks"},
		},
		{
			// Query-performance metrics: one of the two keeps both the stats
			// query and its cache-sizing probe.
			name: "query count alone keeps query perf",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlQueryCount.Enabled = true
			},
			wantQueries: []string{"getQueryStats", "getQueryStatsMax", "listDatabases"},
			wantMetrics: []string{"postgresql.query.count"},
		},
		{
			name: "table bloat alone",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlTableBloat.Enabled = true
			},
			wantQueries: []string{"getTableBloatStats", "listDatabases"},
			wantMetrics: []string{"postgresql.table_bloat"},
		},
		{
			name: "index bloat alone",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlIndexBloat.Enabled = true
			},
			wantQueries: []string{"getIndexBloatStats", "listDatabases"},
			wantMetrics: []string{"postgresql.index_bloat"},
		},
		{
			// A single per-database-stats metric keeps that one maintenance
			// query and neither of the other two that run beside it.
			name: "commits alone keeps only the database stats query",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlCommits.Enabled = true
			},
			wantQueries: []string{"getDatabaseStats", "listDatabases"},
			wantMetrics: []string{"postgresql.commits"},
		},
		{
			name: "db size alone",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlDbSize.Enabled = true
			},
			wantQueries: []string{"getDatabaseSize", "listDatabases"},
			wantMetrics: []string{"postgresql.db_size"},
		},
		{
			name: "buffer hit alone",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlBufferHit.Enabled = true
			},
			wantQueries: []string{"getBufferHit", "listDatabases"},
			wantMetrics: []string{"postgresql.buffer_hit"},
		},
		{
			name: "live rows alone keeps row stats",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlLiveRows.Enabled = true
			},
			wantQueries: []string{"getRowStats", "listDatabases"},
			wantMetrics: []string{"postgresql.live_rows"},
		},
		{
			// database.count is computed from the discovery list alone, so it
			// costs exactly one query.
			name: "database count needs only discovery",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlDatabaseCount.Enabled = true
			},
			wantQueries: []string{"listDatabases"},
			wantMetrics: []string{"postgresql.database.count"},
		},
		{
			name: "wal size alone keeps wal stats",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlWalSize.Enabled = true
			},
			wantQueries: []string{"getWALStats", "listDatabases"},
			wantMetrics: []string{"postgresql.wal.size"},
		},
		{
			name: "two families at once",
			enable: func(m *metadata.MetricsConfig) {
				m.PostgresqlFunctionCalls.Enabled = true
				m.PostgresqlTableCount.Enabled = true
			},
			wantQueries: []string{"getDatabaseTableMetrics", "getFunctionStats", "listDatabases"},
			wantMetrics: []string{"postgresql.function.calls", "postgresql.table.count"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, c, _ := scrapeWith(t, func(mc *metadata.MetricsConfig) {
				*mc = metadata.MetricsConfig{}
				tt.enable(mc)
			})
			assert.Equal(t, tt.wantQueries, c.names())
			assert.Equal(t, tt.wantMetrics, metricNames(m))
		})
	}
}

// TestCollectionPlanTableCountValueSurvivesDetailGating checks the count is not
// merely present but correct when the per-table detail path is skipped: it must
// still be the number of rows the table query returned.
func TestCollectionPlanTableCountValueSurvivesDetailGating(t *testing.T) {
	countOnly, _, _ := scrapeWith(t, func(m *metadata.MetricsConfig) {
		*m = metadata.MetricsConfig{}
		m.PostgresqlTableCount.Enabled = true
	})
	withDetails, _, _ := scrapeWith(t, func(m *metadata.MetricsConfig) {
		*m = metadata.MetricsConfig{}
		m.PostgresqlTableCount.Enabled = true
		m.PostgresqlTableSize.Enabled = true
	})

	assert.Equal(t, int64(2), tableCountValue(t, countOnly))
	assert.Equal(t, tableCountValue(t, withDetails), tableCountValue(t, countOnly),
		"table.count must not change when per-table detail metrics are disabled")
}

func tableCountValue(t *testing.T, m pmetric.Metrics) int64 {
	t.Helper()
	rms := m.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		sms := rms.At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				if ms.At(k).Name() == "postgresql.table.count" {
					return ms.At(k).Sum().DataPoints().At(0).IntValue()
				}
			}
		}
	}
	t.Fatal("postgresql.table.count not found")
	return 0
}

// TestNewCollectionPlanDefaults documents the plan the shipped defaults produce,
// so a future metadata.yaml default flip is visible in this test rather than
// only in a production bill.
func TestNewCollectionPlanDefaults(t *testing.T) {
	p := newCollectionPlan(metadata.DefaultMetricsConfig())

	assert.False(t, p.functions, "postgresql.function.calls is disabled by default")
	assert.False(t, p.databaseLocks, "postgresql.database.locks is disabled by default")

	// Enabled by default, so a default deployment still pays for these. Stating
	// it here keeps the commit honest about who benefits.
	assert.True(t, p.queryPerf)
	assert.True(t, p.tableBloat)
	assert.True(t, p.indexBloat)
	assert.True(t, p.tables)
	assert.True(t, p.tableDetails)
	assert.True(t, p.indexes)
	assert.True(t, p.needsDatabaseClient())
}

// TestNewCollectionPlanEmptyConfig is the other end: an all-off config plans no
// work at all.
func TestNewCollectionPlanEmptyConfig(t *testing.T) {
	p := newCollectionPlan(metadata.MetricsConfig{})

	assert.Equal(t, collectionPlan{}, p)
	assert.False(t, p.needsDatabaseClient())
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// The tests in this file drive a scraper through a sequence of scrapes on a
// fake clock and assert, per scrape, which client methods ran. They are the
// cadence counterpart of collection_plan_test.go: that file proves a disabled
// family issues no SQL, this one proves a throttled family issues none on the
// scrapes where it is not due.

// relationQueries are the client methods behind relation_metrics.
var relationQueries = []string{"getDatabaseTableMetrics", "getBlocksReadByTable", "getIndexStats", "getFunctionStats"}

// bloatQueries are the client methods behind bloat_collection_interval.
var bloatQueries = []string{"getTableBloatStats", "getIndexBloatStats"}

// everyScrapeQueries are database-level and server-wide methods that no
// cadence knob may touch.
var everyScrapeQueries = []string{"listDatabases", "getDatabaseStats", "getDatabaseSize", "getBackends", "getBGWriterStats", "getQueryStats", "getWALStats"}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// scrapeRecord is what one scrape in a sequence did.
type scrapeRecord struct {
	calls    map[string]int
	requests []string
	metrics  pmetric.Metrics
}

// cadenceHarness holds a scraper wired to a counting client and a fake clock.
type cadenceHarness struct {
	scraper *postgreSQLScraper
	client  *countingClient
	factory *countingClientFactory
	clock   *fakeClock
}

func newCadenceHarness(t *testing.T, c client, mutate func(*Config)) *cadenceHarness {
	t.Helper()

	cfg := createDefaultConfig().(*Config)
	// The factory defaults throttle relations and bloat. Each test here opts
	// into the single knob it exercises, so start from every family on every
	// scrape; TestCadenceDefaultsThrottleRelationsAndBloat pins the defaults.
	cfg.RelationMetrics.CollectionInterval = 0
	cfg.BloatCollectionInterval = 0
	// Functions are off by default; turn them on so the relation family is
	// exercised in full.
	cfg.Metrics.PostgresqlFunctionCalls.Enabled = true
	mutate(cfg)

	counting, ok := c.(interface{ counting() *countingClient })
	require.True(t, ok, "client must expose its countingClient")
	factory := &countingClientFactory{c: counting.counting()}
	clock := &fakeClock{t: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)}

	scraper := newPostgreSQLScraper(receivertest.NewNopSettings(metadata.Type), cfg, &recordingFactory{countingClientFactory: factory, c: c}, newStatementStateCache(1), newTTLCache[queryPlanKey, string](1, time.Second))
	scraper.now = clock.now
	return &cadenceHarness{scraper: scraper, client: counting.counting(), factory: factory, clock: clock}
}

// recordingFactory hands out c while recording requests on the embedded
// countingClientFactory, so tests can substitute a client that wraps
// countingClient without losing the request log.
type recordingFactory struct {
	*countingClientFactory
	c client
}

func (f *recordingFactory) getClient(db string) (client, error) {
	f.mu.Lock()
	f.requests = append(f.requests, db)
	f.mu.Unlock()
	return f.c, nil
}

func (c *countingClient) counting() *countingClient { return c }

// run performs n scrapes, stepping the clock by step before each scrape after
// the first, and returns what each scrape did.
func (h *cadenceHarness) run(t *testing.T, n int, step time.Duration) []scrapeRecord {
	t.Helper()
	records := make([]scrapeRecord, 0, n)
	for i := 0; i < n; i++ {
		if i > 0 {
			h.clock.t = h.clock.t.Add(step)
		}
		h.client.reset()
		h.factory.mu.Lock()
		h.factory.requests = nil
		h.factory.mu.Unlock()

		m, err := h.scraper.scrape(t.Context())
		require.NoError(t, err, "scrape %d", i+1)

		h.client.mu.Lock()
		calls := make(map[string]int, len(h.client.calls))
		for k, v := range h.client.calls {
			calls[k] = v
		}
		h.client.mu.Unlock()
		h.factory.mu.Lock()
		requests := append([]string(nil), h.factory.requests...)
		h.factory.mu.Unlock()
		records = append(records, scrapeRecord{calls: calls, requests: requests, metrics: m})
	}
	return records
}

// assertRanOn asserts each query in names ran exactly once on the scrapes
// listed in on (1-based) and not at all on the others.
func assertRanOn(t *testing.T, records []scrapeRecord, names []string, on ...int) {
	t.Helper()
	due := map[int]bool{}
	for _, i := range on {
		due[i] = true
	}
	for i, r := range records {
		scrape := i + 1
		for _, name := range names {
			want := 0
			if due[scrape] {
				want = 1
			}
			assert.Equal(t, want, r.calls[name], "scrape %d: %s", scrape, name)
		}
	}
}

// tableCountsByDatabase returns postgresql.table.count per database resource.
func tableCountsByDatabase(m pmetric.Metrics) map[string]int64 {
	out := map[string]int64{}
	rms := m.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		db, _ := rm.Resource().Attributes().Get("postgresql.database.name")
		sms := rm.ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				if ms.At(k).Name() == "postgresql.table.count" {
					out[db.Str()] = ms.At(k).Sum().DataPoints().At(0).IntValue()
				}
			}
		}
	}
	return out
}

func hasMetric(m pmetric.Metrics, name string) bool {
	for _, n := range metricNames(m) {
		if n == name {
			return true
		}
	}
	return false
}

func TestCadenceRelationIntervalRunsOnOneScrapeInSix(t *testing.T) {
	h := newCadenceHarness(t, newCountingClient(), func(cfg *Config) {
		cfg.RelationMetrics.CollectionInterval = 6 * cfg.CollectionInterval
	})
	records := h.run(t, 7, h.scraper.config.CollectionInterval)

	assertRanOn(t, records, relationQueries, 1, 7)
	assertRanOn(t, records, everyScrapeQueries, 1, 2, 3, 4, 5, 6, 7)
	// Bloat has no cadence of its own here, so it stays on every scrape.
	assertRanOn(t, records, bloatQueries, 1, 2, 3, 4, 5, 6, 7)

	for i, r := range records {
		scrape := i + 1
		assert.Equal(t, map[string]int64{"otel": 2}, tableCountsByDatabase(r.metrics),
			"scrape %d: table.count must be reported every scrape with the last enumerated value", scrape)
		wantDetails := scrape == 1 || scrape == 7
		assert.Equal(t, wantDetails, hasMetric(r.metrics, "postgresql.rows"), "scrape %d: per-table metrics", scrape)
		assert.Equal(t, wantDetails, hasMetric(r.metrics, "postgresql.index.scans"), "scrape %d: per-index metrics", scrape)
		assert.Equal(t, wantDetails, hasMetric(r.metrics, "postgresql.function.calls"), "scrape %d: per-function metrics", scrape)
		assert.True(t, hasMetric(r.metrics, "postgresql.commits"), "scrape %d: database-level metrics", scrape)
		assert.True(t, hasMetric(r.metrics, "postgresql.table_bloat"), "scrape %d: bloat is unthrottled here", scrape)
	}
}

func TestCadenceBloatIntervalRunsOnOneScrapeInSix(t *testing.T) {
	h := newCadenceHarness(t, newCountingClient(), func(cfg *Config) {
		cfg.BloatCollectionInterval = 6 * cfg.CollectionInterval
	})
	records := h.run(t, 7, h.scraper.config.CollectionInterval)

	assertRanOn(t, records, bloatQueries, 1, 7)
	assertRanOn(t, records, relationQueries, 1, 2, 3, 4, 5, 6, 7)
	assertRanOn(t, records, everyScrapeQueries, 1, 2, 3, 4, 5, 6, 7)

	for i, r := range records {
		scrape := i + 1
		wantBloat := scrape == 1 || scrape == 7
		assert.Equal(t, wantBloat, hasMetric(r.metrics, "postgresql.table_bloat"), "scrape %d", scrape)
		assert.Equal(t, wantBloat, hasMetric(r.metrics, "postgresql.index_bloat"), "scrape %d", scrape)
		assert.True(t, hasMetric(r.metrics, "postgresql.rows"), "scrape %d: relation metrics are unthrottled here", scrape)
	}
}

// TestCadenceBothThrottledOpensNoDatabaseConnection is the saving the knobs
// exist for: with every per-database family throttled and none due, the scrape
// touches only the maintenance connection, and the table count is served from
// the last enumeration.
func TestCadenceBothThrottledOpensNoDatabaseConnection(t *testing.T) {
	h := newCadenceHarness(t, newCountingClient(), func(cfg *Config) {
		cfg.RelationMetrics.CollectionInterval = 6 * cfg.CollectionInterval
		cfg.BloatCollectionInterval = 6 * cfg.CollectionInterval
	})
	records := h.run(t, 7, h.scraper.config.CollectionInterval)

	assertRanOn(t, records, relationQueries, 1, 7)
	assertRanOn(t, records, bloatQueries, 1, 7)
	assertRanOn(t, records, everyScrapeQueries, 1, 2, 3, 4, 5, 6, 7)

	for i, r := range records {
		scrape := i + 1
		if scrape == 1 || scrape == 7 {
			assert.Equal(t, []string{defaultPostgreSQLDatabase, "otel"}, r.requests, "scrape %d", scrape)
		} else {
			assert.Equal(t, []string{defaultPostgreSQLDatabase}, r.requests,
				"scrape %d: no per-database connection when nothing per-database is due", scrape)
		}
		assert.Equal(t, map[string]int64{"otel": 2}, tableCountsByDatabase(r.metrics), "scrape %d", scrape)
	}
}

// TestCadenceUnsetRunsEveryFamilyEveryScrape pins the default: with no
// interval configured, seven consecutive scrapes each issue the full
// all-enabled query set, which is what the golden tests assert on.
// TestCadenceDefaultsThrottleRelationsAndBloat pins the factory defaults: on a
// 10s scrape, relation families run once a minute (scrapes 1, 7, 13, ...) and
// bloat once every ten minutes (scrapes 1 and 61), while database-level and
// server-wide families and postgresql.table.count are on every scrape.
func TestCadenceDefaultsThrottleRelationsAndBloat(t *testing.T) {
	defaults := createDefaultConfig().(*Config)
	require.Equal(t, time.Minute, defaults.RelationMetrics.CollectionInterval)
	require.Equal(t, 10*time.Minute, defaults.BloatCollectionInterval)
	require.Equal(t, 10*time.Second, defaults.CollectionInterval)

	h := newCadenceHarness(t, newCountingClient(), func(cfg *Config) {
		cfg.RelationMetrics.CollectionInterval = defaults.RelationMetrics.CollectionInterval
		cfg.BloatCollectionInterval = defaults.BloatCollectionInterval
	})
	const scrapes = 61
	records := h.run(t, scrapes, h.scraper.config.CollectionInterval)

	var every, relationDue []int
	for i := 1; i <= scrapes; i++ {
		every = append(every, i)
		if (i-1)%6 == 0 {
			relationDue = append(relationDue, i)
		}
	}
	assertRanOn(t, records, relationQueries, relationDue...)
	assertRanOn(t, records, bloatQueries, 1, 61)
	assertRanOn(t, records, everyScrapeQueries, every...)
	for i, r := range records {
		assert.Equal(t, map[string]int64{"otel": 2}, tableCountsByDatabase(r.metrics),
			"scrape %d: table.count must be reported every scrape", i+1)
		assert.True(t, hasMetric(r.metrics, "postgresql.commits"), "scrape %d: database-level metrics", i+1)
	}
}

func TestCadenceUnsetRunsEveryFamilyEveryScrape(t *testing.T) {
	h := newCadenceHarness(t, newCountingClient(), func(cfg *Config) {
		enableAll(&cfg.Metrics)
	})
	records := h.run(t, 7, h.scraper.config.CollectionInterval)

	all := []int{1, 2, 3, 4, 5, 6, 7}
	assertRanOn(t, records, relationQueries, all...)
	assertRanOn(t, records, bloatQueries, all...)
	assertRanOn(t, records, everyScrapeQueries, all...)
	for i, r := range records {
		assert.Equal(t, []string{defaultPostgreSQLDatabase, "otel"}, r.requests, "scrape %d", i+1)
		for name, n := range r.calls {
			assert.Equal(t, 1, n, "scrape %d: %s", i+1, name)
		}
	}
	// getQueryStatsMax is a one-time sizing probe, so only the first scrape
	// issues the full 22-query set that TestCollectionPlanAllEnabledIssuesEveryQuery pins.
	assert.Len(t, records[0].calls, 22)
	assert.Len(t, records[1].calls, 21)
}

// TestCadenceToleratesTickJitter covers the two edges of the due check: a
// scrape landing a few milliseconds before the deadline still runs the family,
// and an interval that is not a multiple of the scrape interval rounds up to
// the next scrape rather than down.
func TestCadenceToleratesTickJitter(t *testing.T) {
	t.Run("slightly early scrape is due", func(t *testing.T) {
		h := newCadenceHarness(t, newCountingClient(), func(cfg *Config) {
			cfg.RelationMetrics.CollectionInterval = 6 * cfg.CollectionInterval
		})
		records := h.run(t, 7, h.scraper.config.CollectionInterval-5*time.Millisecond)
		assertRanOn(t, records, relationQueries, 1, 7)
	})

	t.Run("non-multiple interval rounds up", func(t *testing.T) {
		h := newCadenceHarness(t, newCountingClient(), func(cfg *Config) {
			cfg.RelationMetrics.CollectionInterval = 6*cfg.CollectionInterval + cfg.CollectionInterval/2
		})
		records := h.run(t, 8, h.scraper.config.CollectionInterval)
		assertRanOn(t, records, relationQueries, 1, 8)
	})
}

// listingClient is a countingClient whose discovery list can change between
// scrapes.
type listingClient struct {
	*countingClient
	dbs []string
}

func (c *listingClient) listDatabases(context.Context) ([]string, error) {
	c.record("listDatabases")
	return c.dbs, nil
}

// TestCadenceNewDatabaseIsCountedBetweenEnumerations: a database that appears
// while the relation families are throttled has no remembered table count, so
// that one database is enumerated for its count — and only its count — rather
// than reported as empty until the next full run.
func TestCadenceNewDatabaseIsCountedBetweenEnumerations(t *testing.T) {
	c := &listingClient{countingClient: newCountingClient(), dbs: []string{"otel"}}
	h := newCadenceHarness(t, c, func(cfg *Config) {
		cfg.RelationMetrics.CollectionInterval = 6 * cfg.CollectionInterval
		cfg.BloatCollectionInterval = 6 * cfg.CollectionInterval
	})
	step := h.scraper.config.CollectionInterval

	first := h.run(t, 1, step)
	assert.Equal(t, map[string]int64{"otel": 2}, tableCountsByDatabase(first[0].metrics))

	c.dbs = []string{"otel", "newdb"}
	second := h.run(t, 1, step)
	assert.Equal(t, []string{defaultPostgreSQLDatabase, "newdb"}, second[0].requests,
		"only the database without a remembered count opens a connection")
	assert.Equal(t, 1, second[0].calls["getDatabaseTableMetrics"])
	assert.Equal(t, 0, second[0].calls["getBlocksReadByTable"], "count only: no per-table detail query")
	assert.Equal(t, 0, second[0].calls["getIndexStats"])
	assert.False(t, hasMetric(second[0].metrics, "postgresql.rows"), "count only: no per-table data points")
	assert.Equal(t, map[string]int64{"otel": 2, "newdb": 2}, tableCountsByDatabase(second[0].metrics))

	third := h.run(t, 1, step)
	assert.Equal(t, []string{defaultPostgreSQLDatabase}, third[0].requests, "both counts are now remembered")
	assert.Equal(t, map[string]int64{"otel": 2, "newdb": 2}, tableCountsByDatabase(third[0].metrics))
}

// failingTablesClient fails table enumeration until told otherwise.
type failingTablesClient struct {
	*countingClient
	fail bool
}

func (c *failingTablesClient) getDatabaseTableMetrics(ctx context.Context, db string) (map[tableIdentifier]tableStats, error) {
	if c.fail {
		c.record("getDatabaseTableMetrics")
		return nil, errors.New("enumeration failed")
	}
	return c.countingClient.getDatabaseTableMetrics(ctx, db)
}

// TestCadenceFailedEnumerationIsNotRemembered: a failed enumeration must not
// pin postgresql.table.count at zero for the rest of the interval; the next
// scrape reads the count again.
func TestCadenceFailedEnumerationIsNotRemembered(t *testing.T) {
	c := &failingTablesClient{countingClient: newCountingClient(), fail: true}
	h := newCadenceHarness(t, c, func(cfg *Config) {
		cfg.RelationMetrics.CollectionInterval = 6 * cfg.CollectionInterval
		cfg.BloatCollectionInterval = 6 * cfg.CollectionInterval
	})
	step := h.scraper.config.CollectionInterval

	h.clock.t = h.clock.t.Add(step)
	_, err := h.scraper.scrape(t.Context())
	require.Error(t, err, "the failed enumeration is reported as a partial error")

	c.fail = false
	records := h.run(t, 2, step)
	assert.Equal(t, 1, records[0].calls["getDatabaseTableMetrics"], "scrape 2 re-reads the count")
	assert.Equal(t, map[string]int64{"otel": 2}, tableCountsByDatabase(records[0].metrics))
	assert.Equal(t, 0, records[1].calls["getDatabaseTableMetrics"], "scrape 3 serves the remembered count")
	assert.Equal(t, map[string]int64{"otel": 2}, tableCountsByDatabase(records[1].metrics))
}

// topQueryClient answers getTopQuery with no rows so the top-query scraper can
// be driven through a sequence without a pg_stat_statements fixture.
type topQueryClient struct{ *countingClient }

func (c *topQueryClient) getTopQuery(context.Context, int64, databaseSelection, *zap.Logger) ([]topQueryStatRow, error) {
	c.record("getTopQuery")
	return nil, nil
}

func TestCadenceTopQueryIntervalRunsOnOneScrapeInSix(t *testing.T) {
	c := &topQueryClient{countingClient: newCountingClient()}
	h := newCadenceHarness(t, c, func(cfg *Config) {
		cfg.TopQueryCollection.Interval = 6 * cfg.CollectionInterval
	})
	step := h.scraper.config.CollectionInterval

	for scrape := 1; scrape <= 7; scrape++ {
		if scrape > 1 {
			h.clock.t = h.clock.t.Add(step)
		}
		h.client.reset()
		_, err := h.scraper.scrapeTopQuery(t.Context(), 1000, 1000, 1000)
		require.NoError(t, err)
		want := 0
		if scrape == 1 || scrape == 7 {
			want = 1
		}
		assert.Equal(t, want, h.client.count("getTopQuery"), "scrape %d", scrape)
	}
}

func TestCadenceTopQueryUnsetRunsEveryScrape(t *testing.T) {
	c := &topQueryClient{countingClient: newCountingClient()}
	h := newCadenceHarness(t, c, func(*Config) {})
	step := h.scraper.config.CollectionInterval

	for scrape := 1; scrape <= 3; scrape++ {
		h.clock.t = h.clock.t.Add(step)
		h.client.reset()
		_, err := h.scraper.scrapeTopQuery(t.Context(), 1000, 1000, 1000)
		require.NoError(t, err)
		assert.Equal(t, 1, h.client.count("getTopQuery"), "scrape %d", scrape)
	}
}

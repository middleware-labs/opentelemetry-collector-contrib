// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// benchClient is countingClient with a configurable object count, so the
// per-object work a disabled family avoids scales the way it does on a real
// object-heavy database rather than being a single row.
type benchClient struct {
	*countingClient
	objects int
}

func (c *benchClient) getDatabaseTableMetrics(context.Context, string) (map[tableIdentifier]tableStats, error) {
	out := make(map[tableIdentifier]tableStats, c.objects)
	for i := 0; i < c.objects; i++ {
		name := "t" + strconv.Itoa(i)
		out[tableIdentifier("otel|public|"+name)] = tableStats{
			database: "otel", schema: "public", table: name,
			live: 900, dead: 100, inserts: 1, upd: 2, del: 3, hotUpd: 4,
			seqScans: 5, size: 4096, vacuumCount: 6, autovacuumCount: 7,
			analyzeCount: 8, autoanalyzeCount: 9, toastSize: 1024,
		}
	}
	return out, nil
}

func (c *benchClient) getBlocksReadByTable(context.Context, string) (map[tableIdentifier]tableIOStats, error) {
	out := make(map[tableIdentifier]tableIOStats, c.objects)
	for i := 0; i < c.objects; i++ {
		name := "t" + strconv.Itoa(i)
		out[tableIdentifier("otel|public|"+name)] = tableIOStats{
			database: "otel", schema: "public", table: name,
			heapRead: 1, heapHit: 2, idxRead: 3, idxHit: 4,
			toastRead: 5, toastHit: 6, tidxRead: 7, tidxHit: 8,
		}
	}
	return out, nil
}

func (c *benchClient) getIndexStats(context.Context, string) (map[indexIdentifer]indexStat, error) {
	out := make(map[indexIdentifer]indexStat, c.objects)
	for i := 0; i < c.objects; i++ {
		name := "t" + strconv.Itoa(i)
		out[indexIdentifer("otel|public|"+name+"|i")] = indexStat{
			database: "otel", schema: "public", table: name, index: name + "_pkey",
			size: 4096, scans: 1, tuplesRead: 2, blocksRead: 3, blocksHit: 4,
		}
	}
	return out, nil
}

func (c *benchClient) getFunctionStats(context.Context, string) (map[functionIdentifer]functionStat, error) {
	out := make(map[functionIdentifer]functionStat, c.objects)
	for i := 0; i < c.objects; i++ {
		name := "f" + strconv.Itoa(i)
		out[functionIdentifer("otel|public|"+name)] = functionStat{
			database: "otel", schema: "public", function: name, calls: 42,
		}
	}
	return out, nil
}

func (c *benchClient) getTableBloatStats(context.Context, string) (map[tableIdentifier]tableBloatStats, error) {
	out := make(map[tableIdentifier]tableBloatStats, c.objects)
	for i := 0; i < c.objects; i++ {
		name := "t" + strconv.Itoa(i)
		out[tableIdentifier("otel|public|"+name)] = tableBloatStats{
			database: "otel", schema: "public", table: name, bloat: 1.25,
		}
	}
	return out, nil
}

func (c *benchClient) getIndexBloatStats(context.Context, string) (map[indexIdentifer]indexBloatStats, error) {
	out := make(map[indexIdentifer]indexBloatStats, c.objects)
	for i := 0; i < c.objects; i++ {
		name := "t" + strconv.Itoa(i)
		out[indexIdentifer("otel|public|"+name+"|i")] = indexBloatStats{
			database: "otel", schema: "public", table: name, indexName: name + "_pkey", bloat: 2.5,
		}
	}
	return out, nil
}

var _ client = (*benchClient)(nil)

type benchClientFactory struct{ c *benchClient }

func (f *benchClientFactory) getClient(string) (client, error) { return f.c, nil }
func (*benchClientFactory) close() error                       { return nil }

// benchScrape measures a full metric scrape under one metrics configuration.
// It is the measurement the Step 5 gating is for: the difference between the
// configurations is entirely the query, conversion and resource work that
// disabled families no longer do.
func benchScrape(b *testing.B, objects int, mutate func(*metadata.MetricsConfig)) {
	b.Helper()

	cfg := createDefaultConfig().(*Config)
	mutate(&cfg.Metrics)

	c := &benchClient{countingClient: newCountingClient(), objects: objects}
	factory := &benchClientFactory{c: c}
	scraper := newPostgreSQLScraper(receivertest.NewNopSettings(metadata.Type), cfg, factory, newStatementStateCache(1), newTTLCache[queryPlanKey, string](1, time.Second))

	b.ReportAllocs()
	for b.Loop() {
		if _, err := scraper.scrape(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScrapeDefaults is the shipped configuration. Its saving over the
// ungated receiver is exactly the function and lock families, which are off by
// default; the bloat and query-performance families are on, so this
// configuration keeps paying for them.
func BenchmarkScrapeDefaults(b *testing.B) {
	for _, objects := range []int{10, 100, 1000} {
		b.Run("objects="+strconv.Itoa(objects), func(b *testing.B) {
			benchScrape(b, objects, func(*metadata.MetricsConfig) {})
		})
	}
}

// BenchmarkScrapeAllEnabled is the configuration the gating must not change.
func BenchmarkScrapeAllEnabled(b *testing.B) {
	for _, objects := range []int{10, 100, 1000} {
		b.Run("objects="+strconv.Itoa(objects), func(b *testing.B) {
			benchScrape(b, objects, enableAll)
		})
	}
}

// BenchmarkScrapeServerWideOnly is the configuration that benefits most: only
// server-wide metrics enabled, so no per-database connection is opened and no
// per-object query runs at all.
func BenchmarkScrapeServerWideOnly(b *testing.B) {
	for _, objects := range []int{10, 100, 1000} {
		b.Run("objects="+strconv.Itoa(objects), func(b *testing.B) {
			benchScrape(b, objects, func(m *metadata.MetricsConfig) {
				*m = metadata.MetricsConfig{}
				m.PostgresqlDatabaseCount.Enabled = true
				m.PostgresqlConnectionMax.Enabled = true
				m.PostgresqlWalCount.Enabled = true
				m.PostgresqlWalSize.Enabled = true
			})
		})
	}
}

// BenchmarkScrapeNoBloat is the realistic opt-out: defaults minus the two most
// expensive per-database queries.
func BenchmarkScrapeNoBloat(b *testing.B) {
	for _, objects := range []int{10, 100, 1000} {
		b.Run("objects="+strconv.Itoa(objects), func(b *testing.B) {
			benchScrape(b, objects, func(m *metadata.MetricsConfig) {
				m.PostgresqlTableBloat.Enabled = false
				m.PostgresqlIndexBloat.Enabled = false
			})
		})
	}
}

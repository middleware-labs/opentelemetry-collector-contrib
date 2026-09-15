// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// Benchmarks for resource emission, which the plan flags as a conditional
// Step 8 follow-up: EmitForResource allocates a ResourceMetrics and a
// ScopeMetrics, reserves the builder-wide metric capacity, and then visits all
// 61 metric families for every resource - table, index, function or bloat -
// even though a table resource carries only a handful of them.
//
// Step 8 is explicitly conditional on profiles showing this is dominant, so
// these exist to answer that question with a measurement rather than to commit
// to a change. A database with many tables emits one resource per table per
// scrape, so the per-resource constant is multiplied by the object count.

func newBenchMetricsBuilder(b *testing.B) *metadata.MetricsBuilder {
	b.Helper()
	cfg := createDefaultConfig().(*Config)
	settings := receivertest.NewNopSettings(metadata.Type)
	settings.TelemetrySettings = component.TelemetrySettings{Logger: zap.NewNop()}
	return metadata.NewMetricsBuilder(cfg.MetricsBuilderConfig, settings)
}

// benchEmitTableResources emits one resource per table, recording the same
// handful of table metrics the scraper records before each emit.
func benchEmitTableResources(b *testing.B, tables int) {
	b.Helper()

	scraper := newTestTopQueryScraper(b)
	mb := newBenchMetricsBuilder(b)
	now := pcommon.NewTimestampFromTime(time.Now())

	names := make([]string, tables)
	for i := range tables {
		names[i] = "orders_partition_" + strconv.Itoa(i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, name := range names {
			// The metrics a table resource actually carries: a few of the 61
			// families the builder will nonetheless visit.
			mb.RecordPostgresqlTableSizeDataPoint(now, 4096)
			mb.RecordPostgresqlTableVacuumCountDataPoint(now, 3)
			mb.RecordPostgresqlRowsDataPoint(now, 100, metadata.AttributeStateDead)
			mb.RecordPostgresqlRowsDataPoint(now, 900, metadata.AttributeStateLive)

			rb := scraper.setupResourceBuilder(mb.NewResourceBuilder(), "orders_db", "public", name, "")
			mb.EmitForResource(metadata.WithResource(rb.Emit()))
		}

		b.StopTimer()
		// Drain so the benchmark measures per-scrape construction rather than
		// unbounded accumulation across iterations.
		mb.Emit()
		b.StartTimer()
	}
}

// BenchmarkEmitTableResources covers the object counts the plan's validation
// section calls for: a modest database, and one object-heavy database where
// per-resource cost is multiplied hardest.
func BenchmarkEmitTableResources(b *testing.B) {
	for _, tables := range []int{10, 100, 1000} {
		b.Run("tables="+strconv.Itoa(tables), func(b *testing.B) {
			benchEmitTableResources(b, tables)
		})
	}
}

// BenchmarkEmitSingleResource isolates the fixed per-resource cost: the
// ResourceMetrics and ScopeMetrics allocation, the capacity reservation and the
// 61 emit calls, with only one metric actually recorded. This is the constant
// that the table and index counts multiply.
func BenchmarkEmitSingleResource(b *testing.B) {
	scraper := newTestTopQueryScraper(b)
	mb := newBenchMetricsBuilder(b)
	now := pcommon.NewTimestampFromTime(time.Now())

	b.ReportAllocs()
	for b.Loop() {
		mb.RecordPostgresqlTableSizeDataPoint(now, 4096)
		rb := scraper.setupResourceBuilder(mb.NewResourceBuilder(), "orders_db", "public", "orders", "")
		mb.EmitForResource(metadata.WithResource(rb.Emit()))

		b.StopTimer()
		mb.Emit()
		b.StartTimer()
	}
}

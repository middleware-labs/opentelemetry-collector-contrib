// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"database/sql"
	"strconv"
	"testing"

	"go.uber.org/zap"
)

// Benchmarks for the counter-cache path: the delta loop in collectTopQuery,
// which today performs one LRU lookup and one LRU insert per counter per
// candidate statement, each against a freshly concatenated string key.
//
// Step 7 replaces that layout with a single typed snapshot per statement
// identity. These benchmarks are what that change has to beat. They are driven
// through collectTopQuery rather than through an extracted helper, because the
// key construction being removed happens inside the loop and an extracted
// helper would have to reproduce it - which the plan explicitly warns against.
//
// Interpretation note: the plan records that top_n_query and max_rows_per_query
// both default to 1000, so at defaults every candidate is also emitted and the
// heap cuts nothing. The low-N shape (50 of 1000) is measured separately
// because Steps 6 and 7 save different amounts in each: at low N most decoded
// rows are discarded after the counter work has already been paid for.

// benchCollectTopQuery runs repeated scrapes over a fixed candidate set with
// fully warmed baselines, which is the steady state a long-running collector is
// in: every statement has been seen, so every scrape does the full lookup,
// compare and insert for every counter of every candidate.
func benchCollectTopQuery(b *testing.B, candidates int, topN int64) {
	b.Helper()

	cfg := createDefaultConfig().(*Config)
	cfg.Events.DbServerTopQuery.Enabled = true
	cfg.TopNQuery = topN
	cfg.TopQueryCollection.MaxRowsPerQuery = int64(candidates)

	scraper := newTestTopQueryScraperWithConfig(b, cfg,
		newCache(candidates*int(topQueryCounterCount)*2))

	rows := func(scrape int) []topQueryStatRow {
		out := make([]topQueryStatRow, candidates)
		for i := range candidates {
			// Counters advance every scrape, so every statement is reportable
			// and none takes the cheap baseline-only path.
			row := topQueryRow("q"+strconv.Itoa(i),
				float64(1000+scrape),
				float64(5000+scrape*10))
			// Real statement text, so the enrichment this benchmark measures -
			// obfuscation and the comment scan - does the work it does in
			// production rather than running over a two-token literal.
			row.query = sql.NullString{String: representativeQuery, Valid: true}
			out[i] = row
		}
		return out
	}

	// Warm the baselines. The first scrape only stores them, so measuring it
	// would report the cheaper path rather than the steady state.
	logger := zap.NewNop()
	scraper.collectTopQuery(b.Context(), fakeTopQueryClientFactory{rows: rows(0)}, int64(candidates), topN, 0, &errsMux{}, logger)
	scraper.lb.Emit()

	scrape := 1
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		r := rows(scrape)
		scrape++
		b.StartTimer()

		scraper.collectTopQuery(b.Context(), fakeTopQueryClientFactory{rows: r}, int64(candidates), topN, 0, &errsMux{}, logger)

		b.StopTimer()
		// Drain, so emitted records do not accumulate across iterations and
		// turn this into a memory-growth benchmark.
		scraper.lb.Emit()
		b.StartTimer()
	}
}

// BenchmarkCollectTopQueryDefaultShape is the default configuration: 1000
// candidates, all of which are also emitted.
func BenchmarkCollectTopQueryDefaultShape(b *testing.B) {
	benchCollectTopQuery(b, 1000, 1000)
}

// BenchmarkCollectTopQueryLowN is the shape where the counter work is most
// clearly wasted: 1000 candidates are decoded and differenced, then all but 50
// are discarded. This is also the configuration that the old cache sizing broke
// outright, so it is worth keeping measured.
func BenchmarkCollectTopQueryLowN(b *testing.B) {
	benchCollectTopQuery(b, 1000, 50)
}

// BenchmarkCollectTopQuerySmallServer is a modest workload, for scale
// comparison rather than as an optimisation target.
func BenchmarkCollectTopQuerySmallServer(b *testing.B) {
	benchCollectTopQuery(b, 50, 50)
}

// BenchmarkTopQueryDeltaKey isolates the per-counter key construction. The
// delta loop builds deltaKey once per row but then concatenates a column name
// onto it for each of the twelve counters, so this cost is paid 12x per
// candidate per scrape. Step 7 removes the concatenation entirely by keying a
// single snapshot on a typed identity.
func BenchmarkTopQueryDeltaKey(b *testing.B) {
	row := topQueryRow("987654321", 1, 1)

	b.ReportAllocs()
	for b.Loop() {
		key := topQueryStatDeltaKey(&row, "987654321")
		// Reproduce what the delta loop does with the key, since the
		// concatenation is the part being removed.
		for columnName := range updatedOnly {
			sink = key + columnName
		}
	}
}

// sink prevents the compiler from eliminating the work under measurement.
var sink string

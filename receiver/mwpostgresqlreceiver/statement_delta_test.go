// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// topQueryRow builds one pg_stat_statements row as collectTopQuery receives it,
// with the counters the caller wants to control.
//
// collectTopQuery mutates the row map in place, so every scrape needs its own.
func topQueryRow(queryID string, calls, execTime float64) map[string]any {
	return map[string]any{
		"db.namespace":                                  "somedb",
		"db.query.text":                                 "select 1",
		"db.query.comment":                              "",
		dbAttributePrefix + "raw_query":                 "select 1",
		dbAttributePrefix + "rolname":                   "someuser",
		dbAttributePrefix + queryidColumnName:           queryID,
		dbAttributePrefix + callsColumnName:             calls,
		dbAttributePrefix + rowsColumnName:              float64(0),
		dbAttributePrefix + sharedBlksDirtiedColumnName: float64(0),
		dbAttributePrefix + sharedBlksHitColumnName:     float64(0),
		dbAttributePrefix + sharedBlksReadColumnName:    float64(0),
		dbAttributePrefix + sharedBlksWrittenColumnName: float64(0),
		dbAttributePrefix + tempBlksReadColumnName:      float64(0),
		dbAttributePrefix + tempBlksWrittenColumnName:   float64(0),
		dbAttributePrefix + totalExecTimeColumnName:     execTime,
		dbAttributePrefix + totalPlanTimeColumnName:     float64(0),
		postgresqlBlkReadTimeAttributeName:              float64(0),
		postgresqlBlkWriteTimeAttributeName:             float64(0),
	}
}

// scrapeTopQueryRows runs one scrape over the given rows and returns the log
// records it emitted.
func scrapeTopQueryRows(t *testing.T, scraper *postgreSQLScraper, rows ...map[string]any) int {
	t.Helper()
	before := scraper.lb.Emit().LogRecordCount()
	scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: rows}, 1000, 1000, 10, &errsMux{}, zap.NewNop())
	return scraper.lb.Emit().LogRecordCount() - before
}

// emittedExecTime runs a scrape and returns the total_exec_time attribute of
// the single record it emitted.
func emittedExecTime(t *testing.T, scraper *postgreSQLScraper, rows ...map[string]any) float64 {
	t.Helper()
	scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: rows}, 1000, 1000, 10, &errsMux{}, zap.NewNop())
	logs := scraper.lb.Emit()
	require.Equal(t, 1, logs.LogRecordCount(), "expected exactly one emitted record")
	rec := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
	v, ok := rec.Attributes().Get(postgresqlTotalExecTimeAttributeName)
	require.True(t, ok, "record is missing %s", postgresqlTotalExecTimeAttributeName)
	return v.Double()
}

// TestTopQueryFirstObservationEmitsNothing is the core of the correction. A
// pg_stat_statements counter is cumulative for the life of the entry, so the
// first time an entry is seen there is no interval to attribute its value to.
// Emitting it reports however long the statement has been running on that
// server - possibly months - as one collection interval's work.
func TestTopQueryFirstObservationEmitsNothing(t *testing.T) {
	scraper := newTestTopQueryScraper(t)

	emitted := scrapeTopQueryRows(t, scraper, topQueryRow("q1", 100, 5000))
	assert.Equal(t, 0, emitted, "a statement's first observation is a baseline, not a measurement")

	// The next scrape has something to difference against, so it reports the
	// work done in between and nothing more.
	got := emittedExecTime(t, scraper, topQueryRow("q1", 110, 5050))
	assert.InDelta(t, 50.0, got, 1e-9, "second scrape must report only the interval delta")
}

// TestTopQueryLowTopNWithManyCandidatesEmitsNoCumulativeTotals covers the
// interaction that made the first-observation bug permanent rather than
// one-off: the delta cache used to be sized from top_n_query, while every
// max_rows_per_query candidate is traversed each scrape. With a small N the
// whole candidate set was evicted every scrape, so every row took the
// first-observation path forever and every scrape reported lifetime totals.
func TestTopQueryLowTopNWithManyCandidatesEmitsNoCumulativeTotals(t *testing.T) {
	const candidates = 200

	cfg := createDefaultConfig().(*Config)
	cfg.Events.DbServerTopQuery.Enabled = true
	cfg.TopNQuery = 5
	cfg.TopQueryCollection.MaxRowsPerQuery = candidates

	scraper := newTestTopQueryScraperWithConfig(t, cfg,
		newCache(int(cfg.TopQueryCollection.MaxRowsPerQuery*topQueryCounterCount*2)))

	first := make([]map[string]any, 0, candidates)
	second := make([]map[string]any, 0, candidates)
	for i := range candidates {
		id := "q" + strconv.Itoa(i)
		// Large lifetime totals, tiny interval movement. If any cumulative
		// value leaks out it will dwarf the real delta.
		first = append(first, topQueryRow(id, 1_000_000, 900_000))
		second = append(second, topQueryRow(id, 1_000_001, 900_001))
	}

	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, first...),
		"no statement has been observed before, so nothing can be reported yet")

	scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: second}, candidates, cfg.TopNQuery, 10, &errsMux{}, zap.NewNop())
	logs := scraper.lb.Emit()
	require.Positive(t, logs.LogRecordCount(), "baselined statements that did work must be reported")

	records := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	for i := 0; i < records.Len(); i++ {
		v, ok := records.At(i).Attributes().Get(postgresqlTotalExecTimeAttributeName)
		require.True(t, ok)
		assert.InDelta(t, 1.0, v.Double(), 1e-9,
			"emitted value must be the interval delta, never the lifetime total")
	}
}

// TestTopQueryRebaselinesOnCounterDecrease covers a counter going backwards,
// which means the entry was reset or deallocated and re-created. The remembered
// value describes a series that no longer exists.
//
// Leaving the higher value cached, as the code used to, suppresses the
// statement until it climbs past its own former peak - on a busy server, hours.
func TestTopQueryRebaselinesOnCounterDecrease(t *testing.T) {
	scraper := newTestTopQueryScraper(t)

	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 100, 5000)))
	require.InDelta(t, 50.0, emittedExecTime(t, scraper, topQueryRow("q1", 110, 5050)), 1e-9)

	// The counters drop: this entry was reset out from under us.
	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 2, 7)),
		"a decreased counter rebaselines and reports nothing")

	// The statement must be reportable again immediately, differenced against
	// the new low baseline rather than the stale peak.
	got := emittedExecTime(t, scraper, topQueryRow("q1", 5, 10))
	assert.InDelta(t, 3.0, got, 1e-9,
		"after a rebaseline the next delta is measured from the new value")
}

// TestTopQueryUnchangedEntryNotReported covers an entry that was deallocated
// and re-inserted with the counters it already had. Nothing ran, so nothing
// should be reported: without the calls guard the row would be emitted with a
// zero-or-garbage delta as though it had done work.
func TestTopQueryUnchangedEntryNotReported(t *testing.T) {
	scraper := newTestTopQueryScraper(t)

	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 100, 5000)))
	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 100, 5000)),
		"calls did not advance, so this statement did no work this interval")

	// Real work resumes and is measured from the unchanged baseline.
	got := emittedExecTime(t, scraper, topQueryRow("q1", 101, 5010))
	assert.InDelta(t, 10.0, got, 1e-9)
}

// TestTopQueryScrapeAfterPurgeEmitsNothing covers the reset and instance-change
// paths. Both purge the cache and then run the delta loop over the rows just
// read, so before this change the scrape that detected the reset emitted the
// new server's lifetime totals as one interval of work - the exact outcome the
// purge exists to prevent.
func TestTopQueryScrapeAfterPurgeEmitsNothing(t *testing.T) {
	scraper := newTestTopQueryScraper(t)

	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 100, 5000)))
	require.InDelta(t, 50.0, emittedExecTime(t, scraper, topQueryRow("q1", 110, 5050)), 1e-9)

	// Stand in for what the reset and instance-change detectors do.
	scraper.cache.Purge()

	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 3, 9)),
		"the scrape that follows a purge re-baselines and reports nothing")

	got := emittedExecTime(t, scraper, topQueryRow("q1", 4, 12))
	assert.InDelta(t, 3.0, got, 1e-9, "the scrape after that reports a true delta again")
}

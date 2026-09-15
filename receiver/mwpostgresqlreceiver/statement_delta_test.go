// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"database/sql"
	"hash/crc32"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// topQueryRow builds one pg_stat_statements row as collectTopQuery receives it,
// with the counters the caller wants to control.
//
// execTime is given in the unit the emitted attribute uses (seconds), and is
// converted to the milliseconds pg_stat_statements actually reports, so the
// expectations in these tests read in the same unit as the assertion.
func topQueryRow(queryID string, calls, execTime float64) topQueryStatRow {
	id, err := strconv.ParseInt(strings.TrimPrefix(queryID, "q"), 10, 64)
	if err != nil {
		// The tests use short symbolic ids like "q1"; anything else is hashed
		// to a stable number so distinct ids stay distinct.
		id = int64(crc32.ChecksumIEEE([]byte(queryID)))
	}
	return topQueryStatRow{
		calls:         sql.NullInt64{Int64: int64(calls), Valid: true},
		datname:       sql.NullString{String: "somedb", Valid: true},
		query:         sql.NullString{String: "select 1", Valid: true},
		queryID:       sql.NullInt64{Int64: id, Valid: true},
		rolname:       sql.NullString{String: "someuser", Valid: true},
		totalExecTime: sql.NullFloat64{Float64: execTime * 1000.0, Valid: true},
	}
}

// scrapeTopQueryRows runs one scrape over the given rows and returns the log
// records it emitted.
func scrapeTopQueryRows(t *testing.T, scraper *postgreSQLScraper, rows ...topQueryStatRow) int {
	t.Helper()
	before := scraper.lb.Emit().LogRecordCount()
	scraper.collectTopQuery(t.Context(), fakeTopQueryClientFactory{rows: rows}, 1000, 1000, 10, &errsMux{}, zap.NewNop())
	return scraper.lb.Emit().LogRecordCount() - before
}

// emittedExecTime runs a scrape and returns the total_exec_time attribute of
// the single record it emitted.
func emittedExecTime(t *testing.T, scraper *postgreSQLScraper, rows ...topQueryStatRow) float64 {
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
		newStatementStateCache(int(cfg.TopQueryCollection.MaxRowsPerQuery*2)))

	first := make([]topQueryStatRow, 0, candidates)
	second := make([]topQueryStatRow, 0, candidates)
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

	// The counters drop: this entry was reset out from under us. calls rises
	// while exec time falls, so neither the calls guard nor the emit path's
	// exec-time-zero check can account for the row being skipped - only the
	// decrease guard can. A fixture where every counter falls together would
	// pass without it, because the negative exec-time delta is dropped anyway.
	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 120, 7)),
		"a decreased counter rebaselines and reports nothing")

	// The statement must be reportable again immediately, differenced against
	// the new low baseline rather than the stale peak.
	got := emittedExecTime(t, scraper, topQueryRow("q1", 130, 10))
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

	// calls is unchanged while total_exec_time has advanced. That combination
	// is what isolates this guard: the emit path independently drops a row
	// whose exec-time delta is zero, so a fixture holding both constant passes
	// whether or not the calls guard exists. It cannot happen on a real server
	// - time does not accrue without a call - which is exactly why an entry in
	// this state was deallocated and re-inserted rather than executed.
	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 100, 5100)),
		"calls did not advance, so this statement did no work this interval")

	// Real work resumes and is measured from the baseline stored above.
	got := emittedExecTime(t, scraper, topQueryRow("q1", 101, 5110))
	assert.InDelta(t, 10.0, got, 1e-9,
		"the skipped scrape must still have stored its counters as the baseline")
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
	scraper.statements.purge()

	require.Equal(t, 0, scrapeTopQueryRows(t, scraper, topQueryRow("q1", 3, 9)),
		"the scrape that follows a purge re-baselines and reports nothing")

	got := emittedExecTime(t, scraper, topQueryRow("q1", 4, 12))
	assert.InDelta(t, 3.0, got, 1e-9, "the scrape after that reports a true delta again")
}

// The tests below cover the Step 7 storage change: identity, re-entry via
// stats_since, and integer exactness. The Step 2 semantics above must keep
// passing unchanged, since Step 7 rewrote the storage they sit on but not the
// rules themselves.

// identityRow builds a row carrying an explicit identity tuple, so tests can
// vary one component at a time.
func identityRow(id statementIdentity, calls, execTimeMS float64) topQueryStatRow {
	return topQueryStatRow{
		calls:         sql.NullInt64{Int64: int64(calls), Valid: true},
		datname:       sql.NullString{String: "somedb", Valid: true},
		query:         sql.NullString{String: "select 1", Valid: true},
		queryID:       sql.NullInt64{Int64: id.queryID, Valid: true},
		rolname:       sql.NullString{String: "someuser", Valid: true},
		totalExecTime: sql.NullFloat64{Float64: execTimeMS, Valid: true},
		dbid:          sql.NullInt64{Int64: id.dbID, Valid: true},
		userid:        sql.NullInt64{Int64: id.userID, Valid: true},
		toplevel:      sql.NullBool{Bool: id.topLevel, Valid: true},
	}
}

// TestTopQuerySameQueryUnderDifferentIdentitiesDoesNotShareState is the
// identity test the plan asks for.
//
// pg_stat_statements reports the same normalized statement once per (userid,
// dbid, toplevel) combination, each with its own counters. Keyed on anything
// coarser, the rows are differenced against each other rather than against
// their own history: with three siblings arriving in the same scrape, the
// second and third difference against the first, and the emitted numbers
// describe nothing that happened.
func TestTopQuerySameQueryUnderDifferentIdentitiesDoesNotShareState(t *testing.T) {
	scraper := newTestTopQueryScraper(t)

	const queryID = int64(4242)
	siblings := []statementIdentity{
		{queryID: queryID, dbID: 1, userID: 10, topLevel: true},
		{queryID: queryID, dbID: 2, userID: 10, topLevel: true},  // other database
		{queryID: queryID, dbID: 1, userID: 20, topLevel: true},  // other role
		{queryID: queryID, dbID: 1, userID: 10, topLevel: false}, // called from a function
	}

	first := make([]topQueryStatRow, 0, len(siblings))
	second := make([]topQueryStatRow, 0, len(siblings))
	for i, id := range siblings {
		// Deliberately different counters per sibling: if two shared an entry,
		// the delta would be the difference between two siblings rather than
		// between two scrapes of one.
		base := float64(100 * (i + 1))
		first = append(first, identityRow(id, base, base*10))
		second = append(second, identityRow(id, base+7, base*10+1000))
	}

	require.Zero(t, scrapeTopQueryRows(t, scraper, first...),
		"first observation of each identity must emit nothing")
	assert.Equal(t, len(siblings), scraper.statements.len(),
		"each identity must occupy its own cache entry")

	assert.Equal(t, len(siblings), scrapeTopQueryRows(t, scraper, second...),
		"every sibling must be reported against its own previous observation")
}

// TestTopQueryIdentityIgnoresRenamedDatabase checks that the key does not move
// when the joined names change.
//
// The previous key was built from datname and rolname. A database renamed
// between scrapes, or a role renamed, therefore looked like a brand new
// statement and lost its baseline, silently skipping one interval of every
// statement in that database. The OIDs do not move.
func TestTopQueryIdentityIgnoresRenamedDatabase(t *testing.T) {
	scraper := newTestTopQueryScraper(t)
	id := statementIdentity{queryID: 77, dbID: 5, userID: 6, topLevel: true}

	first := identityRow(id, 100, 1000)
	require.Zero(t, scrapeTopQueryRows(t, scraper, first))

	renamed := identityRow(id, 110, 2000)
	renamed.datname = sql.NullString{String: "renamed_db", Valid: true}
	renamed.rolname = sql.NullString{String: "renamed_role", Valid: true}

	assert.Equal(t, 1, scrapeTopQueryRows(t, scraper, renamed),
		"a rename must not lose the statement's baseline")
}

// TestTopQueryStatsSinceDetectsReEntryCountersCannotShow is the 1.11 re-entry
// test.
//
// An entry deallocated and re-created starts counting from zero. By the next
// scrape it can already have climbed past the counters the cache remembers, at
// which point every guard that looks only at the counters is satisfied: nothing
// decreased and calls advanced. The difference is then reported as one
// interval's work when it is really the sum of the old entry's lifetime and the
// new one's. stats_since is the only thing that reveals it, which is why the
// extension added it.
func TestTopQueryStatsSinceDetectsReEntryCountersCannotShow(t *testing.T) {
	scraper := newTestTopQueryScraper(t)
	id := statementIdentity{queryID: 88, dbID: 5, userID: 6, topLevel: true}

	born := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	first := identityRow(id, 1000, 50000)
	first.statsSince = sql.NullTime{Time: born, Valid: true}
	require.Zero(t, scrapeTopQueryRows(t, scraper, first))

	// Re-created, then ran enough to pass the old counters. Every
	// counter-based guard is satisfied here.
	reborn := identityRow(id, 1500, 90000)
	reborn.statsSince = sql.NullTime{Time: born.Add(time.Minute), Valid: true}
	require.Greater(t, reborn.calls.Int64, first.calls.Int64,
		"the fixture must be one the counter guards cannot catch")
	require.Greater(t, reborn.totalExecTime.Float64, first.totalExecTime.Float64,
		"the fixture must be one the counter guards cannot catch")

	assert.Zero(t, scrapeTopQueryRows(t, scraper, reborn),
		"an entry whose stats_since moved forward was re-created, so its "+
			"baseline is invalid and it must be re-baselined rather than reported")

	// Re-baselined, not dropped: the next scrape reports against the new entry.
	next := identityRow(id, 1600, 95000)
	next.statsSince = sql.NullTime{Time: born.Add(time.Minute), Valid: true}
	assert.Equal(t, 1, scrapeTopQueryRows(t, scraper, next),
		"the scrape after a re-entry must report against the new baseline")
}

// TestTopQueryStatsSinceUnchangedStillReports guards the other direction: the
// signal must not suppress an ordinary statement.
func TestTopQueryStatsSinceUnchangedStillReports(t *testing.T) {
	scraper := newTestTopQueryScraper(t)
	id := statementIdentity{queryID: 99, dbID: 5, userID: 6, topLevel: true}
	born := sql.NullTime{Time: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC), Valid: true}

	first := identityRow(id, 100, 1000)
	first.statsSince = born
	require.Zero(t, scrapeTopQueryRows(t, scraper, first))

	second := identityRow(id, 110, 2000)
	second.statsSince = born
	assert.Equal(t, 1, scrapeTopQueryRows(t, scraper, second),
		"an unchanged stats_since must not suppress a real delta")
}

// TestTopQueryBelowStatsSinceFallsBackToCounterGuards checks that a server
// that does not report stats_since is unaffected.
//
// Below extension 1.11 the column is a literal NULL, which reads as the zero
// time. The re-entry check must treat that as "unavailable" and let the Step 2
// counter guards decide, not as a timestamp that never moves or one that moved
// from zero.
func TestTopQueryBelowStatsSinceFallsBackToCounterGuards(t *testing.T) {
	scraper := newTestTopQueryScraper(t)
	id := statementIdentity{queryID: 111, dbID: 5, userID: 6, topLevel: true}

	// No statsSince set anywhere: this is the pre-1.11 shape.
	require.Zero(t, scrapeTopQueryRows(t, scraper, identityRow(id, 100, 1000)))
	assert.Equal(t, 1, scrapeTopQueryRows(t, scraper, identityRow(id, 110, 2000)),
		"a server without stats_since must still report ordinary deltas")
	// And the counter-decrease guard still fires.
	assert.Zero(t, scrapeTopQueryRows(t, scraper, identityRow(id, 5, 100)),
		"the counter-decrease guard must still apply without stats_since")
}

// TestTopQueryEvictionTakesWholeStatement checks that eviction cannot leave a
// statement half-remembered.
//
// With one LRU entry per counter, a capacity boundary could evict some of a
// statement's twelve entries and keep the rest. The survivors then differenced
// against a baseline whose siblings had been re-baselined - a state none of the
// delta rules describe. One entry per statement makes that unrepresentable.
func TestTopQueryEvictionTakesWholeStatement(t *testing.T) {
	cache := newStatementStateCache(2)

	a := statementIdentity{queryID: 1}
	b := statementIdentity{queryID: 2}
	c := statementIdentity{queryID: 3}

	for _, id := range []statementIdentity{a, b, c} {
		cache.observe(id, statementSnapshot{counters: statementCounters{calls: 1}})
	}

	require.Equal(t, 2, cache.len(), "the cache must hold its capacity in statements")
	_, aPresent := cache.lru.Get(a)
	assert.False(t, aPresent, "the oldest statement must have been evicted whole")
	for _, id := range []statementIdentity{b, c} {
		_, present := cache.lru.Get(id)
		assert.True(t, present, "a statement within capacity must be retained whole")
	}
}

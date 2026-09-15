// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwpostgresqlreceiver"

import (
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Delta state for pg_stat_statements, keyed on the identity the server itself
// uses and held as one entry per statement.
//
// The previous layout stored each counter under its own LRU key, built by
// concatenating a per-statement key with the column name. That had three
// problems, all of which this file addresses:
//
//   - Twelve string concatenations per statement per scrape, twice over (once
//     to read, once to write). The Step 6 profile attributes 24.3% of the
//     top-query path's allocation to those keys alone, and 94% of what
//     collectTopQuery allocates on its own.
//   - Partial eviction. A statement's twelve entries are independent as far as
//     the LRU is concerned, so a capacity boundary can evict some of a
//     statement's counters and keep the rest. The survivors then difference
//     against a baseline whose siblings have been re-baselined, which is not a
//     state the delta rules describe at all.
//   - Identity by name. The key was built from datname and rolname, which are
//     the joined display names rather than the OIDs pg_stat_statements keys on.
//     Two servers' worth of history cannot collide inside one cache, but a
//     database dropped and re-created under the same name reuses the entry of
//     the old one, and toplevel was not part of the key at all, so a statement
//     and the same statement called from inside a function shared a baseline.
//
// One entry per statement also makes the capacity units honest: the cache size
// is now a number of statements, not a number of statements times a counter
// count that has already drifted once.

// statementIdentity is the tuple pg_stat_statements uses to identify an entry.
//
// The server keys an entry on (userid, dbid, queryid, toplevel): the same
// normalized query text executed by a different role, against a different
// database, or at top level versus from inside a function, is a separate row
// with independent counters. Differencing one against another reports work
// that never happened.
//
// This is a comparable struct used directly as a map/LRU key, so no key string
// is ever built. The OIDs are the server's own identifiers rather than the
// names they join to, which is what makes it correct across a rename or a
// drop-and-recreate: a new database with a recycled name gets a new OID and
// therefore a new entry, and the stale one ages out of the LRU instead of
// being differenced against.
type statementIdentity struct {
	queryID  int64
	dbID     int64
	userID   int64
	topLevel bool
}

// statementSnapshot is one statement's cumulative counters as of the last
// scrape that observed it.
//
// The counters are stored in the representation the server reports them in.
// pg_stat_statements counts calls, rows and block counts as bigint and the
// timings as double precision milliseconds; keeping the integers as int64
// means a delta of large counters is exact rather than rounded through a
// float64 mantissa, which matters at the top of the range a bigint counter can
// reach. The timings stay float64 because that is what they are.
type statementSnapshot struct {
	// statsSince is the entry's stats_since as of this observation, zero when
	// the extension does not report one (below 1.11). A stats_since that has
	// moved forward means the entry was deallocated and re-created, so the
	// counters below describe a series that no longer exists.
	statsSince time.Time

	counters statementCounters
}

// statementCounters holds the twelve cumulative counters the receiver
// differences, in the types the server reports.
//
// Named fields rather than an array indexed by column name: the set is fixed
// and known at compile time, so there is nothing to look up and no way for a
// slot index and a column name to disagree.
type statementCounters struct {
	calls             int64
	rows              int64
	sharedBlksDirtied int64
	sharedBlksHit     int64
	sharedBlksRead    int64
	sharedBlksWritten int64
	tempBlksRead      int64
	tempBlksWritten   int64

	// Milliseconds, as pg_stat_statements reports them. The conversion to the
	// seconds the emitted attribute uses happens once, on the delta, rather
	// than on both the current and the cached value.
	totalExecTimeMS float64
	totalPlanTimeMS float64
	blkReadTimeMS   float64
	blkWriteTimeMS  float64
}

// anyDecreased reports whether any counter in cur is below the same counter in
// prev.
//
// A cumulative counter that went backwards means the entry was reset or
// deallocated and re-created, so the remembered value describes a series that
// no longer exists and the row must be re-baselined rather than differenced.
func (prev statementCounters) anyDecreased(cur statementCounters) bool {
	return cur.calls < prev.calls ||
		cur.rows < prev.rows ||
		cur.sharedBlksDirtied < prev.sharedBlksDirtied ||
		cur.sharedBlksHit < prev.sharedBlksHit ||
		cur.sharedBlksRead < prev.sharedBlksRead ||
		cur.sharedBlksWritten < prev.sharedBlksWritten ||
		cur.tempBlksRead < prev.tempBlksRead ||
		cur.tempBlksWritten < prev.tempBlksWritten ||
		cur.totalExecTimeMS < prev.totalExecTimeMS ||
		cur.totalPlanTimeMS < prev.totalPlanTimeMS ||
		cur.blkReadTimeMS < prev.blkReadTimeMS ||
		cur.blkWriteTimeMS < prev.blkWriteTimeMS
}

// sub returns cur - prev, with the timings converted from the milliseconds
// pg_stat_statements reports to the seconds the emitted attributes carry.
//
// The integer counters are subtracted as integers and converted afterwards, so
// the difference of two large counters is exact. Converting each operand to
// float64 first, as the previous code did, loses the low bits of any counter
// above 2^53 before the subtraction ever happens.
func (prev statementCounters) sub(cur statementCounters) statementDeltas {
	return statementDeltas{
		calls:             cur.calls - prev.calls,
		rows:              cur.rows - prev.rows,
		sharedBlksDirtied: cur.sharedBlksDirtied - prev.sharedBlksDirtied,
		sharedBlksHit:     cur.sharedBlksHit - prev.sharedBlksHit,
		sharedBlksRead:    cur.sharedBlksRead - prev.sharedBlksRead,
		sharedBlksWritten: cur.sharedBlksWritten - prev.sharedBlksWritten,
		tempBlksRead:      cur.tempBlksRead - prev.tempBlksRead,
		tempBlksWritten:   cur.tempBlksWritten - prev.tempBlksWritten,
		totalExecTime:     msToSeconds(cur.totalExecTimeMS - prev.totalExecTimeMS),
		totalPlanTime:     msToSeconds(cur.totalPlanTimeMS - prev.totalPlanTimeMS),
		blkReadTime:       msToSeconds(cur.blkReadTimeMS - prev.blkReadTimeMS),
		blkWriteTime:      msToSeconds(cur.blkWriteTimeMS - prev.blkWriteTimeMS),
	}
}

func msToSeconds(ms float64) float64 {
	return ms / 1000.0
}

// statementDeltas is one interval's work for one statement, in the units the
// emitted attributes use: counts as integers, timings as seconds.
type statementDeltas struct {
	totalExecTime float64
	totalPlanTime float64
	blkReadTime   float64
	blkWriteTime  float64

	calls             int64
	rows              int64
	sharedBlksDirtied int64
	sharedBlksHit     int64
	sharedBlksRead    int64
	sharedBlksWritten int64
	tempBlksRead      int64
	tempBlksWritten   int64
}

// statementStateCache remembers one snapshot per statement.
//
// It wraps an LRU keyed on the identity struct so the whole of a statement's
// state is one cache entry: it is either present or it is not, and eviction
// takes all twelve counters together. The wrapper exists to keep the delta
// rules in one place rather than spread across the scrape loop.
type statementStateCache struct {
	lru *lru.Cache[statementIdentity, statementSnapshot]
}

func newStatementStateCache(size int) *statementStateCache {
	if size <= 0 {
		size = 1
	}
	// lru.New only errors on a non-positive size, which is excluded above.
	c, _ := lru.New[statementIdentity, statementSnapshot](size)
	return &statementStateCache{lru: c}
}

func (c *statementStateCache) purge() {
	c.lru.Purge()
}

func (c *statementStateCache) len() int {
	return c.lru.Len()
}

// observe records the current counters for id and returns the interval delta,
// or ok=false when this observation cannot produce one.
//
// A row is reportable only when there is a previous observation of the same
// series to difference it against. Four cases are not, and each stores the new
// values as the baseline and reports nothing, so the next scrape produces a
// true interval delta:
//
//   - First observation. The entry has not been seen, because the collector
//     just started, the cache was purged after a reset or an instance change,
//     or the entry was evicted and re-read. Emitting the cumulative value here
//     would report the statement's entire lifetime as one interval's work.
//   - Re-entry, seen directly. Where the extension reports stats_since
//     (1.11+), an entry whose stats_since moved forward was deallocated and
//     re-created. Its counters restarted from zero and then climbed, so they
//     can be higher than the remembered ones while describing different work
//     entirely; nothing in the counters themselves reveals that.
//   - A counter that went backwards. Below 1.11 this is the only visible sign
//     of the same event, and it remains a valid signal above it.
//   - calls that did not advance. The other counters cannot have changed, and
//     an entry deallocated and re-inserted with identical counts must not be
//     reported as though it had run.
func (c *statementStateCache) observe(id statementIdentity, cur statementSnapshot) (statementDeltas, bool) {
	prev, exist := c.lru.Get(id)
	if !exist || reEntered(prev, cur) || prev.counters.anyDecreased(cur.counters) {
		c.lru.Add(id, cur)
		return statementDeltas{}, false
	}

	deltas := prev.counters.sub(cur.counters)
	c.lru.Add(id, cur)
	if deltas.calls <= 0 {
		return statementDeltas{}, false
	}
	return deltas, true
}

// reEntered reports whether the entry was deallocated and re-created between
// the two observations, as told by stats_since.
//
// A zero stats_since on either side means the extension does not report one
// (below 1.11), in which case this signal is unavailable and the caller falls
// back to the counter-decrease and calls guards. The comparison is "moved
// forward" rather than "differs" because a clock that went backwards is a
// property of the host, not evidence about the entry.
func reEntered(prev, cur statementSnapshot) bool {
	if prev.statsSince.IsZero() || cur.statsSince.IsZero() {
		return false
	}
	return cur.statsSince.After(prev.statsSince)
}

// queryPlanKey identifies a cached EXPLAIN result.
//
// A queryid names a normalized statement, not a plan. The same statement text
// against two databases sees different tables, statistics and indexes, so it
// plans differently; the cache must therefore distinguish them. It previously
// did not — the key was the queryid with "-plan" appended — so whichever
// database happened to be EXPLAINed first supplied the plan attached to every
// other database's copy of that statement, for as long as the entry lived.
//
// The database is the name rather than the OID because that is what the
// EXPLAIN connection is opened against; a row whose database could not be
// resolved is never EXPLAINed at all, so it never reaches this key.
type queryPlanKey struct {
	database string
	queryID  int64
}

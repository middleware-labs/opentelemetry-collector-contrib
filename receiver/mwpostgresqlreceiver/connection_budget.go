// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"sync"
)

// defaultMaxTotalConnections caps how many server connections this receiver may
// hold open at once, across every database and both the metrics and logs
// signals.
//
// Why a single global number rather than a per-database or per-pool one:
// PostgreSQL enforces a role's CONNECTION LIMIT (pg_authid.rolconnlimit)
// cluster-wide, counting every backend authenticated as that role regardless of
// which database it connected to. A budget expressed per pool therefore cannot
// describe the limit we are actually subject to. The reference deployment — a
// monitoring role with CONNECTION LIMIT 50 against a server hosting 52
// databases — cannot hold even one connection per database, and the failure is
// not graceful: the 51st connection is refused with
//
//	FATAL: too many connections for role "..."   (SQLSTATE 53300)
//
// and the scrape cannot complete at all.
//
// 10 is deliberately far below any plausible role limit. Scrapes visit
// databases one at a time, so the receiver's useful concurrency is a handful of
// connections; the rest of a large budget would be idle backends held open for
// nothing. Sizing conservatively also leaves room for the documented race
// described below.
//
// Note that PostgreSQL's own enforcement is approximate — the CREATE DATABASE
// documentation says so explicitly: "if two new sessions start at about the
// same time when just one connection slot remains, it is possible that both
// will fail." The check reads the live ProcArray with no reservation, so
// backends starting concurrently can each observe a count under the limit. A
// client must therefore keep headroom rather than aim at the limit exactly.
const defaultMaxTotalConnections = 10

// connectionBudget bounds concurrent connections across every pool and both
// signals.
//
// It sits beneath the client factories rather than inside one because the
// metrics receiver and the logs receiver each construct their own factory, so a
// bound held by a factory can only ever see half the connections the receiver
// opens. The budget is passed to both.
//
// The unit accounted for is a *pool*, not an individual backend: a pool for a
// database is admitted once and may open more than one connection under its own
// MaxOpen setting. Callers keep those settings small — non-default databases are
// held to a single idle connection — so pool count tracks backend count closely
// enough to be the useful lever, and it is the granularity at which the receiver
// can actually make a decision (open this database's pool, or close another
// first).
type connectionBudget struct {
	mu    sync.Mutex
	max   int
	inUse int
}

func newConnectionBudget(maxTotal int) *connectionBudget {
	if maxTotal <= 0 {
		maxTotal = defaultMaxTotalConnections
	}
	return &connectionBudget{max: maxTotal}
}

// tryAcquire claims one unit of the budget, reporting whether it was available.
//
// It does not block. A scrape that cannot get budget should degrade — skip that
// database this cycle and collect it next time — rather than queue: waiting
// would hold up the whole scrape for data that will still be there in ten
// seconds, and a queue of waiters is how a scrape overruns its interval and
// begins to overlap with the next one.
func (b *connectionBudget) tryAcquire() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.inUse >= b.max {
		return false
	}
	b.inUse++
	return true
}

// release returns one unit to the budget. It is safe to call more times than
// tryAcquire succeeded; the count is clamped at zero so that a double release
// cannot manufacture budget that does not exist.
func (b *connectionBudget) release() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.inUse > 0 {
		b.inUse--
	}
}

// available reports the remaining budget. For tests and self-reporting.
func (b *connectionBudget) available() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.max - b.inUse
}

// used reports how much of the budget is currently claimed.
func (b *connectionBudget) used() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inUse
}

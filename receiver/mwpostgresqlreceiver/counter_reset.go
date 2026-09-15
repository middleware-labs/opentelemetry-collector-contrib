// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"context"
	"time"
)

// statsResetQuery reads the moment pg_stat_statements last had its statistics
// discarded.
//
// The view is schema-qualified for the same reason the statement queries are:
// an extension installed outside search_path is only reachable by its schema,
// and an unqualified name there fails with 42P01 -- indistinguishable from the
// view genuinely not existing, which would disable detection permanently on a
// server that can actually answer.
func statsResetQuery(caps pgStatStatementsCapabilities) string {
	return `SELECT stats_reset FROM ` + caps.qualify("pg_stat_statements_info")
}

// resetDetector decides whether cached counter values are still comparable with
// what the server reports now.
//
// The counters this receiver differences are cumulative, so a delta is only
// meaningful while the series it belongs to is continuous. Two things break
// that continuity:
//
//   - someone calls pg_stat_statements_reset(), discarding every counter, and
//   - an entry is evicted and later recreated, which restarts its counters
//     from zero without any server-wide event.
//
// The naive guard is to notice a counter going backwards. That catches the
// common case but not all of it: between two scrapes a reset entry can climb
// back past its former value, and the receiver then reports the difference as
// if it were a single interval's work — a spike with no cause, in the one
// direction an operator is most likely to act on.
//
// pg_stat_statements_info.stats_reset is authoritative for the server-wide
// case: it is a single timestamp that moves whenever the statistics are
// discarded, so a change is unambiguous regardless of what individual counters
// did in between.
type resetDetector struct {
	// lastReset is the stats_reset value observed on the previous scrape.
	lastReset time.Time
	// seen records whether lastReset holds an observation. A zero time.Time is
	// not usable as "no observation": the server can legitimately report a zero
	// timestamp, and treating that as unseen would re-baseline every scrape.
	seen bool
	// supported is false once the server has told us the view does not exist,
	// so we stop asking every scrape on a server that will never answer.
	supported bool
	probed    bool
}

func newResetDetector() *resetDetector {
	return &resetDetector{supported: true}
}

// check reports whether the statistics were reset since the previous call.
//
// The first call establishes a baseline and reports false: there is no previous
// interval to invalidate, and reporting true would discard a cache that is
// already empty.
//
// A server below extension 1.9 has no pg_stat_statements_info and reports false
// without asking. That is the honest answer — we cannot see resets there — and the per-counter
// went-backwards check remains as the weaker fallback. It is not treated as an
// error, because running against PostgreSQL 13 is a supported configuration,
// not a fault.
func (d *resetDetector) check(ctx context.Context, db *IgnoredDB, caps pgStatStatementsCapabilities) bool {
	if !d.supported {
		return false
	}

	// pg_stat_statements_info arrived with extension 1.9. Below that the view
	// is absent by construction, so asking is a round trip whose only possible
	// outcome is a 42P01 we already know about. The gate is the version rather
	// than the error, so a later ALTER EXTENSION ... UPDATE is picked up on the
	// next connection instead of being remembered as unsupported.
	if !caps.hasTopLevel() {
		return false
	}

	var resetAt *time.Time
	err := db.QueryRowContext(ctx, statsResetQuery(caps)).Scan(&resetAt)
	if err != nil {
		// Probe once. A missing view (SQLSTATE 42P01, undefined_table) or a
		// missing column means this server will never answer, so stop asking.
		// Anything else is transient — a timeout, a dropped connection — and
		// must not permanently disable detection.
		if !d.probed {
			switch sqlStateOf(err) {
			case "42P01", "42703":
				d.supported = false
			}
		}
		d.probed = true
		return false
	}
	d.probed = true

	// A NULL stats_reset means the statistics have never been reset on this
	// server. That is a stable state, not a change, so it must be recorded as
	// an observation like any other rather than left to look like "unseen".
	var observed time.Time
	if resetAt != nil {
		observed = *resetAt
	}

	if !d.seen {
		d.lastReset = observed
		d.seen = true
		return false
	}

	if observed.Equal(d.lastReset) {
		return false
	}

	d.lastReset = observed
	return true
}

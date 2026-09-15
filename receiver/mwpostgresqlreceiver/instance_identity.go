// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"context"
	"time"
)

// instanceIdentityQuery reads what identifies the *running server process*.
//
// pg_postmaster_start_time() is the load-bearing column, and the choice of it
// over the more obvious candidate is deliberate.
//
// The obvious candidate is pg_control_system().system_identifier, and it does
// not work for this. A physical standby is *required* to carry the same
// system_identifier as its primary — PostgreSQL refuses to start replication
// otherwise, with "FATAL: database system identifier differs between the
// primary and standby". Promotion assigns a new timeline ID but leaves the
// identifier untouched. So a client that fails over from a primary to its own
// standby observes exactly the same value before and after: the check would
// look correct and catch nothing. The same reasoning applies to Aurora, Azure
// Flexible Server zone-redundant HA and Cloud SQL HA, all of which fail over by
// promoting an existing standby rather than bootstrapping a new cluster.
//
// pg_postmaster_start_time() works because it identifies the *process*. It
// changes on any restart, and a promoted standby is a different postmaster that
// reports its own start time — not the one cached from the old primary. Note
// that promotion itself does not change the value (promotion does not restart
// the postmaster); the signal comes from the value differing between two
// different servers, not from it being reset.
//
// It also has no privilege requirement, unlike pg_control_system(), whose
// availability to a non-superuser on managed platforms could not be established
// from any vendor's documentation.
func instanceIdentityQuery() string {
	return `SELECT pg_postmaster_start_time()`
}

// instanceIdentity is what the receiver remembers about the server it was
// talking to last scrape.
type instanceIdentity struct {
	// startTime is the postmaster's start time. Zero means not yet observed.
	startTime time.Time
}

// instanceTracker notices when the receiver is talking to a different running
// server than it was on the previous scrape.
//
// Deltas of cumulative counters are only meaningful within one server's
// lifetime. Across a restart the counters have been zeroed; across a failover
// they belong to a different machine that has been counting independently. In
// both cases the cached previous values describe a series that no longer
// applies.
//
// The failover case is why identity is checked at all rather than relying on
// noticing a counter go backwards. A promoted standby that has been running for
// a while can hold counters *higher* than the primary's cached values, so the
// difference comes out positive and entirely plausible. Nothing looks wrong;
// the numbers are simply about a different machine. A went-backwards check
// cannot see it, and neither can a stats_reset check, because the new server's
// stats_reset reflects its own history.
type instanceTracker struct {
	current instanceIdentity
	seen    bool
	// supported goes false when the server will never answer, so we stop
	// asking every scrape.
	supported bool
	probed    bool
}

func newInstanceTracker() *instanceTracker {
	return &instanceTracker{supported: true}
}

// check reports whether the server changed since the previous call.
//
// The first call establishes a baseline and reports false: there is no previous
// interval to invalidate. A server that cannot answer reports false forever,
// leaving the per-counter went-backwards check as the weaker fallback rather
// than failing the scrape.
func (t *instanceTracker) check(ctx context.Context, db *IgnoredDB) bool {
	if !t.supported {
		return false
	}

	var startTime time.Time
	if err := db.QueryRowContext(ctx, instanceIdentityQuery()).Scan(&startTime); err != nil {
		// Only a definitively absent function disables further probing.
		// Anything else — a timeout, a dropped connection — is transient and
		// must not silently switch off detection for the process lifetime.
		if !t.probed {
			switch sqlStateOf(err) {
			case "42883", "42501": // undefined_function, insufficient_privilege
				t.supported = false
			}
		}
		t.probed = true
		return false
	}
	t.probed = true

	observed := instanceIdentity{startTime: startTime}

	if !t.seen {
		t.current = observed
		t.seen = true
		return false
	}

	if observed.startTime.Equal(t.current.startTime) {
		return false
	}

	t.current = observed
	return true
}

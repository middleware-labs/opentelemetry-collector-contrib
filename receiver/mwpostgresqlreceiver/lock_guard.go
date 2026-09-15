// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// sqlStateOf extracts the five-character SQLSTATE from a PostgreSQL error,
// returning "" when err did not come from the server (a dial failure, a
// context cancellation, a driver-level error).
//
// errors.As is used rather than a type assertion because the error may be
// wrapped: callers in this package routinely annotate errors with fmt.Errorf.
func sqlStateOf(err error) string {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code)
	}
	return ""
}

// lockedRelationsQuery returns the OIDs of relations in this database that are
// currently held under ACCESS EXCLUSIVE lock, or waiting to be.
//
// Why this matters: the catalog introspection functions this receiver calls per
// table — pg_get_indexdef, pg_get_viewdef, pg_get_constraintdef, pg_get_expr —
// take an ACCESS SHARE lock on the relation they describe. ACCESS SHARE
// conflicts with ACCESS EXCLUSIVE, which is what ALTER TABLE, DROP TABLE,
// TRUNCATE, REINDEX and VACUUM FULL hold.
//
// PostgreSQL's lock queue is ordered and does not allow queue-jumping. So if a
// customer runs ALTER TABLE while we introspect the same relation, we do not
// merely wait for them: we join the queue behind their DDL, and every
// subsequent query from the customer's own application queues behind *us*. A
// monitoring agent then converts a brief DDL statement into an application
// outage lasting as long as our statement_timeout.
//
// The fix is to not enter the queue at all. We ask which relations are locked
// and skip them for this cycle; the xmin change detector will see them again on
// the next pass, once the DDL has committed. Reading pg_locks itself takes no
// lock on the relations it reports.
//
// granted = false is included deliberately: a relation with a *pending*
// ACCESS EXCLUSIVE request is one where a DDL statement is already waiting.
// Introspecting it would place us behind that waiter, which is the exact
// pile-up this guard exists to prevent.
//
// Scope notes: pg_locks is cluster-wide, so the database predicate is
// essential — relation OIDs are only unique within a database, and without it
// we would skip an unrelated table that happens to share an OID with a locked
// relation in another database. locktype = 'relation' excludes advisory,
// transaction and tuple locks, which cannot block catalog introspection.
func lockedRelationsQuery() string {
	return `
		SELECT l.relation
		FROM pg_locks l
		WHERE l.locktype = 'relation'
		  AND l.mode = 'AccessExclusiveLock'
		  AND l.relation IS NOT NULL
		  AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
	`
}

// lockedRelations returns the set of relation OIDs that must not be
// introspected this cycle.
//
// A failure to read pg_locks returns an error rather than an empty set. An
// empty set means "nothing is locked, introspect everything", which is exactly
// the unguarded behaviour this exists to prevent; callers must be able to tell
// the two apart and decide explicitly.
func lockedRelations(ctx context.Context, db *IgnoredDB) (map[uint32]struct{}, error) {
	rows, err := db.QueryContext(ctx, lockedRelationsQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to query pg_locks: %w", err)
	}
	defer rows.Close()

	locked := make(map[uint32]struct{})
	for rows.Next() {
		var oid uint32
		if err := rows.Scan(&oid); err != nil {
			return nil, fmt.Errorf("failed to scan locked relation: %w", err)
		}
		locked[oid] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read locked relations: %w", err)
	}
	return locked, nil
}

// isLockTimeout reports whether err is PostgreSQL's lock_timeout expiry
// (SQLSTATE 55P03, lock_not_available).
//
// The pg_locks pre-check is a race, not a mutex: a relation can acquire an
// ACCESS EXCLUSIVE lock in the window between our check and our query. The DSN
// lock_timeout bounds how long we sit in the queue when that happens, and this
// classifies the resulting error so the caller can treat it as "skip this
// table, try next cycle" rather than as a collection failure.
func isLockTimeout(err error) bool {
	if err == nil {
		return false
	}
	if code := sqlStateOf(err); code != "" {
		return code == "55P03"
	}
	// Fall back to the message when the driver does not surface a SQLSTATE,
	// e.g. an error wrapped by a proxy.
	return strings.Contains(err.Error(), "canceling statement due to lock timeout")
}

// isStatementTimeout reports whether err is PostgreSQL's statement_timeout
// expiry (SQLSTATE 57014, query_canceled).
func isStatementTimeout(err error) bool {
	if err == nil {
		return false
	}
	if code := sqlStateOf(err); code != "" {
		return code == "57014"
	}
	return strings.Contains(err.Error(), "canceling statement due to statement timeout")
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"database/sql"
	"strings"
)

// Typed decoding for the two query-log paths.
//
// Both paths previously went through sqlquery.QueryRows, which scans every
// column into an `any`, renders it with fmt.Sprintf("%v") and stores it in a
// map[string]string, after which the receiver re-parsed the numbers with
// strconv and copied them into a second map[string]any keyed by a freshly
// concatenated attribute name. That is three representations of every value
// and two maps per row before anything is emitted.
//
// The alloc_space profile in BENCHMARKS.md attributes 26.4% of the top-query
// path's allocations to toStringMap and a further 40 MB of 151.7 MB to a
// single line building `dbAttributePrefix+col` keys. Scanning into a struct
// removes both: the driver decodes integers and floats directly (lib/pq uses
// strconv internally, so there is no format-then-reparse round trip), and the
// field names are known at compile time so no key is ever constructed.
//
// NULL is explicit here. The generic scanner omitted a NULL column from the
// map entirely, which is why the attrs.go helpers have to tolerate missing
// keys; sql.Null* records the distinction in the row itself, so callers see
// "absent" rather than inferring it from a lookup that failed.

// topQueryStatRow is one pg_stat_statements entry as projected by
// templates/topQueryTemplate.tmpl.
//
// Field order matches the SELECT list, because scanDest returns pointers in
// that order and the two must not drift.
//
// Nullable columns: pg_stat_statements rows outlive the objects they reference.
// A dropped database leaves entries whose dbid no longer joins to pg_database,
// and a dropped role leaves rolname NULL, so datname and rolname are genuinely
// optional. The counters are NULL only when the caller lacks permission to see
// them, which the null-heavy benchmark shape exercises.
type topQueryStatRow struct {
	calls             sql.NullInt64
	datname           sql.NullString
	sharedBlksDirtied sql.NullInt64
	sharedBlksHit     sql.NullInt64
	sharedBlksRead    sql.NullInt64
	sharedBlksWritten sql.NullInt64
	tempBlksRead      sql.NullInt64
	tempBlksWritten   sql.NullInt64
	query             sql.NullString
	queryID           sql.NullInt64
	rolname           sql.NullString
	rows              sql.NullInt64
	totalExecTime     sql.NullFloat64
	totalPlanTime     sql.NullFloat64
	blkReadTime       sql.NullFloat64
	blkWriteTime      sql.NullFloat64

	// Identity columns. pg_stat_statements keys an entry on
	// (userid, dbid, queryid, toplevel); the delta cache keys on the same
	// tuple, taken from these fields rather than from the names they join to.
	dbid     sql.NullInt64
	userid   sql.NullInt64
	toplevel sql.NullBool

	// statsSince is the moment the entry's statistics began accumulating,
	// reported per entry from extension 1.11. It is NULL on older extensions,
	// where the template selects a literal NULL to keep the projection the
	// same shape across versions, and the delta cache falls back to the
	// counter-decrease guard.
	statsSince sql.NullTime
}

// scanDest returns the scan destinations for one row, in SELECT order.
//
// The pointers address the receiver's own fields, so a caller that scans into
// the same topQueryStatRow on every iteration reuses one set of destinations for
// the whole result set. Anything that must outlive the row has to be copied
// out before the next scan; the only such values are the strings, and Go
// strings are immutable so assigning one out of the struct already copies the
// header rather than aliasing a buffer the driver will overwrite.
func (r *topQueryStatRow) scanDest() []any {
	return []any{
		&r.calls,
		&r.datname,
		&r.sharedBlksDirtied,
		&r.sharedBlksHit,
		&r.sharedBlksRead,
		&r.sharedBlksWritten,
		&r.tempBlksRead,
		&r.tempBlksWritten,
		&r.query,
		&r.queryID,
		&r.rolname,
		&r.rows,
		&r.totalExecTime,
		&r.totalPlanTime,
		&r.blkReadTime,
		&r.blkWriteTime,
		&r.dbid,
		&r.userid,
		&r.toplevel,
		&r.statsSince,
	}
}

// identity returns the (userid, dbid, queryid, toplevel) tuple the server keys
// this entry on, for use as the delta cache key.
//
// A NULL component contributes its zero value rather than being dropped. NULL
// is not something pg_stat_statements produces for these columns - they are the
// entry's own identity, not a join result - but a row that somehow lacks one
// still gets a stable key distinct from rows that have it, which is the same
// contract the string key it replaces offered.
func (r *topQueryStatRow) identity() statementIdentity {
	return statementIdentity{
		queryID:  r.queryID.Int64,
		dbID:     r.dbid.Int64,
		userID:   r.userid.Int64,
		topLevel: r.toplevel.Bool,
	}
}

// snapshot returns the row's cumulative counters and stats_since, in the units
// and types the server reports them in.
//
// A NULL counter reads as zero, as it did when the generic scanner left the key
// out of the row map entirely. A NULL stats_since reads as the zero time, which
// is how the cache recognizes that the signal is unavailable on this server.
func (r *topQueryStatRow) snapshot() statementSnapshot {
	return statementSnapshot{
		statsSince: r.statsSince.Time,
		counters: statementCounters{
			calls:             r.calls.Int64,
			rows:              r.rows.Int64,
			sharedBlksDirtied: r.sharedBlksDirtied.Int64,
			sharedBlksHit:     r.sharedBlksHit.Int64,
			sharedBlksRead:    r.sharedBlksRead.Int64,
			sharedBlksWritten: r.sharedBlksWritten.Int64,
			tempBlksRead:      r.tempBlksRead.Int64,
			tempBlksWritten:   r.tempBlksWritten.Int64,
			totalExecTimeMS:   r.totalExecTime.Float64,
			totalPlanTimeMS:   r.totalPlanTime.Float64,
			blkReadTimeMS:     r.blkReadTime.Float64,
			blkWriteTimeMS:    r.blkWriteTime.Float64,
		},
	}
}

// hasCommentMarker reports whether rawSQL can possibly contain a SQL comment.
//
// sqlCommentPattern is `/\*.*?\*/|--[^\n]*`: every alternative begins with a
// literal two-byte marker, so text containing neither "/*" nor "--" cannot
// produce a match. Testing for the markers first is therefore exact, not an
// approximation — it never changes what extractSQLComments returns.
//
// It is worth doing because the regex costs the same to find nothing as to
// find something, and scales with statement length: the Step 1 profile put
// extractSQLComments at 32% of CPU in the default shape and 74% in the
// long-SQL shape, all inside regexp.FindAllString, running over every
// candidate row on every scrape whether or not a comment was there. Comments
// are rare in ordinary SQL and the strings.Contains scan is a tuned byte
// search, so the common case becomes two cheap passes instead of a backtracking
// regex over the whole statement.
//
// Deferring the scan until after candidate selection does not help at the
// defaults, where top_n_query and max_rows_per_query are both 1000 and no row
// is ever discarded. This does, in every configuration.
func hasCommentMarker(rawSQL string) bool {
	return strings.Contains(rawSQL, "/*") || strings.Contains(rawSQL, "--")
}

// extractSQLCommentsFast is extractSQLComments with the marker pre-check.
func extractSQLCommentsFast(rawSQL string) []string {
	if !hasCommentMarker(rawSQL) {
		return nil
	}
	return extractSQLComments(rawSQL)
}

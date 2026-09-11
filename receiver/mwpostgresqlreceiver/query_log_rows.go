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
	// (userid, dbid, queryid, toplevel); the receiver currently reconstructs
	// that from the joined names. Projecting the OIDs and toplevel here lets
	// Step 7 key the delta cache on the server's own identity without a second
	// template change, as the plan requires.
	dbid     sql.NullInt64
	userid   sql.NullInt64
	toplevel sql.NullBool
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
	}
}

// counter returns one cumulative counter by its column name, as float64.
//
// The delta arithmetic in collectTopQuery is uniform across all twelve
// counters and keyed by column name, so this keeps that loop intact while the
// values behind it become typed fields. A NULL counter reads as 0, matching
// the previous behavior where a missing map key yielded the zero value.
func (r *topQueryStatRow) counter(column string) float64 {
	switch column {
	case callsColumnName:
		return float64(r.calls.Int64)
	case rowsColumnName:
		return float64(r.rows.Int64)
	case sharedBlksDirtiedColumnName:
		return float64(r.sharedBlksDirtied.Int64)
	case sharedBlksHitColumnName:
		return float64(r.sharedBlksHit.Int64)
	case sharedBlksReadColumnName:
		return float64(r.sharedBlksRead.Int64)
	case sharedBlksWrittenColumnName:
		return float64(r.sharedBlksWritten.Int64)
	case tempBlksReadColumnName:
		return float64(r.tempBlksRead.Int64)
	case tempBlksWrittenColumnName:
		return float64(r.tempBlksWritten.Int64)
	case totalExecTimeColumnName:
		// Milliseconds in the view, seconds in the emitted attribute. The
		// conversion used to happen during decoding, before any row was
		// selected; doing it here keeps the delta arithmetic in the same units
		// the previous code used.
		return r.totalExecTime.Float64 / 1000.0
	case totalPlanTimeColumnName:
		return r.totalPlanTime.Float64 / 1000.0
	case blkReadTimeAttributeName:
		return r.blkReadTime.Float64 / 1000.0
	case blkWriteTimeAttributeName:
		return r.blkWriteTime.Float64 / 1000.0
	default:
		return 0
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

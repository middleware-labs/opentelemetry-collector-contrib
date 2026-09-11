// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	semconv "go.opentelemetry.io/otel/semconv/v1.38.0"
)

// Attribute maps built from SQL result rows are not guaranteed to contain every
// key. The sqlquery row scanner omits a column entirely when its value is NULL
// (see internal/sqlquery/row_scanner.go), so a NULL column is an absent map key
// rather than a zero value. Reading such a key yields an untyped nil, and an
// unchecked type assertion on it panics — which, on a scraper goroutine with no
// recover, terminates the whole collector process.
//
// pg_stat_statements makes this routine rather than exotic: its rows outlive the
// databases they came from, so a dropped database leaves entries whose dbid no
// longer joins to pg_database and whose datname comes back NULL.
//
// These helpers read an attribute defensively, returning the zero value when the
// key is missing or holds an unexpected type.

// attrString returns the string at key, or "" if absent or not a string.
func attrString(attrs map[string]any, key string) string {
	v, _ := attrs[key].(string)
	return v
}

// attrInt64 returns the int64 at key, or 0 if absent or not an int64.
func attrInt64(attrs map[string]any, key string) int64 {
	v, _ := attrs[key].(int64)
	return v
}

// attrFloat64 returns the float64 at key, or 0 if absent or not a float64.
// Numeric attributes are stored as either int64 or float64 depending on the
// converter applied upstream, so both are accepted.
func attrFloat64(attrs map[string]any, key string) float64 {
	switch v := attrs[key].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	default:
		return 0
	}
}

// topQueryDeltaKey builds the cache key identifying one pg_stat_statements
// entry across scrapes.
//
// pg_stat_statements identifies an entry by (userid, dbid, queryid, toplevel):
// the same normalized query text executed by different roles, or against
// different databases, produces separate rows with independent counters. A
// delta cache keyed on queryid alone merges those rows, and each scrape then
// differences a row against whichever sibling happened to be cached last,
// emitting deltas that describe nothing real.
//
// Role and database are used in place of the raw userid and dbid because those
// are what the query already joins and carries in the row; they are one-to-one
// with the OIDs within a single server, which is the scope a cache entry lives
// in. A missing component contributes an empty string rather than being
// dropped, so rows that genuinely lack one still get a stable, distinct key.
//
// The separator must not occur in an identifier that could otherwise shift a
// boundary. PostgreSQL identifiers can contain almost anything when quoted, so
// a byte that cannot appear in a UTF-8 string at all is used instead of a
// printable character.
func topQueryDeltaKey(row map[string]any, queryID string) string {
	const sep = "\x00"
	return attrString(row, string(semconv.DBNamespaceKey)) + sep +
		attrString(row, dbAttributePrefix+rolnameColumnName) + sep +
		queryID + sep
}

// topQuerySelection is one candidate that survived delta computation, carried
// through the priority queue.
//
// It holds an index into the row slice rather than the row itself: the rows are
// already retained for the duration of the scrape, and the queue reorders its
// elements, so copying a 19-field struct per sift would be pure waste. The
// deltas travel with it because they are what the emitted attributes report and
// what the queue orders on, and recomputing them after selection would mean
// reading the cache a second time after it has already been updated.
type topQuerySelection struct {
	index   int
	queryID string
	deltas  topQueryDeltas
}

// topQueryStatDeltaKey builds the cache key identifying one pg_stat_statements
// entry across scrapes, from a typed row.
//
// Same contract as the map-based key it replaces: database, role and queryid
// separated by a byte that cannot appear in a UTF-8 identifier, with an absent
// component contributing an empty string rather than being dropped. The typed
// row makes "absent" explicit, so a NULL datname or rolname is distinguishable
// from an empty one here even though both produce the same key component - the
// previous code could not tell them apart at all.
//
// Step 7 replaces this with the server's own (dbid, userid, queryid, toplevel)
// identity, which the row now carries.
func topQueryStatDeltaKey(row *topQueryStatRow, queryID string) string {
	const sep = "\x00"
	return row.datname.String + sep +
		row.rolname.String + sep +
		queryID + sep
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

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

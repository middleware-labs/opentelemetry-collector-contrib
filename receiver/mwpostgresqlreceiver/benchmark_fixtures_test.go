// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"database/sql/driver"
	"strconv"
	"strings"
)

// Fixtures shared by the benchmarks. They exist to make the measured shapes
// explicit and repeatable: the numbers in a benchmark are only comparable
// across runs if the input is identical, and only meaningful if the input
// resembles what a real server returns.
//
// Three row shapes are used throughout, because they exercise different costs:
//
//   - representative: the ordinary case, a few hundred bytes of SQL.
//   - longQuery: a multi-kilobyte statement. Query text dominates both the
//     obfuscator's work and the bytes retained per row, and real workloads
//     (ORM-generated SQL, long IN lists) produce these routinely.
//   - nullHeavy: a row where most optional columns are NULL. The generic
//     scanner treats a NULL as an absent key and returns a warning per
//     column, so this is the path where error construction, not value
//     conversion, dominates.

// benchmarkTopQueryColumns is the column set the top-query template projects,
// in the order the scanner sees them.
var benchmarkTopQueryColumns = []string{
	callsColumnName,
	"datname",
	sharedBlksDirtiedColumnName,
	sharedBlksHitColumnName,
	sharedBlksReadColumnName,
	sharedBlksWrittenColumnName,
	tempBlksReadColumnName,
	tempBlksWrittenColumnName,
	"query",
	queryidColumnName,
	rolnameColumnName,
	rowsColumnName,
	totalExecTimeColumnName,
	totalPlanTimeColumnName,
	blkReadTimeAttributeName,
	blkWriteTimeAttributeName,
	"dbid",
	"userid",
	"toplevel",
}

// representativeQuery is an ordinary application statement: parameter
// placeholders, a join, an ORDER BY. Roughly what a web request issues.
const representativeQuery = `SELECT o.id, o.placed_at, o.total_cents, c.email, c.display_name ` +
	`FROM orders o JOIN customers c ON c.id = o.customer_id ` +
	`WHERE o.tenant_id = $1 AND o.placed_at >= $2 AND o.status = ANY($3) ` +
	`ORDER BY o.placed_at DESC LIMIT 100`

// longQuery stands in for machine-generated SQL. Built rather than written out
// so its size is stated rather than counted by eye.
var longQuery = buildLongQuery(120)

func buildLongQuery(unions int) string {
	var b strings.Builder
	for i := range unions {
		if i > 0 {
			b.WriteString(" UNION ALL ")
		}
		b.WriteString("SELECT id, created_at, payload_json, source_system, ")
		b.WriteString("checksum_sha256, retry_count, last_error_message ")
		b.WriteString("FROM events_partition_")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" WHERE tenant_id = $1 AND created_at BETWEEN $2 AND $3 ")
		b.WriteString("AND status NOT IN ('archived', 'purged', 'superseded')")
	}
	return b.String()
}

// queryWithTraceComment carries a sqlcommenter-style traceparent. Comment
// extraction and W3C trace-context parsing run for every row that has one, so
// this is the enrichment cost Step 6 defers until after selection.
const queryWithTraceComment = `/*traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01',` +
	`controller='orders',action='index'*/ ` + representativeQuery

// benchmarkTopQueryRow renders one top-query row as the SQL driver delivers it.
//
// Values are typed as lib/pq delivers them, not stringified: the template no
// longer casts queryid and rows to TEXT, and integer and float columns arrive
// as int64 and float64. Feeding strings here would measure a decode path the
// server does not produce.
//
// Each row's query text is made unique by i. That matters for the obfuscator
// cache Step 3 enables: a benchmark where every row carried identical text would
// report a cache hit rate no real server produces. Callers that want repetition
// pass a repeating i instead.
func benchmarkTopQueryRow(i int, query string) []driverValue {
	// Vary the statement rather than append a comment, so the text differs in a
	// way the obfuscator must actually normalise.
	text := strings.Replace(query, "$1", "$1 /* shard_"+strconv.Itoa(i)+" */", 1)
	return []driverValue{
		int64(100 + i),    // calls
		"orders_db",       // datname
		int64(10 + i),     // shared_blks_dirtied
		int64(2000 + i),   // shared_blks_hit
		int64(300 + i),    // shared_blks_read
		int64(40 + i),     // shared_blks_written
		int64(5 + i),      // temp_blks_read
		int64(6 + i),      // temp_blks_written
		text,              // query
		int64(900000 + i), // queryid
		"app_user",        // rolname
		int64(700 + i),    // rows
		1234.5678,         // total_exec_time
		234.5678,          // total_plan_time
		12.25,             // blk_read_time
		3.5,               // blk_write_time
		int64(16384),      // dbid
		int64(10),         // userid
		true,              // toplevel
	}
}

// benchmarkTopQueryRowNullHeavy returns a row whose optional counters are all
// NULL, which the driver delivers as nil.
func benchmarkTopQueryRowNullHeavy(i int) []driverValue {
	return []driverValue{
		int64(100 + i),      // calls
		nil,                 // datname: the dropped-database case
		nil,                 // shared_blks_dirtied
		nil,                 // shared_blks_hit
		nil,                 // shared_blks_read
		nil,                 // shared_blks_written
		nil,                 // temp_blks_read
		nil,                 // temp_blks_written
		representativeQuery, // query
		int64(900000 + i),   // queryid
		nil,                 // rolname
		nil,                 // rows
		1234.5678,           // total_exec_time
		nil,                 // total_plan_time
		nil,                 // blk_read_time
		nil,                 // blk_write_time
		nil,                 // dbid
		nil,                 // userid
		nil,                 // toplevel
	}
}

// driverValue is what sqlmock accepts for a cell.
type driverValue = driver.Value

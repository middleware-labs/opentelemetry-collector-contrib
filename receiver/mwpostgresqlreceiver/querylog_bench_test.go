// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"
)

// Benchmarks for the query-log decode and enrichment path: the generic string
// scanner, the per-row conversion maps, obfuscation and trace-context parsing.
//
// This is the path Step 3 makes cheaper without changing output (obfuscator
// cache, hoisted maps, module-level template parsing) and Step 6 replaces with
// typed rows. Measuring it through getTopQuery rather than through an extracted
// helper is deliberate: the cost being attacked is spread across the scanner,
// the map construction and the conversion, and a benchmark of any one piece in
// isolation would not show what removing the others is worth.
//
// sqlmock supplies rows without a server, so what is measured is the receiver's
// own construction cost, not PostgreSQL's or the network's.

// topQueryPrefixForBench matches the top-query statement regardless of its
// LIMIT clause.
var topQueryPrefixForBench = regexp.QuoteMeta(
	strings.SplitN(expectedScrapeTopQueryExtension111, "ORDER BY", 2)[0])

// benchTopQueryRows runs getTopQuery once over n prepared rows.
func benchTopQueryRows(b *testing.B, n int, row func(int) []driverValue) {
	b.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}
	logger := zap.NewNop()

	// Build the rows once. The fixture cost is setup, not the thing measured.
	values := make([][]driverValue, n)
	for i := range n {
		values[i] = row(i)
	}

	// The capability lookup is cached per connection, so it happens once here
	// rather than on each iteration - matching production, where it is resolved
	// on the first scrape and reused.
	expectPgStatStatementsVersionRegexp(mock, "1.11")

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		sqlRows := sqlmock.NewRows(benchmarkTopQueryColumns)
		for _, v := range values {
			sqlRows.AddRow(v...)
		}
		// Matched on the part of the statement that does not vary with the row
		// count, since the golden file pins a specific LIMIT.
		mock.ExpectQuery(topQueryPrefixForBench).WillReturnRows(sqlRows)
		b.StartTimer()

		if _, err := client.getTopQuery(b.Context(), int64(n), logger); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetTopQueryRepresentative is the default shape: max_rows_per_query
// defaults to 1000, and every candidate row is decoded and enriched before any
// selection happens.
func BenchmarkGetTopQueryRepresentative(b *testing.B) {
	for _, n := range []int{50, 1000} {
		b.Run("rows="+strconv.Itoa(n), func(b *testing.B) {
			benchTopQueryRows(b, n, func(i int) []driverValue {
				return benchmarkTopQueryRow(i, representativeQuery)
			})
		})
	}
}

// BenchmarkGetTopQueryLongSQL isolates the cost that scales with query text
// rather than with row count. Obfuscation is superlinear in statement length,
// and long machine-generated SQL is common.
func BenchmarkGetTopQueryLongSQL(b *testing.B) {
	for _, n := range []int{50, 1000} {
		b.Run("rows="+strconv.Itoa(n), func(b *testing.B) {
			benchTopQueryRows(b, n, func(i int) []driverValue {
				return benchmarkTopQueryRow(i, longQuery)
			})
		})
	}
}

// BenchmarkGetTopQueryRepeatedSQL is the shape the obfuscator cache in Step 3
// is meant to help: the same statements observed again on the next scrape.
// pg_stat_statements returns a largely unchanged set every ten seconds, so on a
// real server most text repeats. With no cache configured today, every row is
// re-obfuscated regardless.
func BenchmarkGetTopQueryRepeatedSQL(b *testing.B) {
	benchTopQueryRows(b, 1000, func(i int) []driverValue {
		// A small number of distinct statements, repeated - as a server with a
		// stable workload produces.
		return benchmarkTopQueryRow(i%20, representativeQuery)
	})
}

// BenchmarkGetTopQueryWithTraceComments measures comment extraction and W3C
// trace-context parsing, which run for every row carrying a sqlcommenter
// comment, before any row is selected for emission.
func BenchmarkGetTopQueryWithTraceComments(b *testing.B) {
	benchTopQueryRows(b, 1000, func(i int) []driverValue {
		return benchmarkTopQueryRow(i, queryWithTraceComment)
	})
}

// BenchmarkExtractSQLComments isolates comment extraction, which the CPU
// profile of BenchmarkGetTopQueryRepresentative showed to be the single largest
// CPU consumer in the default shape - larger than obfuscation.
//
// It runs a regex over the full text of every candidate row on every scrape,
// before any row has been selected for emission, and it costs nearly as much
// when there is no comment to find as when there is. Statement text is
// unbounded, so this scales with SQL length as well as with row count.
//
// The plan does not name this path; Step 6's "defer enrichment until after
// selection" covers it, but only if comment extraction is treated as
// enrichment rather than as part of decoding.
func BenchmarkExtractSQLComments(b *testing.B) {
	for _, tt := range []struct {
		name  string
		query string
	}{
		{name: "no comment", query: representativeQuery},
		{name: "trace comment", query: queryWithTraceComment},
		{name: "long sql no comment", query: longQuery},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				commentSink = extractSQLComments(tt.query)
			}
		})
	}
}

var commentSink []string

// BenchmarkGetTopQueryNullHeavy covers rows whose optional columns are NULL.
// The generic scanner returns a wrapped warning per NULL column and the client
// joins them, so this measures error construction rather than value conversion.
// Orphaned pg_stat_statements rows make this shape common on servers with
// database churn.
func BenchmarkGetTopQueryNullHeavy(b *testing.B) {
	benchTopQueryRows(b, 1000, benchmarkTopQueryRowNullHeavy)
}

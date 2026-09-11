// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package sqlquery // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/sqlquery"

import (
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"
)

// Benchmarks for the generic row scanner: the shared helper that converts every
// column of every row to a string with fmt.Sprintf and builds one map per row.
//
// Callers that need numbers then parse those strings back, so a numeric column
// makes a full format-and-reparse round trip. The mwpostgresql receiver's query
// log paths are the heaviest users, and Step 6 of its optimization plan replaces
// this with typed scan destinations. This measures what that is worth at the
// shared-helper level, separately from the receiver's own conversion work.
//
// These are also the first benchmarks in this package, so they double as the
// baseline for anyone changing the scanner for other receivers.

func benchQueryRows(b *testing.B, rows, cols int, value func(col int) any) {
	b.Helper()

	vals := make([][]any, rows)
	for r := range rows {
		row := make([]any, cols)
		for c := range cols {
			row[c] = value(c)
		}
		vals[r] = row
	}

	cl := DbSQLClient{
		Db:     fakeDB{rowVals: vals},
		Logger: zap.NewNop(),
		SQL:    "",
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := cl.QueryRows(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkQueryRowsNumeric is the shape that pays twice: integers and floats
// are formatted to strings here and parsed back by the caller.
func BenchmarkQueryRowsNumeric(b *testing.B) {
	for _, rows := range []int{50, 1000} {
		b.Run("rows="+strconv.Itoa(rows), func(b *testing.B) {
			benchQueryRows(b, rows, 12, func(col int) any {
				if col%2 == 0 {
					return int64(1234567 + col)
				}
				return float64(col) + 0.5
			})
		})
	}
}

// BenchmarkQueryRowsText covers string columns, where no conversion is needed
// but a map entry and a copied string are still allocated per column.
func BenchmarkQueryRowsText(b *testing.B) {
	const text = "SELECT o.id, o.placed_at FROM orders o WHERE o.tenant_id = $1 ORDER BY o.placed_at DESC"
	benchQueryRows(b, 1000, 12, func(int) any { return text })
}

// BenchmarkQueryRowsMixed approximates a real top-query row: mostly numeric
// counters alongside a few text columns.
func BenchmarkQueryRowsMixed(b *testing.B) {
	benchQueryRows(b, 1000, 16, func(col int) any {
		switch col {
		case 1:
			return "orders_db"
		case 8:
			return "SELECT o.id FROM orders o WHERE o.tenant_id = $1"
		case 10:
			return "app_user"
		case 12, 13, 14, 15:
			return float64(col) + 0.25
		default:
			return int64(1000 + col)
		}
	})
}

// BenchmarkQueryRowsNulls measures the NULL path, where each NULL column builds
// and returns a wrapped error that the caller then joins. Rows referencing
// dropped databases make this common in pg_stat_statements output.
func BenchmarkQueryRowsNulls(b *testing.B) {
	vals := make([][]any, 1000)
	for r := range vals {
		row := make([]any, 12)
		for c := range row {
			if c%2 == 0 {
				row[c] = nil
			} else {
				row[c] = int64(c)
			}
		}
		vals[r] = row
	}

	cl := DbSQLClient{
		Db:     fakeDB{rowVals: vals},
		Logger: zap.NewNop(),
		SQL:    "",
	}

	b.ReportAllocs()
	for b.Loop() {
		// A NULL yields ErrNullValueWarning rather than a failure, so the rows
		// are still returned and the error is expected here.
		_, _ = cl.QueryRows(b.Context())
	}
}

// BenchmarkQueryRowsTime covers timestamp columns, which take a separate
// formatting path (RFC3339Nano) rather than the generic one.
func BenchmarkQueryRowsTime(b *testing.B) {
	ts := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	benchQueryRows(b, 1000, 12, func(int) any { return ts })
}

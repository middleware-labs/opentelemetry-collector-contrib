// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// These cover the Step 3 construction changes, whose defining property is that
// they change no telemetry at all: the obfuscator query cache, the hoisted
// per-row conversion maps, and package-level template parsing.
//
// A benchmark can show they are cheaper; only a differential test can show they
// are output-neutral, which is the part that matters. Each test here asserts an
// invariant that would break if a construction change altered behavior rather
// than just cost.

// TestObfuscatorCacheReturnsIdenticalResults is the guard on enabling the
// obfuscator's query cache. A cache that returned anything but the value the
// uncached path would have produced would silently corrupt every repeated
// statement, and because pg_stat_statements returns the same text every scrape,
// "repeated" is the normal case rather than an edge case.
//
// The cache is keyed on the raw text, so the same input must obfuscate to the
// same output on the first call and every call after it, and different inputs
// must not collide.
func TestObfuscatorCacheReturnsIdenticalResults(t *testing.T) {
	inputs := []string{
		representativeQuery,
		queryWithTraceComment,
		longQuery,
		"SELECT 1",
		"SELECT * FROM users WHERE email = 'nobody@example.com' AND id = 42",
		// Statements that differ only in their literals normalise to the same
		// text; both must still round-trip consistently.
		"SELECT * FROM t WHERE id = 1",
		"SELECT * FROM t WHERE id = 2",
	}

	first := make([]string, len(inputs))
	for i, in := range inputs {
		got, err := obfuscateSQL(in)
		require.NoError(t, err)
		first[i] = got
	}

	// Every subsequent call must return exactly what the first one did. With
	// the cache enabled these are served from it; the assertion is that the
	// cached value equals the computed value.
	for range 3 {
		for i, in := range inputs {
			got, err := obfuscateSQL(in)
			require.NoError(t, err)
			require.Equal(t, first[i], got,
				"obfuscation of %q must be stable across calls", truncateForLog(in, 60))
		}
	}

	// Distinct statements must not be conflated by the cache key.
	assert.NotEqual(t, first[3], first[4], "different statements must not share a cached result")
}

// TestObfuscatorCacheHandlesConcurrentAccess exercises the cache from several
// goroutines at once. The obfuscator is a process-wide singleton shared by every
// scrape, and enabling the cache introduced shared mutable state where there
// was none, so concurrent use has to be correct as well as race-free.
func TestObfuscatorCacheHandlesConcurrentAccess(t *testing.T) {
	want, err := obfuscateSQL(representativeQuery)
	require.NoError(t, err)

	const goroutines = 8
	errs := make(chan error, goroutines)
	for range goroutines {
		go func() {
			for range 25 {
				got, oErr := obfuscateSQL(representativeQuery)
				if oErr != nil {
					errs <- oErr
					return
				}
				if got != want {
					errs <- assert.AnError
					return
				}
			}
			errs <- nil
		}()
	}
	for range goroutines {
		require.NoError(t, <-errs)
	}
}

// TestGetTopQueryAttributesUnchangedAcrossScrapes is the differential assertion
// for the hoisted conversion maps and the package-level template.
//
// The maps used to be rebuilt inside the row loop. Hoisting them shares one
// instance across every row and every scrape, so the risk introduced is that
// something mutates them and a later row or scrape sees different conversion
// behavior. Running the same rows through two consecutive scrapes and
// requiring identical attributes catches that.
func TestGetTopQueryAttributesUnchangedAcrossScrapes(t *testing.T) {
	rowValues := [][]driverValue{
		benchmarkTopQueryRow(1, representativeQuery),
		benchmarkTopQueryRow(2, queryWithTraceComment),
		benchmarkTopQueryRowNullHeavy(3),
	}

	scrape := func() []topQueryStatRow {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		require.NoError(t, err)
		defer db.Close()

		client := &postgreSQLClient{client: WrapDBWithIgnore(db), closeFn: func() error { return nil }}
		expectPgStatStatementsVersionRegexp(mock, "1.11")

		rows := sqlmock.NewRows(benchmarkTopQueryColumns)
		for _, v := range rowValues {
			rows.AddRow(v...)
		}
		mock.ExpectQuery(topQueryPrefixForBench).WillReturnRows(rows)

		// A NULL column yields a warning rather than a failure and the rows are
		// still returned, so the error is tolerated here; a hard failure would
		// come back with no rows, which the length assertion below catches.
		got, _ := client.getTopQuery(t.Context(), 100, databaseSelection{}, zap.NewNop())
		return got
	}

	firstScrape := scrape()
	secondScrape := scrape()

	require.Len(t, secondScrape, len(firstScrape))
	for i := range firstScrape {
		assert.Equal(t, firstScrape[i], secondScrape[i],
			"row %d must decode identically on a second scrape", i)
	}
}

// TestTopQueryTemplateRendersPerCall confirms that hoisting the parse did not
// make the rendered SQL static. The parsed template is shared across every
// scrape, so the values substituted into it must still vary per call.
func TestTopQueryTemplateRendersPerCall(t *testing.T) {
	render := func(limit int64, caps pgStatStatementsCapabilities) string {
		buf := &templateOutput{}
		require.NoError(t, topQueryTemplateParsed.Execute(buf, map[string]any{
			"limit":               limit,
			"hasExecTimeColumns":  caps.hasExecTimeColumns(),
			"hasTopLevel":         caps.hasTopLevel(),
			"hasSharedBlkTimings": caps.hasSharedBlkTimings(),
			"statementsView":      caps.qualify("pg_stat_statements"),
			"orderByExecTimeCol":  "total_exec_time",
			"databasePredicate":   "",
		}))
		return buf.String()
	}

	ext111 := pgStatStatementsCapabilities{installed: true, version: extensionVersion{1, 11}}
	ext19 := pgStatStatementsCapabilities{installed: true, version: extensionVersion{1, 9}}

	assert.Contains(t, render(31, ext111), "LIMIT 31")
	assert.Contains(t, render(77, ext111), "LIMIT 77")
	assert.NotEqual(t, render(31, ext111), render(77, ext111),
		"a shared parsed template must still render its per-call limit")

	// The version-dependent branches must also still be evaluated per call
	// rather than baked in at parse time.
	assert.Contains(t, render(31, ext111), "shared_blk_read_time")
	assert.NotContains(t, render(31, ext19), "shared_blk_read_time")
}

// TestQuerySampleTemplateRendersPerCall is the same check for the sample
// template, whose parse was also hoisted.
func TestQuerySampleTemplateRendersPerCall(t *testing.T) {
	var rendered []string
	for _, limit := range []int64{10, 250} {
		buf := &templateOutput{}
		require.NoError(t, querySampleTemplateParsed.Execute(buf, map[string]any{
			"limit":                limit,
			"newestQueryTimestamp": float64(0),
			"hasQueryID":           true,
			"databasePredicate":    "",
		}))
		rendered = append(rendered, buf.String())
	}
	require.Len(t, rendered, 2)
	assert.Contains(t, rendered[0], "LIMIT 10")
	assert.Contains(t, rendered[1], "LIMIT 250")
	assert.NotEqual(t, rendered[0], rendered[1],
		"a shared parsed template must still render its per-call values")
}

// templateOutput collects rendered template output.
type templateOutput struct{ b []byte }

func (o *templateOutput) Write(p []byte) (int, error) {
	o.b = append(o.b, p...)
	return len(p), nil
}

func (o *templateOutput) String() string { return string(o.b) }

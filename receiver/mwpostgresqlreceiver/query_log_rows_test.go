// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommentPrefilterMatchesRegex is the correctness gate for the marker
// pre-check in front of extractSQLComments.
//
// The pre-check is only sound because every alternative in sqlCommentPattern
// begins with a literal two-byte marker, so text containing neither "/*" nor
// "--" cannot match. That is a property of the pattern, and the pattern could
// be edited later, so it is asserted rather than assumed. The cases below
// cover both markers, partial and overlapping markers, markers inside string
// literals, and non-ASCII text.
func TestCommentPrefilterMatchesRegex(t *testing.T) {
	cases := []string{
		"",
		"SELECT 1",
		"SELECT id, name FROM users WHERE tenant_id = $1",
		"/* c */ SELECT 1",
		"-- c\nSELECT 1",
		"/*",
		"--",
		"/",
		"-",
		"*/",
		"/-*",
		"/*/",
		"/**/",
		"/* */ -- x",
		"SELECT 1 -- trailing",
		"SELECT '/*' FROM t",
		"SELECT '--' FROM t",
		"--\n--\n",
		"/*traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01'*/ SELECT 1",
		"SELECT 'unicode' -- 日本語のコメント",
		"日本語 /* ブロック */ SELECT 1",
		"a-b-c",
		"x/y/z",
	}

	for _, sql := range cases {
		t.Run(sql, func(t *testing.T) {
			assert.Equal(t, extractSQLComments(sql), extractSQLCommentsFast(sql),
				"pre-check must not change what the regex finds")
		})
	}
}

// TestCommentPrefilterSkipsRegexWhenNoMarker states the property the pre-check
// exists for: text with no marker never reaches the regex.
func TestCommentPrefilterSkipsRegexWhenNoMarker(t *testing.T) {
	require.False(t, hasCommentMarker("SELECT id FROM users WHERE tenant_id = $1"))
	require.True(t, hasCommentMarker("/* c */ SELECT 1"))
	require.True(t, hasCommentMarker("SELECT 1 -- c"))
}

// TestCommentPrefilterGuardsEveryPatternAlternative is the test that survives
// someone widening sqlCommentPattern.
//
// The table above can only assert agreement for markers it already knows about,
// so it cannot fail for a syntax added later - a dollar-quoted or '#' comment
// form would make the guard unsound while every listed case still passed. This
// derives its cases from the pattern instead: it splits the pattern on its
// top-level alternation and requires each alternative to begin with a literal
// two-byte prefix that hasCommentMarker tests for.
//
// An alternative that starts with something else is precisely the case where
// text can match the regex without containing "/*" or "--", which is what makes
// skipping the regex lossy.
func TestCommentPrefilterGuardsEveryPatternAlternative(t *testing.T) {
	// The markers hasCommentMarker tests for. Kept here rather than read out of
	// the function so that widening one without the other fails.
	guarded := []string{"/*", "--"}

	alternatives := splitTopLevelAlternation(sqlCommentPattern.String())
	require.NotEmpty(t, alternatives, "pattern produced no alternatives to check")

	for _, alt := range alternatives {
		t.Run(alt, func(t *testing.T) {
			literal := leadingLiteral(alt, 2)
			require.Contains(t, guarded, literal,
				"alternative %q starts with %q, which hasCommentMarker does not test for: "+
					"text could match this alternative without containing a guarded marker, "+
					"so the pre-check would skip a real comment. Add the marker to "+
					"hasCommentMarker and to this test's guarded list.", alt, literal)
		})
	}
}

// splitTopLevelAlternation splits a regexp source on "|" that is not inside a
// group or character class. Deliberately simple: it handles the shape
// sqlCommentPattern has, and a pattern complex enough to defeat it is itself a
// reason to re-examine the guard by hand.
func splitTopLevelAlternation(pattern string) []string {
	var (
		out     []string
		depth   int
		inClass bool
		start   int
	)
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '\\':
			i++ // skip the escaped byte
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '(':
			if !inClass {
				depth++
			}
		case ')':
			if !inClass {
				depth--
			}
		case '|':
			if !inClass && depth == 0 {
				out = append(out, pattern[start:i])
				start = i + 1
			}
		}
	}
	return append(out, pattern[start:])
}

// leadingLiteral returns the first n literal bytes an alternative must match,
// unescaping regexp escapes. It stops early at any metacharacter, so an
// alternative that does not begin with n literal bytes yields a short string
// which will not match a guarded marker - the failure the caller wants.
func leadingLiteral(alt string, n int) string {
	var out []byte
	for i := 0; i < len(alt) && len(out) < n; i++ {
		c := alt[i]
		if c == '\\' && i+1 < len(alt) {
			i++
			out = append(out, alt[i])
			continue
		}
		if strings.ContainsRune(`.*+?()[]{}^$|`, rune(c)) {
			break
		}
		out = append(out, c)
	}
	return string(out)
}

// TestTopQueryStatRowScanDestMatchesTemplate guards the one invariant typed
// positional scanning depends on: scanDest must return exactly one destination
// per projected column, in the same order the template selects them.
//
// benchmarkTopQueryColumns is that projection, and sqlmock feeds rows in its
// order, so a drift between the two would silently scan values into the wrong
// fields rather than failing to compile.
func TestTopQueryStatRowScanDestMatchesTemplate(t *testing.T) {
	var row topQueryStatRow
	require.Len(t, row.scanDest(), len(benchmarkTopQueryColumns),
		"scanDest must have one destination per projected column")
}

// TestTopQueryStatRowCounterCoversEveryDeltaColumn asserts the typed counter
// accessor knows every column the delta loop differences. A counter missing
// here would read as a constant zero, so its delta would always be zero and the
// statement would never be reported - a silent telemetry loss.
func TestTopQueryStatRowCounterCoversEveryDeltaColumn(t *testing.T) {
	row := topQueryStatRow{
		calls:             nullInt(1),
		rows:              nullInt(2),
		sharedBlksDirtied: nullInt(3),
		sharedBlksHit:     nullInt(4),
		sharedBlksRead:    nullInt(5),
		sharedBlksWritten: nullInt(6),
		tempBlksRead:      nullInt(7),
		tempBlksWritten:   nullInt(8),
		totalExecTime:     nullFloat(9000),
		totalPlanTime:     nullFloat(10000),
		blkReadTime:       nullFloat(11000),
		blkWriteTime:      nullFloat(12000),
	}

	for columnName := range updatedOnly {
		assert.NotZero(t, row.counter(columnName),
			"counter(%q) returned zero for a non-zero column, so its delta would always be zero", columnName)
	}
}

// TestTopQueryStatRowCounterConvertsMillisecondsToSeconds pins the unit
// conversion that moved out of the decode path and into the typed accessor.
func TestTopQueryStatRowCounterConvertsMillisecondsToSeconds(t *testing.T) {
	row := topQueryStatRow{
		totalExecTime: nullFloat(11000),
		totalPlanTime: nullFloat(12000),
		blkReadTime:   nullFloat(100),
		blkWriteTime:  nullFloat(200),
		calls:         nullInt(123),
	}

	assert.InDelta(t, 11.0, row.counter(totalExecTimeColumnName), 1e-9)
	assert.InDelta(t, 12.0, row.counter(totalPlanTimeColumnName), 1e-9)
	assert.InDelta(t, 0.1, row.counter(blkReadTimeAttributeName), 1e-9)
	assert.InDelta(t, 0.2, row.counter(blkWriteTimeAttributeName), 1e-9)
	// Integer counters are not scaled.
	assert.InDelta(t, 123.0, row.counter(callsColumnName), 1e-9)
}

// TestTopQueryStatRowCounterTreatsNullAsZero preserves the previous behavior,
// where a NULL column was absent from the row map and read back as zero.
func TestTopQueryStatRowCounterTreatsNullAsZero(t *testing.T) {
	var row topQueryStatRow
	for columnName := range updatedOnly {
		assert.Zero(t, row.counter(columnName), "NULL %q must read as zero", columnName)
	}
}

func nullInt(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

func nullFloat(v float64) sql.NullFloat64 { return sql.NullFloat64{Float64: v, Valid: true} }

// TestTopQueryDeltasCoversEveryCounter guards the fixed-size delta array
// against the counter set outgrowing it.
//
// topQueryDeltas is an array so the deltas travel with a selected candidate
// without allocating. That trades a compile-time guarantee for a runtime one:
// adding a counter to updatedOnly without widening the array would write past
// the slots the emit loop reads, so the new counter would be silently dropped
// from every event.
func TestTopQueryDeltasCoversEveryCounter(t *testing.T) {
	var deltas topQueryDeltas
	require.Len(t, orderedTopQueryCounters, len(deltas),
		"topQueryDeltas must have one slot per counter in updatedOnly")
	require.Len(t, orderedTopQueryCounters, len(updatedOnly),
		"orderedTopQueryCounters must list every counter exactly once")

	seen := make(map[string]struct{}, len(orderedTopQueryCounters))
	for _, columnName := range orderedTopQueryCounters {
		_, known := updatedOnly[columnName]
		require.True(t, known, "%q is ordered but not in updatedOnly", columnName)
		_, duplicate := seen[columnName]
		require.False(t, duplicate, "%q appears twice in orderedTopQueryCounters", columnName)
		seen[columnName] = struct{}{}
	}
}

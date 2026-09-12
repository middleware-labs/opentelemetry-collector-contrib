// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"database/sql"
	"reflect"
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

// TestTopQueryStatRowSnapshotCarriesEveryCounter checks that snapshot copies
// every counter out of the row, in the units the server reports.
//
// A field the row scans but snapshot forgets to copy reads as zero forever, so
// its delta is always zero and the counter silently never appears in an event.
// Every value below is distinct, so a copy that reads the wrong field fails
// too, not just one that reads nothing.
func TestTopQueryStatRowSnapshotCarriesEveryCounter(t *testing.T) {
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

	assert.Equal(t, statementCounters{
		calls:             1,
		rows:              2,
		sharedBlksDirtied: 3,
		sharedBlksHit:     4,
		sharedBlksRead:    5,
		sharedBlksWritten: 6,
		tempBlksRead:      7,
		tempBlksWritten:   8,
		// Milliseconds, unconverted: the conversion to seconds happens once on
		// the delta rather than on both operands.
		totalExecTimeMS: 9000,
		totalPlanTimeMS: 10000,
		blkReadTimeMS:   11000,
		blkWriteTimeMS:  12000,
	}, row.snapshot().counters)
}

// TestStatementCountersCoverEveryReportedColumn asserts that the struct
// carrying the counters has exactly one field per counter the receiver reports.
//
// statementCounters uses named fields rather than a map or an array, so a
// counter added to the SQL and to the row without being added here would be
// dropped with nothing to notice it. The two lists are declared independently -
// one from the column names, one by reflecting over the struct - so they can
// only agree if both were updated.
func TestStatementCountersCoverEveryReportedColumn(t *testing.T) {
	require.Len(t, topQueryCounterColumns, reflect.TypeFor[statementCounters]().NumField(),
		"statementCounters must have exactly one field per counter in topQueryCounterColumns")
	require.Len(t, topQueryCounterColumns, reflect.TypeFor[statementDeltas]().NumField(),
		"statementDeltas must have exactly one field per counter in topQueryCounterColumns")

	seen := make(map[string]struct{}, len(topQueryCounterColumns))
	for _, columnName := range topQueryCounterColumns {
		_, duplicate := seen[columnName]
		require.False(t, duplicate, "%q appears twice in topQueryCounterColumns", columnName)
		seen[columnName] = struct{}{}
	}
}

// TestStatementCountersSubConvertsMillisecondsToSeconds pins the unit
// conversion, which happens once on the difference rather than on each operand.
func TestStatementCountersSubConvertsMillisecondsToSeconds(t *testing.T) {
	prev := statementCounters{
		calls:           100,
		totalExecTimeMS: 1000,
		totalPlanTimeMS: 2000,
		blkReadTimeMS:   50,
		blkWriteTimeMS:  150,
	}
	cur := statementCounters{
		calls:           123,
		totalExecTimeMS: 12000,
		totalPlanTimeMS: 14000,
		blkReadTimeMS:   150,
		blkWriteTimeMS:  350,
	}

	deltas := prev.sub(cur)
	assert.InDelta(t, 11.0, deltas.totalExecTime, 1e-9)
	assert.InDelta(t, 12.0, deltas.totalPlanTime, 1e-9)
	assert.InDelta(t, 0.1, deltas.blkReadTime, 1e-9)
	assert.InDelta(t, 0.2, deltas.blkWriteTime, 1e-9)
	// Integer counters are not scaled, and stay integers.
	assert.Equal(t, int64(23), deltas.calls)
}

// TestStatementCountersSubIsExactForLargeCounters checks that a delta of two
// large counters is computed in integer arithmetic.
//
// pg_stat_statements counts calls and rows as bigint. The previous code
// converted each operand to float64 before subtracting, which silently rounds
// any value above 2^53 to the nearest representable one: two counters a few
// apart round to the same float and the delta comes out as zero, so a busy
// statement stops being reported entirely rather than reporting a wrong number.
func TestStatementCountersSubIsExactForLargeCounters(t *testing.T) {
	// Both above 2^53, seven apart. As float64 both round to the same value.
	const prevCalls = int64(1) << 60
	const curCalls = prevCalls + 7
	require.Equal(t, float64(prevCalls), float64(curCalls),
		"the fixture must be in the range where float64 cannot tell the two apart")

	deltas := statementCounters{calls: prevCalls}.sub(statementCounters{calls: curCalls})
	assert.Equal(t, int64(7), deltas.calls,
		"a delta of large counters must be computed as integers, not through float64")
}

// TestTopQueryStatRowSnapshotTreatsNullAsZero preserves the previous behavior,
// where a NULL column was absent from the row map and read back as zero.
func TestTopQueryStatRowSnapshotTreatsNullAsZero(t *testing.T) {
	var row topQueryStatRow
	assert.Equal(t, statementCounters{}, row.snapshot().counters)
	assert.True(t, row.snapshot().statsSince.IsZero(),
		"a NULL stats_since must read as the zero time, which is how the cache "+
			"recognizes that the signal is unavailable")
}

// TestTopQueryStatRowIdentityUsesProjectedOIDs checks that the cache key is
// built from the identity columns rather than the joined names.
func TestTopQueryStatRowIdentityUsesProjectedOIDs(t *testing.T) {
	row := topQueryStatRow{
		queryID:  nullInt(114514),
		dbid:     nullInt(16384),
		userid:   nullInt(10),
		toplevel: sql.NullBool{Bool: true, Valid: true},
		// The names must not participate: they are a join result, and a
		// database dropped and re-created under the same name is a different
		// database with a different OID.
		datname: sql.NullString{String: "orders_db", Valid: true},
		rolname: sql.NullString{String: "app_user", Valid: true},
	}

	assert.Equal(t, statementIdentity{
		queryID: 114514, dbID: 16384, userID: 10, topLevel: true,
	}, row.identity())

	renamed := row
	renamed.datname = sql.NullString{String: "something_else", Valid: true}
	renamed.rolname = sql.NullString{String: "someone_else", Valid: true}
	assert.Equal(t, row.identity(), renamed.identity(),
		"identity must not depend on the joined names")
}

func nullInt(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

func nullFloat(v float64) sql.NullFloat64 { return sql.NullFloat64{Float64: v, Valid: true} }

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDatabaseSelectionStates covers the three states the policy distinguishes
// and, in particular, the one the previous per-path filtering could not
// express: a restricted selection whose allowlist was emptied by exclusions.
//
// Treating that as "no filter" - which is what a bare len(databases) == 0 check
// does - collects from every database on the server, the exact opposite of what
// the configuration asked for.
func TestDatabaseSelectionStates(t *testing.T) {
	for _, tt := range []struct {
		name            string
		databases       []string
		excludes        []string
		restricted      bool
		selectsNothing  bool
		anyRestriction  bool
		wantAllowed     []string
		wantDescription string
	}{
		{
			name:            "no restrictions at all",
			restricted:      false,
			selectsNothing:  false,
			anyRestriction:  false,
			wantDescription: "all databases",
		},
		{
			name:            "exclusion only",
			excludes:        []string{"scratch", "archived"},
			restricted:      false,
			selectsNothing:  false,
			anyRestriction:  true,
			wantDescription: "all databases except 2 excluded",
		},
		{
			name:            "allowlist only",
			databases:       []string{"orders", "billing"},
			restricted:      true,
			anyRestriction:  true,
			wantAllowed:     []string{"billing", "orders"},
			wantDescription: "restricted to 2 database(s)",
		},
		{
			name:            "exclusion wins over inclusion",
			databases:       []string{"orders", "billing"},
			excludes:        []string{"billing"},
			restricted:      true,
			anyRestriction:  true,
			wantAllowed:     []string{"orders"},
			wantDescription: "restricted to 1 database(s)",
		},
		{
			name:            "duplicates are collapsed",
			databases:       []string{"orders", "orders", "billing", "orders"},
			restricted:      true,
			anyRestriction:  true,
			wantAllowed:     []string{"billing", "orders"},
			wantDescription: "restricted to 2 database(s)",
		},
		{
			name:            "exclusions cancel the whole allowlist",
			databases:       []string{"orders", "billing"},
			excludes:        []string{"orders", "billing"},
			restricted:      true,
			selectsNothing:  true,
			anyRestriction:  true,
			wantDescription: "restricted to no databases (every configured name was excluded)",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sel := newDatabaseSelection(tt.databases, tt.excludes)

			assert.Equal(t, tt.restricted, sel.isRestricted())
			assert.Equal(t, tt.selectsNothing, sel.selectsNothing())
			assert.Equal(t, tt.anyRestriction, sel.hasAnyRestriction())
			assert.Equal(t, tt.wantAllowed, sel.allowed)
			assert.Equal(t, tt.wantDescription, sel.describe())
		})
	}
}

// TestDatabaseSelectionEmptySelectionIsNotUnrestricted is the single most
// important property of this type, stated on its own because getting it wrong
// inverts the operator's intent.
func TestDatabaseSelectionEmptySelectionIsNotUnrestricted(t *testing.T) {
	cancelled := newDatabaseSelection([]string{"orders"}, []string{"orders"})
	unrestricted := newDatabaseSelection(nil, nil)

	// Both have an empty `allowed` slice.
	require.Empty(t, cancelled.allowed)
	require.Empty(t, unrestricted.allowed)

	// They must behave in opposite ways.
	assert.False(t, cancelled.includes("orders"), "a cancelled selection includes nothing")
	assert.False(t, cancelled.includes("anything_else"))
	assert.True(t, unrestricted.includes("orders"), "an unrestricted selection includes everything")
	assert.True(t, unrestricted.includes("anything_else"))

	// And the SQL they generate must differ in the same direction.
	cancelledPred, _ := cancelled.datnamePredicate("datname", 1)
	unrestrictedPred, _ := unrestricted.datnamePredicate("datname", 1)
	assert.Equal(t, "false", cancelledPred, "a cancelled selection must match no row")
	assert.Empty(t, unrestrictedPred, "an unrestricted selection needs no predicate")
}

func TestDatabaseSelectionIncludes(t *testing.T) {
	for _, tt := range []struct {
		name      string
		databases []string
		excludes  []string
		in        []string
		out       []string
	}{
		{
			name: "unrestricted admits everything including unresolved",
			in:   []string{"orders", "billing", ""},
		},
		{
			name:     "exclusion only",
			excludes: []string{"scratch"},
			in:       []string{"orders", ""},
			out:      []string{"scratch"},
		},
		{
			name:      "allowlist drops unresolved rows",
			databases: []string{"orders"},
			in:        []string{"orders"},
			out:       []string{"billing", "", "scratch"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sel := newDatabaseSelection(tt.databases, tt.excludes)
			for _, db := range tt.in {
				assert.True(t, sel.includes(db), "%q should be in scope", db)
			}
			for _, db := range tt.out {
				assert.False(t, sel.includes(db), "%q should be out of scope", db)
			}
		})
	}
}

// TestDatabaseSelectionEffectiveDatabases covers the interaction with
// discovery: an allowlist is authoritative and ignores what discovery found, so
// a named database that is temporarily unreachable is a failure to report
// rather than a reason to widen scope.
func TestDatabaseSelectionEffectiveDatabases(t *testing.T) {
	discovered := []string{"postgres", "orders", "billing", "scratch"}

	unrestricted := newDatabaseSelection(nil, nil)
	assert.Equal(t, discovered, unrestricted.effectiveDatabases(discovered))

	exclusionOnly := newDatabaseSelection(nil, []string{"scratch", "postgres"})
	assert.Equal(t, []string{"orders", "billing"}, exclusionOnly.effectiveDatabases(discovered))

	// The allowlist names a database discovery did not return; it stays in the
	// effective set so the failure to reach it surfaces as an error.
	restricted := newDatabaseSelection([]string{"orders", "not_discovered"}, nil)
	assert.Equal(t, []string{"not_discovered", "orders"}, restricted.effectiveDatabases(discovered))

	cancelled := newDatabaseSelection([]string{"orders"}, []string{"orders"})
	assert.Empty(t, cancelled.effectiveDatabases(discovered))
}

// TestDatabaseSelectionPredicateUsesBoundParameters is the injection guard.
// The predicate must never contain a database name: names go to the driver as
// parameters, so quotes, commas and non-ASCII characters cannot alter the SQL.
func TestDatabaseSelectionPredicateUsesBoundParameters(t *testing.T) {
	hostile := []string{
		`o'brien`,
		`tab"le`,
		"comma,name",
		"Ünïcôdé",
		"drop'); DROP TABLE users; --",
	}

	sel := newDatabaseSelection(hostile, nil)
	predicate, args := sel.datnamePredicate("datname", 1)

	assert.Equal(t, "datname = ANY($1)", predicate)
	require.Len(t, args, 1)
	for _, name := range hostile {
		assert.NotContains(t, predicate, name,
			"database names must reach SQL as parameters, never inside the statement text")
	}
}

func TestDatabaseSelectionPredicateForms(t *testing.T) {
	for _, tt := range []struct {
		name      string
		databases []string
		excludes  []string
		column    string
		index     int
		want      string
		wantArgs  int
	}{
		{
			name:   "unrestricted has no predicate",
			column: "datname",
			index:  1,
		},
		{
			name:      "allowlist",
			databases: []string{"orders"},
			column:    "datname",
			index:     1,
			want:      "datname = ANY($1)",
			wantArgs:  1,
		},
		{
			// NULL datname must survive an exclusion-only filter, because
			// unrestricted-with-exclusions still reports unresolved rows.
			name:     "exclusion keeps NULL datname",
			excludes: []string{"scratch"},
			column:   "pg_database.datname",
			index:    2,
			want:     "(pg_database.datname IS NULL OR NOT (pg_database.datname = ANY($2)))",
			wantArgs: 1,
		},
		{
			name:      "cancelled selection matches nothing",
			databases: []string{"orders"},
			excludes:  []string{"orders"},
			column:    "datname",
			index:     1,
			want:      "false",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sel := newDatabaseSelection(tt.databases, tt.excludes)
			got, args := sel.datnamePredicate(tt.column, tt.index)
			assert.Equal(t, tt.want, got)
			assert.Len(t, args, tt.wantArgs)
		})
	}
}

// TestAppendDatnameFilterPlacesKeywordCorrectly covers the replacement for the
// old helper's `strings.Contains(baseQuery, "WHERE")` heuristic, which a query
// containing that word in a column name or literal would have defeated.
func TestAppendDatnameFilterPlacesKeywordCorrectly(t *testing.T) {
	sel := newDatabaseSelection([]string{"orders"}, nil)

	noWhere, args := sel.appendDatnameFilter("SELECT datname FROM pg_stat_database", "datname", false)
	assert.Equal(t, "SELECT datname FROM pg_stat_database WHERE datname = ANY($1)", noWhere)
	assert.Len(t, args, 1)

	withWhere, _ := sel.appendDatnameFilter(
		"SELECT datname FROM pg_database WHERE datistemplate = false", "datname", true)
	assert.Equal(t, "SELECT datname FROM pg_database WHERE datistemplate = false AND datname = ANY($1)", withWhere)

	// An unrestricted selection must leave the query untouched rather than
	// appending an always-true predicate.
	unrestricted := newDatabaseSelection(nil, nil)
	unchanged, noArgs := unrestricted.appendDatnameFilter("SELECT 1", "datname", false)
	assert.Equal(t, "SELECT 1", unchanged)
	assert.Empty(t, noArgs)
}

func TestValidateDatabaseSelection(t *testing.T) {
	require.NoError(t, validateDatabaseSelection(nil, nil))
	require.NoError(t, validateDatabaseSelection([]string{"orders"}, []string{"billing"}))
	require.NoError(t, validateDatabaseSelection(nil, []string{"scratch"}))

	err := validateDatabaseSelection([]string{"orders", "billing"}, []string{"orders", "billing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no database-specific telemetry")
}

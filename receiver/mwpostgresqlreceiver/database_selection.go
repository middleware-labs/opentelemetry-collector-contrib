// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lib/pq"
)

// databaseSelection is the immutable include/exclude policy derived once from
// configuration and shared by every collection path.
//
// The distinction that matters, and that the previous per-path filtering did
// not make, is between three states:
//
//   - Unrestricted: no `databases` and no `exclude_databases`. Every database
//     is in scope. Query telemetry keeps its existing broad behavior,
//     including rows whose database cannot be resolved.
//   - Exclusion only: no `databases`, some `exclude_databases`. Everything
//     except the named databases is in scope. An empty allowlist here means
//     "no allowlist", not "select nothing".
//   - Restricted: a non-empty `databases`, minus any overlap with
//     `exclude_databases`. Only the surviving names are in scope. If the
//     overlap removes everything, the selection is empty *and restricted*,
//     which means collect nothing - never fall back to collecting everything.
//
// Conflating the last two is the bug this type exists to prevent:
// `filterQueryByDatabases` treated an empty list as "no filter", so a
// configuration whose exclusions cancelled its allowlist would have collected
// from every database on the server.
type databaseSelection struct {
	// allowed is the effective allowlist: configured names minus exclusions,
	// deduplicated, sorted. Meaningful only when restricted is true.
	allowed []string

	// excluded is the exclusion set, deduplicated and sorted. It applies to
	// discovered databases when there is no allowlist.
	excluded []string

	// restricted reports whether an allowlist was configured at all. When
	// false, `allowed` is empty because there is no allowlist - not because
	// nothing was selected.
	restricted bool

	// allowedSet and excludedSet back the membership checks.
	allowedSet  map[string]struct{}
	excludedSet map[string]struct{}
}

// newDatabaseSelection builds the policy from raw configuration.
//
// Exclusion wins over inclusion: a database named in both is not collected.
// Names are compared exactly, as PostgreSQL identifiers already resolved to
// their stored form; no case folding or quoting is applied here, because the
// names reach SQL as bound parameters rather than as interpolated literals.
func newDatabaseSelection(databases, excludeDatabases []string) databaseSelection {
	excludedSet := make(map[string]struct{}, len(excludeDatabases))
	for _, db := range excludeDatabases {
		if db == "" {
			continue
		}
		excludedSet[db] = struct{}{}
	}

	sel := databaseSelection{
		restricted:  len(databases) > 0,
		excludedSet: excludedSet,
		excluded:    sortedKeys(excludedSet),
		allowedSet:  map[string]struct{}{},
	}

	if !sel.restricted {
		return sel
	}

	allowedSet := make(map[string]struct{}, len(databases))
	for _, db := range databases {
		if db == "" {
			continue
		}
		if _, excluded := excludedSet[db]; excluded {
			continue
		}
		allowedSet[db] = struct{}{}
	}
	sel.allowedSet = allowedSet
	sel.allowed = sortedKeys(allowedSet)
	return sel
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// includes reports whether one database is in scope.
//
// An unresolved database - a pg_stat_statements row whose dbid no longer joins
// to pg_database - reaches here as the empty string. It is kept unless an
// allowlist is configured: with an allowlist it cannot be proven to belong to a
// selected database, so it is dropped and counted. With only exclusions it
// cannot be proven to belong to an *excluded* database either, and the plan
// keeps the existing behavior of reporting it. This matches the NULL handling
// in datnamePredicate, so the Go-side and SQL-side checks agree.
func (s databaseSelection) includes(database string) bool {
	if database == "" {
		return !s.restricted
	}
	if _, excluded := s.excludedSet[database]; excluded {
		return false
	}
	if !s.restricted {
		return true
	}
	_, ok := s.allowedSet[database]
	return ok
}

// isRestricted reports whether an allowlist was configured.
func (s databaseSelection) isRestricted() bool { return s.restricted }

// selectsNothing reports a restricted selection whose effective allowlist is
// empty, which means no database-specific telemetry at all.
//
// This is distinct from an unrestricted selection, which also has an empty
// `allowed` slice but collects everything.
func (s databaseSelection) selectsNothing() bool {
	return s.restricted && len(s.allowedSet) == 0
}

// hasAnyRestriction reports whether the policy constrains scope at all, by
// allowlist or by exclusion.
func (s databaseSelection) hasAnyRestriction() bool {
	return s.restricted || len(s.excludedSet) > 0
}

// apply filters a list of database names - typically the result of discovery -
// down to those in scope, preserving the input order.
func (s databaseSelection) apply(databases []string) []string {
	if !s.hasAnyRestriction() {
		return databases
	}
	out := make([]string, 0, len(databases))
	for _, db := range databases {
		if s.includes(db) {
			out = append(out, db)
		}
	}
	return out
}

// effectiveDatabases returns the databases to collect from, given what
// discovery found.
//
// A restricted selection uses its own allowlist and ignores discovery: an
// explicitly named database that is not currently connectable is a failure to
// report, not a reason to widen scope. An unrestricted selection uses discovery
// minus exclusions.
func (s databaseSelection) effectiveDatabases(discovered []string) []string {
	if s.restricted {
		return slices.Clone(s.allowed)
	}
	return s.apply(discovered)
}

// datnamePredicate renders a SQL predicate restricting `datname` to the
// selection, together with the bound parameters it references.
//
// Names are passed as bound parameters rather than interpolated, so a database
// whose name contains a quote, a comma or non-ASCII characters is handled by
// the driver instead of by string concatenation. `column` is the qualified
// column to test and must be a caller-supplied constant, never user input.
//
// startIndex is the first positional placeholder number to use, so a caller
// that already has parameters can append. An empty predicate means no
// restriction applies.
func (s databaseSelection) datnamePredicate(column string, startIndex int) (string, []any) {
	switch {
	case s.selectsNothing():
		// Restricted to nothing: a predicate that matches no row. Returning an
		// empty predicate here would collect everything, which is the failure
		// mode this type exists to prevent.
		return "false", nil

	case s.restricted:
		// An allowlist already has exclusions applied, so it alone is enough.
		return fmt.Sprintf("%s = ANY($%d)", column, startIndex), []any{pq.StringArray(s.allowed)}

	case len(s.excludedSet) > 0:
		// Exclusion only. NULL datname (a dropped database) must survive the
		// predicate, because unrestricted-with-exclusions still reports
		// unresolved rows; `NOT (x = ANY(...))` is NULL for a NULL x, so the
		// IS NULL arm is required.
		return fmt.Sprintf("(%s IS NULL OR NOT (%s = ANY($%d)))", column, column, startIndex),
			[]any{pq.StringArray(s.excluded)}

	default:
		return "", nil
	}
}

// dbidPredicate renders a predicate restricting a database OID column to the
// selection, for views keyed on dbid rather than on a name.
//
// The names are resolved to OIDs by the server, in a subquery against
// pg_database, rather than by a separate lookup in Go. That keeps the whole
// thing one round trip and means there is no name-to-OID cache to invalidate: a
// database dropped between the subquery and the outer scan simply stops
// matching, which is the correct outcome.
//
// An OID that no longer resolves to any database - a statement whose database
// was dropped - is dropped under an allowlist and kept otherwise, matching
// includes().
func (s databaseSelection) dbidPredicate(column string, startIndex int) (string, []any) {
	switch {
	case s.selectsNothing():
		return "false", nil

	case s.restricted:
		return fmt.Sprintf(
				"%s IN (SELECT oid FROM pg_database WHERE datname = ANY($%d))", column, startIndex),
			[]any{pq.StringArray(s.allowed)}

	case len(s.excludedSet) > 0:
		return fmt.Sprintf(
				"%s NOT IN (SELECT oid FROM pg_database WHERE datname = ANY($%d))", column, startIndex),
			[]any{pq.StringArray(s.excluded)}

	default:
		return "", nil
	}
}

// appendDatnameFilter attaches the selection predicate to a query that may or
// may not already have a WHERE clause.
//
// This replaces filterQueryByDatabases, which interpolated names as quoted
// string literals and decided between WHERE and AND by searching the query text
// for the substring "WHERE" - a heuristic that a column or literal containing
// that word would defeat.
func (s databaseSelection) appendDatnameFilter(baseQuery, column string, hasWhere bool) (string, []any) {
	predicate, args := s.datnamePredicate(column, 1)
	if predicate == "" {
		return baseQuery, nil
	}
	keyword := " WHERE "
	if hasWhere {
		keyword = " AND "
	}
	return baseQuery + keyword + predicate, args
}

// describe renders the policy for logging, without emitting a database-name
// array on every scrape.
func (s databaseSelection) describe() string {
	switch {
	case s.selectsNothing():
		return "restricted to no databases (every configured name was excluded)"
	case s.restricted:
		return fmt.Sprintf("restricted to %d database(s)", len(s.allowed))
	case len(s.excludedSet) > 0:
		return fmt.Sprintf("all databases except %d excluded", len(s.excluded))
	default:
		return "all databases"
	}
}

// validateDatabaseSelection reports configuration that cannot collect anything,
// so the operator learns at startup rather than from absent telemetry.
func validateDatabaseSelection(databases, excludeDatabases []string) error {
	sel := newDatabaseSelection(databases, excludeDatabases)
	if sel.selectsNothing() {
		return fmt.Errorf(
			"every database in `databases` (%s) is also in `exclude_databases`, so no database-specific telemetry would be collected",
			strings.Join(databases, ", "))
	}
	return nil
}

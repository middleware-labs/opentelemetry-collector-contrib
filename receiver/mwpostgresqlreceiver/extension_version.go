// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwpostgresqlreceiver"

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// pgStatStatementsCapabilities describes what one server's installed
// pg_stat_statements extension actually provides.
//
// The extension version is not implied by the server version. pg_upgrade
// carries the old extension over unchanged, so a PostgreSQL 17 server can be
// running pg_stat_statements 1.8, and a PostgreSQL 14 server upgraded from 13
// can still be on 1.7 until someone runs ALTER EXTENSION ... UPDATE. Gating
// columns on the server's major version therefore produces queries that
// reference columns the installed extension does not have, and the whole
// statement path fails with "column ... does not exist".
//
// Capabilities are derived from the columns the extension's own upgrade
// scripts add:
//
//	1.8  total_exec_time, total_plan_time (renamed from total_time)
//	1.9  toplevel, pg_stat_statements_info (dealloc, stats_reset)
//	1.11 blk_read_time/blk_write_time renamed to shared_blk_read_time/
//	     shared_blk_write_time; local and temp timings split out; per-entry
//	     stats_since added
type pgStatStatementsCapabilities struct {
	// schema is the schema the extension was installed into, already quoted
	// for use as an identifier. Installations that place the extension outside
	// the search path (Supabase uses "extensions") are only reachable when the
	// view and function names are schema-qualified.
	schema string

	// version is the extension version as reported by pg_extension.extversion.
	version extensionVersion

	// installed is false when pg_stat_statements is not present at all.
	installed bool
}

// hasExecTimeColumns reports whether total_exec_time/total_plan_time exist
// (extension 1.8+). Below that the column is total_time and there is no plan
// time at all.
func (c pgStatStatementsCapabilities) hasExecTimeColumns() bool {
	return c.version.atLeast(1, 8)
}

// hasTopLevel reports whether the toplevel column and pg_stat_statements_info
// exist (extension 1.9+).
func (c pgStatStatementsCapabilities) hasTopLevel() bool {
	return c.version.atLeast(1, 9)
}

// hasSharedBlkTimings reports whether the block I/O timing columns use the
// shared_blk_* names (extension 1.11+) rather than blk_read_time and
// blk_write_time.
func (c pgStatStatementsCapabilities) hasSharedBlkTimings() bool {
	return c.version.atLeast(1, 11)
}

// hasStatsSince reports whether each entry carries a stats_since timestamp
// (extension 1.11+), which identifies an entry that was deallocated and
// re-created since the last scrape.
func (c pgStatStatementsCapabilities) hasStatsSince() bool {
	return c.version.atLeast(1, 11)
}

// qualify returns name qualified with the extension's schema, so the object
// resolves regardless of search_path.
func (c pgStatStatementsCapabilities) qualify(name string) string {
	if c.schema == "" {
		return name
	}
	return c.schema + "." + name
}

// extensionVersion is a parsed major.minor extension version.
//
// These must be compared numerically, not as strings: pg_stat_statements has
// shipped versions 1.4 through 1.13, and "1.10" sorts below "1.9" as text
// while being three releases newer.
type extensionVersion struct {
	major int
	minor int
}

func (v extensionVersion) atLeast(major, minor int) bool {
	if v.major != major {
		return v.major > major
	}
	return v.minor >= minor
}

func (v extensionVersion) String() string {
	return fmt.Sprintf("%d.%d", v.major, v.minor)
}

func parseExtensionVersion(s string) (extensionVersion, error) {
	major, minor, found := strings.Cut(strings.TrimSpace(s), ".")
	if !found {
		// Some extensions are versioned with a bare integer.
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return extensionVersion{}, fmt.Errorf("unexpected extension version %q: %w", s, err)
		}
		return extensionVersion{major: n}, nil
	}
	maj, err := strconv.Atoi(strings.TrimSpace(major))
	if err != nil {
		return extensionVersion{}, fmt.Errorf("unexpected extension version %q: %w", s, err)
	}
	// A trailing qualifier such as "1.10-beta" is not something PostgreSQL
	// produces for this extension, but truncating at the first non-digit is
	// cheaper than failing the whole statement path if it ever appears.
	minorDigits := minor
	for i, r := range minor {
		if r < '0' || r > '9' {
			minorDigits = minor[:i]
			break
		}
	}
	parsedMinor := 0
	if minorDigits != "" {
		parsedMinor, err = strconv.Atoi(minorDigits)
		if err != nil {
			return extensionVersion{}, fmt.Errorf("unexpected extension version %q: %w", s, err)
		}
	}
	return extensionVersion{major: maj, minor: parsedMinor}, nil
}

// extensionCapabilityCache resolves and remembers the pg_stat_statements
// capabilities for one connection.
//
// The lookup is a single catalog query, but it would otherwise run on every
// scrape of every statement path, so it is resolved once and reused. A client
// is bound to one database on one server, so the answer cannot change beneath
// it except by an ALTER EXTENSION, which is rare enough to be worth a
// reconnect.
type extensionCapabilityCache struct {
	err   error
	once  sync.Once
	value pgStatStatementsCapabilities
}

func (c *extensionCapabilityCache) get(ctx context.Context, q rowQuerier) (pgStatStatementsCapabilities, error) {
	c.once.Do(func() {
		c.value, c.err = readPgStatStatementsCapabilities(ctx, q)
	})
	return c.value, c.err
}

// rowQuerier is the subset of *IgnoredDB the lookup needs, so it can be
// exercised without a live connection.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// readPgStatStatementsCapabilities reads the installed extension's version and
// schema from the catalog.
//
// quote_ident is applied server-side so a schema needing quoting (mixed case,
// or a non-identifier character) comes back ready to interpolate.
func readPgStatStatementsCapabilities(ctx context.Context, q rowQuerier) (pgStatStatementsCapabilities, error) {
	const query = `SELECT e.extversion, quote_ident(n.nspname)
	FROM pg_extension e
	JOIN pg_namespace n ON n.oid = e.extnamespace
	WHERE e.extname = 'pg_stat_statements'`

	var version, schema string
	if err := q.QueryRowContext(ctx, query).Scan(&version, &schema); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Not an error: the extension simply is not installed here. The
			// caller decides whether that disables a path or moves on to the
			// next candidate database.
			return pgStatStatementsCapabilities{installed: false}, nil
		}
		return pgStatStatementsCapabilities{}, fmt.Errorf("unable to read pg_stat_statements extension version: %w", err)
	}

	parsed, err := parseExtensionVersion(version)
	if err != nil {
		return pgStatStatementsCapabilities{}, err
	}

	return pgStatStatementsCapabilities{
		installed: true,
		version:   parsed,
		schema:    schema,
	}, nil
}

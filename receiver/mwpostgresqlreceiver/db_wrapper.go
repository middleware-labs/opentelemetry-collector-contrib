// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"database/sql"
)

const otelIgnorePrefix = "/* otel-collector-ignore */ "

// IgnoredDB wraps a *sql.DB and automatically prefixes every query with
// /* otel-collector-ignore */ so the collector's own queries are excluded
// from pg_stat_statements (top_query) and pg_stat_activity (query_sample).
// This mirrors Datadog's /* DDIGNORE */ approach.
type IgnoredDB struct {
	db *sql.DB
}

// WrapDBWithIgnore wraps a *sql.DB so all queries are auto-prefixed.
func WrapDBWithIgnore(db *sql.DB) *IgnoredDB {
	return &IgnoredDB{db: db}
}

func (w *IgnoredDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return w.db.QueryContext(ctx, otelIgnorePrefix+query, args...)
}

func (w *IgnoredDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return w.db.QueryRowContext(ctx, otelIgnorePrefix+query, args...)
}

func (w *IgnoredDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return w.db.ExecContext(ctx, otelIgnorePrefix+query, args...)
}

// Unwrap returns the underlying *sql.DB for cases that need it directly.
func (w *IgnoredDB) Unwrap() *sql.DB {
	return w.db
}

// Conn returns a single connection from the underlying pool.
func (w *IgnoredDB) Conn(ctx context.Context) (*sql.Conn, error) {
	return w.db.Conn(ctx)
}

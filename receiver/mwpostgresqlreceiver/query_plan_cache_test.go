// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mwpostgresqlreceiver"

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// planPerDatabaseClient answers EXPLAIN with a plan naming the database it was
// asked in, so a plan served from the wrong database's cache entry is visible
// in the emitted event rather than having to be inferred.
type planPerDatabaseClient struct {
	client
	rows     []topQueryStatRow
	database string
	explains *[]string
}

func (c *planPerDatabaseClient) getTopQuery(context.Context, int64, databaseSelection, *zap.Logger) ([]topQueryStatRow, error) {
	return c.rows, nil
}

func (c *planPerDatabaseClient) explainQuery(string, string, *zap.Logger) (string, error) {
	*c.explains = append(*c.explains, c.database)
	return "plan for " + c.database, nil
}

func (*planPerDatabaseClient) Close() error { return nil }

type planPerDatabaseFactory struct {
	rows     []topQueryStatRow
	explains *[]string
}

func (f planPerDatabaseFactory) getClient(database string) (client, error) {
	return &planPerDatabaseClient{rows: f.rows, database: database, explains: f.explains}, nil
}

func (planPerDatabaseFactory) close() error { return nil }

// planRow builds a row for one statement in one database. The queryid is the
// same across databases, which is the point: pg_stat_statements normalizes the
// text, so the same statement really does carry the same queryid everywhere.
func planRow(database string, dbID int64, calls, execTimeMS float64) topQueryStatRow {
	return topQueryStatRow{
		calls:         sql.NullInt64{Int64: int64(calls), Valid: true},
		datname:       sql.NullString{String: database, Valid: true},
		query:         sql.NullString{String: "select * from orders where id = $1", Valid: true},
		queryID:       sql.NullInt64{Int64: 5150, Valid: true},
		rolname:       sql.NullString{String: "app", Valid: true},
		totalExecTime: sql.NullFloat64{Float64: execTimeMS, Valid: true},
		dbid:          sql.NullInt64{Int64: dbID, Valid: true},
		userid:        sql.NullInt64{Int64: 10, Valid: true},
		toplevel:      sql.NullBool{Bool: true, Valid: true},
	}
}

// TestQueryPlanCacheDoesNotReusePlansAcrossDatabases is the plan-cache fix.
//
// A queryid identifies a normalized statement, not a plan. The same text
// against two databases sees different tables, statistics and indexes, so it
// plans differently. The cache was keyed on the queryid alone, so whichever
// database was EXPLAINed first supplied the plan attached to every other
// database's copy of that statement - a wrong plan reported as that database's
// own, with nothing in the output to indicate it.
func TestQueryPlanCacheDoesNotReusePlansAcrossDatabases(t *testing.T) {
	scraper := newTestTopQueryScraper(t)

	var explains []string
	factory := planPerDatabaseFactory{explains: &explains}

	// The same statement in two databases. Both are in scope, so both are
	// eligible for EXPLAIN.
	baseline := []topQueryStatRow{
		planRow("orders_db", 1, 100, 1000),
		planRow("billing_db", 2, 100, 1000),
	}
	current := []topQueryStatRow{
		planRow("orders_db", 1, 110, 2000),
		planRow("billing_db", 2, 110, 2000),
	}

	factory.rows = baseline
	scraper.collectTopQuery(t.Context(), factory, 1000, 1000, 10, &errsMux{}, zap.NewNop())
	scraper.lb.Emit()

	factory.rows = current
	scraper.collectTopQuery(t.Context(), factory, 1000, 1000, 10, &errsMux{}, zap.NewNop())
	logs := scraper.lb.Emit()

	require.Equal(t, 2, logs.LogRecordCount(), "both databases' rows must be emitted")

	// Each database must have been EXPLAINed in its own right.
	assert.ElementsMatch(t, []string{"orders_db", "billing_db"}, explains,
		"each database must be EXPLAINed separately; a shared cache entry would "+
			"have skipped the second")

	// And each event must carry its own database's plan.
	records := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	got := map[string]string{}
	for i := 0; i < records.Len(); i++ {
		attrs := records.At(i).Attributes()
		database, ok := attrs.Get("db.namespace")
		require.True(t, ok)
		plan, ok := attrs.Get("postgresql.query_plan")
		require.True(t, ok)
		got[database.Str()] = plan.Str()
	}

	assert.Equal(t, map[string]string{
		"orders_db":  "plan for orders_db",
		"billing_db": "plan for billing_db",
	}, got, "each database's event must carry the plan EXPLAINed in that database")
}

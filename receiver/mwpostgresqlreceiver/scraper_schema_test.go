// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// setupCloudDetectorExpectations sets mock expectations for the full cloud detection chain
// (all 8 providers return false → self-hosted).
func setupCloudDetectorExpectations(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT EXISTS.*aurora_control_plane`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS.*rds_superuser`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS.*alloydb_version`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS.*cloudsql_postgres_advisory_locks`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS.*pg_roles.*azure`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS.*heroku_ext`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS.*crunchy_superuser`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(`SELECT EXISTS.*tembo`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
}

func TestScrapeSchemaCollection_Snapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	factory := &mockPostgreSQLClientFactory{db: db}

	cfg := createDefaultConfig().(*Config)
	cfg.SchemaCollection.Enabled = true
	cfg.Databases = []string{"postgres"}

	settings := receivertest.NewNopSettings(metadata.Type)
	core, observedLogs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	settings.Logger = logger

	scraper := newPostgreSQLScraper(settings, cfg, factory, newCache(1), newTTLCache[string](1, time.Second))

	// --- VersionDetector ---
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))

	// --- CloudDetector (self-hosted) ---
	setupCloudDetectorExpectations(mock)

	// --- DatabaseOIDQuery ---
	mock.ExpectQuery(`SELECT oid FROM pg_database`).
		WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(16384))

	// --- probeSizeFunctions: pg_total_size available ---
	mock.ExpectQuery(`SELECT pg_total_size`).
		WillReturnRows(sqlmock.NewRows([]string{"pg_total_size"}).AddRow(0))

	// --- TablesQuery (PG14 builder: 10 columns including total_size_bytes) ---
	mock.ExpectQuery("SELECT.*pg_class.*pg_namespace").WillReturnRows(sqlmock.NewRows([]string{
		"oid", "schema", "table", "type", "hasoids", "tablespace", "desc", "owner", "xmin", "total_size",
	}).AddRow(16385, "public", "test_table", "r", false, 0, nil, "postgres", 100, 2048))

	// --- ColumnsQuery ---
	mock.ExpectQuery("SELECT.*pg_attribute").WithArgs(uint32(16385)).WillReturnRows(sqlmock.NewRows([]string{
		"attnum", "name", "type", "typeoid", "mod", "notnull", "hasdef", "def", "desc", "coll", "xmin",
	}).AddRow(1, "id", "integer", 23, -1, true, false, nil, nil, 0, 100))

	// --- IndexesQuery (12 columns: includes index_size_bytes) ---
	// Returns empty: no indexes → collectIndexColumns and collectIndexStats are skipped
	mock.ExpectQuery("SELECT.*pg_index").WithArgs(uint32(16385)).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "table", "primary", "unique", "valid", "exclusion", "type", "def", "partial", "xmin", "size",
	}))

	// --- ConstraintsQuery ---
	mock.ExpectQuery("SELECT.*pg_constraint").WithArgs(uint32(16385)).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "type", "table", "def", "deferrable", "deferred", "validated",
	}).AddRow(1, "pk_test", "p", 16385, "PRIMARY KEY (id)", true, false, true))

	// --- TableStatsQuery (13 columns: includes total_size_bytes) ---
	mock.ExpectQuery("SELECT.*pg_stat_user_tables").WithArgs(uint32(16385)).WillReturnRows(sqlmock.NewRows([]string{
		"live", "dead", "mod", "vac", "autovac", "ana", "autoana", "seq", "seq_read", "idx", "idx_fetch", "size", "total_size",
	}).AddRow(100, 0, 0, nil, nil, nil, nil, 0, 0, 0, 0, 1024, 2048))

	// --- XminDetectionQuery (for UpdateSnapshot) ---
	// Already handled inside Collect() via changeTracker.UpdateSnapshot

	// --- Execute ---
	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)

	for _, entry := range observedLogs.All() {
		if entry.Level >= zap.WarnLevel {
			t.Logf("Log Entry: %s - %v", entry.Message, entry.Context)
		}
	}

	// New multi-record format: header + 1 table + footer = 3 records
	assert.Equal(t, 1, logs.ResourceLogs().Len())

	// Verify resource attributes (service.instance.id must be on all logs)
	ra := logs.ResourceLogs().At(0).Resource().Attributes()
	rval, rok := ra.Get("service.instance.id")
	assert.True(t, rok, "service.instance.id must be set on log resource")
	assert.NotEmpty(t, rval.Str())

	rval, rok = ra.Get("db.system.name")
	assert.True(t, rok, "db.system.name must be set on log resource")
	assert.Equal(t, "postgresql", rval.Str())

	scopeLogs := logs.ResourceLogs().At(0).ScopeLogs().At(0)
	assert.Equal(t, 3, scopeLogs.LogRecords().Len(), "expected header + 1 table + footer")

	// --- Verify header record ---
	header := scopeLogs.LogRecords().At(0)
	assert.Contains(t, header.Body().Str(), "Schema snapshot: postgres")
	assert.Contains(t, header.Body().Str(), "1 tables")
	ha := header.Attributes()
	val, ok := ha.Get("event.type")
	assert.True(t, ok)
	assert.Equal(t, "schema_collection_header", val.Str())

	val, ok = ha.Get("db.system.name")
	assert.True(t, ok)
	assert.Equal(t, "postgresql", val.Str())

	val, ok = ha.Get("db.namespace")
	assert.True(t, ok)
	assert.Equal(t, "postgres", val.Str())

	val, ok = ha.Get("db.oid")
	assert.True(t, ok)
	assert.Equal(t, int64(16384), val.Int())

	val, ok = ha.Get("collection.table_count")
	assert.True(t, ok)
	assert.Equal(t, int64(1), val.Int())

	val, ok = ha.Get("collection.column_count")
	assert.True(t, ok)
	assert.Equal(t, int64(1), val.Int())

	// --- Verify per-table record ---
	tableRec := scopeLogs.LogRecords().At(1)
	assert.Contains(t, tableRec.Body().Str(), "postgres.public.test_table")
	assert.Contains(t, tableRec.Body().Str(), "1 cols")
	assert.Contains(t, tableRec.Body().Str(), "100 rows")
	ta := tableRec.Attributes()

	val, ok = ta.Get("event.type")
	assert.True(t, ok)
	assert.Equal(t, "schema_table", val.Str())

	val, ok = ta.Get("table.oid")
	assert.True(t, ok)
	assert.Equal(t, int64(16385), val.Int())

	val, ok = ta.Get("table.name")
	assert.True(t, ok)
	assert.Equal(t, "test_table", val.Str())

	val, ok = ta.Get("table.schema_name")
	assert.True(t, ok)
	assert.Equal(t, "public", val.Str())

	val, ok = ta.Get("table.live_tuples")
	assert.True(t, ok)
	assert.Equal(t, int64(100), val.Int())

	val, ok = ta.Get("table.size_bytes")
	assert.True(t, ok)
	assert.Equal(t, int64(1024), val.Int())

	val, ok = ta.Get("table.total_size_bytes")
	assert.True(t, ok)
	assert.Equal(t, int64(2048), val.Int())

	// Columns/indexes/constraints as JSON strings (namespaced under table.*)
	val, ok = ta.Get("table.columns")
	assert.True(t, ok)
	assert.Contains(t, val.Str(), `"name":"id"`)
	assert.Contains(t, val.Str(), `"type_name":"integer"`)
	assert.Contains(t, val.Str(), `"not_null":true`)

	val, ok = ta.Get("table.constraints")
	assert.True(t, ok)
	assert.Contains(t, val.Str(), `"name":"pk_test"`)
	assert.Contains(t, val.Str(), `"type":"p"`)

	// --- Verify footer record ---
	footer := scopeLogs.LogRecords().At(2)
	assert.Contains(t, footer.Body().Str(), "Schema snapshot complete: postgres")
	assert.Contains(t, footer.Body().Str(), "1 tables collected")
	fa := footer.Attributes()
	val, ok = fa.Get("event.type")
	assert.True(t, ok)
	assert.Equal(t, "schema_collection_footer", val.Str())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %s", err)
	}
}

func TestScrapeSchemaCollection_ExcludeTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	factory := &mockPostgreSQLClientFactory{db: db}

	cfg := createDefaultConfig().(*Config)
	cfg.SchemaCollection.Enabled = true
	cfg.Databases = []string{"postgres"}
	cfg.SchemaCollection.ExcludeTables = []string{"public.test_table"}

	settings := receivertest.NewNopSettings(metadata.Type)
	scraper := newPostgreSQLScraper(settings, cfg, factory, newCache(1), newTTLCache[string](1, time.Second))

	// Version Detector
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))

	// Cloud Detector
	setupCloudDetectorExpectations(mock)

	// DatabaseOIDQuery
	mock.ExpectQuery(`SELECT oid FROM pg_database`).
		WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(16384))

	// probeSizeFunctions: pg_total_size available
	mock.ExpectQuery(`SELECT pg_total_size`).
		WillReturnRows(sqlmock.NewRows([]string{"pg_total_size"}).AddRow(0))

	// Tables Query (table gets excluded by filter)
	mock.ExpectQuery("SELECT.*pg_class.*pg_namespace").WillReturnRows(sqlmock.NewRows([]string{
		"oid", "schema", "table", "type", "hasoids", "tablespace", "desc", "owner", "xmin", "total_size",
	}).AddRow(16385, "public", "test_table", "r", false, 0, nil, "postgres", 100, 1024))

	// No column/index/constraint/stats queries expected because the table is excluded

	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)

	// header + footer = 2 records (no table records since excluded)
	assert.Equal(t, 1, logs.ResourceLogs().Len())
	scopeLogs := logs.ResourceLogs().At(0).ScopeLogs().At(0)
	assert.Equal(t, 2, scopeLogs.LogRecords().Len(), "expected header + footer (no table records)")

	header := scopeLogs.LogRecords().At(0)
	ha := header.Attributes()
	val, ok := ha.Get("collection.table_count")
	assert.True(t, ok)
	assert.Equal(t, int64(0), val.Int())

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %s", err)
	}
}

func TestScrapeSchemaCollection_ReltupplesFallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	factory := &mockPostgreSQLClientFactory{db: db}

	cfg := createDefaultConfig().(*Config)
	cfg.SchemaCollection.Enabled = true
	cfg.Databases = []string{"postgres"}

	settings := receivertest.NewNopSettings(metadata.Type)
	scraper := newPostgreSQLScraper(settings, cfg, factory, newCache(1), newTTLCache[string](1, time.Second))

	// --- VersionDetector ---
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))

	// --- CloudDetector (self-hosted) ---
	setupCloudDetectorExpectations(mock)

	// --- DatabaseOIDQuery ---
	mock.ExpectQuery(`SELECT oid FROM pg_database`).
		WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(16384))

	// --- probeSizeFunctions ---
	mock.ExpectQuery(`SELECT pg_total_size`).
		WillReturnRows(sqlmock.NewRows([]string{"pg_total_size"}).AddRow(0))

	// --- TablesQuery ---
	mock.ExpectQuery("SELECT.*pg_class.*pg_namespace").WillReturnRows(sqlmock.NewRows([]string{
		"oid", "schema", "table", "type", "hasoids", "tablespace", "desc", "owner", "xmin", "total_size",
	}).AddRow(16385, "public", "test_table", "r", false, 0, nil, "postgres", 100, 2048))

	// --- ColumnsQuery ---
	mock.ExpectQuery("SELECT.*pg_attribute").WithArgs(uint32(16385)).WillReturnRows(sqlmock.NewRows([]string{
		"attnum", "name", "type", "typeoid", "mod", "notnull", "hasdef", "def", "desc", "coll", "xmin",
	}).AddRow(1, "id", "integer", 23, -1, true, false, nil, nil, 0, 100))

	// --- IndexesQuery (empty) ---
	mock.ExpectQuery("SELECT.*pg_index").WithArgs(uint32(16385)).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "table", "primary", "unique", "valid", "exclusion", "type", "def", "partial", "xmin", "size",
	}))

	// --- ConstraintsQuery ---
	mock.ExpectQuery("SELECT.*pg_constraint").WithArgs(uint32(16385)).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "type", "table", "def", "deferrable", "deferred", "validated",
	}))

	// --- TableStatsQuery: n_live_tup = 0 (ANALYZE hasn't run) ---
	mock.ExpectQuery("SELECT.*pg_stat_user_tables").WithArgs(uint32(16385)).WillReturnRows(sqlmock.NewRows([]string{
		"live", "dead", "mod", "vac", "autovac", "ana", "autoana", "seq", "seq_read", "idx", "idx_fetch", "size", "total_size",
	}).AddRow(0, 0, 0, nil, nil, nil, nil, 0, 0, 0, 0, 1024, 2048))

	// --- fillLiveTuplesFromReltuples: fallback query returns reltuples = 3260 ---
	mock.ExpectQuery("SELECT c.reltuples FROM pg_class").WithArgs(uint32(16385)).WillReturnRows(
		sqlmock.NewRows([]string{"reltuples"}).AddRow(3260.0))

	// --- Execute ---
	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)

	// Verify table record has fallback live_tuples from reltuples
	assert.Equal(t, 1, logs.ResourceLogs().Len())
	scopeLogs := logs.ResourceLogs().At(0).ScopeLogs().At(0)
	assert.Equal(t, 3, scopeLogs.LogRecords().Len(), "expected header + 1 table + footer")

	tableRec := scopeLogs.LogRecords().At(1)
	ta := tableRec.Attributes()

	val, ok := ta.Get("table.live_tuples")
	assert.True(t, ok)
	assert.Equal(t, int64(3260), val.Int(), "live_tuples should fall back to reltuples when n_live_tup is 0")

	// Body should mention 3260 rows
	assert.Contains(t, tableRec.Body().Str(), "3260 rows")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %s", err)
	}
}

// Mock Factory returning real *postgreSQLClient with mock DB
type mockPostgreSQLClientFactory struct {
	db *sql.DB
}

func (m *mockPostgreSQLClientFactory) getClient(database string) (client, error) {
	return &postgreSQLClient{
		client:  WrapDBWithIgnore(m.db),
		closeFn: func() error { return nil },
	}, nil
}

func (m *mockPostgreSQLClientFactory) close() error {
	return nil
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// expectSchemaCollection queues the query sequence for one full schema
// collection of a single-table database.
func expectSchemaCollection(mock sqlmock.Sqlmock, tableOID uint32, xmin uint32) {
	mock.ExpectQuery(`SELECT oid FROM pg_database`).
		WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(16384))

	mock.ExpectQuery(`SELECT pg_total_size`).
		WillReturnRows(sqlmock.NewRows([]string{"pg_total_size"}).AddRow(0))

	mock.ExpectQuery("SELECT.*pg_class.*pg_namespace").WillReturnRows(sqlmock.NewRows([]string{
		"oid", "schema", "table", "type", "hasoids", "tablespace", "desc", "owner", "xmin", "total_size",
	}).AddRow(tableOID, "public", "test_table", "r", false, 0, nil, "postgres", xmin, 2048))

	mock.ExpectQuery("SELECT.*pg_attribute").WithArgs(tableOID).WillReturnRows(sqlmock.NewRows([]string{
		"attnum", "name", "type", "typeoid", "mod", "notnull", "hasdef", "def", "desc", "coll", "xmin",
	}).AddRow(1, "id", "integer", 23, -1, true, false, nil, nil, 0, xmin))

	mock.ExpectQuery("SELECT.*pg_index").WithArgs(tableOID).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "table", "primary", "unique", "valid", "exclusion", "type", "def", "partial", "xmin", "size",
	}))

	mock.ExpectQuery("SELECT.*pg_constraint").WithArgs(tableOID).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "name", "type", "table", "def", "deferrable", "deferred", "validated",
	}))

	mock.ExpectQuery("SELECT.*pg_stat_user_tables").WithArgs(tableOID).WillReturnRows(sqlmock.NewRows([]string{
		"live", "dead", "mod", "vac", "autovac", "ana", "autoana", "seq", "seq_read", "idx", "idx_fetch", "size", "total_size",
	}).AddRow(100, 0, 0, nil, nil, nil, nil, 0, 0, 0, 0, 1024, 2048))
}

// TestSchemaCollectionSkipsUnchangedDatabase is the measurable claim of the
// per-database gate: once a database has been collected, a later cycle that
// finds no xmin change must not collect it again.
//
// Before this change the collect/skip decision was global and xmin detection
// ran only against the default database, so a change anywhere — or, since the
// snapshot was overwritten by each database in turn, effectively every cycle —
// re-collected the full schema of every database. That is the work the customer
// was paying for continuously.
func TestSchemaCollectionSkipsUnchangedDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	factory := &mockPostgreSQLClientFactory{db: db}

	cfg := createDefaultConfig().(*Config)
	cfg.SchemaCollection.Enabled = true
	cfg.SchemaCollection.CollectionInterval = time.Nanosecond // never throttle
	cfg.Databases = []string{"postgres"}

	settings := receivertest.NewNopSettings(metadata.Type)
	settings.Logger = zap.NewNop()

	scraper := newPostgreSQLScraper(settings, cfg, factory, newCache(1), newTTLCache[string](1, time.Second))

	const tableOID = uint32(16385)
	const xmin = uint32(100)

	// --- Cycle 1: never snapshotted, so a full collection happens. ---
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))
	setupCloudDetectorExpectations(mock)
	expectSchemaCollection(mock, tableOID, xmin)

	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, logs.ResourceLogs().Len(), "first cycle must collect")

	// --- Cycle 2: xmin unchanged, so only detection runs and nothing is
	// collected. Note the absence of any table/column/index expectations: if
	// the gate failed to skip, those queries would be unexpected and the mock
	// would error. ---
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))
	setupCloudDetectorExpectations(mock)
	mock.ExpectQuery(`SELECT c\.oid, c\.xmin`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "xmin"}).AddRow(tableOID, xmin))

	logs, err = scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, logs.ResourceLogs().Len(),
		"unchanged database must be skipped, not re-collected")

	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSchemaCollectionRecollectsChangedDatabase is the other half: a real xmin
// change must still trigger a full collection, so the skip above is not simply
// the receiver going quiet.
func TestSchemaCollectionRecollectsChangedDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	factory := &mockPostgreSQLClientFactory{db: db}

	cfg := createDefaultConfig().(*Config)
	cfg.SchemaCollection.Enabled = true
	cfg.SchemaCollection.CollectionInterval = time.Nanosecond
	cfg.Databases = []string{"postgres"}

	settings := receivertest.NewNopSettings(metadata.Type)
	settings.Logger = zap.NewNop()

	scraper := newPostgreSQLScraper(settings, cfg, factory, newCache(1), newTTLCache[string](1, time.Second))

	const tableOID = uint32(16385)

	// --- Cycle 1: initial collection at xmin 100. ---
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))
	setupCloudDetectorExpectations(mock)
	expectSchemaCollection(mock, tableOID, 100)

	_, err = scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)

	// --- Cycle 2: the table's xmin moved, so it is collected again. ---
	mock.ExpectQuery(`SELECT version\(\), current_setting`).
		WillReturnRows(sqlmock.NewRows([]string{"version", "num"}).AddRow("PostgreSQL 15.0", 150000))
	setupCloudDetectorExpectations(mock)
	mock.ExpectQuery(`SELECT c\.oid, c\.xmin`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "xmin"}).AddRow(tableOID, 200))
	expectSchemaCollection(mock, tableOID, 200)

	logs, err := scraper.scrapeSchemaCollection(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, logs.ResourceLogs().Len(),
		"a changed database must be re-collected")

	require.NoError(t, mock.ExpectationsWereMet())
}

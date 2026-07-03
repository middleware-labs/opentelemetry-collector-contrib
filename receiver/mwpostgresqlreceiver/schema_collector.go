// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/lib/pq"
)

// SchemaCollector is the main schema collection engine
type SchemaCollector struct {
	db                  *IgnoredDB
	versionInfo         *VersionInfo
	cloudProvider       CloudProvider
	cloudMetadata       *CloudMetadata
	sqlBuilder          SQLBuilder
	changeTracker       *XminChangeTracker
	settingsCollector   *SettingsCollector
	extensionsCollector *ExtensionsCollector

	// Configuration
	config  *CollectorConfig
	filters *FilterConfig

	// State
	lastCollectionTime time.Time
	lastCollectionSize int64
	collectionErrors   []ErrorInfo
	sizeMode           sizeQueryMode
	sizeProbed         bool
	mu                 sync.RWMutex

	// Metrics
	collectionDuration time.Duration
	tableCount         int
	columnCount        int
	indexCount         int

	logger Logger
}

// NewSchemaCollector creates a new schema collector
func NewSchemaCollector(
	db *IgnoredDB,
	versionInfo *VersionInfo,
	cloudProvider CloudProvider,
	cloudMetadata *CloudMetadata,
	changeTracker *XminChangeTracker,
	config *CollectorConfig,
	filters *FilterConfig,
	logger Logger,
) (*SchemaCollector, error) {
	sqlBuilder := NewSQLBuilder(versionInfo.VersionNum)

	collector := &SchemaCollector{
		db:                  db,
		versionInfo:         versionInfo,
		cloudProvider:       cloudProvider,
		cloudMetadata:       cloudMetadata,
		sqlBuilder:          sqlBuilder,
		changeTracker:       changeTracker,
		settingsCollector:   NewSettingsCollector(db, logger),
		extensionsCollector: NewExtensionsCollector(db, logger),
		config:              config,
		filters:             filters,
		logger:              logger,
		collectionErrors:    []ErrorInfo{},
	}

	return collector, nil
}

type sizeQueryMode int

const (
	sizeDirect    sizeQueryMode = iota // pg_total_size(oid) — standard
	sizeQualified                      // pg_catalog.pg_total_size(oid) — explicit schema
	sizeEstimate                       // relpages * block_size — catalog-only estimate
)

// probeSizeFunctions determines the best available method for querying relation
// sizes. Tries direct calls first, then schema-qualified calls (fixes search_path
// issues with PgBouncer), then falls back to relpages-based estimates.
func (c *SchemaCollector) probeSizeFunctions(ctx context.Context) {
	if c.sizeProbed {
		return
	}
	c.sizeProbed = true

	var dummy int64

	// Tier 1: unqualified — works on standard PostgreSQL
	if err := c.db.QueryRowContext(ctx,
		"SELECT pg_total_size(oid) FROM pg_class LIMIT 1").Scan(&dummy); err == nil {
		c.sizeMode = sizeDirect
		return
	}

	// Tier 2: schema-qualified — fixes missing search_path (PgBouncer, etc.)
	if err := c.db.QueryRowContext(ctx,
		"SELECT pg_catalog.pg_total_size(oid) FROM pg_class LIMIT 1").Scan(&dummy); err == nil {
		c.sizeMode = sizeQualified
		c.logger.Warn("pg_total_size requires schema qualification, using pg_catalog.pg_total_size")
		return
	}

	// Tier 3: relpages estimate
	c.sizeMode = sizeEstimate
	c.logger.Warn("pg_total_size not available, using relpages-based size estimates")
}

func (c *SchemaCollector) totalSizeExpr(arg string) string {
	switch c.sizeMode {
	case sizeQualified:
		return fmt.Sprintf("pg_catalog.pg_total_size(%s)", arg)
	case sizeEstimate:
		return fmt.Sprintf("(c.relpages * current_setting('block_size')::bigint)")
	default:
		return fmt.Sprintf("pg_total_size(%s)", arg)
	}
}

func (c *SchemaCollector) tablesQueryForMode() string {
	switch c.sizeMode {
	case sizeQualified:
		return c.sqlBuilder.TablesQueryQualified()
	case sizeEstimate:
		return c.sqlBuilder.TablesQueryEstimate()
	default:
		return c.sqlBuilder.TablesQuery()
	}
}

func (c *SchemaCollector) tableStatsQueryForMode() string {
	switch c.sizeMode {
	case sizeQualified:
		return c.sqlBuilder.TableStatsQueryQualified()
	case sizeEstimate:
		return c.sqlBuilder.TableStatsQueryEstimate()
	default:
		return c.sqlBuilder.TableStatsQuery()
	}
}

func (c *SchemaCollector) indexesQueryForMode() string {
	switch c.sizeMode {
	case sizeQualified:
		return c.sqlBuilder.IndexesQueryQualified()
	case sizeEstimate:
		return c.sqlBuilder.IndexesQueryEstimate()
	default:
		return c.sqlBuilder.IndexesQuery()
	}
}

// Collect performs a full schema collection
func (c *SchemaCollector) Collect(ctx context.Context) (*SchemaCollectionEvent, error) {
	startTime := time.Now()
	c.probeSizeFunctions(ctx)

	event := &SchemaCollectionEvent{
		DatabaseName:         c.config.DatabaseName,
		DatabaseOID:          c.config.DatabaseOID,
		PostgresVersion:      c.versionInfo.VersionNum,
		VersionString:        c.versionInfo.VersionString,
		CloudProvider:        string(c.cloudProvider),
		CollectionType:       CollectionTypeSnapshot,
		CollectedAt:          startTime,
		CollectedAtTimestamp: startTime.UnixMilli(),
		SchemaVersion:        c.changeTracker.GetSchemaVersion(),
		Tables:               []*TableDefinition{},
		Statistics: &CollectionStatistics{
			CompletionRatio: 1.0,
		},
	}

	// 1. Collect tables
	tables, err := c.collectTables(ctx)
	if err != nil {
		c.recordError(ErrorInfo{
			Category:  "tables",
			Severity:  "error",
			Message:   fmt.Sprintf("failed to collect tables: %v", err),
			Timestamp: time.Now(),
		})
		if !c.config.ContinueOnError {
			return nil, err
		}
	}
	event.Tables = tables

	// 2. Enrich with columns, indexes, constraints, stats
	for _, table := range event.Tables {
		c.enrichTableDefinition(ctx, table)
	}

	// 3. Collect extensions if enabled
	if c.config.CollectExtensions {
		extensionsSnapshot, err := c.extensionsCollector.Collect(ctx)
		if err == nil {
			for _, ext := range extensionsSnapshot.Extensions {
				event.Extensions = append(event.Extensions, ext)
			}
		}
	}

	// 4. Collect settings if enabled
	if c.config.CollectSettings {
		settingsSnapshot, err := c.settingsCollector.Collect(ctx)
		if err == nil {
			event.Settings = settingsSnapshot.Settings
		}
	}

	// 5. Update change tracker with full snapshot
	xminMap := make(map[uint32]uint32)
	for _, table := range event.Tables {
		xminMap[table.OID] = table.Xmin
	}
	c.changeTracker.UpdateSnapshot(xminMap)

	// 6. Set statistics
	duration := time.Since(startTime)
	event.Statistics.CollectionDurationMs = duration.Milliseconds()
	event.Statistics.TableCount = int32(len(event.Tables))
	event.Statistics.ExtensionCount = int32(len(event.Extensions))
	event.Statistics.HasErrors = len(c.collectionErrors) > 0
	event.Statistics.ErrorCount = int32(len(c.collectionErrors))

	var colCount, idxCount, constrCount int32
	var totalSize, totalRows int64
	for _, t := range event.Tables {
		colCount += int32(len(t.Columns))
		idxCount += int32(len(t.Indexes))
		constrCount += int32(len(t.Constraints))
		totalSize += t.TotalSizeBytes
		totalRows += t.LiveTuples
	}
	event.Statistics.ColumnCount = colCount
	event.Statistics.IndexCount = idxCount
	event.Statistics.ConstraintCount = constrCount
	event.Statistics.TotalSizeBytes = totalSize
	event.Statistics.TotalRowCount = totalRows
	event.SchemaVersion = c.changeTracker.GetSchemaVersion()

	c.mu.Lock()
	c.lastCollectionTime = startTime
	c.collectionDuration = duration
	c.tableCount = len(event.Tables)
	c.mu.Unlock()

	return event, nil
}

// DetectChanges detects schema changes using xmin-based detection
func (c *SchemaCollector) DetectChanges(ctx context.Context) ([]uint32, error) {
	changed := []uint32{}

	rows, err := c.db.QueryContext(ctx, c.sqlBuilder.XminDetectionQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to detect changes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var oid uint32
		var xmin uint32
		if err := rows.Scan(&oid, &xmin); err != nil {
			c.logger.Warn("failed to scan xmin", "error", err)
			continue
		}

		if c.changeTracker.HasChanged(oid, xmin) {
			changed = append(changed, oid)
		}
	}

	return changed, rows.Err()
}

// collectTables collects all tables from the database
func (c *SchemaCollector) collectTables(ctx context.Context) ([]*TableDefinition, error) {
	tables := []*TableDefinition{}

	query := c.tablesQueryForMode()
	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			oid            uint32
			schemaName     string
			tableName      string
			tableType      string
			hasOids        bool
			tablespace     uint32
			description    sql.NullString
			owner          sql.NullString
			xmin           uint32
			totalSizeBytes sql.NullInt64
		)

		if err := rows.Scan(&oid, &schemaName, &tableName, &tableType, &hasOids, &tablespace,
			&description, &owner, &xmin, &totalSizeBytes); err != nil {
			c.logger.Warn("failed to scan table", "error", err)
			continue
		}

		if c.shouldExcludeTable(schemaName, tableName) {
			continue
		}

		table := &TableDefinition{
			OID:         oid,
			SchemaName:  schemaName,
			Name:        tableName,
			Type:        tableType,
			Tablespace:  tablespace,
			HasOids:     hasOids,
			Xmin:        xmin,
			Columns:     []*ColumnDefinition{},
			Indexes:     []*IndexDefinition{},
			Constraints: []*ConstraintDefinition{},
		}

		if description.Valid {
			table.Description = description.String
		}
		if owner.Valid {
			table.Owner = owner.String
		}
		if totalSizeBytes.Valid {
			table.TotalSizeBytes = totalSizeBytes.Int64
		}

		tables = append(tables, table)
	}

	return tables, rows.Err()
}

// enrichTableDefinition enriches a table with columns, indexes, constraints, and stats
func (c *SchemaCollector) enrichTableDefinition(ctx context.Context, table *TableDefinition) {
	columns, err := c.collectColumns(ctx, table.OID)
	if err != nil {
		c.logger.Warn("failed to collect columns", "table", table.Name, "error", err)
	}
	table.Columns = columns

	indexes, err := c.collectIndexes(ctx, table.OID)
	if err != nil {
		c.logger.Warn("failed to collect indexes", "table", table.Name, "error", err)
	}
	table.Indexes = indexes

	// Populate index column names
	c.collectIndexColumns(ctx, table)

	// Collect index statistics
	for _, idx := range table.Indexes {
		c.collectIndexStats(ctx, idx)
	}

	constraints, err := c.collectConstraints(ctx, table.OID)
	if err != nil {
		c.logger.Warn("failed to collect constraints", "table", table.Name, "error", err)
	}
	table.Constraints = constraints

	// Enrich FK constraints with referenced table/column details
	c.collectForeignKeyDetails(ctx, table)

	// Collect table stats (live/dead tuples, size, vacuum times) for user and partitioned tables.
	// pg_stat_user_tables only tracks 'r' (regular) and 'p' (partitioned) tables.
	// If that fails (e.g. permissions, PgBouncer), we fall back to pg_class.reltuples inside collectTableStats.
	// For materialized views ('m') use reltuples from pg_class only.
	if table.Type == "r" || table.Type == "p" {
		c.collectTableStats(ctx, table)
	} else if table.Type == "m" {
		c.collectMatViewStats(ctx, table)
	}

	// Collect view definition for views and materialized views
	if table.Type == "v" || table.Type == "m" {
		c.collectViewDefinition(ctx, table)
	}

	// Collect column statistics if configured
	if c.config.CollectColumnStats && (table.Type == "r" || table.Type == "p") {
		c.collectColumnStats(ctx, table)
	}
}

// collectTableByOID collects a single table by OID using version-aware SQL
func (c *SchemaCollector) collectTableByOID(ctx context.Context, oid uint32) (*TableDefinition, error) {
	var (
		schemaName     string
		tableName      string
		tableType      string
		hasOids        bool
		tablespace     uint32
		description    sql.NullString
		owner          sql.NullString
		xmin           uint32
		totalSizeBytes sql.NullInt64
	)

	hasOidsExpr := "false"
	if c.sqlBuilder.MajorVersion() < 12 {
		hasOidsExpr = "c.relhasoids"
	}
	query := fmt.Sprintf(`
		SELECT
			ns.nspname, c.relname, c.relkind::text, %s,
			c.reltablespace, obj_description(c.oid, 'pg_class'),
			pg_roles.rolname, c.xmin, %s
		FROM pg_class c
		JOIN pg_namespace ns ON c.relnamespace = ns.oid
		LEFT JOIN pg_roles ON c.relowner = pg_roles.oid
		WHERE c.oid = $1
	`, hasOidsExpr, c.totalSizeExpr("c.oid"))

	err := c.db.QueryRowContext(ctx, query, oid).
		Scan(&schemaName, &tableName, &tableType, &hasOids, &tablespace,
			&description, &owner, &xmin, &totalSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to collect table by OID %d: %w", oid, err)
	}

	table := &TableDefinition{
		OID:         oid,
		SchemaName:  schemaName,
		Name:        tableName,
		Type:        tableType,
		Tablespace:  tablespace,
		HasOids:     hasOids,
		Xmin:        xmin,
		Columns:     []*ColumnDefinition{},
		Indexes:     []*IndexDefinition{},
		Constraints: []*ConstraintDefinition{},
	}

	if description.Valid {
		table.Description = description.String
	}
	if owner.Valid {
		table.Owner = owner.String
	}
	if totalSizeBytes.Valid {
		table.TotalSizeBytes = totalSizeBytes.Int64
	}

	c.enrichTableDefinition(ctx, table)

	return table, nil
}

// collectColumns collects columns for a table
func (c *SchemaCollector) collectColumns(ctx context.Context, tableOID uint32) ([]*ColumnDefinition, error) {
	columns := []*ColumnDefinition{}

	rows, err := c.db.QueryContext(ctx, c.sqlBuilder.ColumnsQuery(), tableOID)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			attnum       int16
			name         string
			typeName     string
			typeOID      uint32
			typeModifier int32
			notNull      bool
			hasDefault   bool
			defaultValue sql.NullString
			description  sql.NullString
			collation    uint32
			xmin         uint32
		)

		if err := rows.Scan(&attnum, &name, &typeName, &typeOID, &typeModifier,
			&notNull, &hasDefault, &defaultValue, &description, &collation, &xmin); err != nil {
			c.logger.Warn("failed to scan column", "error", err)
			continue
		}

		col := &ColumnDefinition{
			OID:          typeOID,
			Name:         name,
			TypeName:     typeName,
			TypeOID:      typeOID,
			TypeModifier: typeModifier,
			NotNull:      notNull,
			HasDefault:   hasDefault,
			Collation:    collation,
			Xmin:         xmin,
			Position:     attnum,
		}

		if defaultValue.Valid {
			col.DefaultValue = defaultValue.String
		}
		if description.Valid {
			col.Description = description.String
		}

		columns = append(columns, col)
	}

	return columns, rows.Err()
}

// collectIndexes collects indexes for a table
func (c *SchemaCollector) collectIndexes(ctx context.Context, tableOID uint32) ([]*IndexDefinition, error) {
	indexes := []*IndexDefinition{}

	idxQuery := c.indexesQueryForMode()
	rows, err := c.db.QueryContext(ctx, idxQuery, tableOID)
	if err != nil {
		return nil, fmt.Errorf("failed to query indexes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			oid        uint32
			name       string
			table      uint32
			isPrimary  bool
			isUnique   bool
			isValid    bool
			isExcl     bool
			indexType  string
			definition string
			partial    sql.NullString
			xmin       uint32
			sizeBytes  sql.NullInt64
		)

		if err := rows.Scan(&oid, &name, &table, &isPrimary, &isUnique, &isValid,
			&isExcl, &indexType, &definition, &partial, &xmin, &sizeBytes); err != nil {
			c.logger.Warn("failed to scan index", "error", err)
			continue
		}

		idx := &IndexDefinition{
			OID:         oid,
			Name:        name,
			TableOID:    table,
			IsPrimary:   isPrimary,
			IsUnique:    isUnique,
			IsValid:     isValid,
			IsExclusion: isExcl,
			IndexType:   indexType,
			Definition:  definition,
			Xmin:        xmin,
		}

		if partial.Valid {
			idx.Partial = partial.String
		}
		if sizeBytes.Valid {
			idx.SizeBytes = sizeBytes.Int64
		}

		indexes = append(indexes, idx)
	}

	return indexes, rows.Err()
}

// collectIndexColumns populates index column names for all indexes on a table
func (c *SchemaCollector) collectIndexColumns(ctx context.Context, table *TableDefinition) {
	if len(table.Indexes) == 0 {
		return
	}

	rows, err := c.db.QueryContext(ctx, c.sqlBuilder.IndexColumnsQuery(), table.OID)
	if err != nil {
		c.logger.Warn("failed to collect index columns", "table", table.Name, "error", err)
		return
	}
	defer rows.Close()

	colMap := make(map[uint32][]string)
	for rows.Next() {
		var indexOID uint32
		var colNames []string
		if err := rows.Scan(&indexOID, pq.Array(&colNames)); err != nil {
			c.logger.Warn("failed to scan index columns", "error", err)
			continue
		}
		colMap[indexOID] = colNames
	}

	for _, idx := range table.Indexes {
		if cols, ok := colMap[idx.OID]; ok {
			idx.Columns = cols
		}
	}
}

// collectIndexStats collects statistics for a single index
func (c *SchemaCollector) collectIndexStats(ctx context.Context, idx *IndexDefinition) {
	if c.sqlBuilder.MajorVersion() >= 16 {
		// PG16+ has last_idx_scan
		var lastScan sql.NullTime
		err := c.db.QueryRowContext(ctx, c.sqlBuilder.IndexStatsQuery(), idx.OID).
			Scan(&idx.ScanCount, &idx.TupleRead, &idx.TupleFetch, &lastScan)
		if err != nil {
			return
		}
		if lastScan.Valid {
			idx.LastScan = &lastScan.Time
		}
	} else {
		// PG10-15: no last_idx_scan
		err := c.db.QueryRowContext(ctx, c.sqlBuilder.IndexStatsQuery(), idx.OID).
			Scan(&idx.ScanCount, &idx.TupleRead, &idx.TupleFetch)
		if err != nil {
			return
		}
	}
}

// collectConstraints collects constraints for a table
func (c *SchemaCollector) collectConstraints(ctx context.Context, tableOID uint32) ([]*ConstraintDefinition, error) {
	constraints := []*ConstraintDefinition{}

	rows, err := c.db.QueryContext(ctx, c.sqlBuilder.ConstraintsQuery(), tableOID)
	if err != nil {
		return nil, fmt.Errorf("failed to query constraints: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			oid        uint32
			name       string
			conType    string
			table      uint32
			definition string
			deferrable bool
			deferred   bool
			validated  bool
		)

		if err := rows.Scan(&oid, &name, &conType, &table, &definition, &deferrable, &deferred, &validated); err != nil {
			c.logger.Warn("failed to scan constraint", "error", err)
			continue
		}

		constraint := &ConstraintDefinition{
			OID:        oid,
			Name:       name,
			Type:       conType,
			TableOID:   table,
			Definition: definition,
			Deferrable: deferrable,
			Deferred:   deferred,
			Validated:  validated,
		}

		constraints = append(constraints, constraint)
	}

	return constraints, rows.Err()
}

// collectForeignKeyDetails enriches FK constraints with referenced table/column info
func (c *SchemaCollector) collectForeignKeyDetails(ctx context.Context, table *TableDefinition) {
	hasFKs := false
	for _, con := range table.Constraints {
		if con.Type == "f" {
			hasFKs = true
			break
		}
	}
	if !hasFKs {
		return
	}

	rows, err := c.db.QueryContext(ctx, c.sqlBuilder.ForeignKeyDetailsQuery(), table.OID)
	if err != nil {
		c.logger.Warn("failed to collect FK details", "table", table.Name, "error", err)
		return
	}
	defer rows.Close()

	fkDetails := make(map[uint32]struct {
		refTableOID uint32
		colNames    []string
		refCols     []string
	})

	for rows.Next() {
		var (
			oid         uint32
			name        string
			tableOID    uint32
			refTableOID uint32
			colNames    []string
			refCols     []string
		)
		_ = name
		_ = tableOID

		if err := rows.Scan(&oid, &name, &tableOID, &refTableOID,
			pq.Array(&colNames), pq.Array(&refCols)); err != nil {
			c.logger.Warn("failed to scan FK details", "error", err)
			continue
		}

		fkDetails[oid] = struct {
			refTableOID uint32
			colNames    []string
			refCols     []string
		}{refTableOID, colNames, refCols}
	}

	for _, con := range table.Constraints {
		if detail, ok := fkDetails[con.OID]; ok {
			con.ReferencedTableOID = detail.refTableOID
			con.ColumnNames = detail.colNames
			con.ReferencedColumns = detail.refCols
		}
	}
}

// collectTableStats collects statistics for a table
func (c *SchemaCollector) collectTableStats(ctx context.Context, table *TableDefinition) {
	var (
		liveTuples      int64
		deadTuples      int64
		modSinceAnalyze int64
		lastVacuum      sql.NullTime
		lastAutovacuum  sql.NullTime
		lastAnalyze     sql.NullTime
		lastAutoanalyze sql.NullTime
		seqScans        int64
		seqTupRead      int64
		idxScans        int64
		idxTupFetch     int64
		sizeBytes       int64
		totalSizeBytes  int64
	)

	statsQuery := c.tableStatsQueryForMode()
	err := c.db.QueryRowContext(ctx, statsQuery, table.OID).
		Scan(&liveTuples, &deadTuples, &modSinceAnalyze, &lastVacuum, &lastAutovacuum,
			&lastAnalyze, &lastAutoanalyze, &seqScans, &seqTupRead, &idxScans, &idxTupFetch,
			&sizeBytes, &totalSizeBytes)

	if err != nil {
		c.logger.Warn("failed to collect table stats; live_tuples left 0 (actual count only)", "table", table.Name, "error", err)
		// Optional: still try to get size from pg_class so we have size even when stats are unavailable
		c.collectRowEstimateFromPgClass(ctx, table)
		return
	}

	table.LiveTuples = liveTuples
	// If n_live_tup is 0 (ANALYZE hasn't run yet), fall back to reltuples estimate
	if liveTuples == 0 {
		c.fillLiveTuplesFromReltuples(ctx, table)
	}
	table.DeadTuples = deadTuples
	table.ModSinceAnalyze = modSinceAnalyze
	table.SeqScans = seqScans
	table.SeqTupRead = seqTupRead
	table.IndexScans = idxScans
	table.IndexTupFetch = idxTupFetch
	table.SizeBytes = sizeBytes
	table.TotalSizeBytes = totalSizeBytes

	if lastVacuum.Valid {
		table.LastVacuum = &lastVacuum.Time
	}
	if lastAutovacuum.Valid {
		table.LastAutovacuum = &lastAutovacuum.Time
	}
	if lastAnalyze.Valid {
		table.LastAnalyze = &lastAnalyze.Time
	}
	if lastAutoanalyze.Valid {
		table.LastAutoanalyze = &lastAutoanalyze.Time
	}
}

// collectMatViewStats collects row-count and size for a materialized view using
// pg_class.reltuples (an estimate maintained by ANALYZE/VACUUM) because
// pg_stat_user_tables only covers regular and partitioned tables.
func (c *SchemaCollector) collectMatViewStats(ctx context.Context, table *TableDefinition) {
	var reltuples float64
	var sizeBytes, totalSizeBytes sql.NullInt64

	var sizeCol string
	switch c.sizeMode {
	case sizeQualified:
		sizeCol = "pg_catalog.pg_total_size(c.oid)"
	case sizeEstimate:
		sizeCol = "(c.relpages * current_setting('block_size')::bigint)"
	default:
		sizeCol = "pg_total_size(c.oid)"
	}
	query := fmt.Sprintf(`
		SELECT c.reltuples, %s, %s
		FROM pg_class c
		WHERE c.oid = $1
	`, sizeCol, sizeCol)

	err := c.db.QueryRowContext(ctx, query, table.OID).Scan(&reltuples, &sizeBytes, &totalSizeBytes)
	if err != nil {
		c.logger.Warn("failed to collect mat-view stats", "view", table.Name, "error", err)
		return
	}

	// Materialized views are not in pg_stat_user_tables, so use reltuples as estimate
	if reltuples > 0 {
		table.LiveTuples = int64(reltuples)
	}
	if sizeBytes.Valid {
		table.SizeBytes = sizeBytes.Int64
	}
	if totalSizeBytes.Valid {
		table.TotalSizeBytes = totalSizeBytes.Int64
	}
}

// collectRowEstimateFromPgClass sets row count (and optionally size) from pg_class.reltuples
// when pg_stat_user_tables is unavailable (e.g. permissions, connection pooling).
// Used as fallback for regular and partitioned tables.
func (c *SchemaCollector) collectRowEstimateFromPgClass(ctx context.Context, table *TableDefinition) {
	var reltuples float64
	var sizeBytes, totalSizeBytes sql.NullInt64

	var sizeCol string
	switch c.sizeMode {
	case sizeQualified:
		sizeCol = "pg_catalog.pg_total_size(c.oid)"
	case sizeEstimate:
		sizeCol = "(c.relpages * current_setting('block_size')::bigint)"
	default:
		sizeCol = "pg_total_size(c.oid)"
	}
	query := fmt.Sprintf(`
		SELECT c.reltuples, %s, %s
		FROM pg_class c
		WHERE c.oid = $1
	`, sizeCol, sizeCol)

	err := c.db.QueryRowContext(ctx, query, table.OID).Scan(&reltuples, &sizeBytes, &totalSizeBytes)
	if err != nil {
		c.logger.Warn("failed to collect row estimate from pg_class", "table", table.Name, "error", err)
		return
	}

	// When pg_stat_user_tables is unavailable, use reltuples as an estimate for LiveTuples
	if reltuples > 0 {
		table.LiveTuples = int64(reltuples)
	}
	if sizeBytes.Valid {
		table.SizeBytes = sizeBytes.Int64
	}
	if totalSizeBytes.Valid {
		table.TotalSizeBytes = totalSizeBytes.Int64
	}
}

// fillLiveTuplesFromReltuples queries pg_class.reltuples and sets LiveTuples
// when n_live_tup is 0 (i.e. ANALYZE has not yet run on the table).
// reltuples is an estimate maintained by VACUUM/ANALYZE/bulk operations
// and is better than showing 0 for tables that clearly have data.
func (c *SchemaCollector) fillLiveTuplesFromReltuples(ctx context.Context, table *TableDefinition) {
	var reltuples float64
	err := c.db.QueryRowContext(ctx,
		"SELECT c.reltuples FROM pg_class c WHERE c.oid = $1", table.OID).Scan(&reltuples)
	if err != nil || reltuples <= 0 {
		return
	}
	table.LiveTuples = int64(reltuples)
}

// collectViewDefinition collects the SQL definition of a view
func (c *SchemaCollector) collectViewDefinition(ctx context.Context, table *TableDefinition) {
	var viewDef sql.NullString
	err := c.db.QueryRowContext(ctx, c.sqlBuilder.ViewDefinitionQuery(), table.OID).
		Scan(&viewDef)
	if err != nil {
		c.logger.Warn("failed to collect view definition", "view", table.Name, "error", err)
		return
	}
	if viewDef.Valid {
		table.ViewDefinition = viewDef.String
	}
}

// collectColumnStats collects pg_stats for all columns on a table
func (c *SchemaCollector) collectColumnStats(ctx context.Context, table *TableDefinition) {
	rows, err := c.db.QueryContext(ctx, c.sqlBuilder.ColumnStatsQuery(), table.OID)
	if err != nil {
		c.logger.Warn("failed to collect column stats", "table", table.Name, "error", err)
		return
	}
	defer rows.Close()

	statsMap := make(map[string]struct {
		nullFrac    float64
		avgWidth    int32
		nDistinct   float64
		correlation float64
	})

	for rows.Next() {
		var (
			attname     string
			nullFrac    float64
			avgWidth    int32
			nDistinct   float64
			correlation sql.NullFloat64
		)
		if err := rows.Scan(&attname, &nullFrac, &avgWidth, &nDistinct, &correlation); err != nil {
			c.logger.Warn("failed to scan column stats", "error", err)
			continue
		}

		corr := 0.0
		if correlation.Valid {
			corr = correlation.Float64
		}
		statsMap[attname] = struct {
			nullFrac    float64
			avgWidth    int32
			nDistinct   float64
			correlation float64
		}{nullFrac, avgWidth, nDistinct, corr}
	}

	for _, col := range table.Columns {
		if stats, ok := statsMap[col.Name]; ok {
			col.NullFraction = stats.nullFrac
			col.AvgWidth = stats.avgWidth
			col.NDistinct = stats.nDistinct
			col.Correlation = stats.correlation
		}
	}
}

// shouldExcludeTable checks if a table should be excluded
func (c *SchemaCollector) shouldExcludeTable(schemaName, tableName string) bool {
	if len(c.filters.IncludeSchemas) > 0 {
		if !c.filters.IncludeSchemas[schemaName] {
			return true
		}
	}

	if includedTables, ok := c.filters.IncludeTables[schemaName]; ok && len(includedTables) > 0 {
		isIncluded := false
		for _, included := range includedTables {
			if included == tableName {
				isIncluded = true
				break
			}
		}
		if !isIncluded {
			return true
		}
	}

	if c.filters.ExcludeSchemas[schemaName] {
		return true
	}

	if excludedTables, ok := c.filters.ExcludeTables[schemaName]; ok {
		for _, excluded := range excludedTables {
			if excluded == tableName {
				return true
			}
		}
	}

	return false
}

// recordError records a collection error
func (c *SchemaCollector) recordError(err ErrorInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.collectionErrors = append(c.collectionErrors, err)
}

// GetLastCollectionTime returns the last collection time
func (c *SchemaCollector) GetLastCollectionTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.lastCollectionTime
}

// GetCollectionDuration returns the last collection duration
func (c *SchemaCollector) GetCollectionDuration() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.collectionDuration
}

// GetTableCount returns the last table count
func (c *SchemaCollector) GetTableCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.tableCount
}

// ResolveDatabaseOID queries and returns the current database OID
func (c *SchemaCollector) ResolveDatabaseOID(ctx context.Context) (uint32, error) {
	var dbOID uint32
	err := c.db.QueryRowContext(ctx, c.sqlBuilder.DatabaseOIDQuery()).Scan(&dbOID)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve database OID: %w", err)
	}
	return dbOID, nil
}

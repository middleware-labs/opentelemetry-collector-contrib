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
	sizeDirect    sizeQueryMode = iota // pg_total_relation_size(oid) — standard
	sizeQualified                      // pg_catalog.pg_total_relation_size(oid) — explicit schema
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
		"SELECT pg_total_relation_size(oid) FROM pg_class LIMIT 1").Scan(&dummy); err == nil {
		c.sizeMode = sizeDirect
		return
	}

	// Tier 2: schema-qualified — fixes missing search_path (PgBouncer, etc.)
	if err := c.db.QueryRowContext(ctx,
		"SELECT pg_catalog.pg_total_relation_size(oid) FROM pg_class LIMIT 1").Scan(&dummy); err == nil {
		c.sizeMode = sizeQualified
		c.logger.Warn("pg_total_relation_size requires schema qualification, using pg_catalog.pg_total_relation_size")
		return
	}

	// Tier 3: relpages estimate
	c.sizeMode = sizeEstimate
	c.logger.Warn("pg_total_relation_size not available, using relpages-based size estimates")
}

func (c *SchemaCollector) totalSizeExpr(arg string) string {
	switch c.sizeMode {
	case sizeQualified:
		return fmt.Sprintf("pg_catalog.pg_total_relation_size(%s)", arg)
	case sizeEstimate:
		return fmt.Sprintf("(c.relpages * current_setting('block_size')::bigint)")
	default:
		return fmt.Sprintf("pg_total_relation_size(%s)", arg)
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

	// 2. Enrich with columns, indexes, constraints, stats.
	//
	// Relations under an ACCESS EXCLUSIVE lock are skipped rather than waited
	// on. See lock_guard.go for why waiting is actively harmful: our catalog
	// introspection would join the lock queue behind the customer's DDL, and
	// their application queries would then queue behind us.
	locked, lockErr := lockedRelations(ctx, c.db)
	if lockErr != nil {
		// We could not determine what is locked. Introspecting blindly is the
		// unguarded behaviour that can stall the customer's application, so
		// treat it as a collection error and skip enrichment for this cycle.
		// Table identity from the lock-free scan above is still emitted.
		c.recordError(ErrorInfo{
			Category:  "locks",
			Severity:  "error",
			Message:   fmt.Sprintf("failed to determine locked relations, skipping table enrichment: %v", lockErr),
			Timestamp: time.Now(),
		})
		if !c.config.ContinueOnError {
			return nil, lockErr
		}
	} else {
		for _, table := range event.Tables {
			if _, isLocked := locked[table.OID]; isLocked {
				table.ExclusivelyLocked = true
				c.logger.Info("skipping exclusively locked table",
					"database", c.config.DatabaseName,
					"schema", table.SchemaName,
					"table", table.Name)
				continue
			}
			c.enrichTableDefinition(ctx, table)
		}
		c.collectTableStatsForTables(ctx, event.Tables)
		c.collectIndexStatsForTables(ctx, event.Tables)
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
	c.changeTracker.UpdateSnapshot(c.config.DatabaseName, xminMap)

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
		// Counted from the flag rather than incremented at the skip site, so
		// that tables skipped by the pg_locks pre-check and tables abandoned on
		// a mid-flight lock timeout are both included.
		if t.ExclusivelyLocked {
			event.Statistics.SkippedLockedTables++
		}
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
//
// It returns the OIDs of tables that are new or whose definition changed, and
// the number of previously tracked tables that no longer exist. A dropped table
// is a change too, and would otherwise go unnoticed: every remaining table
// matches, so nothing is reported as changed and the dropped table lingers in
// the last emitted schema indefinitely.
//
// Detection reads only OIDs and xmins from pg_class, which is far cheaper than
// a full collection, so an unchanged database costs one small query.
func (c *SchemaCollector) DetectChanges(ctx context.Context) (changed []uint32, dropped int, err error) {
	rows, err := c.db.QueryContext(ctx, c.sqlBuilder.XminDetectionQuery())
	if err != nil {
		return nil, 0, fmt.Errorf("failed to detect changes: %w", err)
	}
	defer rows.Close()

	trackedSeen := 0
	for rows.Next() {
		var oid uint32
		var xmin uint32
		var schemaName, tableName string
		if err := rows.Scan(&oid, &xmin, &schemaName, &tableName); err != nil {
			c.logger.Warn("failed to scan xmin", "error", err)
			continue
		}
		if c.shouldExcludeTable(schemaName, tableName) {
			continue
		}

		tracked, altered := c.changeTracker.Compare(c.config.DatabaseName, oid, xmin)
		if tracked {
			trackedSeen++
		}
		if !tracked || altered {
			changed = append(changed, oid)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Anything tracked that did not appear this time has been dropped.
	if tracked := c.changeTracker.TrackedTableCountFor(c.config.DatabaseName); tracked > trackedSeen {
		dropped = tracked - trackedSeen
	}

	return changed, dropped, nil
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

// enrichTableDefinition enriches a table with columns, indexes, constraints, and stats.
//
// Callers must have already skipped relations known to be locked. The pg_locks
// pre-check is a snapshot, not a mutex, so a relation can still acquire an
// ACCESS EXCLUSIVE lock in the window between that check and these queries. The
// DSN lock_timeout bounds how long we wait; if it fires we abandon the whole
// table rather than running the four remaining per-table queries, each of which
// would queue on the same lock and pay the same timeout.
func (c *SchemaCollector) enrichTableDefinition(ctx context.Context, table *TableDefinition) {
	columns, err := c.collectColumns(ctx, table.OID)
	if err != nil {
		if isLockTimeout(err) {
			table.ExclusivelyLocked = true
			c.logger.Info("table locked during introspection, skipping",
				"database", c.config.DatabaseName,
				"schema", table.SchemaName,
				"table", table.Name)
			return
		}
		c.logger.Warn("failed to collect columns", "table", table.Name, "error", err)
	}
	table.Columns = columns

	indexes, err := c.collectIndexes(ctx, table.OID)
	if err != nil {
		c.logger.Warn("failed to collect indexes", "table", table.Name, "error", err)
	}
	table.Indexes = indexes

	constraints, err := c.collectConstraints(ctx, table.OID)
	if err != nil {
		c.logger.Warn("failed to collect constraints", "table", table.Name, "error", err)
	}
	table.Constraints = constraints

	// Materialized views are not covered by pg_stat_user_tables.
	if table.Type == "m" {
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
	c.collectTableStatsForTables(ctx, []*TableDefinition{table})
	c.collectIndexStats(ctx, table.Indexes)

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
			columns    []string
		)

		if err := rows.Scan(&oid, &name, &table, &isPrimary, &isUnique, &isValid,
			&isExcl, &indexType, &definition, &partial, &xmin, &sizeBytes, pq.Array(&columns)); err != nil {
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
			Columns:     columns,
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

// collectIndexStatsForTables collects statistics for every unlocked table in a
// schema snapshot in one catalog query.
func (c *SchemaCollector) collectIndexStatsForTables(ctx context.Context, tables []*TableDefinition) {
	indexes := make([]*IndexDefinition, 0)
	for _, table := range tables {
		if table.ExclusivelyLocked {
			continue
		}
		indexes = append(indexes, table.Indexes...)
	}
	c.collectIndexStats(ctx, indexes)
}

// collectIndexStats collects statistics for all supplied indexes in one query.
func (c *SchemaCollector) collectIndexStats(ctx context.Context, indexes []*IndexDefinition) {
	if len(indexes) == 0 {
		return
	}

	oids := make([]uint32, 0, len(indexes))
	byOID := make(map[uint32]*IndexDefinition, len(indexes))
	for _, idx := range indexes {
		oids = append(oids, idx.OID)
		byOID[idx.OID] = idx
	}

	rows, err := c.db.QueryContext(ctx, c.sqlBuilder.IndexStatsQuery(), pq.Array(oids))
	if err != nil {
		return
	}
	defer rows.Close()

	if c.sqlBuilder.MajorVersion() >= 16 {
		// PG16+ has last_idx_scan
		for rows.Next() {
			var indexOID uint32
			var lastScan sql.NullTime
			var scanCount, tupleRead, tupleFetch int64
			if err := rows.Scan(&indexOID, &scanCount, &tupleRead, &tupleFetch, &lastScan); err != nil {
				continue
			}
			idx, ok := byOID[indexOID]
			if !ok {
				continue
			}
			idx.ScanCount, idx.TupleRead, idx.TupleFetch = scanCount, tupleRead, tupleFetch
			if lastScan.Valid {
				idx.LastScan = &lastScan.Time
			}
		}
	} else {
		// PG10-15: no last_idx_scan
		for rows.Next() {
			var indexOID uint32
			var scanCount, tupleRead, tupleFetch int64
			if err := rows.Scan(&indexOID, &scanCount, &tupleRead, &tupleFetch); err != nil {
				continue
			}
			idx, ok := byOID[indexOID]
			if !ok {
				continue
			}
			idx.ScanCount, idx.TupleRead, idx.TupleFetch = scanCount, tupleRead, tupleFetch
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
			oid               uint32
			name              string
			conType           string
			table             uint32
			definition        string
			deferrable        bool
			deferred          bool
			validated         bool
			refTableOID       uint32
			columnNames       []string
			referencedColumns []string
		)

		if err := rows.Scan(&oid, &name, &conType, &table, &definition, &deferrable, &deferred, &validated,
			&refTableOID, pq.Array(&columnNames), pq.Array(&referencedColumns)); err != nil {
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
		if conType == "f" {
			constraint.ReferencedTableOID = refTableOID
			constraint.ColumnNames = columnNames
			constraint.ReferencedColumns = referencedColumns
		}

		constraints = append(constraints, constraint)
	}

	return constraints, rows.Err()
}

// collectTableStatsForTables collects pg_stat_user_tables data for every
// regular and partitioned table in one query. Tables without a statistics row,
// and all tables after a query failure, retain the pg_class fallback.
func (c *SchemaCollector) collectTableStatsForTables(ctx context.Context, tables []*TableDefinition) {
	byOID := make(map[uint32]*TableDefinition)
	oids := make([]uint32, 0, len(tables))
	for _, table := range tables {
		if table.ExclusivelyLocked || (table.Type != "r" && table.Type != "p") {
			continue
		}
		byOID[table.OID] = table
		oids = append(oids, table.OID)
	}
	if len(oids) == 0 {
		return
	}

	rows, err := c.db.QueryContext(ctx, c.tableStatsQueryForMode(), pq.Array(oids))
	if err != nil {
		c.logger.Warn("failed to collect batched table stats; using pg_class estimates", "error", err)
		for _, table := range byOID {
			c.collectRowEstimateFromPgClass(ctx, table)
		}
		return
	}

	seen := make(map[uint32]bool, len(byOID))
	for rows.Next() {
		var (
			tableOID        uint32
			liveTuples      int64
			deadTuples      int64
			modified        int64
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
		if err := rows.Scan(&tableOID, &liveTuples, &deadTuples, &modified,
			&lastVacuum, &lastAutovacuum, &lastAnalyze, &lastAutoanalyze,
			&seqScans, &seqTupRead, &idxScans, &idxTupFetch, &sizeBytes, &totalSizeBytes); err != nil {
			continue
		}
		table, ok := byOID[tableOID]
		if !ok {
			continue
		}
		seen[tableOID] = true
		table.LiveTuples = liveTuples
		table.DeadTuples = deadTuples
		table.ModSinceAnalyze = modified
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
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		c.logger.Warn("failed while reading batched table stats", "error", rowsErr)
	}

	zeroLiveTuples := make([]*TableDefinition, 0)
	for oid, table := range byOID {
		if !seen[oid] {
			c.collectRowEstimateFromPgClass(ctx, table)
		} else if table.LiveTuples == 0 {
			zeroLiveTuples = append(zeroLiveTuples, table)
		}
	}
	c.fillLiveTuplesFromReltuples(ctx, zeroLiveTuples)
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
		sizeCol = "pg_catalog.pg_total_relation_size(c.oid)"
	case sizeEstimate:
		sizeCol = "(c.relpages * current_setting('block_size')::bigint)"
	default:
		sizeCol = "pg_total_relation_size(c.oid)"
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
		sizeCol = "pg_catalog.pg_total_relation_size(c.oid)"
	case sizeEstimate:
		sizeCol = "(c.relpages * current_setting('block_size')::bigint)"
	default:
		sizeCol = "pg_total_relation_size(c.oid)"
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

// fillLiveTuplesFromReltuples queries pg_class.reltuples once and sets
// LiveTuples for tables whose n_live_tup is 0 (i.e. ANALYZE has not yet run).
// reltuples is an estimate maintained by VACUUM/ANALYZE/bulk operations
// and is better than showing 0 for tables that clearly have data.
func (c *SchemaCollector) fillLiveTuplesFromReltuples(ctx context.Context, tables []*TableDefinition) {
	if len(tables) == 0 {
		return
	}

	byOID := make(map[uint32]*TableDefinition, len(tables))
	oids := make([]uint32, 0, len(tables))
	for _, table := range tables {
		byOID[table.OID] = table
		oids = append(oids, table.OID)
	}

	rows, err := c.db.QueryContext(ctx,
		"SELECT c.oid, c.reltuples FROM pg_class c WHERE c.oid = ANY($1::oid[])", pq.Array(oids))
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var oid uint32
		var reltuples float64
		if err := rows.Scan(&oid, &reltuples); err != nil || reltuples <= 0 {
			continue
		}
		if table, ok := byOID[oid]; ok {
			table.LiveTuples = int64(reltuples)
		}
	}
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

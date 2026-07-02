// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

// SQLBuilder provides version-specific SQL queries for schema collection
type SQLBuilder interface {
	// Table queries: Direct / schema-qualified / relpages estimate
	TablesQuery() string
	TablesQueryQualified() string
	TablesQueryEstimate() string
	ColumnsQuery() string
	IndexesQuery() string
	IndexesQueryQualified() string
	IndexesQueryEstimate() string
	ConstraintsQuery() string
	ForeignKeyDetailsQuery() string

	// Statistics queries: Direct / schema-qualified / relpages estimate
	TableStatsQuery() string
	TableStatsQueryQualified() string
	TableStatsQueryEstimate() string
	ColumnStatsQuery() string
	IndexStatsQuery() string

	// View definitions
	ViewDefinitionQuery() string

	// Change detection
	XminDetectionQuery() string

	// Metadata queries
	ExtensionsQuery() string
	SettingsQuery() string
	DatabaseOIDQuery() string

	// Index column names
	IndexColumnsQuery() string

	// Feature support
	SupportsHelperFunctions() bool
	MajorVersion() int
}

// NewSQLBuilder creates a version-appropriate SQL builder
func NewSQLBuilder(versionNum uint32) SQLBuilder {
	majorVersion := int(versionNum) / 10000

	switch majorVersion {
	case 10, 11:
		return &PostgreSQL10Builder{majorVer: majorVersion}
	case 12, 13:
		return &PostgreSQL12Builder{majorVer: majorVersion}
	case 14, 15:
		return &PostgreSQL14Builder{majorVer: majorVersion}
	default: // 16+
		return &PostgreSQL16Builder{majorVer: majorVersion}
	}
}

// --- Shared PG12+ table queries ---

func pg12PlusTablesQuery() string {
	return `
		SELECT
			c.oid,
			ns.nspname AS schema_name,
			c.relname AS table_name,
			c.relkind::text AS table_type,
			false AS relhasoids,
			c.reltablespace,
			obj_description(c.oid, 'pg_class') AS description,
			pg_roles.rolname AS owner_name,
			c.xmin,
			pg_total_size(c.oid) AS total_size_bytes
		FROM pg_class c
		JOIN pg_namespace ns ON c.relnamespace = ns.oid
		LEFT JOIN pg_roles ON c.relowner = pg_roles.oid
		WHERE c.relkind IN ('r', 't', 'v', 'm', 'p')
		AND ns.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		AND ns.nspname NOT LIKE 'pg_temp_%'
		AND ns.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY ns.nspname, c.relname
	`
}

func pg12PlusTablesQueryQualified() string {
	return `
		SELECT
			c.oid,
			ns.nspname AS schema_name,
			c.relname AS table_name,
			c.relkind::text AS table_type,
			false AS relhasoids,
			c.reltablespace,
			obj_description(c.oid, 'pg_class') AS description,
			pg_roles.rolname AS owner_name,
			c.xmin,
			pg_catalog.pg_total_size(c.oid) AS total_size_bytes
		FROM pg_class c
		JOIN pg_namespace ns ON c.relnamespace = ns.oid
		LEFT JOIN pg_roles ON c.relowner = pg_roles.oid
		WHERE c.relkind IN ('r', 't', 'v', 'm', 'p')
		AND ns.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		AND ns.nspname NOT LIKE 'pg_temp_%'
		AND ns.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY ns.nspname, c.relname
	`
}

func pg12PlusTablesQueryEstimate() string {
	return `
		SELECT
			c.oid,
			ns.nspname AS schema_name,
			c.relname AS table_name,
			c.relkind::text AS table_type,
			false AS relhasoids,
			c.reltablespace,
			obj_description(c.oid, 'pg_class') AS description,
			pg_roles.rolname AS owner_name,
			c.xmin,
			(c.relpages * current_setting('block_size')::bigint) AS total_size_bytes
		FROM pg_class c
		JOIN pg_namespace ns ON c.relnamespace = ns.oid
		LEFT JOIN pg_roles ON c.relowner = pg_roles.oid
		WHERE c.relkind IN ('r', 't', 'v', 'm', 'p')
		AND ns.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		AND ns.nspname NOT LIKE 'pg_temp_%'
		AND ns.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY ns.nspname, c.relname
	`
}

// --- Common queries shared across versions ---

func commonConstraintsQuery() string {
	return `
		SELECT
			c.oid,
			c.conname,
			c.contype::text,
			c.conrelid AS table_oid,
			pg_get_constraintdef(c.oid) AS definition,
			c.condeferrable,
			c.condeferred,
			c.convalidated
		FROM pg_constraint c
		WHERE c.conrelid = $1
		ORDER BY c.conname
	`
}

func commonForeignKeyDetailsQuery() string {
	return `
		SELECT
			c.oid,
			c.conname,
			c.conrelid AS table_oid,
			c.confrelid AS referenced_table_oid,
			(SELECT array_agg(a.attname ORDER BY x.n)
			 FROM unnest(c.conkey) WITH ORDINALITY AS x(attnum, n)
			 JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = x.attnum
			) AS column_names,
			(SELECT array_agg(a.attname ORDER BY x.n)
			 FROM unnest(c.confkey) WITH ORDINALITY AS x(attnum, n)
			 JOIN pg_attribute a ON a.attrelid = c.confrelid AND a.attnum = x.attnum
			) AS referenced_columns
		FROM pg_constraint c
		WHERE c.conrelid = $1
		AND c.contype = 'f'
		ORDER BY c.conname
	`
}

func commonViewDefinitionQuery() string {
	// Cast to oid: with a prepared statement, $1 is otherwise inferred as a
	// generic integer and PostgreSQL picks pg_get_viewdef(text/name, bool),
	// which treats the value as a relation name (e.g. "16401") and fails.
	return `SELECT pg_get_viewdef($1::oid, true)`
}

func commonExtensionsQuery() string {
	return `
		SELECT
			e.oid,
			e.extname,
			e.extversion,
			e.extrelocatable,
			ns.nspname AS schema_name,
			obj_description(e.oid, 'pg_extension') AS description
		FROM pg_extension e
		LEFT JOIN pg_namespace ns ON e.extnamespace = ns.oid
		WHERE e.extname NOT IN ('plpgsql')
		ORDER BY e.extname
	`
}

func commonSettingsQuery() string {
	return `
		SELECT name, setting,
			COALESCE(unit, '') AS unit,
			context,
			vartype,
			COALESCE(source, '') AS source
		FROM pg_settings
		WHERE name IN (
			'log_connections', 'log_disconnections', 'log_statement', 'log_duration',
			'max_connections', 'shared_buffers', 'effective_cache_size', 'work_mem',
			'maintenance_work_mem', 'random_page_cost', 'join_collapse_limit',
			'from_collapse_limit', 'wal_level', 'max_wal_senders', 'max_replication_slots',
			'track_activities', 'track_counts', 'track_io_timing', 'track_functions',
			'shared_preload_libraries', 'default_transaction_isolation', 'data_checksums'
		)
		ORDER BY name
	`
}

func commonDatabaseOIDQuery() string {
	return `SELECT oid FROM pg_database WHERE datname = current_database()`
}

func commonXminDetectionQuery() string {
	return `
		SELECT c.oid, c.xmin
		FROM pg_class c
		JOIN pg_namespace ns ON c.relnamespace = ns.oid
		WHERE c.relkind IN ('r', 't', 'v', 'm', 'p')
		AND ns.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		AND ns.nspname NOT LIKE 'pg_temp_%'
		AND ns.nspname NOT LIKE 'pg_toast_temp_%'
	`
}

func commonIndexColumnsQuery() string {
	return `
		SELECT
			i.oid AS index_oid,
			array_agg(a.attname ORDER BY x.n) AS column_names
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		CROSS JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS x(attnum, n)
		JOIN pg_attribute a ON a.attrelid = ix.indrelid AND a.attnum = x.attnum
		WHERE ix.indrelid = $1
		AND x.attnum > 0
		GROUP BY i.oid
	`
}

// commonColumnsQuery returns the columns query using pg_get_expr (PG12+)
func commonColumnsQuery() string {
	return `
		SELECT
			a.attnum,
			a.attname,
			format_type(a.atttypid, a.atttypmod) AS type_name,
			a.atttypid AS type_oid,
			a.atttypmod,
			a.attnotnull,
			a.atthasdef,
			pg_get_expr(d.adbin, d.adrelid) AS default_value,
			col_description(a.attrelid, a.attnum) AS description,
			a.attcollation,
			a.xmin
		FROM pg_attribute a
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = $1
		AND a.attnum > 0
		AND NOT a.attisdropped
		ORDER BY a.attnum
	`
}

func commonIndexesQuery() string {
	return `
		SELECT
			i.oid,
			i.relname AS index_name,
			ix.indrelid AS table_oid,
			ix.indisprimary,
			ix.indisunique,
			ix.indisvalid,
			ix.indisexclusion,
			a.amname AS index_type,
			pg_get_indexdef(i.oid) AS index_definition,
			pg_get_expr(ix.indpred, ix.indrelid) AS partial_predicate,
			i.xmin,
			pg_relation_size(i.oid) AS index_size_bytes
		FROM pg_class i
		JOIN pg_index ix ON i.oid = ix.indexrelid
		JOIN pg_am a ON i.relam = a.oid
		WHERE ix.indrelid = $1
		ORDER BY i.relname
	`
}

func commonIndexesQueryQualified() string {
	return `
		SELECT
			i.oid,
			i.relname AS index_name,
			ix.indrelid AS table_oid,
			ix.indisprimary,
			ix.indisunique,
			ix.indisvalid,
			ix.indisexclusion,
			a.amname AS index_type,
			pg_get_indexdef(i.oid) AS index_definition,
			pg_get_expr(ix.indpred, ix.indrelid) AS partial_predicate,
			i.xmin,
			pg_catalog.pg_relation_size(i.oid) AS index_size_bytes
		FROM pg_class i
		JOIN pg_index ix ON i.oid = ix.indexrelid
		JOIN pg_am a ON i.relam = a.oid
		WHERE ix.indrelid = $1
		ORDER BY i.relname
	`
}

func commonIndexesQueryEstimate() string {
	return `
		SELECT
			i.oid,
			i.relname AS index_name,
			ix.indrelid AS table_oid,
			ix.indisprimary,
			ix.indisunique,
			ix.indisvalid,
			ix.indisexclusion,
			a.amname AS index_type,
			pg_get_indexdef(i.oid) AS index_definition,
			pg_get_expr(ix.indpred, ix.indrelid) AS partial_predicate,
			i.xmin,
			(i.relpages * current_setting('block_size')::bigint) AS index_size_bytes
		FROM pg_class i
		JOIN pg_index ix ON i.oid = ix.indexrelid
		JOIN pg_am a ON i.relam = a.oid
		WHERE ix.indrelid = $1
		ORDER BY i.relname
	`
}

func commonTableStatsQuery() string {
	return `
		SELECT
			n_live_tup,
			n_dead_tup,
			n_mod_since_analyze,
			last_vacuum,
			last_autovacuum,
			last_analyze,
			last_autoanalyze,
			seq_scan,
			seq_tup_read,
			COALESCE(idx_scan, 0),
			COALESCE(idx_tup_fetch, 0),
			pg_table_size(relid) AS size_bytes,
			pg_total_size(relid) AS total_size_bytes
		FROM pg_stat_user_tables
		WHERE relid = $1
	`
}

func commonTableStatsQueryQualified() string {
	return `
		SELECT
			n_live_tup,
			n_dead_tup,
			n_mod_since_analyze,
			last_vacuum,
			last_autovacuum,
			last_analyze,
			last_autoanalyze,
			seq_scan,
			seq_tup_read,
			COALESCE(idx_scan, 0),
			COALESCE(idx_tup_fetch, 0),
			pg_catalog.pg_table_size(relid) AS size_bytes,
			pg_catalog.pg_total_size(relid) AS total_size_bytes
		FROM pg_stat_user_tables
		WHERE relid = $1
	`
}

func commonTableStatsQueryEstimate() string {
	return `
		SELECT
			sut.n_live_tup,
			sut.n_dead_tup,
			sut.n_mod_since_analyze,
			sut.last_vacuum,
			sut.last_autovacuum,
			sut.last_analyze,
			sut.last_autoanalyze,
			sut.seq_scan,
			sut.seq_tup_read,
			COALESCE(sut.idx_scan, 0),
			COALESCE(sut.idx_tup_fetch, 0),
			(c.relpages * current_setting('block_size')::bigint) AS size_bytes,
			(c.relpages * current_setting('block_size')::bigint) AS total_size_bytes
		FROM pg_stat_user_tables sut
		JOIN pg_class c ON c.oid = sut.relid
		WHERE sut.relid = $1
	`
}

func commonColumnStatsQuery() string {
	return `
		SELECT
			s.attname,
			s.null_frac,
			s.avg_width,
			s.n_distinct,
			s.correlation
		FROM pg_stats s
		JOIN pg_class c ON c.relname = s.tablename
		JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = s.schemaname
		WHERE c.oid = $1
		AND s.schemaname NOT IN ('pg_catalog', 'information_schema')
	`
}

// --- PostgreSQL 10-11 Builder ---

type PostgreSQL10Builder struct {
	majorVer int
}

func (b *PostgreSQL10Builder) MajorVersion() int { return b.majorVer }

func (b *PostgreSQL10Builder) TablesQuery() string {
	return `
		SELECT
			c.oid,
			ns.nspname AS schema_name,
			c.relname AS table_name,
			c.relkind::text AS table_type,
			c.relhasoids,
			c.reltablespace,
			obj_description(c.oid, 'pg_class') AS description,
			pg_roles.rolname AS owner_name,
			c.xmin,
			pg_total_size(c.oid) AS total_size_bytes
		FROM pg_class c
		JOIN pg_namespace ns ON c.relnamespace = ns.oid
		LEFT JOIN pg_roles ON c.relowner = pg_roles.oid
		WHERE c.relkind IN ('r', 't', 'v', 'm', 'p')
		AND ns.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		AND ns.nspname NOT LIKE 'pg_temp_%'
		AND ns.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY ns.nspname, c.relname
	`
}

func (b *PostgreSQL10Builder) TablesQueryQualified() string {
	return `
		SELECT
			c.oid,
			ns.nspname AS schema_name,
			c.relname AS table_name,
			c.relkind::text AS table_type,
			c.relhasoids,
			c.reltablespace,
			obj_description(c.oid, 'pg_class') AS description,
			pg_roles.rolname AS owner_name,
			c.xmin,
			pg_catalog.pg_total_size(c.oid) AS total_size_bytes
		FROM pg_class c
		JOIN pg_namespace ns ON c.relnamespace = ns.oid
		LEFT JOIN pg_roles ON c.relowner = pg_roles.oid
		WHERE c.relkind IN ('r', 't', 'v', 'm', 'p')
		AND ns.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		AND ns.nspname NOT LIKE 'pg_temp_%'
		AND ns.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY ns.nspname, c.relname
	`
}

func (b *PostgreSQL10Builder) TablesQueryEstimate() string {
	return `
		SELECT
			c.oid,
			ns.nspname AS schema_name,
			c.relname AS table_name,
			c.relkind::text AS table_type,
			c.relhasoids,
			c.reltablespace,
			obj_description(c.oid, 'pg_class') AS description,
			pg_roles.rolname AS owner_name,
			c.xmin,
			(c.relpages * current_setting('block_size')::bigint) AS total_size_bytes
		FROM pg_class c
		JOIN pg_namespace ns ON c.relnamespace = ns.oid
		LEFT JOIN pg_roles ON c.relowner = pg_roles.oid
		WHERE c.relkind IN ('r', 't', 'v', 'm', 'p')
		AND ns.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		AND ns.nspname NOT LIKE 'pg_temp_%'
		AND ns.nspname NOT LIKE 'pg_toast_temp_%'
		ORDER BY ns.nspname, c.relname
	`
}

func (b *PostgreSQL10Builder) ColumnsQuery() string {
	// PG10-11 still have adsrc but pg_get_expr works too
	return commonColumnsQuery()
}

func (b *PostgreSQL10Builder) IndexesQuery() string {
	return commonIndexesQuery()
}

func (b *PostgreSQL10Builder) IndexesQueryQualified() string {
	return commonIndexesQueryQualified()
}

func (b *PostgreSQL10Builder) IndexesQueryEstimate() string {
	return commonIndexesQueryEstimate()
}

func (b *PostgreSQL10Builder) ConstraintsQuery() string {
	return commonConstraintsQuery()
}

func (b *PostgreSQL10Builder) ForeignKeyDetailsQuery() string {
	return commonForeignKeyDetailsQuery()
}

func (b *PostgreSQL10Builder) TableStatsQuery() string {
	return commonTableStatsQuery()
}

func (b *PostgreSQL10Builder) TableStatsQueryQualified() string {
	return commonTableStatsQueryQualified()
}

func (b *PostgreSQL10Builder) TableStatsQueryEstimate() string {
	return commonTableStatsQueryEstimate()
}

func (b *PostgreSQL10Builder) ColumnStatsQuery() string {
	return commonColumnStatsQuery()
}

func (b *PostgreSQL10Builder) IndexStatsQuery() string {
	// PG10-15: no last_idx_scan column
	return `
		SELECT
			idx_scan,
			idx_tup_read,
			idx_tup_fetch
		FROM pg_stat_user_indexes
		WHERE indexrelid = $1
	`
}

func (b *PostgreSQL10Builder) ViewDefinitionQuery() string {
	return commonViewDefinitionQuery()
}

func (b *PostgreSQL10Builder) XminDetectionQuery() string {
	return commonXminDetectionQuery()
}

func (b *PostgreSQL10Builder) ExtensionsQuery() string {
	return commonExtensionsQuery()
}

func (b *PostgreSQL10Builder) SettingsQuery() string {
	return commonSettingsQuery()
}

func (b *PostgreSQL10Builder) DatabaseOIDQuery() string {
	return commonDatabaseOIDQuery()
}

func (b *PostgreSQL10Builder) IndexColumnsQuery() string {
	return commonIndexColumnsQuery()
}

func (b *PostgreSQL10Builder) SupportsHelperFunctions() bool {
	return false
}

// --- PostgreSQL 12-13 Builder ---

type PostgreSQL12Builder struct {
	majorVer int
}

func (b *PostgreSQL12Builder) MajorVersion() int { return b.majorVer }

func (b *PostgreSQL12Builder) TablesQuery() string {
	return pg12PlusTablesQuery()
}

func (b *PostgreSQL12Builder) TablesQueryQualified() string {
	return pg12PlusTablesQueryQualified()
}

func (b *PostgreSQL12Builder) TablesQueryEstimate() string {
	return pg12PlusTablesQueryEstimate()
}

func (b *PostgreSQL12Builder) ColumnsQuery() string {
	return commonColumnsQuery()
}

func (b *PostgreSQL12Builder) IndexesQuery() string {
	return commonIndexesQuery()
}

func (b *PostgreSQL12Builder) IndexesQueryQualified() string {
	return commonIndexesQueryQualified()
}

func (b *PostgreSQL12Builder) IndexesQueryEstimate() string {
	return commonIndexesQueryEstimate()
}

func (b *PostgreSQL12Builder) ConstraintsQuery() string {
	return commonConstraintsQuery()
}

func (b *PostgreSQL12Builder) ForeignKeyDetailsQuery() string {
	return commonForeignKeyDetailsQuery()
}

func (b *PostgreSQL12Builder) TableStatsQuery() string {
	return commonTableStatsQuery()
}

func (b *PostgreSQL12Builder) TableStatsQueryQualified() string {
	return commonTableStatsQueryQualified()
}

func (b *PostgreSQL12Builder) TableStatsQueryEstimate() string {
	return commonTableStatsQueryEstimate()
}

func (b *PostgreSQL12Builder) ColumnStatsQuery() string {
	return commonColumnStatsQuery()
}

func (b *PostgreSQL12Builder) IndexStatsQuery() string {
	// PG12-13: no last_idx_scan column
	return `
		SELECT
			idx_scan,
			idx_tup_read,
			idx_tup_fetch
		FROM pg_stat_user_indexes
		WHERE indexrelid = $1
	`
}

func (b *PostgreSQL12Builder) ViewDefinitionQuery() string {
	return commonViewDefinitionQuery()
}

func (b *PostgreSQL12Builder) XminDetectionQuery() string {
	return commonXminDetectionQuery()
}

func (b *PostgreSQL12Builder) ExtensionsQuery() string {
	return commonExtensionsQuery()
}

func (b *PostgreSQL12Builder) SettingsQuery() string {
	return commonSettingsQuery()
}

func (b *PostgreSQL12Builder) DatabaseOIDQuery() string {
	return commonDatabaseOIDQuery()
}

func (b *PostgreSQL12Builder) IndexColumnsQuery() string {
	return commonIndexColumnsQuery()
}

func (b *PostgreSQL12Builder) SupportsHelperFunctions() bool {
	return false
}

// --- PostgreSQL 14-15 Builder ---

type PostgreSQL14Builder struct {
	majorVer int
}

func (b *PostgreSQL14Builder) MajorVersion() int { return b.majorVer }

func (b *PostgreSQL14Builder) TablesQuery() string {
	return pg12PlusTablesQuery()
}

func (b *PostgreSQL14Builder) TablesQueryQualified() string {
	return pg12PlusTablesQueryQualified()
}

func (b *PostgreSQL14Builder) TablesQueryEstimate() string {
	return pg12PlusTablesQueryEstimate()
}

func (b *PostgreSQL14Builder) ColumnsQuery() string {
	return commonColumnsQuery()
}

func (b *PostgreSQL14Builder) IndexesQuery() string {
	return commonIndexesQuery()
}

func (b *PostgreSQL14Builder) IndexesQueryQualified() string {
	return commonIndexesQueryQualified()
}

func (b *PostgreSQL14Builder) IndexesQueryEstimate() string {
	return commonIndexesQueryEstimate()
}

func (b *PostgreSQL14Builder) ConstraintsQuery() string {
	return commonConstraintsQuery()
}

func (b *PostgreSQL14Builder) ForeignKeyDetailsQuery() string {
	return commonForeignKeyDetailsQuery()
}

func (b *PostgreSQL14Builder) TableStatsQuery() string {
	return commonTableStatsQuery()
}

func (b *PostgreSQL14Builder) TableStatsQueryQualified() string {
	return commonTableStatsQueryQualified()
}

func (b *PostgreSQL14Builder) TableStatsQueryEstimate() string {
	return commonTableStatsQueryEstimate()
}

func (b *PostgreSQL14Builder) ColumnStatsQuery() string {
	return commonColumnStatsQuery()
}

func (b *PostgreSQL14Builder) IndexStatsQuery() string {
	// PG14-15: no last_idx_scan column
	return `
		SELECT
			idx_scan,
			idx_tup_read,
			idx_tup_fetch
		FROM pg_stat_user_indexes
		WHERE indexrelid = $1
	`
}

func (b *PostgreSQL14Builder) ViewDefinitionQuery() string {
	return commonViewDefinitionQuery()
}

func (b *PostgreSQL14Builder) XminDetectionQuery() string {
	return commonXminDetectionQuery()
}

func (b *PostgreSQL14Builder) ExtensionsQuery() string {
	return commonExtensionsQuery()
}

func (b *PostgreSQL14Builder) SettingsQuery() string {
	return commonSettingsQuery()
}

func (b *PostgreSQL14Builder) DatabaseOIDQuery() string {
	return commonDatabaseOIDQuery()
}

func (b *PostgreSQL14Builder) IndexColumnsQuery() string {
	return commonIndexColumnsQuery()
}

func (b *PostgreSQL14Builder) SupportsHelperFunctions() bool {
	return false
}

// --- PostgreSQL 16+ Builder ---

type PostgreSQL16Builder struct {
	majorVer int
}

func (b *PostgreSQL16Builder) MajorVersion() int { return b.majorVer }

func (b *PostgreSQL16Builder) TablesQuery() string {
	return pg12PlusTablesQuery()
}

func (b *PostgreSQL16Builder) TablesQueryQualified() string {
	return pg12PlusTablesQueryQualified()
}

func (b *PostgreSQL16Builder) TablesQueryEstimate() string {
	return pg12PlusTablesQueryEstimate()
}

func (b *PostgreSQL16Builder) ColumnsQuery() string {
	return commonColumnsQuery()
}

func (b *PostgreSQL16Builder) IndexesQuery() string {
	return commonIndexesQuery()
}

func (b *PostgreSQL16Builder) IndexesQueryQualified() string {
	return commonIndexesQueryQualified()
}

func (b *PostgreSQL16Builder) IndexesQueryEstimate() string {
	return commonIndexesQueryEstimate()
}

func (b *PostgreSQL16Builder) ConstraintsQuery() string {
	return commonConstraintsQuery()
}

func (b *PostgreSQL16Builder) ForeignKeyDetailsQuery() string {
	return commonForeignKeyDetailsQuery()
}

func (b *PostgreSQL16Builder) TableStatsQuery() string {
	return `
		SELECT
			sut.n_live_tup,
			sut.n_dead_tup,
			sut.n_mod_since_analyze,
			sut.last_vacuum,
			sut.last_autovacuum,
			sut.last_analyze,
			sut.last_autoanalyze,
			sut.seq_scan,
			sut.seq_tup_read,
			COALESCE(sut.idx_scan, 0),
			COALESCE(sut.idx_tup_fetch, 0),
			pg_table_size(sut.relid) AS size_bytes,
			pg_total_size(sut.relid) AS total_size_bytes
		FROM pg_stat_user_tables sut
		WHERE sut.relid = $1
	`
}

func (b *PostgreSQL16Builder) TableStatsQueryQualified() string {
	return `
		SELECT
			sut.n_live_tup,
			sut.n_dead_tup,
			sut.n_mod_since_analyze,
			sut.last_vacuum,
			sut.last_autovacuum,
			sut.last_analyze,
			sut.last_autoanalyze,
			sut.seq_scan,
			sut.seq_tup_read,
			COALESCE(sut.idx_scan, 0),
			COALESCE(sut.idx_tup_fetch, 0),
			pg_catalog.pg_table_size(sut.relid) AS size_bytes,
			pg_catalog.pg_total_size(sut.relid) AS total_size_bytes
		FROM pg_stat_user_tables sut
		WHERE sut.relid = $1
	`
}

func (b *PostgreSQL16Builder) TableStatsQueryEstimate() string {
	return `
		SELECT
			sut.n_live_tup,
			sut.n_dead_tup,
			sut.n_mod_since_analyze,
			sut.last_vacuum,
			sut.last_autovacuum,
			sut.last_analyze,
			sut.last_autoanalyze,
			sut.seq_scan,
			sut.seq_tup_read,
			COALESCE(sut.idx_scan, 0),
			COALESCE(sut.idx_tup_fetch, 0),
			(c.relpages * current_setting('block_size')::bigint) AS size_bytes,
			(c.relpages * current_setting('block_size')::bigint) AS total_size_bytes
		FROM pg_stat_user_tables sut
		JOIN pg_class c ON c.oid = sut.relid
		WHERE sut.relid = $1
	`
}

func (b *PostgreSQL16Builder) ColumnStatsQuery() string {
	return commonColumnStatsQuery()
}

func (b *PostgreSQL16Builder) IndexStatsQuery() string {
	// PG16+: last_idx_scan is available
	return `
		SELECT
			sui.idx_scan,
			sui.idx_tup_read,
			sui.idx_tup_fetch,
			sui.last_idx_scan
		FROM pg_stat_user_indexes sui
		WHERE sui.indexrelid = $1
	`
}

func (b *PostgreSQL16Builder) ViewDefinitionQuery() string {
	return commonViewDefinitionQuery()
}

func (b *PostgreSQL16Builder) XminDetectionQuery() string {
	return commonXminDetectionQuery()
}

func (b *PostgreSQL16Builder) ExtensionsQuery() string {
	return commonExtensionsQuery()
}

func (b *PostgreSQL16Builder) SettingsQuery() string {
	return commonSettingsQuery()
}

func (b *PostgreSQL16Builder) DatabaseOIDQuery() string {
	return commonDatabaseOIDQuery()
}

func (b *PostgreSQL16Builder) IndexColumnsQuery() string {
	return commonIndexColumnsQuery()
}

func (b *PostgreSQL16Builder) SupportsHelperFunctions() bool {
	return false
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SettingsCollector collects PostgreSQL configuration settings
type SettingsCollector struct {
	db     *IgnoredDB
	logger Logger
}

// Logger interface for logging
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
}

// NewSettingsCollector creates a new settings collector
func NewSettingsCollector(db *IgnoredDB, logger Logger) *SettingsCollector {
	return &SettingsCollector{
		db:     db,
		logger: logger,
	}
}

// Collect collects critical PostgreSQL settings
func (c *SettingsCollector) Collect(ctx context.Context) (*SettingsSnapshot, error) {
	criticalSettings := []string{
		// Logging
		"log_connections",
		"log_disconnections",
		"log_statement",
		"log_duration",
		"log_lock_waits",

		// Performance
		"max_connections",
		"shared_buffers",
		"effective_cache_size",
		"work_mem",
		"maintenance_work_mem",
		"random_page_cost",

		// Query Planning
		"join_collapse_limit",
		"from_collapse_limit",

		// Replication
		"wal_level",
		"max_wal_senders",
		"max_replication_slots",

		// Monitoring
		"track_activities",
		"track_counts",
		"track_io_timing",
		"track_functions",

		// Extensions
		"shared_preload_libraries",
		"session_preload_libraries",

		// Transactions
		"default_transaction_isolation",
		"default_transaction_deferrable",

		// Checksums
		"data_checksums",
	}

	snapshot := &SettingsSnapshot{
		CollectedAt: time.Now(),
		Settings:    make(map[string]*Setting),
		Errors:      []ErrorInfo{},
	}

	// Build placeholders
	placeholders := make([]string, len(criticalSettings))
	args := make([]interface{}, len(criticalSettings))
	for i, s := range criticalSettings {
		placeholders[i] = "$" + fmt.Sprintf("%d", i+1)
		args[i] = s
	}

	query := `
		SELECT name, setting, unit, context, vartype, source
		FROM pg_settings
		WHERE name IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY name
	`

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		errInfo := ErrorInfo{
			Category:  "settings",
			Severity:  "error",
			Message:   fmt.Sprintf("failed to query settings: %v", err),
			Timestamp: time.Now(),
		}
		snapshot.Errors = append(snapshot.Errors, errInfo)
		return snapshot, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			name    string
			setting string
			unit    sql.NullString
			ctxVal  string
			vartype string
			source  sql.NullString
		)
		if err := rows.Scan(&name, &setting, &unit, &ctxVal, &vartype, &source); err != nil {
			snapshot.Errors = append(snapshot.Errors, ErrorInfo{
				Category:  "settings",
				Severity:  "warning",
				Code:      "SCAN_ERROR",
				Message:   fmt.Sprintf("failed to scan setting: %v", err),
				Timestamp: time.Now(),
			})
			continue
		}

		s := &Setting{
			Name:    name,
			Value:   setting,
			Context: ctxVal,
			Type:    vartype,
		}
		if unit.Valid {
			s.Unit = unit.String
		}
		if source.Valid {
			s.Source = source.String
		}
		snapshot.Settings[name] = s
	}

	if err = rows.Err(); err != nil {
		snapshot.Errors = append(snapshot.Errors, ErrorInfo{
			Category:  "settings",
			Severity:  "error",
			Message:   fmt.Sprintf("error iterating settings: %v", err),
			Timestamp: time.Now(),
		})
		return snapshot, err
	}

	return snapshot, nil
}

// CollectSetting collects a single setting value
func (c *SettingsCollector) CollectSetting(ctx context.Context, settingName string) (*Setting, error) {
	var (
		name    string
		setting string
		unit    sql.NullString
		ctxVal  string
		vartype string
		source  sql.NullString
	)

	err := c.db.QueryRowContext(ctx,
		"SELECT name, setting, unit, context, vartype, source FROM pg_settings WHERE name = $1",
		settingName).
		Scan(&name, &setting, &unit, &ctxVal, &vartype, &source)

	if err != nil {
		return nil, fmt.Errorf("failed to collect setting %s: %w", settingName, err)
	}

	s := &Setting{
		Name:    name,
		Value:   setting,
		Context: ctxVal,
		Type:    vartype,
	}
	if unit.Valid {
		s.Unit = unit.String
	}
	if source.Valid {
		s.Source = source.String
	}
	return s, nil
}

// ExtensionsCollector collects information about loaded PostgreSQL extensions
type ExtensionsCollector struct {
	db     *IgnoredDB
	logger Logger
}

// NewExtensionsCollector creates a new extensions collector
func NewExtensionsCollector(db *IgnoredDB, logger Logger) *ExtensionsCollector {
	return &ExtensionsCollector{
		db:     db,
		logger: logger,
	}
}

// Collect collects all installed extensions
func (c *ExtensionsCollector) Collect(ctx context.Context) (*ExtensionsSnapshot, error) {
	snapshot := &ExtensionsSnapshot{
		CollectedAt: time.Now(),
		Extensions:  []*ExtensionDefinition{},
		Errors:      []ErrorInfo{},
	}

	query := `
		SELECT
			e.oid,
			e.extname,
			e.extversion,
			e.extrelocatable,
			ns.nspname as schema_name,
			obj_description(e.oid, 'pg_extension') as description,
			string_agg(c.relname, ', ' ORDER BY c.relname) as object_names
		FROM pg_extension e
		LEFT JOIN pg_namespace ns ON e.extnamespace = ns.oid
		LEFT JOIN pg_depend d ON d.refclassid = 'pg_extension'::regclass AND d.refobjid = e.oid
		LEFT JOIN pg_class c ON d.classid = 'pg_class'::regclass AND d.objid = c.oid
		WHERE e.extname NOT IN ('plpgsql')
		GROUP BY e.oid, e.extname, e.extversion, e.extrelocatable, ns.nspname
		ORDER BY e.extname
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		errInfo := ErrorInfo{
			Category:  "extensions",
			Severity:  "error",
			Message:   fmt.Sprintf("failed to query extensions: %v", err),
			Timestamp: time.Now(),
		}
		snapshot.Errors = append(snapshot.Errors, errInfo)
		return snapshot, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			oid         uint32
			name        string
			version     string
			relocatable bool
			schema      sql.NullString
			description sql.NullString
			objects     sql.NullString
		)

		if err := rows.Scan(&oid, &name, &version, &relocatable, &schema, &description, &objects); err != nil {
			snapshot.Errors = append(snapshot.Errors, ErrorInfo{
				Category:  "extensions",
				Severity:  "warning",
				Code:      "SCAN_ERROR",
				Message:   fmt.Sprintf("failed to scan extension: %v", err),
				Timestamp: time.Now(),
			})
			continue
		}

		ext := &ExtensionDefinition{
			OID:         oid,
			Name:        name,
			Version:     version,
			Relocatable: relocatable,
		}

		if schema.Valid {
			ext.SchemaName = schema.String
		}

		if description.Valid {
			ext.Description = description.String
		}

		if objects.Valid {
			ext.Objects = strings.Split(objects.String, ", ")
		}

		snapshot.Extensions = append(snapshot.Extensions, ext)
	}

	if err = rows.Err(); err != nil {
		snapshot.Errors = append(snapshot.Errors, ErrorInfo{
			Category:  "extensions",
			Severity:  "error",
			Message:   fmt.Sprintf("error iterating extensions: %v", err),
			Timestamp: time.Now(),
		})
		return snapshot, err
	}

	return snapshot, nil
}

// CollectExtension collects information about a specific extension
func (c *ExtensionsCollector) CollectExtension(ctx context.Context, extName string) (*ExtensionDefinition, error) {
	var (
		oid         uint32
		name        string
		version     string
		relocatable bool
		schema      sql.NullString
		description sql.NullString
	)

	err := c.db.QueryRowContext(ctx,
		`SELECT e.oid, e.extname, e.extversion, e.extrelocatable, ns.nspname, 
		        obj_description(e.oid, 'pg_extension')
		 FROM pg_extension e
		 LEFT JOIN pg_namespace ns ON e.extnamespace = ns.oid
		 WHERE e.extname = $1`,
		extName).
		Scan(&oid, &name, &version, &relocatable, &schema, &description)

	if err != nil {
		return nil, fmt.Errorf("failed to collect extension %s: %w", extName, err)
	}

	ext := &ExtensionDefinition{
		OID:         oid,
		Name:        name,
		Version:     version,
		Relocatable: relocatable,
	}

	if schema.Valid {
		ext.SchemaName = schema.String
	}

	if description.Valid {
		ext.Description = description.String
	}

	return ext, nil
}

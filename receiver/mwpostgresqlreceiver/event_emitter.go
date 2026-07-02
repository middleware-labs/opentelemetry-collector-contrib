// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

// EventEmitter emits OTEL Logs Events for schema collection.
//
// Emission strategy: one log record per table (+ header + footer) instead of
// one giant record.  All records share db.oid + collected_at + schema_version
// so the UI can correlate them and query individual tables efficiently.
type EventEmitter struct {
	logger            Logger
	serviceInstanceID string
}

// NewEventEmitter creates a new event emitter
func NewEventEmitter(logger Logger, serviceInstanceID string) *EventEmitter {
	return &EventEmitter{
		logger:            logger,
		serviceInstanceID: serviceInstanceID,
	}
}

// setResourceAttrs sets the resource-level attributes that identify this instance.
func (ee *EventEmitter) setResourceAttrs(resource pcommon.Resource) {
	ra := resource.Attributes()
	ra.PutStr("service.instance.id", ee.serviceInstanceID)
	ra.PutStr("db.system.name", "postgresql")
}

// EmitSchemaCollectionEvent emits a full schema snapshot as multiple log records:
//   - 1 header record  (event.type = schema_collection_header)
//   - N table records   (event.type = schema_table)
//   - 1 footer record   (event.type = schema_collection_footer)
func (ee *EventEmitter) EmitSchemaCollectionEvent(_ context.Context, event *SchemaCollectionEvent) (plog.Logs, error) {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	ee.setResourceAttrs(resourceLogs.Resource())
	scopeLogs := resourceLogs.ScopeLogs().AppendEmpty()
	logRecords := scopeLogs.LogRecords()

	ts := pcommon.NewTimestampFromTime(event.CollectedAt)

	// --- Header record ---
	header := logRecords.AppendEmpty()
	header.SetTimestamp(ts)
	header.SetSeverityNumber(plog.SeverityNumberInfo)
	header.SetSeverityText("info")
	header.Body().SetStr(fmt.Sprintf("Schema snapshot: %s — %d tables, %d rows, %d columns, %d indexes (%s)",
		event.DatabaseName,
		event.Statistics.TableCount,
		event.Statistics.TotalRowCount,
		event.Statistics.ColumnCount,
		event.Statistics.IndexCount,
		formatBytes(event.Statistics.TotalSizeBytes)))

	ha := header.Attributes()
	ee.setCommonAttrs(ha, event)
	ha.PutStr("event.type", "schema_collection_header")
	if event.Statistics != nil {
		ha.PutInt("collection.table_count", int64(event.Statistics.TableCount))
		ha.PutInt("collection.total_row_count", event.Statistics.TotalRowCount)
		ha.PutInt("collection.column_count", int64(event.Statistics.ColumnCount))
		ha.PutInt("collection.index_count", int64(event.Statistics.IndexCount))
		ha.PutInt("collection.constraint_count", int64(event.Statistics.ConstraintCount))
		ha.PutInt("collection.extension_count", int64(event.Statistics.ExtensionCount))
		ha.PutInt("collection.total_size_bytes", event.Statistics.TotalSizeBytes)
		ha.PutInt("collection.duration_ms", event.Statistics.CollectionDurationMs)
		ha.PutBool("collection.has_errors", event.Statistics.HasErrors)
		ha.PutInt("collection.error_count", int64(event.Statistics.ErrorCount))
		ha.PutDouble("collection.completion_ratio", event.Statistics.CompletionRatio)
	}

	// --- Per-table records ---
	for _, table := range event.Tables {
		rec := logRecords.AppendEmpty()
		rec.SetTimestamp(ts)
		rec.SetSeverityNumber(plog.SeverityNumberInfo)
		rec.SetSeverityText("info")
		rec.Body().SetStr(ee.tableBodyString(event.DatabaseName, table))

		ta := rec.Attributes()
		ee.setCommonAttrs(ta, event)
		ee.setTableAttrs(ta, table)
	}

	// --- Footer record ---
	footer := logRecords.AppendEmpty()
	footer.SetTimestamp(ts)
	footer.SetSeverityNumber(plog.SeverityNumberInfo)
	footer.SetSeverityText("info")
	footer.Body().SetStr(fmt.Sprintf("Schema snapshot complete: %s — %d tables collected in %dms",
		event.DatabaseName, event.Statistics.TableCount, event.Statistics.CollectionDurationMs))

	fa := footer.Attributes()
	ee.setCommonAttrs(fa, event)
	fa.PutStr("event.type", "schema_collection_footer")

	if len(event.Extensions) > 0 {
		fa.PutStr("collection.extensions", ee.encodeExtensions(event.Extensions))
		fa.PutInt("collection.extension_count", int64(len(event.Extensions)))
	}
	if len(event.Settings) > 0 {
		fa.PutStr("collection.settings", ee.encodeSettings(event.Settings))
		fa.PutInt("collection.setting_count", int64(len(event.Settings)))
	}
	if len(event.ErrorMessages) > 0 {
		fa.PutStr("collection.errors", ee.encodeErrors(event.ErrorMessages))
	}

	ee.logger.Debug("Emitted schema collection",
		"database", event.DatabaseName,
		"type", event.CollectionType,
		"table_count", len(event.Tables),
		"record_count", logRecords.Len(),
		"duration_ms", event.Statistics.CollectionDurationMs)

	return logs, nil
}

// EmitSettingsEvent emits a standalone settings collection event.
// It carries the same common attributes as the schema snapshot so the two can
// be correlated by schema_version + collected_at.
func (ee *EventEmitter) EmitSettingsEvent(_ context.Context, event *SchemaCollectionEvent) (plog.Logs, error) {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	ee.setResourceAttrs(resourceLogs.Resource())
	scopeLogs := resourceLogs.ScopeLogs().AppendEmpty()

	rec := scopeLogs.LogRecords().AppendEmpty()
	rec.SetTimestamp(pcommon.NewTimestampFromTime(event.CollectedAt))
	rec.SetSeverityNumber(plog.SeverityNumberInfo)
	rec.SetSeverityText("info")
	rec.Body().SetStr(fmt.Sprintf("Settings snapshot: %s — %d settings collected",
		event.DatabaseName, len(event.Settings)))

	attrs := rec.Attributes()
	ee.setCommonAttrs(attrs, event)
	attrs.PutStr("event.type", "settings_collection")
	attrs.PutInt("collection.setting_count", int64(len(event.Settings)))
	attrs.PutStr("collection.settings", ee.encodeSettings(event.Settings))

	return logs, nil
}

// EmitExtensionsEvent emits a standalone extensions collection event.
// It carries the same common attributes as the schema snapshot so the two can
// be correlated by schema_version + collected_at.
func (ee *EventEmitter) EmitExtensionsEvent(_ context.Context, event *SchemaCollectionEvent) (plog.Logs, error) {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	ee.setResourceAttrs(resourceLogs.Resource())
	scopeLogs := resourceLogs.ScopeLogs().AppendEmpty()

	extNames := make([]string, 0, len(event.Extensions))
	for _, e := range event.Extensions {
		extNames = append(extNames, e.Name+"@"+e.Version)
	}

	rec := scopeLogs.LogRecords().AppendEmpty()
	rec.SetTimestamp(pcommon.NewTimestampFromTime(event.CollectedAt))
	rec.SetSeverityNumber(plog.SeverityNumberInfo)
	rec.SetSeverityText("info")
	rec.Body().SetStr(fmt.Sprintf("Extensions snapshot: %s — %d extensions: %s",
		event.DatabaseName, len(event.Extensions),
		strings.Join(extNames, ", ")))

	attrs := rec.Attributes()
	ee.setCommonAttrs(attrs, event)
	attrs.PutStr("event.type", "extensions_collection")
	attrs.PutInt("collection.extension_count", int64(len(event.Extensions)))
	attrs.PutStr("collection.extensions", ee.encodeExtensions(event.Extensions))

	return logs, nil
}

// --- Internal helpers ---

// setCommonAttrs sets the attributes shared by every record in a collection batch.
func (ee *EventEmitter) setCommonAttrs(attrs pcommon.Map, event *SchemaCollectionEvent) {
	attrs.PutStr("db.system.name", "postgresql")
	attrs.PutStr("db.namespace", event.DatabaseName)
	attrs.PutInt("db.oid", int64(event.DatabaseOID))
	attrs.PutInt("db.postgresql.version_num", int64(event.PostgresVersion))
	attrs.PutStr("db.postgresql.version_string", event.VersionString)
	attrs.PutStr("cloud.provider", event.CloudProvider)
	attrs.PutStr("collection.type", string(event.CollectionType))
	attrs.PutInt("collection.collected_at", event.CollectedAtTimestamp)
	attrs.PutInt("collection.schema_version", int64(event.SchemaVersion))
}

// setTableAttrs writes per-table attributes.
// Table-level scalars are top-level attributes (queryable in the backend).
// Detail arrays (columns, indexes, constraints) are JSON strings.
func (ee *EventEmitter) setTableAttrs(attrs pcommon.Map, table *TableDefinition) {
	attrs.PutStr("event.type", "schema_table")
	attrs.PutInt("table.oid", int64(table.OID))
	attrs.PutStr("table.schema_name", table.SchemaName)
	attrs.PutStr("table.name", table.Name)
	attrs.PutStr("table.type", table.Type)
	attrs.PutStr("table.owner", table.Owner)
	attrs.PutInt("table.xmin", int64(table.Xmin))
	attrs.PutInt("table.tablespace", int64(table.Tablespace))
	attrs.PutBool("table.has_oids", table.HasOids)

	if table.Description != "" {
		attrs.PutStr("table.description", table.Description)
	}

	// Stats as top-level attributes — directly queryable
	attrs.PutInt("table.live_tuples", table.LiveTuples)
	attrs.PutInt("table.dead_tuples", table.DeadTuples)
	attrs.PutInt("table.mod_since_analyze", table.ModSinceAnalyze)
	attrs.PutInt("table.seq_scans", table.SeqScans)
	attrs.PutInt("table.seq_tup_read", table.SeqTupRead)
	attrs.PutInt("table.index_scans", table.IndexScans)
	attrs.PutInt("table.index_tup_fetch", table.IndexTupFetch)
	attrs.PutInt("table.size_bytes", table.SizeBytes)
	attrs.PutInt("table.total_size_bytes", table.TotalSizeBytes)

	if table.LastVacuum != nil {
		attrs.PutStr("table.last_vacuum", table.LastVacuum.Format(time.RFC3339))
	}
	if table.LastAutovacuum != nil {
		attrs.PutStr("table.last_autovacuum", table.LastAutovacuum.Format(time.RFC3339))
	}
	if table.LastAnalyze != nil {
		attrs.PutStr("table.last_analyze", table.LastAnalyze.Format(time.RFC3339))
	}
	if table.LastAutoanalyze != nil {
		attrs.PutStr("table.last_autoanalyze", table.LastAutoanalyze.Format(time.RFC3339))
	}

	if table.ViewDefinition != "" {
		attrs.PutStr("table.view_definition", table.ViewDefinition)
	}
	if table.IsPartitioned {
		attrs.PutBool("table.is_partitioned", true)
		attrs.PutInt("table.parent_oid", int64(table.ParentOID))
		attrs.PutStr("table.partition_expr", table.PartitionExpr)
	}

	// Counts for quick filtering
	attrs.PutInt("table.column_count", int64(len(table.Columns)))
	attrs.PutInt("table.index_count", int64(len(table.Indexes)))
	attrs.PutInt("table.constraint_count", int64(len(table.Constraints)))

	// Detail arrays as JSON (parseable on the UI side)
	attrs.PutStr("table.columns", ee.encodeColumns(table.Columns))
	attrs.PutStr("table.indexes", ee.encodeIndexes(table.Indexes))
	attrs.PutStr("table.constraints", ee.encodeConstraints(table.Constraints))
}

// --- JSON encoding helpers ---

func (ee *EventEmitter) encodeColumns(columns []*ColumnDefinition) string {
	type colJSON struct {
		OID          uint32  `json:"oid"`
		Name         string  `json:"name"`
		TypeName     string  `json:"type_name"`
		TypeOID      uint32  `json:"type_oid"`
		TypeModifier int32   `json:"type_modifier"`
		NotNull      bool    `json:"not_null"`
		HasDefault   bool    `json:"has_default"`
		DefaultValue string  `json:"default_value,omitempty"`
		Description  string  `json:"description,omitempty"`
		Position     int16   `json:"position"`
		NullFraction float64 `json:"null_fraction,omitempty"`
		AvgWidth     int32   `json:"avg_width,omitempty"`
		NDistinct    float64 `json:"n_distinct,omitempty"`
		Correlation  float64 `json:"correlation,omitempty"`
	}

	out := make([]colJSON, len(columns))
	for i, c := range columns {
		out[i] = colJSON{
			OID:          c.OID,
			Name:         c.Name,
			TypeName:     c.TypeName,
			TypeOID:      c.TypeOID,
			TypeModifier: c.TypeModifier,
			NotNull:      c.NotNull,
			HasDefault:   c.HasDefault,
			DefaultValue: c.DefaultValue,
			Description:  c.Description,
			Position:     c.Position,
			NullFraction: c.NullFraction,
			AvgWidth:     c.AvgWidth,
			NDistinct:    c.NDistinct,
			Correlation:  c.Correlation,
		}
	}
	return ee.marshal(out)
}

func (ee *EventEmitter) encodeIndexes(indexes []*IndexDefinition) string {
	type idxJSON struct {
		OID         uint32     `json:"oid"`
		Name        string     `json:"name"`
		TableOID    uint32     `json:"table_oid"`
		IsPrimary   bool       `json:"is_primary"`
		IsUnique    bool       `json:"is_unique"`
		IsValid     bool       `json:"is_valid"`
		IsExclusion bool       `json:"is_exclusion"`
		IndexType   string     `json:"index_type"`
		Definition  string     `json:"definition"`
		Partial     string     `json:"partial,omitempty"`
		Columns     []string   `json:"columns,omitempty"`
		SizeBytes   int64      `json:"size_bytes"`
		ScanCount   int64      `json:"scan_count"`
		TupleRead   int64      `json:"tuple_read"`
		TupleFetch  int64      `json:"tuple_fetch"`
		LastScan    *time.Time `json:"last_scan,omitempty"`
	}

	out := make([]idxJSON, len(indexes))
	for i, idx := range indexes {
		out[i] = idxJSON{
			OID:         idx.OID,
			Name:        idx.Name,
			TableOID:    idx.TableOID,
			IsPrimary:   idx.IsPrimary,
			IsUnique:    idx.IsUnique,
			IsValid:     idx.IsValid,
			IsExclusion: idx.IsExclusion,
			IndexType:   idx.IndexType,
			Definition:  idx.Definition,
			Partial:     idx.Partial,
			Columns:     idx.Columns,
			SizeBytes:   idx.SizeBytes,
			ScanCount:   idx.ScanCount,
			TupleRead:   idx.TupleRead,
			TupleFetch:  idx.TupleFetch,
			LastScan:    idx.LastScan,
		}
	}
	return ee.marshal(out)
}

func (ee *EventEmitter) encodeConstraints(constraints []*ConstraintDefinition) string {
	type conJSON struct {
		OID                uint32   `json:"oid"`
		Name               string   `json:"name"`
		Type               string   `json:"type"`
		TableOID           uint32   `json:"table_oid"`
		Definition         string   `json:"definition"`
		Deferrable         bool     `json:"deferrable"`
		Deferred           bool     `json:"deferred"`
		Validated          bool     `json:"validated"`
		ReferencedTableOID uint32   `json:"referenced_table_oid,omitempty"`
		ColumnNames        []string `json:"column_names,omitempty"`
		ReferencedColumns  []string `json:"referenced_columns,omitempty"`
	}

	out := make([]conJSON, len(constraints))
	for i, cn := range constraints {
		out[i] = conJSON{
			OID:                cn.OID,
			Name:               cn.Name,
			Type:               cn.Type,
			TableOID:           cn.TableOID,
			Definition:         cn.Definition,
			Deferrable:         cn.Deferrable,
			Deferred:           cn.Deferred,
			Validated:          cn.Validated,
			ReferencedTableOID: cn.ReferencedTableOID,
			ColumnNames:        cn.ColumnNames,
			ReferencedColumns:  cn.ReferencedColumns,
		}
	}
	return ee.marshal(out)
}

func (ee *EventEmitter) encodeExtensions(extensions []*ExtensionDefinition) string {
	type extJSON struct {
		OID         uint32   `json:"oid"`
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		SchemaName  string   `json:"schema_name"`
		Relocatable bool     `json:"relocatable"`
		Objects     []string `json:"objects,omitempty"`
	}

	out := make([]extJSON, len(extensions))
	for i, e := range extensions {
		out[i] = extJSON{
			OID:         e.OID,
			Name:        e.Name,
			Version:     e.Version,
			SchemaName:  e.SchemaName,
			Relocatable: e.Relocatable,
			Objects:     e.Objects,
		}
	}
	return ee.marshal(out)
}

func (ee *EventEmitter) encodeSettings(settings map[string]*Setting) string {
	type settingJSON struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Unit   string `json:"unit,omitempty"`
		Source string `json:"source,omitempty"`
	}

	out := make([]settingJSON, 0, len(settings))
	for _, s := range settings {
		out = append(out, settingJSON{
			Name:   s.Name,
			Value:  s.Value,
			Unit:   s.Unit,
			Source: s.Source,
		})
	}
	return ee.marshal(out)
}

func (ee *EventEmitter) encodeErrors(errors []string) string {
	return ee.marshal(errors)
}

func (ee *EventEmitter) marshal(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		ee.logger.Error("failed to encode JSON", "error", err)
		return "[]"
	}
	return string(data)
}

// relkindLabel maps pg_class.relkind to a readable label.
func relkindLabel(kind string) string {
	switch kind {
	case "r":
		return "table"
	case "p":
		return "partitioned_table"
	case "v":
		return "view"
	case "m":
		return "materialized_view"
	case "f":
		return "foreign_table"
	case "t":
		return "toast"
	default:
		return kind
	}
}

// tableBodyString builds a human-readable body for a per-table log record.
func (ee *EventEmitter) tableBodyString(dbName string, table *TableDefinition) string {
	kind := relkindLabel(table.Type)
	rowInfo := ""
	if table.Type == "r" || table.Type == "p" || table.Type == "m" {
		rowInfo = fmt.Sprintf(", %d rows", table.LiveTuples)
	}
	return fmt.Sprintf("%s: %s.%s.%s — %d cols, %d idx, %d con%s, %s",
		kind, dbName, table.SchemaName, table.Name,
		len(table.Columns), len(table.Indexes), len(table.Constraints),
		rowInfo, formatBytes(table.TotalSizeBytes))
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

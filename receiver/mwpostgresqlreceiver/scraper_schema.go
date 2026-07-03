// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
)

// zapAdapter adapts zap.Logger to the Logger interface expected by SchemaCollector
type zapAdapter struct {
	l *zap.Logger
}

func (z *zapAdapter) Debug(msg string, fields ...interface{}) {
	z.l.Sugar().Debugw(msg, fields...)
}

func (z *zapAdapter) Info(msg string, fields ...interface{}) {
	z.l.Sugar().Infow(msg, fields...)
}

func (z *zapAdapter) Warn(msg string, fields ...interface{}) {
	z.l.Sugar().Warnw(msg, fields...)
}

func (z *zapAdapter) Error(msg string, fields ...interface{}) {
	z.l.Sugar().Errorw(msg, fields...)
}

// scrapeSchemaCollection scrapes the schema collection metrics.
//
// This function is called at the global collection_interval (e.g. 1s) but
// internally throttles to schema_collection.collection_interval (e.g. 1h).
// On each run at collection_interval, xmin change detection is used to decide if a full snapshot is needed
// (e.g. 30s) and triggers a full snapshot when changes are found.
func (p *postgreSQLScraper) scrapeSchemaCollection(ctx context.Context) (plog.Logs, error) {
	// Throttle: the OTel scraper framework calls us at the global
	// collection_interval (e.g. 1s), but we only want to check for changes
	// at schema_collection.collection_interval (default 60s).
	checkInterval := 60 * time.Second
	if p.config.SchemaCollection.CollectionInterval > 0 {
		checkInterval = p.config.SchemaCollection.CollectionInterval
	}
	if !p.lastSchemaCheck.IsZero() && time.Since(p.lastSchemaCheck) < checkInterval {
		return plog.NewLogs(), nil
	}
	p.lastSchemaCheck = time.Now()

	// 1. Acquire a connection to the default database for discovery/detection
	listClient, err := p.clientFactory.getClient(defaultPostgreSQLDatabase)
	if err != nil {
		p.logger.Error("Failed to initialize connection to postgres for schema collection", zap.Error(err))
		return plog.NewLogs(), err
	}
	defer listClient.Close()

	pgClient, ok := listClient.(*postgreSQLClient)
	if !ok {
		return plog.NewLogs(), fmt.Errorf("incompatible client type for schema collection")
	}

	wrappedDB := pgClient.client

	// 2. Detect Version (server-level, done once)
	versionDetector := NewVersionDetector(wrappedDB)
	versionInfo, err := versionDetector.Detect(ctx)
	if err != nil {
		p.logger.Error("Failed to detect PostgreSQL version", zap.Error(err))
		return plog.NewLogs(), err
	}

	// 3. Detect Cloud Platform (server-level, done once)
	cloudDetector := NewCloudDetector(wrappedDB)
	cloudProvider, cloudMetadata, err := cloudDetector.Detect(ctx)
	if err != nil {
		p.logger.Error("Failed to detect cloud provider", zap.Error(err))
		return plog.NewLogs(), err
	}

	SetCloudFlags(versionInfo, cloudProvider)

	// 4. Resolve database list: use configured databases, or discover all
	databases := p.config.Databases
	if len(databases) == 0 {
		dbList, dbErr := listClient.listDatabases(ctx)
		if dbErr != nil {
			p.logger.Error("Failed to list databases for schema collection", zap.Error(dbErr))
			return plog.NewLogs(), dbErr
		}
		databases = dbList
	}
	// Apply exclusions
	var filteredDatabases []string
	for _, db := range databases {
		if _, excluded := p.excludes[db]; !excluded {
			filteredDatabases = append(filteredDatabases, db)
		}
	}
	databases = filteredDatabases

	if len(databases) == 0 {
		p.logger.Warn("No databases to collect schema from")
		return plog.NewLogs(), nil
	}

	// 5. Pre-compute shared config pieces
	logger := &zapAdapter{l: p.logger}

	parseTableConfig := func(tables []string) map[string][]string {
		result := make(map[string][]string)
		for _, t := range tables {
			parts := strings.SplitN(t, ".", 2)
			if len(parts) == 2 {
				result[parts[0]] = append(result[parts[0]], parts[1])
			} else {
				result["public"] = append(result["public"], t)
			}
		}
		return result
	}

	excludeSchemas := make(map[string]bool)
	includeSchemas := make(map[string]bool)
	for _, s := range p.config.SchemaCollection.ExcludeSchemas {
		excludeSchemas[s] = true
	}
	for _, s := range p.config.SchemaCollection.IncludeSchemas {
		includeSchemas[s] = true
	}

	filterConfig := &FilterConfig{
		IncludeSchemas: includeSchemas,
		ExcludeSchemas: excludeSchemas,
		ExcludeTables:  parseTableConfig(p.config.SchemaCollection.ExcludeTables),
		IncludeTables:  parseTableConfig(p.config.SchemaCollection.IncludeTables),
	}

	// 6. Decide whether to collect (global check — applies to all databases)
	eventEmitter := NewEventEmitter(logger, p.serviceInstanceID)
	lastSnapshot := p.changeTracker.GetLastSnapshot()
	timeSinceLastSnapshot := time.Since(lastSnapshot)

	snapshotInterval := 24 * time.Hour
	if p.config.SchemaCollection.RefreshInterval > 0 {
		snapshotInterval = p.config.SchemaCollection.RefreshInterval
	}

	needsCollection := false
	reason := ""

	if lastSnapshot.IsZero() {
		needsCollection = true
		reason = "initial"
	} else if timeSinceLastSnapshot >= snapshotInterval {
		needsCollection = true
		reason = "scheduled_refresh"
	}

	// For xmin detection, use the default connection for a quick check
	if !needsCollection {
		sqlBuilder := NewSQLBuilder(versionInfo.VersionNum)
		_ = sqlBuilder // xmin detection needs a collector; we'll try with the first database
		// Create a temporary collector on the default connection for change detection
		tmpConfig := &CollectorConfig{
			DatabaseName:    defaultPostgreSQLDatabase,
			ContinueOnError: true,
			ExcludeSchemas:  excludeSchemas,
			IncludeSchemas:  includeSchemas,
		}
		tmpCollector, tmpErr := NewSchemaCollector(
			wrappedDB, versionInfo, cloudProvider, cloudMetadata,
			p.changeTracker, tmpConfig, filterConfig, logger,
		)
		if tmpErr == nil {
			changedOIDs, detectErr := tmpCollector.DetectChanges(ctx)
			if detectErr != nil {
				p.logger.Error("Failed to detect changes", zap.Error(detectErr))
				return plog.NewLogs(), detectErr
			}
			if len(changedOIDs) > 0 {
				needsCollection = true
				reason = fmt.Sprintf("xmin_change_detected(%d tables)", len(changedOIDs))
			}
		}
	}

	if !needsCollection {
		p.logger.Debug("No schema changes detected, skipping collection")
		return plog.NewLogs(), nil
	}

	p.logger.Info("Starting full schema snapshot",
		zap.String("reason", reason),
		zap.Strings("databases", databases),
		zap.Duration("time_since_last", timeSinceLastSnapshot))

	// 7. Collect schema from each database
	allEvents := plog.NewLogs()

	for _, dbName := range databases {
		dbEvents, dbErr := p.collectSchemaForDatabase(ctx, dbName, versionInfo, cloudProvider, cloudMetadata, excludeSchemas, includeSchemas, filterConfig, eventEmitter, logger, reason)
		if dbErr != nil {
			p.logger.Error("Failed to collect schema for database",
				zap.String("database", dbName), zap.Error(dbErr))
			if !p.config.SchemaCollection.ContinueOnError {
				return plog.NewLogs(), dbErr
			}
			continue
		}
		dbEvents.ResourceLogs().MoveAndAppendTo(allEvents.ResourceLogs())
	}

	stripEmptyLogAttrs(allEvents)
	return allEvents, nil
}

// collectSchemaForDatabase collects schema from a single database.
func (p *postgreSQLScraper) collectSchemaForDatabase(
	ctx context.Context,
	dbName string,
	versionInfo *VersionInfo,
	cloudProvider CloudProvider,
	cloudMetadata *CloudMetadata,
	excludeSchemas, includeSchemas map[string]bool,
	filterConfig *FilterConfig,
	eventEmitter *EventEmitter,
	logger *zapAdapter,
	reason string,
) (plog.Logs, error) {
	// Connect to this specific database
	dbClient, err := p.clientFactory.getClient(dbName)
	if err != nil {
		return plog.NewLogs(), fmt.Errorf("failed to connect to database %s: %w", dbName, err)
	}
	defer dbClient.Close()

	pgClient, ok := dbClient.(*postgreSQLClient)
	if !ok {
		return plog.NewLogs(), fmt.Errorf("incompatible client type for database %s", dbName)
	}

	wrappedDbSQL := pgClient.client

	// Resolve database OID
	var dbOID uint32
	sqlBuilder := NewSQLBuilder(versionInfo.VersionNum)
	err = wrappedDbSQL.QueryRowContext(ctx, sqlBuilder.DatabaseOIDQuery()).Scan(&dbOID)
	if err != nil {
		p.logger.Warn("Failed to resolve database OID, using 0",
			zap.String("database", dbName), zap.Error(err))
	}

	collectorConfig := &CollectorConfig{
		DatabaseName:      dbName,
		DatabaseOID:      dbOID,
		ContinueOnError:  p.config.SchemaCollection.ContinueOnError,
		CollectExtensions: p.config.SchemaCollection.CollectExtensions,
		CollectSettings:   p.config.SchemaCollection.CollectSettings,
		ExcludeSchemas:    excludeSchemas,
		IncludeSchemas:    includeSchemas,
	}

	collector, err := NewSchemaCollector(
		wrappedDbSQL, versionInfo, cloudProvider, cloudMetadata,
		p.changeTracker, collectorConfig, filterConfig, logger,
	)
	if err != nil {
		return plog.NewLogs(), fmt.Errorf("failed to initialize collector for %s: %w", dbName, err)
	}

	event, collectErr := collector.Collect(ctx)
	if collectErr != nil {
		return plog.NewLogs(), fmt.Errorf("failed to collect schema for %s: %w", dbName, collectErr)
	}

	tableNames := make([]string, 0, len(event.Tables))
	for _, t := range event.Tables {
		tableNames = append(tableNames, t.SchemaName+"."+t.Name)
	}
	p.logger.Info("Schema snapshot collected",
		zap.String("database", event.DatabaseName),
		zap.String("trigger", reason),
		zap.Int("table_count", len(event.Tables)),
		zap.Strings("tables", tableNames),
		zap.Int64("duration_ms", event.Statistics.CollectionDurationMs))

	events, emitErr := eventEmitter.EmitSchemaCollectionEvent(ctx, event)
	if emitErr != nil {
		return plog.NewLogs(), emitErr
	}

	// Emit standalone settings event
	if len(event.Settings) > 0 {
		settingsLogs, err := eventEmitter.EmitSettingsEvent(ctx, event)
		if err == nil {
			settingsLogs.ResourceLogs().MoveAndAppendTo(events.ResourceLogs())
		} else {
			p.logger.Warn("Failed to emit settings event",
				zap.String("database", dbName), zap.Error(err))
		}
	}

	// Emit standalone extensions event
	if len(event.Extensions) > 0 {
		extLogs, err := eventEmitter.EmitExtensionsEvent(ctx, event)
		if err == nil {
			extLogs.ResourceLogs().MoveAndAppendTo(events.ResourceLogs())
		} else {
			p.logger.Warn("Failed to emit extensions event",
				zap.String("database", dbName), zap.Error(err))
		}
	}

	return events, nil
}

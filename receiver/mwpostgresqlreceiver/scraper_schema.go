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
func (p *postgreSQLScraper) scrapeSchemaCollection(ctx context.Context) (retLogs plog.Logs, retErr error) {
	defer recoverScrape(p.logger, "schema_collection", &retErr)

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

	// 6. Collect schema from each database, deciding per database whether it
	// needs collecting at all.
	//
	// The decision is per database because the state it consults is per
	// database: each database has its own catalog, its own OIDs and its own
	// xmin snapshot. A single global decision cannot express "this database
	// changed and that one did not", so any change anywhere forced a full
	// collection of every database — which, for a server with many databases
	// and a stable schema, is nearly all of the work this receiver does.
	eventEmitter := NewEventEmitter(logger, p.serviceInstanceID)

	snapshotInterval := 24 * time.Hour
	if p.config.SchemaCollection.RefreshInterval > 0 {
		snapshotInterval = p.config.SchemaCollection.RefreshInterval
	}

	allEvents := plog.NewLogs()
	collected, skipped := 0, 0

	for _, dbName := range databases {
		reason, needsCollection, decideErr := p.schemaCollectionReason(
			ctx, dbName, snapshotInterval, versionInfo, cloudProvider, cloudMetadata,
			excludeSchemas, includeSchemas, filterConfig, logger,
		)
		if decideErr != nil {
			p.logger.Error("Failed to detect schema changes for database",
				zap.String("database", dbName), zap.Error(decideErr))
			if !p.config.SchemaCollection.ContinueOnError {
				return plog.NewLogs(), decideErr
			}
			continue
		}

		if !needsCollection {
			skipped++
			continue
		}

		dbEvents, dbErr := p.collectSchemaForDatabase(ctx, dbName, versionInfo, cloudProvider, cloudMetadata, excludeSchemas, includeSchemas, filterConfig, eventEmitter, logger, reason)
		if dbErr != nil {
			p.logger.Error("Failed to collect schema for database",
				zap.String("database", dbName), zap.Error(dbErr))
			if !p.config.SchemaCollection.ContinueOnError {
				return plog.NewLogs(), dbErr
			}
			continue
		}
		collected++
		dbEvents.ResourceLogs().MoveAndAppendTo(allEvents.ResourceLogs())
	}

	if collected == 0 {
		p.logger.Debug("No schema changes detected, skipping collection",
			zap.Int("databases_skipped", skipped))
		return plog.NewLogs(), nil
	}

	p.logger.Info("Schema snapshot complete",
		zap.Int("databases_collected", collected),
		zap.Int("databases_skipped", skipped))

	stripEmptyLogAttrs(allEvents)
	return allEvents, nil
}

// schemaCollectionReason decides whether a single database needs a full schema
// collection this cycle, and why.
//
// A database is collected when it has never been snapshotted, when its snapshot
// is older than the refresh interval, or when xmin detection shows at least one
// table whose definition changed since that snapshot. Detection reads only OIDs
// and xmins from pg_class, which is far cheaper than reading every column,
// index and constraint — so an unchanged database costs one small query instead
// of a full collection.
func (p *postgreSQLScraper) schemaCollectionReason(
	ctx context.Context,
	dbName string,
	snapshotInterval time.Duration,
	versionInfo *VersionInfo,
	cloudProvider CloudProvider,
	cloudMetadata *CloudMetadata,
	excludeSchemas, includeSchemas map[string]bool,
	filterConfig *FilterConfig,
	logger *zapAdapter,
) (string, bool, error) {
	lastSnapshot, tracked := p.changeTracker.GetLastSnapshotFor(dbName)
	if !tracked || lastSnapshot.IsZero() {
		return "initial", true, nil
	}
	if time.Since(lastSnapshot) >= snapshotInterval {
		return "scheduled_refresh", true, nil
	}

	// Detection must run against the database being checked: OIDs are only
	// unique within a database, so another database's catalog cannot answer
	// this question.
	dbClient, err := p.clientFactory.getClient(dbName)
	if err != nil {
		return "", false, fmt.Errorf("failed to connect to database %s for change detection: %w", dbName, err)
	}
	defer dbClient.Close()

	pgClient, ok := dbClient.(*postgreSQLClient)
	if !ok {
		return "", false, fmt.Errorf("incompatible client type for database %s", dbName)
	}

	collector, err := NewSchemaCollector(
		pgClient.client, versionInfo, cloudProvider, cloudMetadata,
		p.changeTracker,
		&CollectorConfig{
			DatabaseName:    dbName,
			ContinueOnError: true,
			ExcludeSchemas:  excludeSchemas,
			IncludeSchemas:  includeSchemas,
		},
		filterConfig, logger,
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to initialize change detector for %s: %w", dbName, err)
	}

	changedOIDs, err := collector.DetectChanges(ctx)
	if err != nil {
		return "", false, fmt.Errorf("failed to detect changes for %s: %w", dbName, err)
	}
	if len(changedOIDs) > 0 {
		return fmt.Sprintf("xmin_change_detected(%d tables)", len(changedOIDs)), true, nil
	}

	return "", false, nil
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

	p.logger.Info("Schema snapshot collected",
		zap.String("database", event.DatabaseName),
		zap.String("trigger", reason),
		zap.Int("table_count", len(event.Tables)),
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

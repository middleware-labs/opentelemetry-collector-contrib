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

	// 1. Acquire a connection to the default database for discovery
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

	// 2. Detect version and cloud platform. These are server-level facts that
	// hold for the life of the process, so they are detected once and cached.
	server, err := p.detectSchemaServer(ctx, pgClient.client)
	if err != nil {
		return plog.NewLogs(), err
	}

	// 3. Resolve database list: use configured databases, or discover all
	databases := p.config.Databases
	// Schema collection uses the same policy as every other path: discovery
	// only when there is no allowlist, and a discovery failure is reported
	// rather than resolved by widening scope.
	var discovered []string
	if !p.selection.isRestricted() {
		dbList, dbErr := listClient.listDatabases(ctx)
		if dbErr != nil {
			p.logger.Error("Failed to list databases for schema collection", zap.Error(dbErr))
			return plog.NewLogs(), dbErr
		}
		discovered = dbList
	}
	databases = p.selection.effectiveDatabases(discovered)

	if len(databases) == 0 {
		p.logger.Warn("No databases to collect schema from")
		return plog.NewLogs(), nil
	}

	// 4. Pre-compute shared config pieces
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

	snapshotInterval := 24 * time.Hour
	if p.config.SchemaCollection.RefreshInterval > 0 {
		snapshotInterval = p.config.SchemaCollection.RefreshInterval
	}

	run := &schemaRun{
		server:           server,
		excludeSchemas:   excludeSchemas,
		includeSchemas:   includeSchemas,
		filterConfig:     filterConfig,
		snapshotInterval: snapshotInterval,
		eventEmitter:     NewEventEmitter(logger, p.serviceInstanceID),
		logger:           logger,
	}

	// 5. Visit each database, deciding per database whether it needs
	// collecting at all.
	//
	// The decision is per database because the state it consults is per
	// database: each database has its own catalog, its own OIDs and its own
	// xmin snapshot. A single global decision cannot express "this database
	// changed and that one did not", so any change anywhere forced a full
	// collection of every database — which, for a server with many databases
	// and a stable schema, is nearly all of the work this receiver does.
	allEvents := plog.NewLogs()
	collected, skipped := 0, 0

	for _, dbName := range databases {
		dbEvents, didCollect, dbErr := p.collectSchemaForDatabase(ctx, dbName, run)
		if dbErr != nil {
			p.logger.Error("Failed to collect schema for database",
				zap.String("database", dbName), zap.Error(dbErr))
			if !p.config.SchemaCollection.ContinueOnError {
				return plog.NewLogs(), dbErr
			}
			continue
		}
		if !didCollect {
			skipped++
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

// detectSchemaServer returns the cached server-level facts, detecting them on
// first use.
func (p *postgreSQLScraper) detectSchemaServer(ctx context.Context, db *IgnoredDB) (*schemaServerInfo, error) {
	if p.schemaServer != nil {
		return p.schemaServer, nil
	}

	versionInfo, err := NewVersionDetector(db).Detect(ctx)
	if err != nil {
		p.logger.Error("Failed to detect PostgreSQL version", zap.Error(err))
		return nil, err
	}

	cloudProvider, cloudMetadata, err := NewCloudDetector(db).Detect(ctx)
	if err != nil {
		p.logger.Error("Failed to detect cloud provider", zap.Error(err))
		return nil, err
	}

	SetCloudFlags(versionInfo, cloudProvider)

	p.schemaServer = &schemaServerInfo{
		version:       versionInfo,
		cloudProvider: cloudProvider,
		cloudMetadata: cloudMetadata,
	}
	return p.schemaServer, nil
}

// schemaRun carries the state shared by every database visited in one schema
// collection cycle.
type schemaRun struct {
	server           *schemaServerInfo
	excludeSchemas   map[string]bool
	includeSchemas   map[string]bool
	filterConfig     *FilterConfig
	snapshotInterval time.Duration
	eventEmitter     *EventEmitter
	logger           *zapAdapter
}

// collectSchemaForDatabase decides whether a single database needs a full
// schema collection this cycle and, if so, performs it. It reports whether a
// collection happened.
//
// Detection and collection share one client: detection must run against the
// database being checked, since OIDs are only unique within a database, and
// opening a second connection to the same database for it would double the
// connection demand of every cycle.
func (p *postgreSQLScraper) collectSchemaForDatabase(
	ctx context.Context,
	dbName string,
	run *schemaRun,
) (plog.Logs, bool, error) {
	dbClient, err := p.clientFactory.getClient(dbName)
	if err != nil {
		return plog.NewLogs(), false, fmt.Errorf("failed to connect to database %s: %w", dbName, err)
	}
	defer dbClient.Close()

	pgClient, ok := dbClient.(*postgreSQLClient)
	if !ok {
		return plog.NewLogs(), false, fmt.Errorf("incompatible client type for database %s", dbName)
	}
	db := pgClient.client

	reason, needsCollection, err := p.schemaCollectionReason(ctx, db, dbName, run)
	if err != nil {
		return plog.NewLogs(), false, err
	}
	if !needsCollection {
		return plog.NewLogs(), false, nil
	}

	// Resolve database OID
	var dbOID uint32
	sqlBuilder := NewSQLBuilder(run.server.version.VersionNum)
	if err := db.QueryRowContext(ctx, sqlBuilder.DatabaseOIDQuery()).Scan(&dbOID); err != nil {
		p.logger.Warn("Failed to resolve database OID, using 0",
			zap.String("database", dbName), zap.Error(err))
	}

	collector, err := NewSchemaCollector(
		db, run.server.version, run.server.cloudProvider, run.server.cloudMetadata,
		p.changeTracker,
		&CollectorConfig{
			DatabaseName:      dbName,
			DatabaseOID:       dbOID,
			ContinueOnError:   p.config.SchemaCollection.ContinueOnError,
			CollectExtensions: p.config.SchemaCollection.CollectExtensions,
			CollectSettings:   p.config.SchemaCollection.CollectSettings,
			ExcludeSchemas:    run.excludeSchemas,
			IncludeSchemas:    run.includeSchemas,
		},
		run.filterConfig, run.logger,
	)
	if err != nil {
		return plog.NewLogs(), false, fmt.Errorf("failed to initialize collector for %s: %w", dbName, err)
	}

	event, err := collector.Collect(ctx)
	if err != nil {
		return plog.NewLogs(), false, fmt.Errorf("failed to collect schema for %s: %w", dbName, err)
	}

	p.logger.Info("Schema snapshot collected",
		zap.String("database", event.DatabaseName),
		zap.String("trigger", reason),
		zap.Int("table_count", len(event.Tables)),
		zap.Int64("duration_ms", event.Statistics.CollectionDurationMs))

	events, err := run.eventEmitter.EmitSchemaCollectionEvent(ctx, event)
	if err != nil {
		return plog.NewLogs(), false, err
	}

	// Emit standalone settings event
	if len(event.Settings) > 0 {
		settingsLogs, err := run.eventEmitter.EmitSettingsEvent(ctx, event)
		if err == nil {
			settingsLogs.ResourceLogs().MoveAndAppendTo(events.ResourceLogs())
		} else {
			p.logger.Warn("Failed to emit settings event",
				zap.String("database", dbName), zap.Error(err))
		}
	}

	// Emit standalone extensions event
	if len(event.Extensions) > 0 {
		extLogs, err := run.eventEmitter.EmitExtensionsEvent(ctx, event)
		if err == nil {
			extLogs.ResourceLogs().MoveAndAppendTo(events.ResourceLogs())
		} else {
			p.logger.Warn("Failed to emit extensions event",
				zap.String("database", dbName), zap.Error(err))
		}
	}

	return events, true, nil
}

// schemaCollectionReason decides whether a database needs a full schema
// collection this cycle, and why.
//
// A database is collected when it has never been snapshotted, when its snapshot
// is older than the refresh interval, or when xmin detection shows a table that
// is new, altered or dropped since that snapshot.
func (p *postgreSQLScraper) schemaCollectionReason(
	ctx context.Context,
	db *IgnoredDB,
	dbName string,
	run *schemaRun,
) (string, bool, error) {
	lastSnapshot, tracked := p.changeTracker.GetLastSnapshotFor(dbName)
	if !tracked || lastSnapshot.IsZero() {
		return "initial", true, nil
	}
	if time.Since(lastSnapshot) >= run.snapshotInterval {
		return "scheduled_refresh", true, nil
	}

	detector, err := NewSchemaCollector(
		db, run.server.version, run.server.cloudProvider, run.server.cloudMetadata,
		p.changeTracker,
		&CollectorConfig{
			DatabaseName:    dbName,
			ContinueOnError: true,
			ExcludeSchemas:  run.excludeSchemas,
			IncludeSchemas:  run.includeSchemas,
		},
		run.filterConfig, run.logger,
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to initialize change detector for %s: %w", dbName, err)
	}

	changed, dropped, err := detector.DetectChanges(ctx)
	if err != nil {
		return "", false, fmt.Errorf("failed to detect changes for %s: %w", dbName, err)
	}
	switch {
	case len(changed) > 0:
		return fmt.Sprintf("xmin_change_detected(%d tables)", len(changed)), true, nil
	case dropped > 0:
		return fmt.Sprintf("tables_dropped(%d tables)", dropped), true, nil
	}

	return "", false, nil
}

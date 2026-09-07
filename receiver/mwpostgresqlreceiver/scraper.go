// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"go.opentelemetry.io/collector/featuregate"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper/scrapererror"
	semconv "go.opentelemetry.io/otel/semconv/v1.38.0"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/common/priorityqueue"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

const (
	readmeURL            = "https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.88.0/receiver/postgresqlreceiver/README.md"
	separateSchemaAttrID = "receiver.postgresql.separateSchemaAttr"

	defaultPostgreSQLDatabase = "postgres"
)

var separateSchemaAttrGate = featuregate.GlobalRegistry().MustRegister(
	separateSchemaAttrID,
	featuregate.StageAlpha,
	featuregate.WithRegisterDescription("Moves Schema Names into dedicated Attribute"),
	featuregate.WithRegisterReferenceURL("https://github.com/open-telemetry/opentelemetry-collector-contrib/issues/29559"),
)

type postgreSQLScraper struct {
	logger        *zap.Logger
	config        *Config
	clientFactory postgreSQLClientFactory
	mb            *metadata.MetricsBuilder
	lb            *metadata.LogsBuilder
	excludes      map[string]struct{}
	cache         *lru.Cache[string, float64]
	changeTracker *XminChangeTracker
	// if enabled, uses a separated attribute for the schema
	separateSchemaAttr   bool
	queryPlanCache       *expirable.LRU[string, string]
	newestQueryTimestamp float64
	topQueryDisabled     bool
	// seenQuerySamples tracks (pid:query_start) keys for queries already
	// emitted, so the same execution is never sent twice across scrapes.
	seenQuerySamples  map[string]struct{}
	serviceInstanceID string
	lastSchemaCheck   time.Time
	// schemaServer caches the server-level facts schema collection needs —
	// version and cloud platform — which do not change for the life of the
	// process. Detecting them costs several queries; doing so once rather
	// than every cycle keeps the unchanged-schema cycle to one query per
	// database.
	schemaServer *schemaServerInfo
}

// schemaServerInfo is what schema collection learns about the server once.
type schemaServerInfo struct {
	version       *VersionInfo
	cloudProvider CloudProvider
	cloudMetadata *CloudMetadata
}

type errsMux struct {
	sync.RWMutex
	errs scrapererror.ScrapeErrors
}

func (e *errsMux) add(err error) {
	e.Lock()
	defer e.Unlock()
	e.errs.Add(err)
}

func (e *errsMux) addPartial(err error) {
	e.Lock()
	defer e.Unlock()
	e.errs.AddPartial(1, err)
}

func (e *errsMux) combine() error {
	e.Lock()
	defer e.Unlock()
	return e.errs.Combine()
}

func newPostgreSQLScraper(
	settings receiver.Settings,
	config *Config,
	clientFactory postgreSQLClientFactory,
	cache *lru.Cache[string, float64],
	queryPlanCache *expirable.LRU[string, string],
) *postgreSQLScraper {
	excludes := make(map[string]struct{})
	for _, db := range config.ExcludeDatabases {
		excludes[db] = struct{}{}
	}
	separateSchemaAttr := separateSchemaAttrGate.IsEnabled()

	if !separateSchemaAttr {
		settings.Logger.Warn(
			fmt.Sprintf("Feature gate %s is not enabled. Please see the README for more information: %s", separateSchemaAttrID, readmeURL),
		)
	}

	return &postgreSQLScraper{
		logger:             settings.Logger,
		config:             config,
		clientFactory:      clientFactory,
		mb:                 metadata.NewMetricsBuilder(config.MetricsBuilderConfig, settings),
		lb:                 metadata.NewLogsBuilder(config.LogsBuilderConfig, settings),
		excludes:           excludes,
		cache:              cache,
		changeTracker:      NewXminChangeTracker(1000), // Maintain state across scrapes
		queryPlanCache:     queryPlanCache,
		separateSchemaAttr: separateSchemaAttr,
		seenQuerySamples:   make(map[string]struct{}),
		serviceInstanceID:  getInstanceID(config.Endpoint, settings.Logger),
	}
}

type dbRetrieval struct {
	sync.RWMutex
	activityMap map[databaseName]int64
	dbSizeMap   map[databaseName]int64
	dbStats     map[databaseName]databaseStats
}

// scrape scrapes the metric stats, transforms them and attributes them into a metric slices.
func (p *postgreSQLScraper) scrape(ctx context.Context) (retMetrics pmetric.Metrics, retErr error) {
	defer recoverScrape(p.logger, "metrics", &retErr)

	databases := p.config.Databases
	listClient, err := p.clientFactory.getClient(defaultPostgreSQLDatabase)
	if err != nil {
		p.logger.Error("Failed to initialize connection to postgres", zap.Error(err))
		return pmetric.NewMetrics(), err
	}
	defer listClient.Close()

	if len(databases) == 0 {
		dbList, dbErr := listClient.listDatabases(ctx)
		if dbErr != nil {
			p.logger.Error("Failed to request list of databases from postgres", zap.Error(dbErr))
			return pmetric.NewMetrics(), dbErr
		}
		databases = dbList
	}
	var filteredDatabases []string
	for _, db := range databases {
		if _, ok := p.excludes[db]; !ok {
			filteredDatabases = append(filteredDatabases, db)
		}
	}
	databases = filteredDatabases

	now := pcommon.NewTimestampFromTime(time.Now())

	var errs errsMux
	r := &dbRetrieval{
		activityMap: make(map[databaseName]int64),
		dbSizeMap:   make(map[databaseName]int64),
		dbStats:     make(map[databaseName]databaseStats),
	}
	p.retrieveDBMetrics(ctx, listClient, databases, r, &errs)

	for _, database := range databases {
		p.collectDatabaseMetrics(ctx, now, database, r, &errs)
	}

	p.mb.RecordPostgresqlDatabaseCountDataPoint(now, int64(len(databases)))
	p.collectBGWriterStats(ctx, now, listClient, &errs)
	p.collectWalAge(ctx, now, listClient, &errs)
	p.collectReplicationStats(ctx, now, listClient, &errs)
	p.collectMaxConnections(ctx, now, listClient, &errs)
	p.collectActiveConnections(ctx, now, listClient, &errs)
	p.collectDatabaseLocks(ctx, now, listClient, &errs)
	p.collectRowStats(ctx, now, listClient, &errs)
	p.collectQueryPerfStats(ctx, now, listClient, &errs)
	p.collectBufferHits(ctx, now, listClient, &errs)
	p.collectWALStats(ctx, now, listClient, &errs)
	p.collectTransactionsStats(ctx, now, listClient, &errs)

	rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), "", "", "", "")
	return p.mb.Emit(metadata.WithResource(rb.Emit())), errs.combine()
}

// collectDatabaseMetrics collects the per-database metrics for a single
// database.
//
// This is a separate function so that the client is closed when the database is
// finished with, rather than when the whole scrape returns. A deferred Close
// inside the scrape loop would be function-scoped, holding one connection open
// per database for the duration of the scrape — with enough databases that
// exhausts the role's connection limit and the server starts refusing new
// connections mid-scrape.
func (p *postgreSQLScraper) collectDatabaseMetrics(
	ctx context.Context,
	now pcommon.Timestamp,
	database string,
	r *dbRetrieval,
	errs *errsMux,
) {
	dbClient, dbErr := p.clientFactory.getClient(database)
	if dbErr != nil {
		errs.add(dbErr)
		p.logger.Error("Failed to initialize connection to postgres", zap.String("database", database), zap.Error(dbErr))
		return
	}
	defer dbClient.Close()

	numTables := p.collectTables(ctx, now, dbClient, database, errs)

	p.recordDatabase(now, database, r, numTables)
	p.collectIndexes(ctx, now, dbClient, database, errs)
	p.collectFunctions(ctx, now, dbClient, database, errs)
	p.collectTableBloat(ctx, now, dbClient, database, errs)
	p.collectIndexBloat(ctx, now, dbClient, database, errs)
}

// recoverScrape converts a panic on a scrape goroutine into an error. The
// collector runs each scraper on its own goroutine with no recover of its own,
// so an unexpected panic here terminates the entire agent process — taking down
// host metrics, logs and every other integration along with this receiver. A
// malformed row in one receiver should not have that blast radius.
func recoverScrape(logger *zap.Logger, path string, err *error) {
	if r := recover(); r != nil {
		logger.Error("recovered from panic during scrape",
			zap.String("path", path),
			zap.Any("panic", r),
			zap.Stack("stack"))
		*err = fmt.Errorf("panic during %s scrape: %v", path, r)
	}
}

func (p *postgreSQLScraper) scrapeQuerySamples(ctx context.Context, maxRowsPerQuery int64) (retLogs plog.Logs, retErr error) {
	defer recoverScrape(p.logger, "query_samples", &retErr)

	dbClient, err := p.clientFactory.getClient(defaultPostgreSQLDatabase)
	if err != nil {
		p.logger.Error("Failed to initialize connection to postgres", zap.Error(err))
		return plog.NewLogs(), err
	}

	var errs errsMux

	p.collectQuerySamples(ctx, dbClient, maxRowsPerQuery, &errs, p.logger)

	defer dbClient.Close()

	rb := p.setupResourceBuilder(p.lb.NewResourceBuilder(), "", "", "", "")
	logs := p.lb.Emit(metadata.WithLogsResource(rb.Emit()))
	stripEmptyLogAttrs(logs)
	return logs, nil
}

func (p *postgreSQLScraper) scrapeTopQuery(ctx context.Context, maxRowsPerQuery, topNQuery, maxExplainEachInterval int64) (retLogs plog.Logs, retErr error) {
	defer recoverScrape(p.logger, "top_query", &retErr)

	var errs errsMux

	p.collectTopQuery(ctx, p.clientFactory, maxRowsPerQuery, topNQuery, maxExplainEachInterval, &errs, p.logger)

	rb := p.setupResourceBuilder(p.lb.NewResourceBuilder(), "", "", "", "")
	logs := p.lb.Emit(metadata.WithLogsResource(rb.Emit()))
	stripEmptyLogAttrs(logs)
	return logs, nil
}

func (p *postgreSQLScraper) collectQuerySamples(ctx context.Context, dbClient client, limit int64, mux *errsMux, logger *zap.Logger) {
	timestamp := pcommon.NewTimestampFromTime(time.Now())

	attributes, newestQueryTimestamp, err := dbClient.getQuerySamples(ctx, limit, p.newestQueryTimestamp, logger)
	p.newestQueryTimestamp = newestQueryTimestamp
	if err != nil {
		mux.addPartial(err)
		return
	}

	// Build a set of (pid:query_start:sorted_blocking_pids) keys visible in this
	// scrape. Any key already in seenQuerySamples was emitted before and is
	// skipped. Including the sorted blocking-pid set in the key means a query
	// re-emits whenever its blocking-pid set changes (becomes blocked, blocking
	// changes, or unblocks), even though pid+query_start are unchanged.
	currentSeen := make(map[string]struct{}, len(attributes))

	for _, atts := range attributes {
		state := attrString(atts, dbAttributePrefix+querySampleColumnState)
		pid := attrInt64(atts, dbAttributePrefix+querySampleColumnPID)
		queryStart := attrString(atts, dbAttributePrefix+querySampleColumnQueryStart)
		blockingPids, _ := atts[dbAttributePrefix+querySampleColumnBlockingPids].([]any)

		key := fmt.Sprintf("%d:%s:%s", pid, queryStart, blockingPidsToKey(blockingPids))
		currentSeen[key] = struct{}{}
		if _, alreadySeen := p.seenQuerySamples[key]; alreadySeen {
			continue
		}

		// Use a background context so query-sample logs are not automatically linked to the scrape context.
		logCtx := context.Background()
		if ctxFromQuery, ok := atts[querySampleTraceContextKey]; ok {
			if ctx, ok := ctxFromQuery.(context.Context); ok {
				logCtx = ctx
			}
		}
		comment, _ := atts["db.query.comment"].(string)
		p.lb.RecordDbServerQuerySampleEvent(logCtx,
			timestamp,
			metadata.AttributeDbSystemNamePostgresql,
			attrString(atts, string(semconv.DBNamespaceKey)),
			attrString(atts, "event.type"),
			attrString(atts, string(semconv.DBQueryTextKey)),
			comment,
			attrString(atts, "db.query.tables"),
			attrString(atts, string(semconv.UserNameKey)),
			state,
			pid,
			attrString(atts, dbAttributePrefix+querySampleColumnApplicationName),
			attrString(atts, string(semconv.NetworkPeerAddressKey)),
			attrInt64(atts, string(semconv.NetworkPeerPortKey)),
			attrString(atts, dbAttributePrefix+querySampleColumnClientHostname),
			attrString(atts, dbAttributePrefix+querySampleColumnBackendType),
			attrString(atts, dbAttributePrefix+querySampleColumnXactStart),
			queryStart,
			attrString(atts, dbAttributePrefix+querySampleColumnStateChange),
			attrString(atts, dbAttributePrefix+querySampleColumnWaitEvent),
			attrString(atts, dbAttributePrefix+querySampleColumnWaitEventType),
			blockingPids,
			attrInt64(atts, dbAttributePrefix+querySampleColumnBackendXid),
			attrString(atts, dbAttributePrefix+querySampleColumnQueryID),
			attrFloat64(atts, postgresqlTotalExecTimeAttributeName),
		)
	}

	// Swap in the current set. Rows that disappeared from pg_stat_activity
	// (connection closed or new query started on that PID) are automatically
	// removed, so a re-execution with a fresh query_start will be emitted.
	p.seenQuerySamples = currentSeen
}

func (p *postgreSQLScraper) collectTopQuery(ctx context.Context, clientFactory postgreSQLClientFactory, limit, topNQuery, maxExplainEachInterval int64, mux *errsMux, logger *zap.Logger) {
	timestamp := pcommon.NewTimestampFromTime(time.Now())

	if p.topQueryDisabled {
		return
	}

	candidateDatabases := make([]string, 0, len(p.config.Databases)+1)
	seen := make(map[string]struct{}, len(p.config.Databases)+1)
	for _, db := range p.config.Databases {
		if _, excluded := p.excludes[db]; excluded {
			continue
		}
		if _, ok := seen[db]; ok {
			continue
		}
		seen[db] = struct{}{}
		candidateDatabases = append(candidateDatabases, db)
	}
	if _, ok := seen[defaultPostgreSQLDatabase]; !ok {
		candidateDatabases = append(candidateDatabases, defaultPostgreSQLDatabase)
	}

	var rows []map[string]any
	extensionMissing := false
	var lastErr error
	scrapedTopQuery := false
	for _, database := range candidateDatabases {
		dbClient, err := clientFactory.getClient(database)
		if err != nil {
			lastErr = err
			logger.Warn("failed to create db client while scraping top query", zap.String("database", database), zap.Error(err))
			continue
		}

		rows, err = dbClient.getTopQuery(ctx, limit, logger)
		closeErr := dbClient.Close()
		if closeErr != nil {
			logger.Error("failed to close", zap.Error(closeErr))
		}
		if err == nil {
			scrapedTopQuery = true
			break
		}
		lastErr = err
		if strings.Contains(err.Error(), `relation "pg_stat_statements" does not exist`) {
			extensionMissing = true
			logger.Warn("pg_stat_statements is unavailable on database while scraping top query", zap.String("database", database))
			continue
		}
		logger.Error("failed to get top query", zap.String("database", database), zap.Error(err))
		mux.addPartial(err)
		return
	}
	if !scrapedTopQuery && extensionMissing && lastErr != nil {
		p.topQueryDisabled = true
		logger.Warn("disabling top query collection for this receiver instance because pg_stat_statements is unavailable in all candidate databases")
		return
	}
	if !scrapedTopQuery && lastErr != nil {
		mux.addPartial(lastErr)
		return
	}

	type updatedOnlyInfo struct {
		finalConverter func(float64) any
	}

	convertToInt := func(f float64) any {
		return int64(f)
	}

	updatedOnly := map[string]updatedOnlyInfo{
		totalExecTimeColumnName:     {},
		totalPlanTimeColumnName:     {},
		blkReadTimeAttributeName:    {},
		blkWriteTimeAttributeName:   {},
		rowsColumnName:              {finalConverter: convertToInt},
		callsColumnName:             {finalConverter: convertToInt},
		sharedBlksDirtiedColumnName: {finalConverter: convertToInt},
		sharedBlksHitColumnName:     {finalConverter: convertToInt},
		sharedBlksReadColumnName:    {finalConverter: convertToInt},
		sharedBlksWrittenColumnName: {finalConverter: convertToInt},
		tempBlksReadColumnName:      {finalConverter: convertToInt},
		tempBlksWrittenColumnName:   {finalConverter: convertToInt},
	}

	pq := make(priorityqueue.PriorityQueue[map[string]any, float64], 0)

	for i, row := range rows {
		queryID := attrString(row, dbAttributePrefix+queryidColumnName)

		if queryID == "" {
			// this should not happen, but in case
			logger.Error("queryid is nil", zap.Any("atts", row))
			mux.addPartial(errors.New("queryid is nil"))
			continue
		}

		// pg_stat_statements keys its entries on (userid, dbid, queryid,
		// toplevel), not on queryid alone: the same normalised query executed
		// by two roles, or in two databases, is two separate rows with
		// independent counters. Keying the delta cache on queryid alone merges
		// them, so each row is differenced against whichever of its siblings
		// was seen last and the emitted deltas are meaningless - typically
		// oscillating between a large positive value and zero as the rows take
		// turns. Key on the same tuple the server does.
		deltaKey := topQueryDeltaKey(row, queryID)

		for columnName, info := range updatedOnly {
			// A NULL column is absent from the row map entirely, so this must
			// tolerate a missing key rather than assert on it.
			valInAtts := attrFloat64(row, dbAttributePrefix+columnName)
			valInCache, exist := p.cache.Get(deltaKey + columnName)
			valDelta := valInAtts
			if exist {
				valDelta = valInAtts - valInCache
			}
			finalValue := float64(0)
			if valDelta > 0 {
				p.cache.Add(deltaKey+columnName, valInAtts)
				finalValue = valDelta
			}
			if info.finalConverter != nil {
				row[dbAttributePrefix+columnName] = info.finalConverter(finalValue)
			} else {
				row[dbAttributePrefix+columnName] = finalValue
			}
		}
		if row[dbAttributePrefix+totalExecTimeColumnName] == 0.0 {
			continue
		}
		item := priorityqueue.QueueItem[map[string]any, float64]{
			Value:    row,
			Priority: attrFloat64(row, dbAttributePrefix+totalExecTimeColumnName),
			Index:    i,
		}
		pq.Push(&item)
	}

	heap.Init(&pq)
	explained := int64(0)
	count := 0
	// Counted rather than logged per row: on a server with churn a large share
	// of pg_stat_statements rows can be orphaned, so a per-row log would itself
	// become a significant source of allocation.
	unresolvedDatabases := 0
	for pq.Len() > 0 && count < int(topNQuery) {
		item := heap.Pop(&pq).(*priorityqueue.QueueItem[map[string]any, float64])
		query := attrString(item.Value, string(semconv.DBQueryTextKey))
		queryID := attrString(item.Value, dbAttributePrefix+queryidColumnName)
		// Use raw query (with $1, $2 placeholders) for EXPLAIN, not the obfuscated one (with ?)
		rawQuery, _ := item.Value[dbAttributePrefix+"raw_query"].(string)

		// pg_stat_statements rows outlive the databases they came from: once a
		// database is dropped, its dbid no longer joins to pg_database and
		// datname comes back NULL, which leaves db.namespace absent from the
		// row. Such a row cannot be EXPLAINed (there is no database to connect
		// to), but it is still a real query worth reporting, so it is emitted
		// under a placeholder rather than dropped.
		database := attrString(item.Value, string(semconv.DBNamespaceKey))
		if database == "" {
			unresolvedDatabases++
			database = unknownDatabaseName
		}

		plan, ok := p.queryPlanCache.Get(queryID + "-plan")
		if !ok && explained < maxExplainEachInterval && database != unknownDatabaseName {
			dbClient, err := clientFactory.getClient(database)
			if err != nil {
				logger.Warn("skipping explain: failed to get db client",
					zap.String("queryID", queryID),
					zap.String("database", database),
					zap.Error(err))
			} else {
				plan, err = dbClient.explainQuery(rawQuery, queryID, logger)
				if err != nil {
					logger.Warn("explain failed for query, caching empty plan",
						zap.String("queryID", queryID),
						zap.Error(err))
				}
				// Cache the plan (empty or not) to avoid flooding errors on every scrape.
				// The plan cache TTL controls when a re-attempt is made.
				p.queryPlanCache.Add(queryID+"-plan", plan)
				if closeErr := dbClient.Close(); closeErr != nil {
					logger.Error("failed to close db client after explain", zap.Error(closeErr))
				}
				explained++
			}
		}

		// Extract table names from raw query for db.query.tables enrichment
		tables := strings.Join(extractTablesFromQuery(rawQuery), ",")
		// user.name is aliased from rolname
		rolname, _ := item.Value[dbAttributePrefix+"rolname"].(string)

		logCtx := context.Background()
		if ctxFromQuery, ok := item.Value[querySampleTraceContextKey]; ok {
			if c, ok := ctxFromQuery.(context.Context); ok {
				logCtx = c
			}
		}

		topComment, _ := item.Value["db.query.comment"].(string)
		p.lb.RecordDbServerTopQueryEvent(
			logCtx,
			timestamp,
			metadata.AttributeDbSystemNamePostgresql,
			database,
			"top_query",
			query,
			topComment,
			tables,
			rolname,
			attrInt64(item.Value, dbAttributePrefix+callsColumnName),
			attrInt64(item.Value, dbAttributePrefix+rowsColumnName),
			attrInt64(item.Value, dbAttributePrefix+sharedBlksDirtiedColumnName),
			attrInt64(item.Value, dbAttributePrefix+sharedBlksHitColumnName),
			attrInt64(item.Value, dbAttributePrefix+sharedBlksReadColumnName),
			attrInt64(item.Value, dbAttributePrefix+sharedBlksWrittenColumnName),
			attrInt64(item.Value, dbAttributePrefix+tempBlksReadColumnName),
			attrInt64(item.Value, dbAttributePrefix+tempBlksWrittenColumnName),
			queryID,
			rolname,
			attrFloat64(item.Value, dbAttributePrefix+totalExecTimeColumnName),
			attrFloat64(item.Value, dbAttributePrefix+totalPlanTimeColumnName),
			plan,
			attrFloat64(item.Value, postgresqlBlkReadTimeAttributeName),
			attrFloat64(item.Value, postgresqlBlkWriteTimeAttributeName),
		)
		count++
	}

	if unresolvedDatabases > 0 {
		logger.Debug("top query rows had no resolvable database, reported as unknown",
			zap.Int("count", unresolvedDatabases),
			zap.Int("emitted", count))
	}
}

func (p *postgreSQLScraper) shutdown(_ context.Context) error {
	if p.clientFactory != nil {
		p.clientFactory.close()
	}
	return nil
}

func (p *postgreSQLScraper) retrieveDBMetrics(
	ctx context.Context,
	listClient client,
	databases []string,
	r *dbRetrieval,
	errs *errsMux,
) {
	wg := &sync.WaitGroup{}

	wg.Add(3)
	go p.retrieveBackends(ctx, wg, listClient, databases, r, errs)
	go p.retrieveDatabaseSize(ctx, wg, listClient, databases, r, errs)
	go p.retrieveDatabaseStats(ctx, wg, listClient, databases, r, errs)

	wg.Wait()
}

func (p *postgreSQLScraper) recordDatabase(now pcommon.Timestamp, db string, r *dbRetrieval, numTables int64) {
	dbName := databaseName(db)
	p.mb.RecordPostgresqlTableCountDataPoint(now, numTables)
	if activeConnections, ok := r.activityMap[dbName]; ok {
		p.mb.RecordPostgresqlBackendsDataPoint(now, activeConnections)
	}
	if size, ok := r.dbSizeMap[dbName]; ok {
		p.mb.RecordPostgresqlDbSizeDataPoint(now, size)
	}
	if stats, ok := r.dbStats[dbName]; ok {
		p.mb.RecordPostgresqlCommitsDataPoint(now, stats.transactionCommitted)
		p.mb.RecordPostgresqlRollbacksDataPoint(now, stats.transactionRollback)
		p.mb.RecordPostgresqlDeadlocksDataPoint(now, stats.deadlocks)
		p.mb.RecordPostgresqlTempFilesDataPoint(now, stats.tempFiles)
		p.mb.RecordPostgresqlTempIoDataPoint(now, stats.tempIo)
		p.mb.RecordPostgresqlTupUpdatedDataPoint(now, stats.tupUpdated)
		p.mb.RecordPostgresqlTupReturnedDataPoint(now, stats.tupReturned)
		p.mb.RecordPostgresqlTupFetchedDataPoint(now, stats.tupFetched)
		p.mb.RecordPostgresqlTupInsertedDataPoint(now, stats.tupInserted)
		p.mb.RecordPostgresqlTupDeletedDataPoint(now, stats.tupDeleted)
		p.mb.RecordPostgresqlBlksHitDataPoint(now, stats.blksHit)
		p.mb.RecordPostgresqlBlksReadDataPoint(now, stats.blksRead)
		p.mb.RecordPostgresqlBlkReadTimeDataPoint(now, stats.blkReadTime)   // requires track_io_timing = on
		p.mb.RecordPostgresqlBlkWriteTimeDataPoint(now, stats.blkWriteTime) // requires track_io_timing = on
	}
	rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), db, "", "", "")
	p.mb.EmitForResource(metadata.WithResource(rb.Emit()))
}

func (p *postgreSQLScraper) collectTables(ctx context.Context, now pcommon.Timestamp, dbClient client, db string, errs *errsMux) (numTables int64) {
	blockReads, err := dbClient.getBlocksReadByTable(ctx, db)
	if err != nil {
		errs.addPartial(err)
	}

	tableMetrics, err := dbClient.getDatabaseTableMetrics(ctx, db)
	if err != nil {
		errs.addPartial(err)
	}

	for tableKey := range tableMetrics {
		tm := tableMetrics[tableKey]
		p.mb.RecordPostgresqlRowsDataPoint(now, tm.dead, metadata.AttributeStateDead)
		p.mb.RecordPostgresqlRowsDataPoint(now, tm.live, metadata.AttributeStateLive)
		p.mb.RecordPostgresqlOperationsDataPoint(now, tm.inserts, metadata.AttributeOperationIns)
		p.mb.RecordPostgresqlOperationsDataPoint(now, tm.del, metadata.AttributeOperationDel)
		p.mb.RecordPostgresqlOperationsDataPoint(now, tm.upd, metadata.AttributeOperationUpd)
		p.mb.RecordPostgresqlOperationsDataPoint(now, tm.hotUpd, metadata.AttributeOperationHotUpd)
		p.mb.RecordPostgresqlTableSizeDataPoint(now, tm.size)
		p.mb.RecordPostgresqlTableVacuumCountDataPoint(now, tm.vacuumCount)
		p.mb.RecordPostgresqlAutovacuumedDataPoint(now, tm.autovacuumCount)
		p.mb.RecordPostgresqlAnalyzedDataPoint(now, tm.analyzeCount)
		p.mb.RecordPostgresqlAutoanalyzedDataPoint(now, tm.autoanalyzeCount)
		p.mb.RecordPostgresqlSequentialScansDataPoint(now, tm.seqScans)

		br, ok := blockReads[tableKey]
		if ok {
			p.mb.RecordPostgresqlBlocksReadDataPoint(now, br.heapRead, metadata.AttributeSourceHeapRead)
			p.mb.RecordPostgresqlBlocksReadDataPoint(now, br.heapHit, metadata.AttributeSourceHeapHit)
			p.mb.RecordPostgresqlBlocksReadDataPoint(now, br.idxRead, metadata.AttributeSourceIdxRead)
			p.mb.RecordPostgresqlBlocksReadDataPoint(now, br.idxHit, metadata.AttributeSourceIdxHit)
			p.mb.RecordPostgresqlBlocksReadDataPoint(now, br.toastHit, metadata.AttributeSourceToastHit)
			p.mb.RecordPostgresqlBlocksReadDataPoint(now, br.toastRead, metadata.AttributeSourceToastRead)
			p.mb.RecordPostgresqlBlocksReadDataPoint(now, br.tidxRead, metadata.AttributeSourceTidxRead)
			p.mb.RecordPostgresqlBlocksReadDataPoint(now, br.tidxHit, metadata.AttributeSourceTidxHit)
		}

		p.mb.RecordPostgresqlToastSizeDataPoint(now, tm.toastSize)

		var schemaName string
		var tableName string
		if p.separateSchemaAttr {
			schemaName = tm.schema
			tableName = tm.table
		} else {
			tableName = fmt.Sprintf("%s.%s", tm.schema, tm.table)
		}

		rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), db, schemaName, tableName, "")
		p.mb.EmitForResource(metadata.WithResource(rb.Emit()))
	}
	return int64(len(tableMetrics))
}

func (p *postgreSQLScraper) collectIndexes(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	database string,
	errs *errsMux,
) {
	idxStats, err := client.getIndexStats(ctx, database)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, stat := range idxStats {
		p.mb.RecordPostgresqlIndexScansDataPoint(now, stat.scans)
		p.mb.RecordPostgresqlIndexSizeDataPoint(now, stat.size)
		p.mb.RecordPostgresqlIndexRowsReadDataPoint(now, stat.tuplesRead)
		p.mb.RecordPostgresqlIndexBlocksReadDataPoint(now, stat.blocksRead, metadata.AttributeSourceIdxRead)
		p.mb.RecordPostgresqlIndexBlocksReadDataPoint(now, stat.blocksHit, metadata.AttributeSourceIdxHit)

		var schemaName string
		if p.separateSchemaAttr {
			schemaName = stat.schema
		}

		rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), database, schemaName, stat.table, stat.index)
		p.mb.EmitForResource(metadata.WithResource(rb.Emit()))
	}
}

func (p *postgreSQLScraper) collectFunctions(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	database string,
	errs *errsMux,
) {
	funcStats, err := client.getFunctionStats(ctx, database)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, stat := range funcStats {
		p.mb.RecordPostgresqlFunctionCallsDataPoint(now, stat.calls, stat.function)

		var schemaName string
		if p.separateSchemaAttr {
			schemaName = stat.schema
		}
		rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), database, schemaName, "", "")

		p.mb.EmitForResource(metadata.WithResource(rb.Emit()))
	}
}

func (p *postgreSQLScraper) collectTableBloat(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	database string,
	errs *errsMux,
) {
	bloatStats, err := client.getTableBloatStats(ctx, database)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, stat := range bloatStats {
		p.mb.RecordPostgresqlTableBloatDataPoint(now, stat.bloat)

		var schemaName string
		var tableName string
		if p.separateSchemaAttr {
			schemaName = stat.schema
			tableName = stat.table
		} else {
			tableName = fmt.Sprintf("%s.%s", stat.schema, stat.table)
		}

		rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), database, schemaName, tableName, "")
		p.mb.EmitForResource(metadata.WithResource(rb.Emit()))
	}
}

func (p *postgreSQLScraper) collectIndexBloat(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	database string,
	errs *errsMux,
) {
	bloatStats, err := client.getIndexBloatStats(ctx, database)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, stat := range bloatStats {
		p.mb.RecordPostgresqlIndexBloatDataPoint(now, stat.bloat)

		var schemaName string
		if p.separateSchemaAttr {
			schemaName = stat.schema
		}

		rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), database, schemaName, stat.table, stat.indexName)
		p.mb.EmitForResource(metadata.WithResource(rb.Emit()))
	}
}

func (p *postgreSQLScraper) collectWALStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	count, size, err := client.getWALStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}
	p.mb.RecordPostgresqlWalCountDataPoint(now, count)
	p.mb.RecordPostgresqlWalSizeDataPoint(now, size)
}

func (p *postgreSQLScraper) collectTransactionsStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	maxDuration, sumDuration, err := client.getTransactionsStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}
	p.mb.RecordPostgresqlTransactionsDurationMaxDataPoint(now, maxDuration)
	p.mb.RecordPostgresqlTransactionsDurationSumDataPoint(now, sumDuration)
}

func (p *postgreSQLScraper) collectActiveConnections(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	stats, err := client.getConnectionStats(ctx, nil)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for dbName, connStats := range stats {
		for _, s := range connStats {
			p.mb.RecordPostgresqlConnectionCountDataPoint(now, s.count, s.state, s.app, s.user)
		}
		rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), string(dbName), "", "", "")
		p.mb.EmitForResource(metadata.WithResource(rb.Emit()))
	}
}

func (p *postgreSQLScraper) collectBGWriterStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	bgStats, err := client.getBGWriterStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}

	p.mb.RecordPostgresqlBgwriterBuffersAllocatedDataPoint(now, bgStats.buffersAllocated)

	p.mb.RecordPostgresqlBgwriterBuffersWritesDataPoint(now, bgStats.bgWrites, metadata.AttributeBgBufferSourceBgwriter)
	if bgStats.bufferBackendWrites >= 0 {
		p.mb.RecordPostgresqlBgwriterBuffersWritesDataPoint(now, bgStats.bufferBackendWrites, metadata.AttributeBgBufferSourceBackend)
	}
	p.mb.RecordPostgresqlBgwriterBuffersWritesDataPoint(now, bgStats.bufferCheckpoints, metadata.AttributeBgBufferSourceCheckpoints)
	if bgStats.bufferFsyncWrites >= 0 {
		p.mb.RecordPostgresqlBgwriterBuffersWritesDataPoint(now, bgStats.bufferFsyncWrites, metadata.AttributeBgBufferSourceBackendFsync)
	}

	p.mb.RecordPostgresqlBgwriterCheckpointCountDataPoint(now, bgStats.checkpointsReq, metadata.AttributeBgCheckpointTypeRequested)
	p.mb.RecordPostgresqlBgwriterCheckpointCountDataPoint(now, bgStats.checkpointsScheduled, metadata.AttributeBgCheckpointTypeScheduled)

	p.mb.RecordPostgresqlBgwriterDurationDataPoint(now, bgStats.checkpointSyncTime, metadata.AttributeBgDurationTypeSync)
	p.mb.RecordPostgresqlBgwriterDurationDataPoint(now, bgStats.checkpointWriteTime, metadata.AttributeBgDurationTypeWrite)

	p.mb.RecordPostgresqlBgwriterMaxwrittenDataPoint(now, bgStats.maxWritten)
}

func (p *postgreSQLScraper) collectDatabaseLocks(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	dbLocks, err := client.getDatabaseLocks(ctx)
	if err != nil {
		p.logger.Error("Errors encountered while fetching database locks", zap.Error(err))
		errs.addPartial(err)
		return
	}
	for _, dbLock := range dbLocks {
		p.mb.RecordPostgresqlDatabaseLocksDataPoint(now, dbLock.locks, dbLock.relation, dbLock.mode, dbLock.lockType)
	}
}

func (p *postgreSQLScraper) collectMaxConnections(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	mc, err := client.getMaxConnections(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}
	p.mb.RecordPostgresqlConnectionMaxDataPoint(now, mc)
}

func (p *postgreSQLScraper) collectReplicationStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	rss, err := client.getReplicationStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}
	for _, rs := range rss {
		if rs.pendingBytes >= 0 {
			p.mb.RecordPostgresqlReplicationDataDelayDataPoint(now, rs.pendingBytes, rs.clientAddr)
		}
		if preciseLagMetricsFg.IsEnabled() {
			if rs.writeLag >= 0 {
				p.mb.RecordPostgresqlWalDelayDataPoint(now, rs.writeLag, metadata.AttributeWalOperationLagWrite, rs.clientAddr)
			}
			if rs.replayLag >= 0 {
				p.mb.RecordPostgresqlWalDelayDataPoint(now, rs.replayLag, metadata.AttributeWalOperationLagReplay, rs.clientAddr)
			}
			if rs.flushLag >= 0 {
				p.mb.RecordPostgresqlWalDelayDataPoint(now, rs.flushLag, metadata.AttributeWalOperationLagFlush, rs.clientAddr)
			}
		} else {
			if rs.writeLagInt >= 0 {
				p.mb.RecordPostgresqlWalLagDataPoint(now, rs.writeLagInt, metadata.AttributeWalOperationLagWrite, rs.clientAddr)
			}
			if rs.replayLagInt >= 0 {
				p.mb.RecordPostgresqlWalLagDataPoint(now, rs.replayLagInt, metadata.AttributeWalOperationLagReplay, rs.clientAddr)
			}
			if rs.flushLagInt >= 0 {
				p.mb.RecordPostgresqlWalLagDataPoint(now, rs.flushLagInt, metadata.AttributeWalOperationLagFlush, rs.clientAddr)
			}
		}
	}
}

func (p *postgreSQLScraper) collectWalAge(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	walAge, err := client.getLatestWalAgeSeconds(ctx)
	if errors.Is(err, errNoLastArchive) {
		// return no error as there is no last archive to derive the value from
		return
	}
	if err != nil {
		errs.addPartial(fmt.Errorf("unable to determine latest WAL age: %w", err))
		return
	}
	p.mb.RecordPostgresqlWalAgeDataPoint(now, walAge)
}

func (p *postgreSQLScraper) collectRowStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	rs, err := client.getRowStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}
	// pp.Println(rs)
	for _, s := range rs {
		// p.mb.RecordPostgresqlRowsReturnedDataPoint(now, s.rowsReturned, s.relationName)
		p.mb.RecordPostgresqlRowsFetchedDataPoint(now, s.rowsFetched, s.relationName)
		p.mb.RecordPostgresqlRowsInsertedDataPoint(now, s.rowsInserted, s.relationName)
		p.mb.RecordPostgresqlRowsUpdatedDataPoint(now, s.rowsUpdated, s.relationName)
		p.mb.RecordPostgresqlRowsDeletedDataPoint(now, s.rowsDeleted, s.relationName)
		// p.mb.RecordPostgresqlRowsHotUpdatedDataPoint(now, s.rowsHotUpdated, s.relationName)
		p.mb.RecordPostgresqlLiveRowsDataPoint(now, s.liveRows, s.relationName)
		// p.mb.RecordPostgresqlDeadRowsDataPoint(now, s.deadRows, s.relationName)
	}
}

func (p *postgreSQLScraper) collectQueryPerfStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	queryStats, err := client.getQueryStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, s := range queryStats {
		p.mb.RecordPostgresqlQueryCountDataPoint(now, s.queryCount, s.queryText, s.queryID)
		p.mb.RecordPostgresqlQueryTotalExecTimeDataPoint(now, s.queryExecTime, s.queryText, s.queryID)
	}
}

func (p *postgreSQLScraper) collectBufferHits(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	bhs, err := client.getBufferHit(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, s := range bhs {
		p.mb.RecordPostgresqlBufferHitDataPoint(now, s.hits, s.dbName)
	}
}

func (p *postgreSQLScraper) retrieveDatabaseStats(
	ctx context.Context,
	wg *sync.WaitGroup,
	client client,
	databases []string,
	r *dbRetrieval,
	errs *errsMux,
) {
	defer wg.Done()
	dbStats, err := client.getDatabaseStats(ctx, databases)
	if err != nil {
		p.logger.Error("Errors encountered while fetching commits and rollbacks", zap.Error(err))
		errs.addPartial(err)
		return
	}
	r.Lock()
	r.dbStats = dbStats
	r.Unlock()
}

func (p *postgreSQLScraper) retrieveDatabaseSize(
	ctx context.Context,
	wg *sync.WaitGroup,
	client client,
	databases []string,
	r *dbRetrieval,
	errs *errsMux,
) {
	defer wg.Done()
	databaseSizeMetrics, err := client.getDatabaseSize(ctx, databases)
	if err != nil {
		p.logger.Error("Errors encountered while fetching database size", zap.Error(err))
		errs.addPartial(err)
		return
	}
	r.Lock()
	r.dbSizeMap = databaseSizeMetrics
	r.Unlock()
}

func (*postgreSQLScraper) retrieveBackends(
	ctx context.Context,
	wg *sync.WaitGroup,
	client client,
	databases []string,
	r *dbRetrieval,
	errs *errsMux,
) {
	defer wg.Done()
	activityByDB, err := client.getBackends(ctx, databases)
	if err != nil {
		errs.addPartial(err)
		return
	}
	r.Lock()
	r.activityMap = activityByDB
	r.Unlock()
}

func (p *postgreSQLScraper) setupResourceBuilder(rb *metadata.ResourceBuilder, database, schema, table, index string) *metadata.ResourceBuilder {
	rb.SetServiceInstanceID(p.serviceInstanceID)
	if database != "" {
		rb.SetPostgresqlDatabaseName(database)
	}
	if schema != "" {
		rb.SetPostgresqlSchemaName(schema)
	}
	if table != "" {
		rb.SetPostgresqlTableName(table)
	}
	if index != "" {
		rb.SetPostgresqlIndexName(index)
	}
	return rb
}

func getInstanceID(instanceString string, logger *zap.Logger) string {
	const fallback = "unknown:5432"
	host, port, err := net.SplitHostPort(instanceString)
	if err != nil {
		logger.Warn("Unable to determine actual instance ID for constructing service.instance.id", zap.Error(err))
		return fallback
	}

	if strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback() {
		localhost, hostNameErr := os.Hostname()
		if hostNameErr != nil {
			logger.Warn("Failed getting localhost machine name to construct service.instance.id.")
		} else {
			host = localhost
		}
	}
	return host + ":" + port
}

// blockingPidsToKey renders a (presumed already-sorted) []any of int64 PIDs as a
// comma-joined string suitable for inclusion in the seenQuerySamples dedup key.
// The empty slice yields "".
func blockingPidsToKey(pids []any) string {
	if len(pids) == 0 {
		return ""
	}
	var b strings.Builder
	for i, v := range pids {
		if i > 0 {
			b.WriteByte(',')
		}
		switch n := v.(type) {
		case int64:
			b.WriteString(strconv.FormatInt(n, 10))
		case int:
			b.WriteString(strconv.Itoa(n))
		default:
			fmt.Fprintf(&b, "%v", v)
		}
	}
	return b.String()
}

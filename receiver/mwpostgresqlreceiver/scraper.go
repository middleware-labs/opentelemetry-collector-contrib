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
	"sort"
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
	// defaultQueryTextCacheSize matches PostgreSQL's default
	// pg_stat_statements.max. Query text is retrieved only for cache misses, so
	// this bounds the receiver's retained text while avoiding a full reload on
	// every scrape for the usual server configuration.
	defaultQueryTextCacheSize = 5000
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
	// selection is the immutable include/exclude policy every collection path
	// consults, so metrics, schema and query telemetry cannot disagree about
	// which databases are in scope.
	selection databaseSelection
	// plan records which query families any enabled metric still needs. It is
	// derived from config.Metrics once at construction so the per-scrape path
	// only reads booleans.
	plan               collectionPlan
	statements         *statementStateCache
	queryTextCache     *lru.Cache[queryStatsKey, string]
	queryTextCacheOnce sync.Once
	changeTracker      *XminChangeTracker
	// if enabled, uses a separated attribute for the schema
	separateSchemaAttr   bool
	queryPlanCache       *expirable.LRU[queryPlanKey, string]
	newestQueryTimestamp float64
	topQueryDisabled     bool
	// resetDetector notices when the server discards the counters that cache
	// holds previous values for. It lives beside the cache because its only
	// purpose is deciding when that cache has become meaningless.
	resetDetector *resetDetector
	// instanceTracker notices when the server on the other end of the
	// connection is no longer the same running process, which invalidates the
	// same cache for a different reason.
	instanceTracker *instanceTracker
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
	statements *statementStateCache,
	queryPlanCache *expirable.LRU[queryPlanKey, string],
) *postgreSQLScraper {
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
		selection:          newDatabaseSelection(config.Databases, config.ExcludeDatabases),
		plan:               newCollectionPlan(config.Metrics),
		statements:         statements,
		queryTextCache:     newQueryTextCache(defaultQueryTextCacheSize),
		resetDetector:      newResetDetector(),
		instanceTracker:    newInstanceTracker(),
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

	listClient, err := p.clientFactory.getClient(defaultPostgreSQLDatabase)
	if err != nil {
		p.logger.Error("Failed to initialize connection to postgres", zap.Error(err))
		return pmetric.NewMetrics(), err
	}
	defer listClient.Close()

	// Discovery runs only when there is no allowlist. An allowlist is
	// authoritative: a named database that is not currently connectable is a
	// failure to report, not a reason to widen scope, and a discovery failure
	// must never be resolved by collecting from everything.
	var discovered []string
	if !p.selection.isRestricted() {
		dbList, dbErr := listClient.listDatabases(ctx)
		if dbErr != nil {
			p.logger.Error("Failed to request list of databases from postgres", zap.Error(dbErr))
			return pmetric.NewMetrics(), dbErr
		}
		discovered = dbList
	}
	databases := p.selection.effectiveDatabases(discovered)

	now := pcommon.NewTimestampFromTime(time.Now())

	var errs errsMux
	r := &dbRetrieval{
		activityMap: make(map[databaseName]int64),
		dbSizeMap:   make(map[databaseName]int64),
		dbStats:     make(map[databaseName]databaseStats),
	}
	p.retrieveDBMetrics(ctx, listClient, p.selection, r, &errs)

	for _, database := range databases {
		p.collectDatabaseMetrics(ctx, now, database, r, &errs)
	}

	// Active connections emit one resource per database, and EmitForResource
	// flushes every data point recorded since the previous emit into that
	// resource. Run it first, while nothing server-wide is pending: recorded
	// after it, the server-wide families below all reach the final resource
	// with no database name. Ordered after it, they were swept into whichever
	// database's connection resource happened to be emitted first, which is a
	// map iteration and therefore a different database on different scrapes.
	if p.plan.activeConnections {
		p.collectActiveConnections(ctx, now, listClient, &errs)
	}

	p.mb.RecordPostgresqlDatabaseCountDataPoint(now, int64(len(databases)))
	if p.plan.bgWriter {
		p.collectBGWriterStats(ctx, now, listClient, &errs)
	}
	if p.plan.walAge {
		p.collectWalAge(ctx, now, listClient, &errs)
	}
	if p.plan.replication {
		p.collectReplicationStats(ctx, now, listClient, &errs)
	}
	if p.plan.maxConnections {
		p.collectMaxConnections(ctx, now, listClient, &errs)
	}
	// These two read database-local catalogs (pg_locks joined to pg_class, and
	// pg_stat_all_tables) but run only on the maintenance connection, so their
	// values describe `postgres` alone while their resource identity does not
	// say so. Moving them into the per-database loop would change that identity
	// and collide with existing series, which the plan defers to a separately
	// reviewed change. Until then the honest behavior is to stop collecting
	// them when `postgres` itself is out of scope, rather than reporting one
	// database's locks as though they were the server's.
	if p.selection.includes(defaultPostgreSQLDatabase) {
		if p.plan.databaseLocks {
			p.collectDatabaseLocks(ctx, now, listClient, &errs)
		}
		if p.plan.rowStats {
			p.collectRowStats(ctx, now, listClient, &errs)
		}
	}
	if p.plan.queryPerf {
		p.collectQueryPerfStats(ctx, now, listClient, &errs)
	}
	if p.plan.bufferHit {
		p.collectBufferHits(ctx, now, listClient, &errs)
	}
	if p.plan.walStats {
		p.collectWALStats(ctx, now, listClient, &errs)
	}
	if p.plan.transactions {
		p.collectTransactionsStats(ctx, now, listClient, &errs)
	}

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
	// With every per-database collector disabled there is nothing this
	// connection would be used for, so do not open it. The database still gets
	// its resource and whatever data points the maintenance-connection queries
	// produced for it.
	if !p.plan.needsDatabaseClient() {
		p.recordDatabase(now, database, r, 0)
		return
	}

	dbClient, dbErr := p.clientFactory.getClient(database)
	if dbErr != nil {
		errs.add(dbErr)
		p.logger.Error("Failed to initialize connection to postgres", zap.String("database", database), zap.Error(dbErr))
		return
	}
	defer dbClient.Close()

	var numTables int64
	if p.plan.tables {
		numTables = p.collectTables(ctx, now, dbClient, database, errs)
	}

	p.recordDatabase(now, database, r, numTables)
	if p.plan.indexes {
		p.collectIndexes(ctx, now, dbClient, database, errs)
	}
	if p.plan.functions {
		p.collectFunctions(ctx, now, dbClient, database, errs)
	}
	if p.plan.tableBloat {
		p.collectTableBloat(ctx, now, dbClient, database, errs)
	}
	if p.plan.indexBloat {
		p.collectIndexBloat(ctx, now, dbClient, database, errs)
	}
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

	attributes, newestQueryTimestamp, err := dbClient.getQuerySamples(ctx, limit, p.newestQueryTimestamp, p.selection, logger)
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

// topQueryCounterColumns names the twelve cumulative pg_stat_statements
// counters the receiver differences and reports as per-interval deltas.
//
// The delta arithmetic itself lives in statementCounters, whose fields are
// named and typed, so nothing looks a counter up by name at runtime. This list
// exists so a test can assert that the struct and the emitted attributes cover
// exactly the same set: adding a counter to the SQL and the row without adding
// it here, or the reverse, is the mistake worth catching.
var topQueryCounterColumns = []string{
	callsColumnName,
	rowsColumnName,
	sharedBlksDirtiedColumnName,
	sharedBlksHitColumnName,
	sharedBlksReadColumnName,
	sharedBlksWrittenColumnName,
	tempBlksReadColumnName,
	tempBlksWrittenColumnName,
	totalExecTimeColumnName,
	totalPlanTimeColumnName,
	blkReadTimeAttributeName,
	blkWriteTimeAttributeName,
}

func (p *postgreSQLScraper) collectTopQuery(ctx context.Context, clientFactory postgreSQLClientFactory, limit, topNQuery, maxExplainEachInterval int64, mux *errsMux, logger *zap.Logger) {
	timestamp := pcommon.NewTimestampFromTime(time.Now())

	if p.topQueryDisabled {
		return
	}

	// Candidates for the *extension* connection, not for data scope.
	//
	// pg_stat_statements reports statistics for the whole server regardless of
	// which database the extension is installed in, so this list only decides
	// where to connect to read the view. The rows it returns are filtered to the
	// configured selection separately, below.
	//
	// `postgres` is appended even when it is outside the selection: it is a
	// control connection used to reach server-wide statistics, not permission
	// to collect that database's telemetry. This exception is documented in
	// Configuration.md.
	// `postgres` is tried first because it is where pg_stat_statements is
	// installed on the large majority of servers, and every candidate that does
	// not have the extension costs a failed connection and a logged error
	// before the next one is tried.
	allowed := p.selection.effectiveDatabases(nil)
	candidateDatabases := make([]string, 0, len(allowed)+1)
	seen := map[string]struct{}{defaultPostgreSQLDatabase: {}}
	candidateDatabases = append(candidateDatabases, defaultPostgreSQLDatabase)
	for _, db := range allowed {
		if _, ok := seen[db]; ok {
			continue
		}
		seen[db] = struct{}{}
		candidateDatabases = append(candidateDatabases, db)
	}

	var rows []topQueryStatRow
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

		rows, err = dbClient.getTopQuery(ctx, limit, p.selection, logger)
		if err == nil {
			// Ask the same connection that just read the counters whether they
			// were discarded since last scrape. pg_stat_statements_reset()
			// zeroes every counter, so the cached previous values describe a
			// series that no longer exists; differencing against them reports
			// the new absolute values as if they were one interval's work.
			//
			// This is checked here, on a successful read, so a reset is only
			// acted on when there are fresh counters to re-baseline against.
			if pgClient, ok := dbClient.(*postgreSQLClient); ok {
				// Two independent reasons the cached counters may no longer
				// describe the same series, checked on the connection that
				// just read them.
				if p.instanceTracker.check(ctx, pgClient.client) {
					// A restart zeroed the counters, or a failover pointed us
					// at a different server that has been counting on its own.
					// The second case is why this check exists: a promoted
					// standby can report counters HIGHER than the cached ones,
					// so the delta comes out positive and plausible while
					// describing a different machine entirely. Nothing else
					// notices that.
					logger.Info("postgres instance changed, discarding cached counters",
						zap.String("database", database))
					p.statements.purge()
				} else if caps, capsErr := pgClient.statementCapabilities(ctx); capsErr == nil &&
					p.resetDetector.check(ctx, pgClient.client, caps) {
					logger.Info("pg_stat_statements was reset, discarding cached counters",
						zap.String("database", database))
					p.statements.purge()
				}
			}
		}
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

	// Selection works on the typed rows, and a row's text is obfuscated and
	// scanned for comments only once it has been selected for emission. The
	// queue therefore carries an index into rows plus the deltas computed for
	// it, rather than a decorated attribute map per candidate.
	pq := make(priorityqueue.PriorityQueue[topQuerySelection, float64], 0)

	for i := range rows {
		row := &rows[i]

		if !row.queryID.Valid {
			// this should not happen, but in case
			logger.Error("queryid is nil", zap.Int("row", i))
			mux.addPartial(errors.New("queryid is nil"))
			continue
		}
		queryID := strconv.FormatInt(row.queryID.Int64, 10)

		// pg_stat_statements keys its entries on (userid, dbid, queryid,
		// toplevel), not on queryid alone: the same normalized query executed
		// by two roles, or in two databases, is two separate rows with
		// independent counters. Keying the delta cache on queryid alone merges
		// them, so each row is differenced against whichever of its siblings
		// was seen last and the emitted deltas are meaningless - typically
		// oscillating between a large positive value and zero as the rows take
		// turns. Key on the same tuple the server does, taken from the OIDs the
		// row projects rather than from the names they join to.
		//
		// The cache holds one entry per statement, so the twelve counters are
		// stored, read and evicted together, and no key string is built: the
		// identity is a comparable struct used as the map key directly.
		// observe applies the first-observation, re-entry, decrease and
		// calls-did-not-advance rules and stores the new baseline in every case.
		deltas, reportable := p.statements.observe(row.identity(), row.snapshot())
		if !reportable {
			continue
		}
		if deltas.totalExecTime == 0.0 {
			continue
		}
		item := priorityqueue.QueueItem[topQuerySelection, float64]{
			Value:    topQuerySelection{index: i, queryID: queryID, deltas: deltas},
			Priority: deltas.totalExecTime,
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
		item := heap.Pop(&pq).(*priorityqueue.QueueItem[topQuerySelection, float64])
		sel := item.Value
		row := &rows[sel.index]

		// Enrichment happens here, for selected rows only: obfuscation, comment
		// extraction and trace-context parsing used to run for every candidate
		// during decoding, whether or not the row was ever emitted.
		enriched := enrichTopQueryRow(row, logger)

		query := enriched.obfuscated
		queryID := sel.queryID
		// Use raw query (with $1, $2 placeholders) for EXPLAIN, not the obfuscated one (with ?)
		rawQuery := enriched.rawQuery

		// pg_stat_statements rows outlive the databases they came from: once a
		// database is dropped, its dbid no longer joins to pg_database and
		// datname comes back NULL, which leaves db.namespace absent from the
		// row. Such a row cannot be EXPLAINed (there is no database to connect
		// to), but it is still a real query worth reporting, so it is emitted
		// under a placeholder rather than dropped.
		database := row.datname.String
		if database == "" {
			unresolvedDatabases++
			database = unknownDatabaseName
		}

		// The plan cache is keyed on the database as well as the statement.
		// A queryid identifies a normalized statement, not a plan: the same
		// text against two databases has two different sets of tables,
		// statistics and indexes, so it plans differently. Keyed on queryid
		// alone, whichever database was EXPLAINed first supplied the plan
		// reported for every other database's copy of that statement - and
		// silently, since the plan is plausible SQL either way. The key is a
		// comparable struct, so no key string is built per row either.
		planKey := queryPlanKey{queryID: row.queryID.Int64, database: database}
		plan, ok := p.queryPlanCache.Get(planKey)
		// Check membership again before opening a connection. The SQL predicate
		// already restricts the rows, but EXPLAIN is the one place the receiver
		// connects to a database named by the data rather than by
		// configuration, so it gets an independent check: a row that reached
		// here out of scope must not cause a connection to an unselected
		// database.
		if !ok && explained < maxExplainEachInterval && database != unknownDatabaseName &&
			p.selection.includes(database) {
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
				p.queryPlanCache.Add(planKey, plan)
				if closeErr := dbClient.Close(); closeErr != nil {
					logger.Error("failed to close db client after explain", zap.Error(closeErr))
				}
				explained++
			}
		}

		// Extract table names from raw query for db.query.tables enrichment
		tables := strings.Join(extractTablesFromQuery(rawQuery), ",")
		// user.name is aliased from rolname
		rolname := row.rolname.String

		logCtx := context.Background()
		if enriched.traceCtx != nil {
			logCtx = enriched.traceCtx
		}

		topComment := enriched.comment
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
			sel.deltas.calls,
			sel.deltas.rows,
			sel.deltas.sharedBlksDirtied,
			sel.deltas.sharedBlksHit,
			sel.deltas.sharedBlksRead,
			sel.deltas.sharedBlksWritten,
			sel.deltas.tempBlksRead,
			sel.deltas.tempBlksWritten,
			queryID,
			rolname,
			sel.deltas.totalExecTime,
			sel.deltas.totalPlanTime,
			plan,
			sel.deltas.blkReadTime,
			sel.deltas.blkWriteTime,
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
	sel databaseSelection,
	r *dbRetrieval,
	errs *errsMux,
) {
	wg := &sync.WaitGroup{}

	// Each of these three is one query on the maintenance connection feeding one
	// group of per-database data points; a group with nothing enabled leaves its
	// map empty and recordDatabase simply finds no entry for it.
	if p.plan.backends {
		wg.Add(1)
		go p.retrieveBackends(ctx, wg, listClient, sel, r, errs)
	}
	if p.plan.databaseSize {
		wg.Add(1)
		go p.retrieveDatabaseSize(ctx, wg, listClient, sel, r, errs)
	}
	if p.plan.databaseStats {
		wg.Add(1)
		go p.retrieveDatabaseStats(ctx, wg, listClient, sel, r, errs)
	}

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
	var blockReads map[tableIdentifier]tableIOStats
	if p.plan.blocksReadByTable {
		var brErr error
		blockReads, brErr = dbClient.getBlocksReadByTable(ctx, db)
		if brErr != nil {
			errs.addPartial(brErr)
		}
	}

	tableMetrics, err := dbClient.getDatabaseTableMetrics(ctx, db)
	if err != nil {
		errs.addPartial(err)
	}

	// postgresql.table.count needs only the row count. When no per-table metric
	// is enabled, skip the conversion and the resource per table entirely.
	if !p.plan.tableDetails && !p.plan.blocksReadByTable {
		return int64(len(tableMetrics))
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
	// Server-wide: active connections are a property of the instance, not of
	// any one database, so this deliberately passes an unrestricted selection.
	stats, err := client.getConnectionStats(ctx, databaseSelection{})
	if err != nil {
		errs.addPartial(err)
		return
	}

	// Emit in a fixed order so a scrape's output does not depend on map
	// iteration; the resources are independent, so only determinism is gained.
	names := make([]string, 0, len(stats))
	for dbName := range stats {
		names = append(names, string(dbName))
	}
	sort.Strings(names)
	for _, name := range names {
		for _, s := range stats[databaseName(name)] {
			p.mb.RecordPostgresqlConnectionCountDataPoint(now, s.count, s.state, s.app, s.user)
		}
		rb := p.setupResourceBuilder(p.mb.NewResourceBuilder(), name, "", "", "")
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
	p.queryTextCacheOnce.Do(func() {
		max, err := client.getQueryStatsMax(ctx)
		if err != nil {
			p.logger.Debug("using default query text cache size", zap.Error(err))
			return
		}
		if max > 0 {
			p.queryTextCache.Resize(max)
		}
	})

	queryStats, err := client.getQueryStats(ctx, p.selection)
	if err != nil {
		errs.addPartial(err)
		return
	}

	missing := make([]queryStatsKey, 0)
	for i := range queryStats {
		stat := &queryStats[i]
		if stat.queryText != "" {
			// Keep this path for test clients and for callers that already
			// resolved text. Production clients intentionally leave it empty.
			p.queryTextCache.Add(stat.key, stat.queryText)
			continue
		}
		if text, ok := p.queryTextCache.Get(stat.key); ok {
			stat.queryText = text
			continue
		}
		missing = append(missing, stat.key)
	}

	if len(missing) > 0 {
		texts, textErr := client.getQueryTexts(ctx, missing)
		if textErr != nil {
			errs.addPartial(textErr)
			return
		}
		for key, text := range texts {
			p.queryTextCache.Add(key, text)
		}
		for i := range queryStats {
			if queryStats[i].queryText == "" {
				queryStats[i].queryText, _ = p.queryTextCache.Get(queryStats[i].key)
			}
		}
	}

	for _, s := range queryStats {
		if s.queryText == "" || s.queryText == excludedQueryText {
			// Text can be unavailable after pg_stat_statements garbage-collects
			// its external query-text file, or until a DBA grants access. Keep
			// retrying on later scrapes rather than caching that absence.
			continue
		}
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
	bhs, err := client.getBufferHit(ctx, p.selection)
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
	sel databaseSelection,
	r *dbRetrieval,
	errs *errsMux,
) {
	defer wg.Done()
	dbStats, err := client.getDatabaseStats(ctx, sel)
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
	sel databaseSelection,
	r *dbRetrieval,
	errs *errsMux,
) {
	defer wg.Done()
	databaseSizeMetrics, err := client.getDatabaseSize(ctx, sel)
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
	sel databaseSelection,
	r *dbRetrieval,
	errs *errsMux,
) {
	defer wg.Done()
	activityByDB, err := client.getBackends(ctx, sel)
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

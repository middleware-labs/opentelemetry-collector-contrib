// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/scrapererror"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

type postgreSQLScraper struct {
	logger        *zap.Logger
	config        *Config
	clientFactory postgreSQLClientFactory
	mb            *metadata.MetricsBuilder
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

type postgreSQLClientFactory interface {
	getClient(c *Config, database string) (client, error)
}

type defaultClientFactory struct{}

func (d *defaultClientFactory) getClient(c *Config, database string) (client, error) {
	return newPostgreSQLClient(postgreSQLConfig{
		username: c.Username,
		password: string(c.Password),
		database: database,
		tls:      c.TLSClientSetting,
		address:  c.NetAddr,
	})
}

func newPostgreSQLScraper(
	settings receiver.CreateSettings,
	config *Config,
	clientFactory postgreSQLClientFactory,
) *postgreSQLScraper {
	return &postgreSQLScraper{
		logger:        settings.Logger,
		config:        config,
		clientFactory: clientFactory,
		mb:            metadata.NewMetricsBuilder(config.MetricsBuilderConfig, settings),
	}
}

type dbRetrieval struct {
	sync.RWMutex
	activityMap map[databaseName]int64
	dbSizeMap   map[databaseName]int64
	dbStats     map[databaseName]databaseStats
}

// scrape scrapes the metric stats, transforms them and attributes them into a metric slices.
func (p *postgreSQLScraper) scrape(ctx context.Context) (pmetric.Metrics, error) {
	databases := p.config.Databases
	listClient, err := p.clientFactory.getClient(p.config, "")
	if err != nil {
		p.logger.Error("Failed to initialize connection to postgres", zap.Error(err))
		return pmetric.NewMetrics(), err
	}
	defer listClient.Close()

	if len(databases) == 0 {
		dbList, err := listClient.listDatabases(ctx)
		if err != nil {
			p.logger.Error("Failed to request list of databases from postgres", zap.Error(err))
			return pmetric.NewMetrics(), err
		}
		databases = dbList
	}

	now := pcommon.NewTimestampFromTime(time.Now())

	var errs errsMux
	r := &dbRetrieval{
		activityMap: make(map[databaseName]int64),
		dbSizeMap:   make(map[databaseName]int64),
		dbStats:     make(map[databaseName]databaseStats),
	}
	p.retrieveDBMetrics(ctx, listClient, databases, r, &errs)

	for _, database := range databases {
		dbClient, err := p.clientFactory.getClient(p.config, database)
		if err != nil {
			errs.add(err)
			p.logger.Error("Failed to initialize connection to postgres", zap.String("database", database), zap.Error(err))
			continue
		}
		defer dbClient.Close()
		numTables := p.collectTables(ctx, now, dbClient, database, &errs)

		p.recordDatabase(now, database, r, numTables)
		p.collectIndexes(ctx, now, dbClient, database, &errs)
	}

	p.mb.RecordPostgresqlDatabaseCountDataPoint(now, int64(len(databases)))
	p.collectBGWriterStats(ctx, now, listClient, &errs)
	p.collectWalAge(ctx, now, listClient, &errs)
	p.collectReplicationStats(ctx, now, listClient, &errs)
	p.collectMaxConnections(ctx, now, listClient, &errs)
	p.collectActivityStats(ctx, now, listClient, &errs)
	p.collectQueryStats(ctx, now, listClient, &errs)
	p.collectIOStats(ctx, now, listClient, &errs)
	p.collectAnalyzeCount(ctx, now, listClient, &errs)
	p.collectChecksumStats(ctx, now, listClient, &errs)
	p.collectBufferHits(ctx, now, listClient, &errs)
	p.collectClusterVacuumStats(ctx, now, listClient, &errs)
	p.collectDatabaseConflictStats(ctx, now, listClient, &errs)
	p.collectCommts(ctx, now, listClient, &errs)
	p.collectSessionStats(ctx, now, listClient, &errs)
	p.collectDiskReads(ctx, now, listClient, &errs)
	p.collectFuncStats(ctx, now, listClient, &errs)
	p.collectHeapBlockStats(ctx, now, listClient, &errs)

	rb := p.mb.NewResourceBuilder()
	rb.SetPostgresqlDatabaseName("N/A")
	p.mb.EmitForResource(metadata.WithResource(rb.Emit()))

	return p.mb.Emit(), errs.combine()
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
		p.mb.RecordPostgresqlRollbacksDataPoint(now, stats.transactionRollback)
		p.mb.RecordPostgresqlDeadlocksDataPoint(now, stats.deadlocks)
		p.mb.RecordPostgresqlTempFilesDataPoint(now, stats.tempFiles)
	}
	rb := p.mb.NewResourceBuilder()
	rb.SetPostgresqlDatabaseName(db)
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

	for tableKey, tm := range tableMetrics {
		p.mb.RecordPostgresqlRowsDataPoint(now, tm.dead, metadata.AttributeStateDead)
		p.mb.RecordPostgresqlRowsDataPoint(now, tm.live, metadata.AttributeStateLive)
		p.mb.RecordPostgresqlOperationsDataPoint(now, tm.inserts, metadata.AttributeOperationIns)
		p.mb.RecordPostgresqlOperationsDataPoint(now, tm.del, metadata.AttributeOperationDel)
		p.mb.RecordPostgresqlOperationsDataPoint(now, tm.upd, metadata.AttributeOperationUpd)
		p.mb.RecordPostgresqlOperationsDataPoint(now, tm.hotUpd, metadata.AttributeOperationHotUpd)
		p.mb.RecordPostgresqlTableSizeDataPoint(now, tm.size)
		p.mb.RecordPostgresqlTableVacuumCountDataPoint(now, tm.vacuumCount)
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
		rb := p.mb.NewResourceBuilder()
		rb.SetPostgresqlDatabaseName(db)
		rb.SetPostgresqlTableName(tm.table)
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
		rb := p.mb.NewResourceBuilder()
		rb.SetPostgresqlDatabaseName(database)
		rb.SetPostgresqlTableName(stat.table)
		rb.SetPostgresqlIndexName(stat.index)
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
	p.mb.RecordPostgresqlBgwriterBuffersWritesDataPoint(now, bgStats.bufferBackendWrites, metadata.AttributeBgBufferSourceBackend)
	p.mb.RecordPostgresqlBgwriterBuffersWritesDataPoint(now, bgStats.bufferCheckpoints, metadata.AttributeBgBufferSourceCheckpoints)
	p.mb.RecordPostgresqlBgwriterBuffersWritesDataPoint(now, bgStats.bufferFsyncWrites, metadata.AttributeBgBufferSourceBackendFsync)

	p.mb.RecordPostgresqlBgwriterCheckpointCountDataPoint(now, bgStats.checkpointsReq, metadata.AttributeBgCheckpointTypeRequested)
	p.mb.RecordPostgresqlBgwriterCheckpointCountDataPoint(now, bgStats.checkpointsScheduled, metadata.AttributeBgCheckpointTypeScheduled)

	p.mb.RecordPostgresqlBgwriterDurationDataPoint(now, bgStats.checkpointSyncTime, metadata.AttributeBgDurationTypeSync)
	p.mb.RecordPostgresqlBgwriterDurationDataPoint(now, bgStats.checkpointWriteTime, metadata.AttributeBgDurationTypeWrite)

	p.mb.RecordPostgresqlBgwriterMaxwrittenDataPoint(now, bgStats.maxWritten)
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
		if rs.writeLag >= 0 {
			p.mb.RecordPostgresqlWalLagDataPoint(now, rs.writeLag, metadata.AttributeWalOperationLagWrite, rs.clientAddr)
		}
		if rs.replayLag >= 0 {
			p.mb.RecordPostgresqlWalLagDataPoint(now, rs.replayLag, metadata.AttributeWalOperationLagReplay, rs.clientAddr)
		}
		if rs.flushLag >= 0 {
			p.mb.RecordPostgresqlWalLagDataPoint(now, rs.flushLag, metadata.AttributeWalOperationLagFlush, rs.clientAddr)
		}
	}
}

func (p *postgreSQLScraper) collectBufferHits(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	buffhits, err := client.getBufferHit(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}
	for _, bh := range buffhits {
		p.mb.RecordPostgresqlBufferHitDataPoint(now, bh.hits, bh.dbName)
	}
}

func (p *postgreSQLScraper) collectActivityStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	as, err := client.getActivityStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, s := range as {
		p.mb.RecordPostgresqlActiveQueriesDataPoint(now, s.activeQueries, s.queryStatment)
		p.mb.RecordPostgresqlActiveWaitingQueriesDataPoint(now, s.activeQueries, s.queryStatment)
		p.mb.RecordPostgresqlActivityBackendXidAgeDataPoint(now, s.backendXidAge, s.queryStatment)
		p.mb.RecordPostgresqlActivityBackendXminAgeDataPoint(now, s.backendXminAge, s.queryStatment)
		p.mb.RecordPostgresqlActivityXactStartAgeDataPoint(now, s.xactStartAge, s.queryStatment)
	}
}

func (p *postgreSQLScraper) collectQueryStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	qs, err := client.getQueryStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}
	for _, s := range qs {
		blkReadTime := strconv.FormatFloat(s.blkReadTime, 'f', -1, 64)
		blkWriteTime := strconv.FormatFloat(s.blkWriteTime, 'f', -1, 64)
		count := strconv.FormatInt(s.count, 10)

		// durationMax := strconv.FormatInt(s.durationMax, 10)
		// durationSum := strconv.FormatInt(s.durationSum, 10)
		localBlksDirtied := strconv.FormatInt(s.localBlksDirtied, 10)
		localBlksHit := strconv.FormatInt(s.localBlksHit, 10)
		localBlksRead := strconv.FormatInt(s.localBlksRead, 10)
		localBlksWritten := strconv.FormatInt(s.localBlksWritten, 10)
		rows := strconv.FormatInt(s.rows, 10)
		sharedBlksDirtied := strconv.FormatInt(s.sharedBlksDirtied, 10)
		sharedBlksHit := strconv.FormatInt(s.sharedBlksHit, 10)
		sharedBlksRead := strconv.FormatInt(s.sharedBlksRead, 10)
		sharedBlksWritten := strconv.FormatInt(s.sharedBlksWritten, 10)
		tempBlksRead := strconv.FormatInt(s.tempBlksRead, 10)
		tempBlksWritten := strconv.FormatInt(s.tempBlksWritten, 10)
		time := strconv.FormatFloat(s.time, 'f', -1, 64)
		p.mb.RecordPostgresqlQueriesBlkReadTimeDataPoint(now, blkReadTime, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesBlkWriteTimeDataPoint(now, blkWriteTime, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesCountDataPoint(now, count, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesDurationMaxDataPoint(now, s.durationMax) // Add here
		p.mb.RecordPostgresqlQueriesDurationSumDataPoint(now, s.durationSum)
		p.mb.RecordPostgresqlQueriesLocalBlksDirtiedDataPoint(now, localBlksDirtied, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesLocalBlksHitDataPoint(now, localBlksHit, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesLocalBlksReadDataPoint(now, localBlksRead, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesLocalBlksWrittenDataPoint(now, localBlksWritten, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesRowsDataPoint(now, rows, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesSharedBlksDirtiedDataPoint(now, sharedBlksDirtied, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesLocalBlksHitDataPoint(now, sharedBlksHit, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesSharedBlksReadDataPoint(now, sharedBlksRead, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesSharedBlksWrittenDataPoint(now, sharedBlksWritten, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesTempBlksReadDataPoint(now, tempBlksRead, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesTempBlksWrittenDataPoint(now, tempBlksWritten, s.userid, s.dbid, s.queryid, s.query)
		p.mb.RecordPostgresqlQueriesTimeDataPoint(now, time, s.userid, s.dbid, s.queryid, s.query)
	}
}

func (p *postgreSQLScraper) collectIOStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	iostats, err := client.getIOStats(ctx)

	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, s := range iostats {
		evictions := strconv.FormatInt(s.evictions, 10)
		extend_time := strconv.FormatFloat(s.extend_time, 'f', -1, 64)
		extends := strconv.FormatInt(s.extends, 10)
		fsyncs := strconv.FormatInt(s.fsyncs, 10)
		fsync_time := strconv.FormatFloat(s.fsync_time, 'f', -1, 64)
		hits := strconv.FormatInt(s.hits, 10)
		read_time := strconv.FormatFloat(s.read_time, 'f', -1, 64)
		reads := strconv.FormatInt(s.reads, 10)
		write_time := strconv.FormatFloat(s.write_time, 'f', -1, 64)
		writes := strconv.FormatInt(s.writes, 10)

		p.mb.RecordPostgresqlIoEvictionsDataPoint(now, evictions, s.backendType)
		p.mb.RecordPostgresqlIoExtendTimeDataPoint(now, extend_time, s.backendType)
		p.mb.RecordPostgresqlIoExtendsDataPoint(now, extends, s.backendType)
		p.mb.RecordPostgresqlIoFsyncsDataPoint(now, fsyncs, s.backendType)
		p.mb.RecordPostgresqlIoFsyncTimeDataPoint(now, fsync_time, s.backendType)
		p.mb.RecordPostgresqlIoHitsDataPoint(now, hits, s.backendType)
		p.mb.RecordPostgresqlIoReadTimeDataPoint(now, read_time, s.backendType)
		p.mb.RecordPostgresqlIoReadsDataPoint(now, reads, s.backendType)
		p.mb.RecordPostgresqlIoWriteTimeDataPoint(now, write_time, s.backendType)
		p.mb.RecordPostgresqlIoWritesDataPoint(now, writes, s.backendType)
	}
}

func (p *postgreSQLScraper) collectDiskReads(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	drs, err := client.getDiskReads(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}
	for _, s := range drs {
		p.mb.RecordPostgresqlDiskReadDataPoint(now, s.diskRead, s.dbid, s.dbname)
	}
}

func (p *postgreSQLScraper) collectCommts(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	cmts, err := client.getCommits(ctx)

	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, s := range cmts {
		p.mb.RecordPostgresqlCommitsDataPoint(now, s.commits, s.dbid, s.dbname)
	}
}

func (p *postgreSQLScraper) collectDatabaseConflictStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	cs, err := client.getDatabaseConflictsStats(ctx)

	if err != nil {
		errs.addPartial(err)
		return
	}
	for _, s := range cs {
		tbs := strconv.FormatInt(s.tableSpace, 10)
		l := strconv.FormatInt(s.lock, 10)
		ss := strconv.FormatInt(s.snapshot, 10)
		bp := strconv.FormatInt(s.bufferPin, 10)
		dl := strconv.FormatInt(s.deadlock, 10)

		p.mb.RecordPostgresqlConflictsTablespaceDataPoint(now, tbs, s.dbid, s.dbname)
		p.mb.RecordPostgresqlConflictsLockDataPoint(now, l, s.dbid, s.dbname)
		p.mb.RecordPostgresqlConflictsSnapshotDataPoint(now, ss, s.dbid, s.dbname)
		p.mb.RecordPostgresqlConflictsBufferpinDataPoint(now, bp, s.dbid, s.dbname)
		p.mb.RecordPostgresqlConflictsDeadlockDataPoint(now, dl, s.dbid, s.dbname)

	}

}

func (p *postgreSQLScraper) collectChecksumStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	cs, err := client.getChecksumStats(ctx)

	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, c := range cs {
		enabled := strconv.FormatInt(int64(c.checksumEnabled), 10)
		failures := strconv.FormatInt(c.checksumFailures, 10)
		dbname := c.dbname

		p.mb.RecordPostgresqlChecksumsChecksumFailuresDataPoint(now, failures, dbname)
		p.mb.RecordPostgresqlChecksumsEnabledDataPoint(now, enabled, dbname)
	}
}

func (p *postgreSQLScraper) collectSessionStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	ss, err := client.getSessionStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, s := range ss {
		abandoned := strconv.FormatInt(s.sessionsAbandoned, 10)
		activeTime := strconv.FormatFloat(s.activeTime, 'f', -1, 64)
		count := strconv.FormatInt(s.sessionCount, 10)
		fatal := strconv.FormatInt(s.sessionCount, 10)
		killed := strconv.FormatInt(s.sessionsKilled, 10)
		time := strconv.FormatFloat(s.sessionTime, 'f', -1, 64)
		idleXactTime := strconv.FormatFloat(s.idleInTransctionTime, 'f', -1, 64)
		p.mb.RecordPostgresqlSessionsAbandonedDataPoint(now, abandoned, s.dbid, s.dbname)
		p.mb.RecordPostgresqlSessionsActiveTimeDataPoint(now, activeTime, s.dbid, s.dbname)
		p.mb.RecordPostgresqlSessionsCountDataPoint(now, count, s.dbid, s.dbname)
		p.mb.RecordPostgresqlSessionsFatalDataPoint(now, fatal, s.dbid, s.dbname)
		p.mb.RecordPostgresqlSessionsKilledDataPoint(now, killed, s.dbid, s.dbname)
		p.mb.RecordPostgresqlSessionsSessionTimeDataPoint(now, time, s.dbid, s.dbname)
		p.mb.RecordPostgresqlSessionsIdleInTransactionTimeDataPoint(now, idleXactTime, s.dbid, s.dbname)
	}
}

func (p *postgreSQLScraper) collectClusterVacuumStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	cvs, err := client.getClusterVacuumStats(ctx)

	if err != nil {
		errs.addPartial(err)
		return
	}
	for _, s := range cvs {
		p.mb.RecordPostgresqlClusterVacuumHeapBlksScannedDataPoint(
			now, s.heapBlksScanned, s.datname, s.relname, s.command, s.phase, s.index,
		)
		p.mb.RecordPostgresqlClusterVacuumHeapBlksTotalDataPoint(
			now, s.heapBlksTotal, s.datname, s.relname, s.command, s.phase, s.index,
		)
		p.mb.RecordPostgresqlClusterVacuumHeapTuplesScannedDataPoint(
			now, s.heapTuplesScanned, s.datname, s.relname, s.command, s.phase, s.index,
		)
		p.mb.RecordPostgresqlClusterVacuumHeapTuplesWrittenDataPoint(
			now, s.heapTuplesWrites, s.datname, s.relname, s.command, s.phase, s.index,
		)
		p.mb.RecordPostgresqlClusterVacuumIndexRebuildCountDataPoint(
			now, s.indexRebuildCount, s.datname, s.relname, s.command, s.phase, s.index,
		)
	}
}

func (p *postgreSQLScraper) collectAnalyzeCount(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	analyzeCounts, err := client.getAnalyzeCount(ctx)

	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, c := range analyzeCounts {
		ac := strconv.FormatInt(c.analyzeCount, 10)
		aac := strconv.FormatInt(c.autoAnalyzeCount, 10)
		avc := strconv.FormatInt(c.autoVacuumCount, 10)
		p.mb.RecordPostgresqlAnalyzedDataPoint(now, ac, c.schemaname, c.relname)
		p.mb.RecordPostgresqlAutoanalyzedDataPoint(now, aac, c.schemaname, c.relname)
		p.mb.RecordPostgresqlAutovacuumedDataPoint(now, avc, c.relname, c.relname)
	}
}

func (p *postgreSQLScraper) collectFuncStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	fs, err := client.getFunctionStats(ctx)
	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, s := range fs {
		calls := strconv.FormatInt(s.calls, 10)
		selfTime := strconv.FormatFloat(s.selfTime, 'f', -1, 64)
		totalTime := strconv.FormatFloat(s.totalTime, 'f', -1, 64)
		p.mb.RecordPostgresqlFunctionCallsDataPoint(now, calls, s.fname, s.fid, s.schemaName)
		p.mb.RecordPostgresqlFunctionSelfTimeDataPoint(now, selfTime, s.fname, s.fid, s.schemaName)
		p.mb.RecordPostgresqlFunctionTotalTimeDataPoint(now, totalTime, s.fname, s.fid, s.schemaName)
	}
}

func (p *postgreSQLScraper) collectHeapBlockStats(
	ctx context.Context,
	now pcommon.Timestamp,
	client client,
	errs *errsMux,
) {
	hbs, err := client.getHeapBlocksStats(ctx)

	if err != nil {
		errs.addPartial(err)
		return
	}

	for _, s := range hbs {
		p.mb.RecordPostgresqlHeapBlocksHitDataPoint(now, s.hits, s.relId, s.schemaName, s.relName)
		p.mb.RecordPostgresqlHeapBlocksReadDataPoint(now, s.reads, s.relId, s.schemaName, s.relName)
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

func (p *postgreSQLScraper) retrieveBackends(
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

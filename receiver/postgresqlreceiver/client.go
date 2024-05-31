// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/lib/pq"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/config/configtls"
	"go.uber.org/multierr"
)

// databaseName is a name that refers to a database so that it can be uniquely referred to later
// i.e. database1
type databaseName string

// tableIdentifier is an identifier that contains both the database and table separated by a "|"
// i.e. database1|table2
type tableIdentifier string

// indexIdentifier is a unique string that identifies a particular index and is separated by the "|" character
type indexIdentifer string

// errNoLastArchive is an error that occurs when there is no previous wal archive, so there is no way to compute the
// last archived point
var errNoLastArchive = errors.New("no last archive found, not able to calculate oldest WAL age")

type client interface {
	Close() error
	getDatabaseStats(ctx context.Context, databases []string) (map[databaseName]databaseStats, error)
	getBGWriterStats(ctx context.Context) (*bgStat, error)
	getBackends(ctx context.Context, databases []string) (map[databaseName]int64, error)
	getDatabaseSize(ctx context.Context, databases []string) (map[databaseName]int64, error)
	getDatabaseTableMetrics(ctx context.Context, db string) (map[tableIdentifier]tableStats, error)
	getBlocksReadByTable(ctx context.Context, db string) (map[tableIdentifier]tableIOStats, error)
	getReplicationStats(ctx context.Context) ([]replicationStats, error)
	getLatestWalAgeSeconds(ctx context.Context) (int64, error)
	getMaxConnections(ctx context.Context) (int64, error)
	getIndexStats(ctx context.Context, database string) (map[indexIdentifer]indexStat, error)
	listDatabases(ctx context.Context) ([]string, error)
	getActivityStats(ctx context.Context) ([]queryActivityStats, error)
	getQueryStats(ctx context.Context) ([]queryStats, error)
	getIOStats(ctx context.Context) ([]IOStats, error)
	getAnalyzeCount(ctx context.Context) ([]AnalyzeCount, error)
	getChecksumStats(ctx context.Context) ([]ChecksumStats, error)
	getBufferHit(ctx context.Context) ([]BufferHit, error)
	getClusterVacuumStats(ctx context.Context) ([]ClusterVacuumStats, error)
	getDatabaseConflictsStats(ctx context.Context) ([]conflictStats, error)
	getCommits(ctx context.Context) ([]Commits, error)
	getSessionStats(ctx context.Context) ([]SessionStats, error)
	getDiskReads(ctx context.Context) ([]DiskReads, error)
	getFunctionStats(ctx context.Context) ([]FuncStats, error)
	getHeapBlocksStats(ctx context.Context) ([]HeapBlockStats, error)
	getBloatStats(ctx context.Context) ([]BloatStats, error)
	getRowStats(ctx context.Context) ([]RowStats, error)
	getTransactionsStats(ctx context.Context) ([]TransactionStats, error)
}

type postgreSQLClient struct {
	client   *sql.DB
	database string
}

var _ client = (*postgreSQLClient)(nil)

type postgreSQLConfig struct {
	username string
	password string
	database string
	address  confignet.NetAddr
	tls      configtls.TLSClientSetting
}

func sslConnectionString(tls configtls.TLSClientSetting) string {
	if tls.Insecure {
		return "sslmode='disable'"
	}

	conn := ""

	if tls.InsecureSkipVerify {
		conn += "sslmode='require'"
	} else {
		conn += "sslmode='verify-full'"
	}

	if tls.CAFile != "" {
		conn += fmt.Sprintf(" sslrootcert='%s'", tls.CAFile)
	}

	if tls.KeyFile != "" {
		conn += fmt.Sprintf(" sslkey='%s'", tls.KeyFile)
	}

	if tls.CertFile != "" {
		conn += fmt.Sprintf(" sslcert='%s'", tls.CertFile)
	}

	return conn
}

func newPostgreSQLClient(conf postgreSQLConfig) (*postgreSQLClient, error) {
	// postgres will assume the supplied user as the database name if none is provided,
	// so we must specify a databse name even when we are just collecting the list of databases.
	dbField := "dbname=postgres"
	if conf.database != "" {
		dbField = fmt.Sprintf("dbname=%s ", conf.database)
	}

	host, port, err := net.SplitHostPort(conf.address.Endpoint)
	if err != nil {
		return nil, err
	}

	if conf.address.Transport == "unix" {
		// lib/pg expects a unix socket host to start with a "/" and appends the appropriate .s.PGSQL.port internally
		host = fmt.Sprintf("/%s", host)
	}

	connStr := fmt.Sprintf("port=%s host=%s user=%s password=%s %s %s", port, host, conf.username, conf.password, dbField, sslConnectionString(conf.tls))

	conn, err := pq.NewConnector(connStr)
	if err != nil {
		return nil, err
	}

	db := sql.OpenDB(conn)

	return &postgreSQLClient{
		client:   db,
		database: conf.database,
	}, nil
}

func (c *postgreSQLClient) Close() error {
	return c.client.Close()
}

type databaseStats struct {
	transactionCommitted int64
	transactionRollback  int64
	deadlocks            int64
	tempFiles            int64
}

func (c *postgreSQLClient) getDatabaseStats(ctx context.Context, databases []string) (map[databaseName]databaseStats, error) {
	query := filterQueryByDatabases("SELECT datname, xact_commit, xact_rollback, deadlocks, temp_files FROM pg_stat_database", databases, false)
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	var errs error
	dbStats := map[databaseName]databaseStats{}
	for rows.Next() {
		var datname string
		var transactionCommitted, transactionRollback, deadlocks, tempFiles int64
		err = rows.Scan(&datname, &transactionCommitted, &transactionRollback, &deadlocks, &tempFiles)
		if err != nil {
			errs = multierr.Append(errs, err)
			continue
		}
		if datname != "" {
			dbStats[databaseName(datname)] = databaseStats{
				transactionCommitted: transactionCommitted,
				transactionRollback:  transactionRollback,
				deadlocks:            deadlocks,
				tempFiles:            tempFiles,
			}
		}
	}
	return dbStats, errs
}

// getBackends returns a map of database names to the number of active connections
func (c *postgreSQLClient) getBackends(ctx context.Context, databases []string) (map[databaseName]int64, error) {
	query := filterQueryByDatabases("SELECT datname, count(*) as count from pg_stat_activity", databases, true)
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ars := map[databaseName]int64{}
	var errors error
	for rows.Next() {
		var datname string
		var count int64
		err = rows.Scan(&datname, &count)
		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		if datname != "" {
			ars[databaseName(datname)] = count
		}
	}
	return ars, errors
}

func (c *postgreSQLClient) getDatabaseSize(ctx context.Context, databases []string) (map[databaseName]int64, error) {
	query := filterQueryByDatabases("SELECT datname, pg_database_size(datname) FROM pg_catalog.pg_database WHERE datistemplate = false", databases, false)
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sizes := map[databaseName]int64{}
	var errors error
	for rows.Next() {
		var datname string
		var size int64
		err = rows.Scan(&datname, &size)
		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		if datname != "" {
			sizes[databaseName(datname)] = size
		}
	}
	return sizes, errors
}

// tableStats contains a result for a row of the getDatabaseTableMetrics result
type tableStats struct {
	database    string
	table       string
	live        int64
	dead        int64
	inserts     int64
	upd         int64
	del         int64
	hotUpd      int64
	seqScans    int64
	size        int64
	vacuumCount int64
}

func (c *postgreSQLClient) getDatabaseTableMetrics(ctx context.Context, db string) (map[tableIdentifier]tableStats, error) {
	query := `SELECT schemaname || '.' || relname AS table,
	n_live_tup AS live,
	n_dead_tup AS dead,
	n_tup_ins AS ins,
	n_tup_upd AS upd,
	n_tup_del AS del,
	n_tup_hot_upd AS hot_upd,
	seq_scan AS seq_scans,
	pg_relation_size(relid) AS table_size,
	vacuum_count
	FROM pg_stat_user_tables;`

	ts := map[tableIdentifier]tableStats{}
	var errors error
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var table string
		var live, dead, ins, upd, del, hotUpd, seqScans, tableSize, vacuumCount int64
		err = rows.Scan(&table, &live, &dead, &ins, &upd, &del, &hotUpd, &seqScans, &tableSize, &vacuumCount)
		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		ts[tableKey(db, table)] = tableStats{
			database:    db,
			table:       table,
			live:        live,
			inserts:     ins,
			upd:         upd,
			del:         del,
			hotUpd:      hotUpd,
			seqScans:    seqScans,
			size:        tableSize,
			vacuumCount: vacuumCount,
		}
	}
	return ts, errors
}

type tableIOStats struct {
	database  string
	table     string
	heapRead  int64
	heapHit   int64
	idxRead   int64
	idxHit    int64
	toastRead int64
	toastHit  int64
	tidxRead  int64
	tidxHit   int64
}

func (c *postgreSQLClient) getBlocksReadByTable(ctx context.Context, db string) (map[tableIdentifier]tableIOStats, error) {
	query := `SELECT schemaname || '.' || relname AS table,
	coalesce(heap_blks_read, 0) AS heap_read,
	coalesce(heap_blks_hit, 0) AS heap_hit,
	coalesce(idx_blks_read, 0) AS idx_read,
	coalesce(idx_blks_hit, 0) AS idx_hit,
	coalesce(toast_blks_read, 0) AS toast_read,
	coalesce(toast_blks_hit, 0) AS toast_hit,
	coalesce(tidx_blks_read, 0) AS tidx_read,
	coalesce(tidx_blks_hit, 0) AS tidx_hit
	FROM pg_statio_user_tables;`

	tios := map[tableIdentifier]tableIOStats{}
	var errors error
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var table string
		var heapRead, heapHit, idxRead, idxHit, toastRead, toastHit, tidxRead, tidxHit int64
		err = rows.Scan(&table, &heapRead, &heapHit, &idxRead, &idxHit, &toastRead, &toastHit, &tidxRead, &tidxHit)
		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		tios[tableKey(db, table)] = tableIOStats{
			database:  db,
			table:     table,
			heapRead:  heapRead,
			heapHit:   heapHit,
			idxRead:   idxRead,
			idxHit:    idxHit,
			toastRead: toastRead,
			toastHit:  toastHit,
			tidxRead:  tidxRead,
			tidxHit:   tidxHit,
		}
	}
	return tios, errors
}

type indexStat struct {
	index    string
	table    string
	database string
	size     int64
	scans    int64
}

func (c *postgreSQLClient) getIndexStats(ctx context.Context, database string) (map[indexIdentifer]indexStat, error) {
	query := `SELECT relname, indexrelname,
	pg_relation_size(indexrelid) AS index_size,
	idx_scan
	FROM pg_stat_user_indexes;`

	stats := map[indexIdentifer]indexStat{}

	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var errs []error
	for rows.Next() {
		var (
			table, index          string
			indexSize, indexScans int64
		)
		err := rows.Scan(&table, &index, &indexSize, &indexScans)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		stats[indexKey(database, table, index)] = indexStat{
			index:    index,
			table:    table,
			database: database,
			size:     indexSize,
			scans:    indexScans,
		}
	}
	return stats, multierr.Combine(errs...)
}

type bgStat struct {
	checkpointsReq       int64
	checkpointsScheduled int64
	checkpointWriteTime  float64
	checkpointSyncTime   float64
	bgWrites             int64
	backendWrites        int64
	bufferBackendWrites  int64
	bufferFsyncWrites    int64
	bufferCheckpoints    int64
	buffersAllocated     int64
	maxWritten           int64
}

func (c *postgreSQLClient) getBGWriterStats(ctx context.Context) (*bgStat, error) {
	query := `SELECT
	checkpoints_req AS checkpoint_req,
	checkpoints_timed AS checkpoint_scheduled,
	checkpoint_write_time AS checkpoint_duration_write,
	checkpoint_sync_time AS checkpoint_duration_sync,
	buffers_clean AS bg_writes,
	buffers_backend AS backend_writes,
	buffers_backend_fsync AS buffers_written_fsync,
	buffers_checkpoint AS buffers_checkpoints,
	buffers_alloc AS buffers_allocated,
	maxwritten_clean AS maxwritten_count
	FROM pg_stat_bgwriter;`

	row := c.client.QueryRowContext(ctx, query)
	var (
		checkpointsReq, checkpointsScheduled               int64
		checkpointSyncTime, checkpointWriteTime            float64
		bgWrites, bufferCheckpoints, bufferAllocated       int64
		bufferBackendWrites, bufferFsyncWrites, maxWritten int64
	)
	err := row.Scan(
		&checkpointsReq,
		&checkpointsScheduled,
		&checkpointWriteTime,
		&checkpointSyncTime,
		&bgWrites,
		&bufferBackendWrites,
		&bufferFsyncWrites,
		&bufferCheckpoints,
		&bufferAllocated,
		&maxWritten,
	)
	if err != nil {
		return nil, err
	}
	return &bgStat{
		checkpointsReq:       checkpointsReq,
		checkpointsScheduled: checkpointsScheduled,
		checkpointWriteTime:  checkpointWriteTime,
		checkpointSyncTime:   checkpointSyncTime,
		bgWrites:             bgWrites,
		backendWrites:        bufferBackendWrites,
		bufferBackendWrites:  bufferBackendWrites,
		bufferFsyncWrites:    bufferFsyncWrites,
		bufferCheckpoints:    bufferCheckpoints,
		buffersAllocated:     bufferAllocated,
		maxWritten:           maxWritten,
	}, nil
}

func (c *postgreSQLClient) getMaxConnections(ctx context.Context) (int64, error) {
	query := `SHOW max_connections;`
	row := c.client.QueryRowContext(ctx, query)
	var maxConns int64
	err := row.Scan(&maxConns)
	return maxConns, err
}

type replicationStats struct {
	clientAddr   string
	pendingBytes int64
	flushLag     int64
	replayLag    int64
	writeLag     int64
}

func (c *postgreSQLClient) getReplicationStats(ctx context.Context) ([]replicationStats, error) {
	query := `SELECT
	client_addr,
	coalesce(pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn), -1) AS replication_bytes_pending,
	extract('epoch' from coalesce(write_lag, '-1 seconds')),
	extract('epoch' from coalesce(flush_lag, '-1 seconds')),
	extract('epoch' from coalesce(replay_lag, '-1 seconds'))
	FROM pg_stat_replication;
	`
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_replication: %w", err)
	}
	defer rows.Close()
	var rs []replicationStats
	var errors error
	for rows.Next() {
		var client string
		var replicationBytes, writeLag, flushLag, replayLag int64
		err = rows.Scan(&client, &replicationBytes, &writeLag, &flushLag, &replayLag)
		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		rs = append(rs, replicationStats{
			clientAddr:   client,
			pendingBytes: replicationBytes,
			replayLag:    replayLag,
			writeLag:     writeLag,
			flushLag:     flushLag,
		})
	}

	return rs, errors
}

type queryActivityStats struct {
	activeQueries        int64
	activeWaitingQueries int64
	backendXidAge        int64
	backendXminAge       int64
	xactStartAge         float64
	queryStatment        string
}

func (c *postgreSQLClient) getActivityStats(ctx context.Context) ([]queryActivityStats, error) {
	query := `SELECT
	query,
    COUNT(*) FILTER (WHERE state = 'active') OVER () AS active_queries,
    COUNT(*) FILTER (WHERE state = 'active' AND wait_event IS NOT NULL) OVER () AS active_waiting_queries,
    MAX(age(backend_xid)) OVER () AS backend_xid_age,
    MAX(age(backend_xmin)) OVER () AS backend_xmin_age,
    MAX(EXTRACT(EPOCH FROM (clock_timestamp() - xact_start))) OVER () AS xact_start_age
	FROM pg_stat_activity
	WHERE backend_type = 'client backend' AND query !~* '^vacuum ';	
	`
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_activity: %w", err)
	}

	defer rows.Close()

	var as []queryActivityStats
	var errors error
	for rows.Next() {
		var activeQueries, activeWaitingQueries int64
		var backendXidAge, backendXminAge sql.NullInt64
		var xactStartAge float64
		var queryStatment string
		err = rows.Scan(
			&queryStatment,
			&activeQueries,
			&activeWaitingQueries,
			&backendXidAge,
			&backendXminAge,
			&xactStartAge,
		)
		if err != nil {
			errors = multierr.Append(errors, err)
		}

		as = append(as, queryActivityStats{
			queryStatment:        queryStatment,
			activeQueries:        activeQueries,
			activeWaitingQueries: activeWaitingQueries,
			backendXidAge:        backendXidAge.Int64,
			backendXminAge:       backendXminAge.Int64,
			xactStartAge:         xactStartAge,
		})
	}

	return as, errors
}

type queryStats struct {
	userid            int64
	dbid              int64
	queryid           int64
	query             string
	blkReadTime       float64
	blkWriteTime      float64
	count             int64
	durationMax       float64
	durationSum       float64
	localBlksDirtied  int64
	localBlksHit      int64
	localBlksRead     int64
	localBlksWritten  int64
	rows              int64
	sharedBlksDirtied int64
	sharedBlksHit     int64
	sharedBlksRead    int64
	sharedBlksWritten int64
	tempBlksRead      int64
	tempBlksWritten   int64
	time              float64
}

func (c *postgreSQLClient) getQueryStats(ctx context.Context) ([]queryStats, error) {
	query := `SELECT
    userid,
    dbid,
    queryid,
    query,
    SUM(blk_read_time) AS blk_read_time,
    SUM(blk_write_time) AS blk_write_time,
    MAX(calls) AS query_count,
    MAX(total_exec_time) AS max_query_duration,
    SUM(total_exec_time) AS sum_query_duration,
    SUM(local_blks_dirtied) AS local_blks_dirtied,
    SUM(local_blks_hit) AS local_blks_hit,
    SUM(local_blks_read) AS local_blks_read,
    SUM(local_blks_written) AS local_blks_written,
    SUM(rows) AS total_rows,
    SUM(shared_blks_dirtied) AS shared_blks_dirtied,
    SUM(shared_blks_hit) AS shared_blks_hit,
    SUM(shared_blks_read) AS shared_blks_read,
    SUM(shared_blks_written) AS shared_blks_written,
    SUM(temp_blks_read) AS temp_blks_read,
    SUM(temp_blks_written) AS temp_blks_written,
    SUM(total_exec_time) AS total_query_time
	FROM pg_stat_statements
	GROUP BY userid, dbid, queryid, query;
	`
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_statements: %w", err)
	}

	defer rows.Close()

	var errors error
	var qs []queryStats

	for rows.Next() {
		var (
			userid            int64
			dbid              int64
			queryid           int64
			querytext         string
			blkReadTime       float64
			blkWriteTime      float64
			count             int64
			durationMax       float64
			durationSum       float64
			localBlksDirtied  int64
			localBlksHit      int64
			localBlksRead     int64
			localBlksWritten  int64
			rowsCount         int64
			sharedBlksDirtied int64
			sharedBlksHit     int64
			sharedBlksRead    int64
			sharedBlksWritten int64
			tempBlksRead      int64
			tempBlksWritten   int64
			time              float64
		)

		err := rows.Scan(
			&userid,
			&dbid,
			&queryid,
			&querytext,
			&blkReadTime,
			&blkWriteTime,
			&count,
			&durationMax,
			&durationSum,
			&localBlksDirtied,
			&localBlksHit,
			&localBlksRead,
			&localBlksWritten,
			&rowsCount,
			&sharedBlksDirtied,
			&sharedBlksHit,
			&sharedBlksRead,
			&sharedBlksWritten,
			&tempBlksRead,
			&tempBlksWritten,
			&time,
		)
		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		qs = append(qs, queryStats{
			userid:            userid,
			dbid:              dbid,
			queryid:           queryid,
			query:             querytext,
			blkReadTime:       blkReadTime,
			blkWriteTime:      blkWriteTime,
			count:             count,
			durationMax:       durationMax,
			durationSum:       durationSum,
			localBlksDirtied:  localBlksDirtied,
			localBlksHit:      localBlksHit,
			localBlksRead:     localBlksRead,
			localBlksWritten:  localBlksWritten,
			rows:              rowsCount,
			sharedBlksDirtied: sharedBlksDirtied,
			sharedBlksHit:     sharedBlksHit,
			sharedBlksRead:    sharedBlksRead,
			sharedBlksWritten: sharedBlksWritten,
			tempBlksRead:      tempBlksRead,
			tempBlksWritten:   tempBlksWritten,
			time:              time,
		})
	}
	return qs, errors
}

type SessionStats struct {
	dbid                 int64
	dbname               string
	sessionsAbandoned    int64
	activeTime           float64
	sessionCount         int64
	sessionsFatal        int64
	idleInTransctionTime float64
	sessionsKilled       int64
	sessionTime          float64
}

func (c *postgreSQLClient) getSessionStats(ctx context.Context) ([]SessionStats, error) {
	query := ` SELECT datid, datname, sessions_abandoned, active_time, sessions, sessions_fatal, idle_in_transaction_time, sessions_killed, session_time FROM pg_stat_database;`
	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_database:: %w", err)
	}

	defer rows.Close()

	var ss []SessionStats
	var errors error

	for rows.Next() {
		var (
			dbid                 sql.NullInt64
			dbname               sql.NullString
			sessionsAbandoned    sql.NullInt64
			activeTime           sql.NullFloat64
			sessionCount         sql.NullInt64
			sessionsFatal        sql.NullInt64
			idleInTransctionTime sql.NullFloat64
			sessionsKilled       sql.NullInt64
			sessionTime          sql.NullFloat64
		)
		err := rows.Scan(
			&dbid,
			&dbname,
			&sessionsAbandoned,
			&activeTime,
			&sessionCount,
			&sessionsFatal,
			&idleInTransctionTime,
			&sessionsKilled,
			&sessionTime,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		ss = append(ss, SessionStats{
			dbid:                 dbid.Int64,
			dbname:               dbname.String,
			sessionsAbandoned:    sessionCount.Int64,
			activeTime:           activeTime.Float64,
			sessionCount:         sessionCount.Int64,
			sessionsFatal:        sessionsFatal.Int64,
			idleInTransctionTime: idleInTransctionTime.Float64,
			sessionsKilled:       sessionsKilled.Int64,
			sessionTime:          sessionTime.Float64,
		})
	}
	return ss, nil
}

func (c *postgreSQLClient) getLatestWalAgeSeconds(ctx context.Context) (int64, error) {
	query := `SELECT
	coalesce(last_archived_time, CURRENT_TIMESTAMP) AS last_archived_wal,
	CURRENT_TIMESTAMP
	FROM pg_stat_archiver;
	`
	row := c.client.QueryRowContext(ctx, query)
	var lastArchivedWal, currentInstanceTime time.Time
	err := row.Scan(&lastArchivedWal, &currentInstanceTime)
	if err != nil {
		return 0, err
	}

	if lastArchivedWal.Equal(currentInstanceTime) {
		return 0, errNoLastArchive
	}

	age := int64(currentInstanceTime.Sub(lastArchivedWal).Seconds())
	return age, nil
}

func (c *postgreSQLClient) listDatabases(ctx context.Context) ([]string, error) {
	query := `SELECT datname FROM pg_database
	WHERE datistemplate = false;`
	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var databases []string
	for rows.Next() {
		var database string
		if err := rows.Scan(&database); err != nil {
			return nil, err
		}

		databases = append(databases, database)
	}
	return databases, nil
}

type IOStats struct {
	backendType string
	evictions   int64
	extend_time float64
	extends     int64
	fsync_time  float64
	fsyncs      int64
	hits        int64
	read_time   float64
	reads       int64
	write_time  float64
	writes      int64
}

func (c *postgreSQLClient) getIOStats(ctx context.Context) ([]IOStats, error) {
	query := `SELECT backend_type,
		evictions,
		extend_time,
		extends,
		fsync_time,
		fsyncs,
		hits,
		read_time,
		reads,
		write_time,
		writes
	FROM pg_stat_io
	LIMIT 200;
	`
	majorVersion, err := c.getVersion(ctx)
	if err != nil {
		return nil, err
	}
	if majorVersion < 16 {
		return nil, fmt.Errorf("PostgreSQL version %d is less than 16", majorVersion)
	}
	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_io:: %w", err)
	}

	defer rows.Close()

	var errors error
	var ioS []IOStats

	for rows.Next() {
		var (
			backend_type sql.NullString
			evictions    sql.NullInt64
			extend_time  sql.NullFloat64
			extends      sql.NullInt64
			fsync_time   sql.NullFloat64
			fsyncs       sql.NullInt64
			hits         sql.NullInt64
			read_time    sql.NullFloat64
			reads        sql.NullInt64
			write_time   sql.NullFloat64
			writes       sql.NullInt64
		)
		err := rows.Scan(
			&backend_type,
			&evictions,
			&extend_time,
			&extends,
			&fsync_time,
			&fsyncs,
			&hits,
			&read_time,
			&reads,
			&write_time,
			&writes,
		)
		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}

		ioS = append(ioS, IOStats{
			backendType: backend_type.String,
			evictions:   evictions.Int64,
			extend_time: extend_time.Float64,
			extends:     extends.Int64,
			fsync_time:  fsync_time.Float64,
			fsyncs:      fsyncs.Int64,
			hits:        hits.Int64,
			read_time:   read_time.Float64,
			reads:       reads.Int64,
			write_time:  write_time.Float64,
			writes:      writes.Int64,
		})
	}
	return ioS, errors
}

type ChecksumStats struct {
	dbname           string
	checksumFailures int64
	checksumEnabled  int
}

func (c *postgreSQLClient) getChecksumStats(ctx context.Context) ([]ChecksumStats, error) {
	query := `SELECT datname, checksum_failures FROM pg_stat_database;`

	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_database:: %w", err)
	}

	defer rows.Close()

	var errors error
	var cs []ChecksumStats

	enabled, err := c.getChecksumEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot check if checksums are enabled:: %w", err)
	}
	for rows.Next() {
		var (
			dbname   sql.NullString
			failures sql.NullInt64
		)
		err := rows.Scan(
			&dbname,
			&failures,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
		}

		cs = append(cs, ChecksumStats{
			dbname:           dbname.String,
			checksumEnabled:  enabled,
			checksumFailures: failures.Int64,
		})
	}
	return cs, errors
}

func (c *postgreSQLClient) getChecksumEnabled(ctx context.Context) (int, error) {
	/*
		-1 : Error
		 0 : Checksums disabled
		 1 : Checksums enabled
	*/
	var enabled sql.NullString
	err := c.client.QueryRowContext(ctx, "SHOW data_checksums;").Scan(&enabled)
	if err != nil {
		return -1, fmt.Errorf("failed to get data_checksums")
	}

	if enabled.String == "off" {
		return 0, nil
	} else {
		return 1, nil
	}

}

type AnalyzeCount struct {
	schemaname       string
	relname          string
	analyzeCount     int64
	autoAnalyzeCount int64
	autoVacuumCount  int64
}

func (c *postgreSQLClient) getAnalyzeCount(ctx context.Context) ([]AnalyzeCount, error) {
	query := `SELECT
    schemaname AS schema,
    relname AS table,
    analyze_count,
	autovacuum_count,
	autoanalyze_count
	FROM
    pg_stat_all_tables;
	`
	/*
	   SELECT analyze_count, autovacuum_count, autoanalyze_count FROM pg_stat_all_tables;
	*/
	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unabled to query pg_stat_user_tables:: %w", err)
	}
	defer rows.Close()

	var errors error
	var analyzeCounts []AnalyzeCount

	for rows.Next() {
		var analyzeCount sql.NullInt64
		var autoAnalyzeCount sql.NullInt64
		var autoVacuumCount sql.NullInt64
		var schemaname sql.NullString
		var relname sql.NullString

		err := rows.Scan(
			&schemaname,
			&relname,
			&analyzeCount,
			&autoVacuumCount,
			&autoAnalyzeCount,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		analyzeCounts = append(analyzeCounts, AnalyzeCount{
			schemaname:       schemaname.String,
			relname:          relname.String,
			analyzeCount:     analyzeCount.Int64,
			autoAnalyzeCount: autoAnalyzeCount.Int64,
			autoVacuumCount:  autoVacuumCount.Int64,
		})
	}
	return analyzeCounts, errors
}

type conflictStats struct {
	dbid       int64
	dbname     string
	tableSpace int64
	lock       int64
	snapshot   int64
	bufferPin  int64
	deadlock   int64
}

func (c *postgreSQLClient) getDatabaseConflictsStats(ctx context.Context) ([]conflictStats, error) {
	query := ` SELECT
	datid,
	datname,
	confl_tablespace,
	confl_lock,
	confl_snapshot,
	confl_bufferpin,
	confl_deadlock
	FROM pg_catalog.pg_stat_database_conflicts;
	`

	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_database_conflicts:: %w", err)
	}

	defer rows.Close()

	var cs []conflictStats
	var errors error

	for rows.Next() {
		var (
			dbid       sql.NullInt64
			dbname     sql.NullString
			tableSpace sql.NullInt64
			lock       sql.NullInt64
			snapshot   sql.NullInt64
			bufferPin  sql.NullInt64
			deadlock   sql.NullInt64
		)
		err := rows.Scan(
			&dbid,
			&dbname,
			&tableSpace,
			&lock,
			&snapshot,
			&bufferPin,
			&deadlock,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		cs = append(cs, conflictStats{
			dbid:       dbid.Int64,
			dbname:     dbname.String,
			tableSpace: tableSpace.Int64,
			lock:       lock.Int64,
			snapshot:   snapshot.Int64,
			bufferPin:  bufferPin.Int64,
			deadlock:   deadlock.Int64,
		})
	}
	return cs, nil
}

type Commits struct {
	dbid    int64
	dbname  string
	commits int64
}

func (c *postgreSQLClient) getCommits(ctx context.Context) ([]Commits, error) {
	query := `SELECT datid, datname, xact_commit FROM pg_stat_database;`

	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_database:: %w", err)
	}

	defer rows.Close()

	var cmts []Commits
	var errors error

	for rows.Next() {
		var dbname sql.NullString
		var dbid, commts sql.NullInt64

		err = rows.Scan(&dbid, &dbname, &commts)

		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		cmts = append(cmts, Commits{
			dbid:    dbid.Int64,
			dbname:  dbname.String,
			commits: commts.Int64,
		})
	}
	return cmts, nil
}

type BufferHit struct {
	dbName string
	hits   int64
}

func (c *postgreSQLClient) getBufferHit(ctx context.Context) ([]BufferHit, error) {
	query := `SELECT datname, blks_hit FROM pg_stat_database;`

	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_database:: %w", err)
	}

	defer rows.Close()

	var bh []BufferHit
	var errors error

	for rows.Next() {
		var dbname sql.NullString
		var hits sql.NullInt64

		err = rows.Scan(&dbname, &hits)

		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}
		bh = append(bh, BufferHit{
			dbName: dbname.String,
			hits:   hits.Int64,
		})
	}
	return bh, errors
}

type ClusterVacuumStats struct {
	datname           string
	relname           string
	command           string
	phase             string
	index             string
	heapTuplesScanned int64
	heapTuplesWrites  int64
	heapBlksScanned   int64
	heapBlksTotal     int64
	indexRebuildCount int64
}

func (c *postgreSQLClient) getClusterVacuumStats(ctx context.Context) ([]ClusterVacuumStats, error) {
	query := `
	SELECT
       v.datname, c.relname, v.command, v.phase,
       i.relname,
       heap_tuples_scanned, heap_tuples_written, heap_blks_total, heap_blks_scanned, index_rebuild_count
  FROM pg_stat_progress_cluster as v
  LEFT JOIN pg_class c on c.oid = v.relid
  LEFT JOIN pg_class i on i.oid = v.cluster_index_relid;
	`

	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query pg_catalog.pg_stat_progress_vacuum")
	}
	defer rows.Close()

	var errors error
	var cvs []ClusterVacuumStats

	for rows.Next() {
		var (
			dbname  sql.NullString
			relname sql.NullString
			command sql.NullString
			phase   sql.NullString
			index   sql.NullString
		)

		var (
			heapBlksScanned   sql.NullInt64
			heapBlksTotal     sql.NullInt64
			heapTuplesWritten sql.NullInt64
			heapTuplesScanned sql.NullInt64
			indexRebuildCount sql.NullInt64
		)

		err := rows.Scan(
			&dbname,
			&relname,
			&command,
			&phase,
			&index,
			&heapTuplesScanned,
			&heapTuplesWritten,
			&heapBlksTotal,
			&heapBlksScanned,
			&indexRebuildCount,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
			continue
		}

		cvs = append(cvs, ClusterVacuumStats{
			datname:           getStringValue(dbname),
			relname:           getStringValue(relname),
			command:           getStringValue(command),
			phase:             getStringValue(phase),
			index:             getStringValue(index),
			heapTuplesScanned: getInt64Value(heapTuplesScanned),
			heapTuplesWrites:  getInt64Value(heapTuplesWritten),
			heapBlksTotal:     getInt64Value(heapBlksTotal),
			heapBlksScanned:   getInt64Value(heapBlksScanned),
			indexRebuildCount: getInt64Value(indexRebuildCount),
		})
	}
	if len(cvs) == 0 {
		cvs = append(cvs, ClusterVacuumStats{
			datname:           "",
			relname:           "",
			command:           "",
			phase:             "",
			index:             "",
			heapTuplesScanned: 0,
			heapTuplesWrites:  0,
			heapBlksTotal:     0,
			heapBlksScanned:   0,
			indexRebuildCount: 0,
		})
	}
	return cvs, nil
}

type DiskReads struct {
	diskRead int64
	dbname   string
	dbid     int64
}

func (c *postgreSQLClient) getDiskReads(ctx context.Context) ([]DiskReads, error) {
	query := `SELECT datid, datname, blks_read FROM pg_stat_database;`

	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_database")
	}

	defer rows.Close()

	var dr []DiskReads
	var errors error

	for rows.Next() {
		var (
			diskRead sql.NullInt64
			dbname   sql.NullString
			dbid     sql.NullInt64
		)

		err = rows.Scan(
			&dbid,
			&dbname,
			&diskRead,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
		}

		dr = append(dr, DiskReads{
			diskRead: diskRead.Int64,
			dbname:   dbname.String,
			dbid:     dbid.Int64,
		})
	}

	return dr, errors
}

type FuncStats struct {
	fid        int64
	schemaName string
	fname      string
	calls      int64
	totalTime  float64
	selfTime   float64
}

func (c *postgreSQLClient) getFunctionStats(ctx context.Context) ([]FuncStats, error) {
	query := `SELECT funcid, schemaname, funcname, calls, total_time, self_time FROM pg_stat_user_functions;`

	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_user_functions:: %w", err)
	}

	defer rows.Close()

	var fs []FuncStats
	var errors error

	for rows.Next() {
		var (
			fid        sql.NullInt64
			schemaName sql.NullString
			fname      sql.NullString
			calls      sql.NullInt64
			totalTime  sql.NullFloat64
			selfTime   sql.NullFloat64
		)

		err := rows.Scan(
			&fid,
			&schemaName,
			&fname,
			&calls,
			&totalTime,
			&selfTime,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
		}

		fs = append(fs, FuncStats{
			fid:        fid.Int64,
			schemaName: schemaName.String,
			fname:      fname.String,
			calls:      calls.Int64,
			totalTime:  totalTime.Float64,
			selfTime:   selfTime.Float64,
		})
	}
	return fs, nil
}

type HeapBlockStats struct {
	relId      int64
	schemaName string
	relName    string
	hits       int64
	reads      int64
}

func (c *postgreSQLClient) getHeapBlocksStats(ctx context.Context) ([]HeapBlockStats, error) {
	query := `SELECT relid, schemaname, relname, heap_blks_hit, heap_blks_read FROM pg_statio_all_tables;`

	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query pg_statio_all_tables:: %w", err)
	}

	defer rows.Close()

	var hbs []HeapBlockStats
	var errors error

	for rows.Next() {
		var (
			relId      sql.NullInt64
			schemaName sql.NullString
			relName    sql.NullString
			hits       sql.NullInt64
			reads      sql.NullInt64
		)

		err := rows.Scan(
			&relId,
			&schemaName,
			&relName,
			&hits,
			&reads,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
		}

		hbs = append(hbs, HeapBlockStats{
			relId:      relId.Int64,
			schemaName: schemaName.String,
			relName:    relName.String,
			hits:       hits.Int64,
			reads:      reads.Int64,
		})
	}
	return hbs, nil
}

type BloatStats struct {
	dbname       string
	schemaName   string
	relName      string
	tbloat       float64
	wastedBytes  int64
	indexName    string
	ibloat       float64
	wastedIBytes int64
}

func (c *postgreSQLClient) getBloatStats(ctx context.Context) ([]BloatStats, error) {
	query := `SELECT
	current_database(), schemaname, tablename, /*reltuples::bigint, relpages::bigint, otta,*/
	ROUND((CASE WHEN otta=0 THEN 0.0 ELSE sml.relpages::float/otta END)::numeric,1) AS tbloat,
	CASE WHEN relpages < otta THEN 0 ELSE bs*(sml.relpages-otta)::BIGINT END AS wastedbytes,
	iname, /*ituples::bigint, ipages::bigint, iotta,*/
	ROUND((CASE WHEN iotta=0 OR ipages=0 THEN 0.0 ELSE ipages::float/iotta END)::numeric,1) AS ibloat,
	CASE WHEN ipages < iotta THEN 0 ELSE bs*(ipages-iotta) END AS wastedibytes
	FROM (
		SELECT
		schemaname, tablename, cc.reltuples, cc.relpages, bs,
		CEIL((cc.reltuples*((datahdr+ma-
			(CASE WHEN datahdr%ma=0 THEN ma ELSE datahdr%ma END))+nullhdr2+4))/(bs-20::float)) AS otta,
		COALESCE(c2.relname,'?') AS iname, COALESCE(c2.reltuples,0) AS ituples, COALESCE(c2.relpages,0) AS ipages,
		COALESCE(CEIL((c2.reltuples*(datahdr-12))/(bs-20::float)),0) AS iotta -- very rough approximation, assumes all cols
		FROM (
		SELECT
			ma,bs,schemaname,tablename,
			(datawidth+(hdr+ma-(case when hdr%ma=0 THEN ma ELSE hdr%ma END)))::numeric AS datahdr,
			(maxfracsum*(nullhdr+ma-(case when nullhdr%ma=0 THEN ma ELSE nullhdr%ma END))) AS nullhdr2
		FROM (
			SELECT
			schemaname, tablename, hdr, ma, bs,
			SUM((1-null_frac)*avg_width) AS datawidth,
			MAX(null_frac) AS maxfracsum,
			hdr+(
				SELECT 1+count(*)/8
				FROM pg_stats s2
				WHERE null_frac<>0 AND s2.schemaname = s.schemaname AND s2.tablename = s.tablename
			) AS nullhdr
			FROM pg_stats s, (
			SELECT
				(SELECT current_setting('block_size')::numeric) AS bs,
				CASE WHEN substring(v,12,3) IN ('8.0','8.1','8.2') THEN 27 ELSE 23 END AS hdr,
				CASE WHEN v ~ 'mingw32' THEN 8 ELSE 4 END AS ma
			FROM (SELECT version() AS v) AS foo
			) AS constants
			GROUP BY 1,2,3,4,5
		) AS foo
		) AS rs
		JOIN pg_class cc ON cc.relname = rs.tablename
		JOIN pg_namespace nn ON cc.relnamespace = nn.oid AND nn.nspname = rs.schemaname AND nn.nspname <> 'information_schema'
		LEFT JOIN pg_index i ON indrelid = cc.oid
		LEFT JOIN pg_class c2 ON c2.oid = i.indexrelid
	) AS sml
	ORDER BY wastedbytes DESC;`

	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query bloat stats:: %w", err)
	}

	defer rows.Close()

	var bs []BloatStats
	var errors error

	for rows.Next() {
		var (
			dbname       sql.NullString
			schemaName   sql.NullString
			relName      sql.NullString
			tbloat       sql.NullFloat64
			wastedBytes  sql.NullInt64
			iname        sql.NullString
			ibloat       sql.NullFloat64
			wastedIBytes sql.NullInt64
		)

		err := rows.Scan(
			&dbname,
			&schemaName,
			&relName,
			&tbloat,
			&wastedBytes,
			&iname,
			&ibloat,
			&wastedIBytes,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
		}

		bs = append(bs, BloatStats{
			dbname:       dbname.String,
			schemaName:   schemaName.String,
			relName:      relName.String,
			tbloat:       tbloat.Float64,
			wastedBytes:  wastedBytes.Int64,
			indexName:    iname.String,
			ibloat:       ibloat.Float64,
			wastedIBytes: wastedIBytes.Int64,
		})
	}
	return bs, nil
}

type RowStats struct {
	relationName   string
	rowsReturned   int64
	rowsFetched    int64
	rowsInserted   int64
	rowsUpdated    int64
	rowsDeleted    int64
	rowsHotUpdated int64
	liveRows       int64
	deadRows       int64
}

func (c *postgreSQLClient) getRowStats(ctx context.Context) ([]RowStats, error) {
	query := `SELECT
    relname,
	pg_stat_get_tuples_returned(relid) AS rows_returned,
	pg_stat_get_tuples_fetched(relid) AS rows_fetched,
    pg_stat_get_tuples_inserted(relid) AS rows_inserted,
    pg_stat_get_tuples_updated(relid) AS rows_updated,
    pg_stat_get_tuples_deleted(relid) AS rows_deleted, 
    pg_stat_get_tuples_hot_updated(relid) AS rows_hot_updated,
    pg_stat_get_live_tuples(relid) AS live_rows,
    pg_stat_get_dead_tuples(relid) AS dead_rows
	FROM
    pg_stat_all_tables;
	`

	rows, err := c.client.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("unable to query pg_stat_all_tables:: %w", err)
	}

	defer rows.Close()

	var rs []RowStats
	var errors error

	for rows.Next() {
		var (
			relname        sql.NullString
			rowsReturned   sql.NullInt64
			rowsFetched    sql.NullInt64
			rowsInserted   sql.NullInt64
			rowsUpdated    sql.NullInt64
			rowsDeleted    sql.NullInt64
			rowsHotUpdated sql.NullInt64
			liveRows       sql.NullInt64
			deadRows       sql.NullInt64
		)

		err := rows.Scan(
			&relname,
			&rowsReturned,
			&rowsFetched,
			&rowsInserted,
			&rowsUpdated,
			&rowsDeleted,
			&rowsHotUpdated,
			&liveRows,
			&deadRows,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
		}

		rs = append(rs, RowStats{
			relname.String,
			rowsReturned.Int64,
			rowsFetched.Int64,
			rowsInserted.Int64,
			rowsUpdated.Int64,
			rowsDeleted.Int64,
			rowsHotUpdated.Int64,
			liveRows.Int64,
			deadRows.Int64,
		})
	}
	return rs, nil
}

type TransactionStats struct {
	totalDurationNanoseconds float64
	idleInTransactionCount   int64
	openInTransactionCount   int64
	pid                      int64
	duration                 float64
	userName                 string
	dbname                   string
	applicationName          string
}

func (c *postgreSQLClient) getTransactionsStats(ctx context.Context) ([]TransactionStats, error) {
	query := `WITH TransactionStats AS (
		SELECT
			SUM(EXTRACT(EPOCH FROM (now() - xact_start))) * 1e9 AS total_duration_nanoseconds, -- Convert seconds to nanoseconds for total duration
			COUNT(*) FILTER (WHERE state = 'idle in transaction') AS idle_in_transaction_count,
			COUNT(*) FILTER (WHERE xact_start IS NOT NULL) AS open_transactions_count
		FROM
			pg_stat_activity
	), LongestTransaction AS (
		SELECT
			pid,
			EXTRACT(EPOCH FROM (now() - xact_start)) * 1e9 AS duration_nanoseconds, -- Convert duration to nanoseconds
			usename AS user_name,
			datname AS database_name,
			application_name
		FROM
			pg_stat_activity
		WHERE
			xact_start IS NOT NULL
		ORDER BY
			duration_nanoseconds DESC
		LIMIT 1
	)
	SELECT
		TS.total_duration_nanoseconds,
		TS.idle_in_transaction_count,
		TS.open_transactions_count,
		LT.pid,
		LT.duration_nanoseconds AS duration, -- Alias for consistency
		LT.user_name,
		LT.database_name,
		LT.application_name
	FROM
		TransactionStats TS, LongestTransaction LT;
	
	`
	rows, err := c.client.QueryContext(ctx, query)

	if err != nil {
		return nil, fmt.Errorf("unable to query the transaction stats:: %w", err)
	}

	defer rows.Close()
	var ts []TransactionStats
	var errors error

	for rows.Next() {
		var (
			totalDurationNanoseconds sql.NullFloat64
			idleInTransactionCount   sql.NullInt64
			openInTransactionCount   sql.NullInt64
			pid                      sql.NullInt64
			duration                 sql.NullFloat64
			userName                 sql.NullString
			dbname                   sql.NullString
			applicationName          sql.NullString
		)

		err := rows.Scan(
			&totalDurationNanoseconds,
			&idleInTransactionCount,
			&openInTransactionCount,
			&pid,
			&duration,
			&userName,
			&dbname,
			&applicationName,
		)

		if err != nil {
			errors = multierr.Append(errors, err)
		}

		ts = append(ts, TransactionStats{
			totalDurationNanoseconds: totalDurationNanoseconds.Float64,
			idleInTransactionCount:   idleInTransactionCount.Int64,
			openInTransactionCount:   openInTransactionCount.Int64,
			pid:                      pid.Int64,
			duration:                 duration.Float64,
			userName:                 userName.String,
			dbname:                   dbname.String,
			applicationName:          applicationName.String,
		})
	}
	return ts, nil
}

func (c *postgreSQLClient) getVersion(ctx context.Context) (int, error) {
	var version string
	err := c.client.QueryRowContext(ctx, "SHOW server_version").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("failed to get PostgreSQL version: %w", err)
	}

	// Parse the major version number from the version string
	parts := strings.Split(version, ".")
	if len(parts) < 1 {
		return 0, fmt.Errorf("invalid PostgreSQL version string: %s", version)
	}

	var majorVersion int
	_, err = fmt.Sscanf(parts[0], "%d", &majorVersion)
	if err != nil {
		return 0, fmt.Errorf("failed to parse PostgreSQL major version: %w", err)
	}

	return majorVersion, nil
}

func filterQueryByDatabases(baseQuery string, databases []string, groupBy bool) string {
	if len(databases) > 0 {
		var queryDatabases []string
		for _, db := range databases {
			queryDatabases = append(queryDatabases, fmt.Sprintf("'%s'", db))
		}
		if strings.Contains(baseQuery, "WHERE") {
			baseQuery += fmt.Sprintf(" AND datname IN (%s)", strings.Join(queryDatabases, ","))
		} else {
			baseQuery += fmt.Sprintf(" WHERE datname IN (%s)", strings.Join(queryDatabases, ","))
		}
	}
	if groupBy {
		baseQuery += " GROUP BY datname"
	}

	return baseQuery + ";"
}

func tableKey(database, table string) tableIdentifier {
	return tableIdentifier(fmt.Sprintf("%s|%s", database, table))
}

func indexKey(database, table, index string) indexIdentifer {
	return indexIdentifer(fmt.Sprintf("%s|%s|%s", database, table, index))
}

func getStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func getInt64Value(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

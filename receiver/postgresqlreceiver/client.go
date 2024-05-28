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

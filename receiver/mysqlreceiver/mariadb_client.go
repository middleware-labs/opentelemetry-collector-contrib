package mysqlreceiver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/k0kubun/pp"
	parser "github.com/middleware-labs/innoParser/pkg/metricParser"
)

type mariaDBClient struct {
	connStr                        string
	client                         *sql.DB
	statementEventsDigestTextLimit int
	statementEventsLimit           int
	statementEventsTimeLimit       time.Duration
}

var _ client = (*mariaDBClient)(nil)

func newMariaDBClient(conf *Config) (client, error) {
	tls, err := conf.TLS.LoadTLSConfig(context.Background())
	if err != nil {
		return nil, err
	}
	tlsConfig := ""
	if tls != nil {
		err := mysql.RegisterTLSConfig("custom", tls)
		if err != nil {
			return nil, err
		}
		tlsConfig = "custom"
	}

	driverConf := mysql.Config{
		User:                 conf.Username,
		Passwd:               string(conf.Password),
		Net:                  string(conf.Transport),
		Addr:                 conf.Endpoint,
		DBName:               conf.Database,
		AllowNativePasswords: conf.AllowNativePasswords,
		TLS:                  tls,
		TLSConfig:            tlsConfig,
	}
	connStr := driverConf.FormatDSN()

	return &mariaDBClient{
		connStr:                        connStr,
		statementEventsDigestTextLimit: conf.StatementEvents.DigestTextLimit,
		statementEventsLimit:           conf.StatementEvents.Limit,
		statementEventsTimeLimit:       conf.StatementEvents.TimeLimit,
	}, nil
}

func (c *mariaDBClient) Connect() error {
	clientDB, err := sql.Open("mysql", c.connStr)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %w", err)
	}
	c.client = clientDB
	return nil
}

func (c *mariaDBClient) getVersion() (string, error) {
	pp.Println("GETTING VERSION--------------------")
	query := "SELECT VERSION();"
	var version string
	err := c.client.QueryRow(query).Scan(&version)
	if err != nil {
		return "", err
	}
	pp.Println(version)
	return version, nil
}

// getGlobalStats queries the db for global status metrics.
func (c *mariaDBClient) getGlobalStats() (map[string]string, error) {
	pp.Println("GETTING GLOBAL STATS --------------------")
	q := "SHOW GLOBAL STATUS;"
	return queryMariaDB(*c, q)
}

// getInnodbStats queries the db for innodb metrics.
func (c *mariaDBClient) getInnodbStats() (map[string]string, error) {
	pp.Println("GETTING INNODB STATS -------------------------------")
	q := "SELECT name, count FROM information_schema.innodb_metrics WHERE name LIKE '%buffer_pool_size%';"
	return queryMariaDB(*c, q)
}

func (c *mariaDBClient) getInnodbStatusStats() (map[string]int64, error, int) {
	pp.Println("GETTING INNODB STATUS STATS -------------------------------------")
	/*
		RETURNS:
			map[string]int64 :
				A map with metric names as the key and metric value as the
				value.
			error:
				Error encountered, there are two types of error here.
					1. Error that should cause panic:
						- Could not create the parser
						- error querying the mysql db for innodb status
					2. Errors that should not cause a panic:
						- Errors while parsing a metric. If one metric fails
						to get parsed causing an panic would stop other metrics from
						being recorded.
			int:
				The number metrics that are fail being parsed.
	*/
	innodbParser, err := parser.NewInnodbStatusParser()
	if err != nil {
		pp.Println(err)
		err := fmt.Errorf("could not create parser for innodb stats, %s", err)
		return nil, err, 0
	}
	var (
		typeVar string
		name    string
		status  string
	)

	query := "SHOW /*!50000 ENGINE*/ INNODB STATUS;"
	row := c.client.QueryRow(query)
	mysqlErr := row.Scan(&typeVar, &name, &status)

	// TODO: Suggest better value if there's an error for the metric.
	if mysqlErr != nil {

		pp.Println(err)
		err := fmt.Errorf("error querying the mysql db for innodb status %v", mysqlErr)
		return nil, err, 0
	}

	innodbParser.SetInnodbStatusFromString(status)
	//Some metrics fail to get parserd, then they are recorded into errs as a value with key as)
	//the metric name. We don't want to panic if there are a few errors but we do want to record them.
	metrics, errs := innodbParser.ParseStatus()

	total_errs := 0
	for key := range errs {
		if errs[key][0] != nil {
			total_errs += 1
		}
	}

	var parserErrs error
	parserErrs = nil
	if total_errs > 0 {

		errorString := flattenErrorMap(errs)
		parserErrs = fmt.Errorf(errorString)
	}

	return metrics, parserErrs, total_errs
}

func (c *mariaDBClient) getTotalRows() ([]NRows, error) {
	pp.Println("GETTING TOTAL ROWS -------------------------------------")
	query := `SELECT TABLE_SCHEMA AS DatabaseName, SUM(TABLE_ROWS) AS TotalRows
	FROM INFORMATION_SCHEMA.TABLES
	GROUP BY TABLE_SCHEMA;
	`

	rows, err := c.client.Query(query)

	if err != nil {
		pp.Println(err)
		return nil, err
	}

	defer rows.Close()
	var nr []NRows
	for rows.Next() {
		var r NRows
		err := rows.Scan(&r.dbname, &r.totalRows)
		if err != nil {

			pp.Println(err)
			return nil, err
		}
		nr = append(nr, r)
	}
	return nr, nil
}

// getTableStats queries the db for information_schema table size metrics.
func (c *mariaDBClient) getTableStats() ([]TableStats, error) {
	pp.Println("GETTING TABLE STATS--------------------------")
	query := "SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_ROWS, " +
		"AVG_ROW_LENGTH, DATA_LENGTH, INDEX_LENGTH " +
		"FROM information_schema.TABLES " +
		"WHERE TABLE_SCHEMA NOT in ('information_schema', 'sys');"
	rows, err := c.client.Query(query)
	if err != nil {

		pp.Println(err)
		return nil, err
	}
	defer rows.Close()
	var stats []TableStats
	for rows.Next() {
		var s TableStats
		err := rows.Scan(&s.schema, &s.name,
			&s.rows, &s.averageRowLength,
			&s.dataLength, &s.indexLength)
		if err != nil {

			pp.Println(err)
			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

// getTableIoWaitsStats queries the db for table_io_waits metrics.
func (c *mariaDBClient) getTableIoWaitsStats() ([]TableIoWaitsStats, error) {
	pp.Println("GETTING TABLE IO WAIT STATS  ---------------------------")
	query := "SELECT OBJECT_SCHEMA, OBJECT_NAME, " +
		"COUNT_DELETE, COUNT_FETCH, COUNT_INSERT, COUNT_UPDATE," +
		"SUM_TIMER_DELETE, SUM_TIMER_FETCH, SUM_TIMER_INSERT, SUM_TIMER_UPDATE " +
		"FROM performance_schema.table_io_waits_summary_by_table " +
		"WHERE OBJECT_SCHEMA NOT IN ('mysql', 'performance_schema');"
	rows, err := c.client.Query(query)
	if err != nil {

		pp.Println(err)
		return nil, err
	}
	defer rows.Close()
	var stats []TableIoWaitsStats
	for rows.Next() {
		var s TableIoWaitsStats
		err := rows.Scan(&s.schema, &s.name,
			&s.countDelete, &s.countFetch, &s.countInsert, &s.countUpdate,
			&s.timeDelete, &s.timeFetch, &s.timeInsert, &s.timeUpdate)
		if err != nil {

			pp.Println(err)
			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

// getIndexIoWaitsStats queries the db for index_io_waits metrics.
func (c *mariaDBClient) getIndexIoWaitsStats() ([]IndexIoWaitsStats, error) {
	pp.Println("INDEX IO WAITS STATS ------------------------------------------------")
	query := "SELECT OBJECT_SCHEMA, OBJECT_NAME, ifnull(INDEX_NAME, 'NONE') as INDEX_NAME," +
		"COUNT_FETCH, COUNT_INSERT, COUNT_UPDATE, COUNT_DELETE," +
		"SUM_TIMER_FETCH, SUM_TIMER_INSERT, SUM_TIMER_UPDATE, SUM_TIMER_DELETE " +
		"FROM performance_schema.table_io_waits_summary_by_index_usage " +
		"WHERE OBJECT_SCHEMA NOT IN ('mysql', 'performance_schema');"

	rows, err := c.client.Query(query)
	if err != nil {

		pp.Println(err)
		return nil, err
	}
	defer rows.Close()
	var stats []IndexIoWaitsStats
	for rows.Next() {
		var s IndexIoWaitsStats
		err := rows.Scan(&s.schema, &s.name, &s.index,
			&s.countDelete, &s.countFetch, &s.countInsert, &s.countUpdate,
			&s.timeDelete, &s.timeFetch, &s.timeInsert, &s.timeUpdate)
		if err != nil {

			pp.Println(err)
			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func (c *mariaDBClient) getStatementEventsStats() ([]StatementEventStats, error) {
	pp.Println("STATEMENT EVENT STATS -----------------------------------------------")
	query := fmt.Sprintf("SELECT ifnull(SCHEMA_NAME, 'NONE') as SCHEMA_NAME, DIGEST,"+
		"LEFT(DIGEST_TEXT, %d) as DIGEST_TEXT, SUM_TIMER_WAIT, SUM_ERRORS,"+
		"SUM_WARNINGS, SUM_ROWS_AFFECTED, SUM_ROWS_SENT, SUM_ROWS_EXAMINED,"+
		"SUM_CREATED_TMP_DISK_TABLES, SUM_CREATED_TMP_TABLES, SUM_SORT_MERGE_PASSES,"+
		"SUM_SORT_ROWS, SUM_NO_INDEX_USED , COUNT_STAR "+
		"FROM performance_schema.events_statements_summary_by_digest "+
		"WHERE SCHEMA_NAME NOT IN ('mysql', 'performance_schema', 'information_schema') "+
		"AND last_seen > DATE_SUB(NOW(), INTERVAL %d SECOND) "+
		"ORDER BY SUM_TIMER_WAIT DESC "+
		"LIMIT %d",
		c.statementEventsDigestTextLimit,
		int64(c.statementEventsTimeLimit.Seconds()),
		c.statementEventsLimit)

	pp.Println(query)

	rows, err := c.client.Query(query)
	if err != nil {
		pp.Println(err)
		fmt.Println(err.Error())
		return nil, err
	}
	defer rows.Close()

	var stats []StatementEventStats
	for rows.Next() {
		var s StatementEventStats
		err := rows.Scan(
			&s.schema,
			&s.digest,
			&s.digestText,
			&s.sumTimerWait,
			&s.countErrors,
			&s.countWarnings,
			&s.countRowsAffected,
			&s.countRowsSent,
			&s.countRowsExamined,
			&s.countCreatedTmpDiskTables,
			&s.countCreatedTmpTables,
			&s.countSortMergePasses,
			&s.countSortRows,
			&s.countNoIndexUsed,
			&s.countStar,
		)
		if err != nil {

			pp.Println(err)
			return nil, err
		}
		stats = append(stats, s)
	}
	pp.Print(stats)
	return stats, nil
}

func (c *mariaDBClient) getTotalErrors() (int64, error) {
	pp.Println("TOTAL ERRORS --------------------------------------------------")
	query := `SELECT SUM_ERRORS FROM performance_schema.events_statements_summary_by_digest;`

	rows, err := c.client.Query(query)
	if err != nil {

		pp.Println(err)
		return -1, err
	}

	var nerrors int64 = 0

	for rows.Next() {
		var ec int64

		err := rows.Scan(&ec)
		if err != nil {

			pp.Println(err)
			return -1, err
		}
		nerrors += ec
	}

	return nerrors, nil
}

func (c *mariaDBClient) getTableLockWaitEventStats() ([]tableLockWaitEventStats, error) {
	pp.Println("TABLE LOCK WAIT EVENT STATS --------------------------------------")
	query := "SELECT OBJECT_SCHEMA, OBJECT_NAME, COUNT_READ_NORMAL, COUNT_READ_WITH_SHARED_LOCKS," +
		"COUNT_READ_HIGH_PRIORITY, COUNT_READ_NO_INSERT, COUNT_READ_EXTERNAL, COUNT_WRITE_ALLOW_WRITE," +
		"COUNT_WRITE_CONCURRENT_INSERT, COUNT_WRITE_LOW_PRIORITY, COUNT_WRITE_NORMAL," +
		"COUNT_WRITE_EXTERNAL, SUM_TIMER_READ_NORMAL, SUM_TIMER_READ_WITH_SHARED_LOCKS," +
		"SUM_TIMER_READ_HIGH_PRIORITY, SUM_TIMER_READ_NO_INSERT, SUM_TIMER_READ_EXTERNAL," +
		"SUM_TIMER_WRITE_ALLOW_WRITE, SUM_TIMER_WRITE_CONCURRENT_INSERT, SUM_TIMER_WRITE_LOW_PRIORITY," +
		"SUM_TIMER_WRITE_NORMAL, SUM_TIMER_WRITE_EXTERNAL " +
		"FROM performance_schema.table_lock_waits_summary_by_table " +
		"WHERE OBJECT_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema')"

	rows, err := c.client.Query(query)
	if err != nil {

		pp.Println(err)
		return nil, err
	}
	defer rows.Close()

	var stats []tableLockWaitEventStats
	for rows.Next() {
		var s tableLockWaitEventStats
		err := rows.Scan(&s.schema, &s.name,
			&s.countReadNormal, &s.countReadWithSharedLocks, &s.countReadHighPriority, &s.countReadNoInsert, &s.countReadExternal,
			&s.countWriteAllowWrite, &s.countWriteConcurrentInsert, &s.countWriteLowPriority, &s.countWriteNormal, &s.countWriteExternal,
			&s.sumTimerReadNormal, &s.sumTimerReadWithSharedLocks, &s.sumTimerReadHighPriority, &s.sumTimerReadNoInsert, &s.sumTimerReadExternal,
			&s.sumTimerWriteAllowWrite, &s.sumTimerWriteConcurrentInsert, &s.sumTimerWriteLowPriority, &s.sumTimerWriteNormal, &s.sumTimerWriteExternal)
		if err != nil {

			pp.Println(err)
			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func (c *mariaDBClient) getReplicaStatusStats() ([]ReplicaStatusStats, error) {
	pp.Println("REPLICATION STATS ------------------------------------------")
	version, err := c.getVersion()
	if err != nil {

		pp.Println(err)
		return nil, err
	}

	if version < "8.0.22" {

		pp.Println(err)
		return nil, nil
	}

	query := "SHOW REPLICA STATUS"
	rows, err := c.client.Query(query)

	if err != nil {

		pp.Println(err)
		return nil, err
	}

	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {

		pp.Println(err)
		return nil, err
	}

	var stats []ReplicaStatusStats
	for rows.Next() {
		var s ReplicaStatusStats
		dest := []any{}
		for _, col := range cols {
			switch strings.ToLower(col) {
			case "replica_io_state":
				dest = append(dest, &s.replicaIOState)
			case "source_host":
				dest = append(dest, &s.sourceHost)
			case "source_user":
				dest = append(dest, &s.sourceUser)
			case "source_port":
				dest = append(dest, &s.sourcePort)
			case "connect_retry":
				dest = append(dest, &s.connectRetry)
			case "source_log_file":
				dest = append(dest, &s.sourceLogFile)
			case "read_source_log_pos":
				dest = append(dest, &s.readSourceLogPos)
			case "relay_log_file":
				dest = append(dest, &s.relayLogFile)
			case "relay_log_pos":
				dest = append(dest, &s.relayLogPos)
			case "relay_source_log_file":
				dest = append(dest, &s.relaySourceLogFile)
			case "replica_io_running":
				dest = append(dest, &s.replicaIORunning)
			case "replica_sql_running":
				dest = append(dest, &s.replicaSQLRunning)
			case "replicate_do_db":
				dest = append(dest, &s.replicateDoDB)
			case "replicate_ignore_db":
				dest = append(dest, &s.replicateIgnoreDB)
			case "replicate_do_table":
				dest = append(dest, &s.replicateDoTable)
			case "replicate_ignore_table":
				dest = append(dest, &s.replicateIgnoreTable)
			case "replicate_wild_do_table":
				dest = append(dest, &s.replicateWildDoTable)
			case "replicate_wild_ignore_table":
				dest = append(dest, &s.replicateWildIgnoreTable)
			case "last_errno":
				dest = append(dest, &s.lastErrno)
			case "last_error":
				dest = append(dest, &s.lastError)
			case "skip_counter":
				dest = append(dest, &s.skipCounter)
			case "exec_source_log_pos":
				dest = append(dest, &s.execSourceLogPos)
			case "relay_log_space":
				dest = append(dest, &s.relayLogSpace)
			case "until_condition":
				dest = append(dest, &s.untilCondition)
			case "until_log_file":
				dest = append(dest, &s.untilLogFile)
			case "until_log_pos":
				dest = append(dest, &s.untilLogPos)
			case "source_ssl_allowed":
				dest = append(dest, &s.sourceSSLAllowed)
			case "source_ssl_ca_file":
				dest = append(dest, &s.sourceSSLCAFile)
			case "source_ssl_ca_path":
				dest = append(dest, &s.sourceSSLCAPath)
			case "source_ssl_cert":
				dest = append(dest, &s.sourceSSLCert)
			case "source_ssl_cipher":
				dest = append(dest, &s.sourceSSLCipher)
			case "source_ssl_key":
				dest = append(dest, &s.sourceSSLKey)
			case "seconds_behind_source":
				dest = append(dest, &s.secondsBehindSource)
			case "source_ssl_verify_server_cert":
				dest = append(dest, &s.sourceSSLVerifyServerCert)
			case "last_io_errno":
				dest = append(dest, &s.lastIOErrno)
			case "last_io_error":
				dest = append(dest, &s.lastIOError)
			case "last_sql_errno":
				dest = append(dest, &s.lastSQLErrno)
			case "last_sql_error":
				dest = append(dest, &s.lastSQLError)
			case "replicate_ignore_server_ids":
				dest = append(dest, &s.replicateIgnoreServerIDs)
			case "source_server_id":
				dest = append(dest, &s.sourceServerID)
			case "source_uuid":
				dest = append(dest, &s.sourceUUID)
			case "source_info_file":
				dest = append(dest, &s.sourceInfoFile)
			case "sql_delay":
				dest = append(dest, &s.sqlDelay)
			case "sql_remaining_delay":
				dest = append(dest, &s.sqlRemainingDelay)
			case "replica_sql_running_state":
				dest = append(dest, &s.replicaSQLRunningState)
			case "source_retry_count":
				dest = append(dest, &s.sourceRetryCount)
			case "source_bind":
				dest = append(dest, &s.sourceBind)
			case "last_io_error_timestamp":
				dest = append(dest, &s.lastIOErrorTimestamp)
			case "last_sql_error_timestamp":
				dest = append(dest, &s.lastSQLErrorTimestamp)
			case "source_ssl_crl":
				dest = append(dest, &s.sourceSSLCrl)
			case "source_ssl_crlpath":
				dest = append(dest, &s.sourceSSLCrlpath)
			case "retrieved_gtid_set":
				dest = append(dest, &s.retrievedGtidSet)
			case "executed_gtid_set":
				dest = append(dest, &s.executedGtidSet)
			case "auto_position":
				dest = append(dest, &s.autoPosition)
			case "replicate_rewrite_db":
				dest = append(dest, &s.replicateRewriteDB)
			case "channel_name":
				dest = append(dest, &s.channelName)
			case "source_tls_version":
				dest = append(dest, &s.sourceTLSVersion)
			case "source_public_key_path":
				dest = append(dest, &s.sourcePublicKeyPath)
			case "get_source_public_key":
				dest = append(dest, &s.getSourcePublicKey)
			case "network_namespace":
				dest = append(dest, &s.networkNamespace)
			default:
				return nil, fmt.Errorf("unknown column name %s for replica status", col)
			}
		}
		err := rows.Scan(dest...)

		if err != nil {

			pp.Println(err)
			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func queryMariaDB(c mariaDBClient, query string) (map[string]string, error) {
	rows, err := c.client.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := map[string]string{}
	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			return nil, err
		}
		stats[key] = val
	}
	pp.Println(stats)
	return stats, nil
}

func (c *mariaDBClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package mysqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver"

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	// registers the mysql driver
	"github.com/go-sql-driver/mysql"
	"github.com/hashicorp/go-version"
	parser "github.com/middleware-labs/innoParser/pkg/metricParser"
)

type client interface {
	Connect() error
	getVersion() (*version.Version, error)
	getGlobalStats() (map[string]string, error)
	getInnodbStats() (map[string]string, error)
	getTableStats() ([]TableStats, error)
	getTableIoWaitsStats() ([]TableIoWaitsStats, error)
	getIndexIoWaitsStats() ([]IndexIoWaitsStats, error)
	getStatementEventsStats() ([]StatementEventStats, error)
	getTableLockWaitEventStats() ([]tableLockWaitEventStats, error)
	getReplicaStatusStats() ([]ReplicaStatusStats, error)
	getInnodbStatusStats() (map[string]int64, error, int)
	getTotalRows() ([]NRows, error)
	getTotalErrors() (int64, error)
	getRowOperationStats() (RowOperationStats, error)
	getActiveConnections() (int64, error)
	Close() error
}

type mySQLClient struct {
	connStr                        string
	client                         *sql.DB
	statementEventsDigestTextLimit int
	statementEventsLimit           int
	statementEventsTimeLimit       time.Duration
}

type RowOperationStats struct {
	rowsRead     int64
	rowsUpdated  int64
	rowsDeleted  int64
	rowsInserted int64
}
type IoWaitsStats struct {
	schema      string
	name        string
	countDelete int64
	countFetch  int64
	countInsert int64
	countUpdate int64
	timeDelete  int64
	timeFetch   int64
	timeInsert  int64
	timeUpdate  int64
}

type TableIoWaitsStats struct {
	IoWaitsStats
}

type IndexIoWaitsStats struct {
	IoWaitsStats
	index string
}

type TableStats struct {
	schema           string
	name             string
	rows             sql.NullInt64
	averageRowLength sql.NullInt64
	dataLength       sql.NullInt64
	indexLength      sql.NullInt64
}

type StatementEventStats struct {
	schema                    string
	digest                    string
	digestText                string
	sumTimerWait              int64
	countErrors               int64
	countWarnings             int64
	countRowsAffected         int64
	countRowsSent             int64
	countRowsExamined         int64
	countCreatedTmpDiskTables int64
	countCreatedTmpTables     int64
	countSortMergePasses      int64
	countSortRows             int64
	countNoIndexUsed          int64
	countStar                 int64
}

type tableLockWaitEventStats struct {
	schema                        string
	name                          string
	countReadNormal               int64
	countReadWithSharedLocks      int64
	countReadHighPriority         int64
	countReadNoInsert             int64
	countReadExternal             int64
	countWriteAllowWrite          int64
	countWriteConcurrentInsert    int64
	countWriteLowPriority         int64
	countWriteNormal              int64
	countWriteExternal            int64
	sumTimerReadNormal            int64
	sumTimerReadWithSharedLocks   int64
	sumTimerReadHighPriority      int64
	sumTimerReadNoInsert          int64
	sumTimerReadExternal          int64
	sumTimerWriteAllowWrite       int64
	sumTimerWriteConcurrentInsert int64
	sumTimerWriteLowPriority      int64
	sumTimerWriteNormal           int64
	sumTimerWriteExternal         int64
}

type ReplicaStatusStats struct {
	replicaIOState              string
	sourceHost                  string
	sourceUser                  string
	sourcePort                  int64
	connectRetry                int64
	sourceLogFile               string
	readSourceLogPos            int64
	relayLogFile                string
	relayLogPos                 int64
	relaySourceLogFile          string
	replicaIORunning            string
	replicaSQLRunning           string
	replicateDoDB               string
	replicateIgnoreDB           string
	replicateDoTable            string
	replicateIgnoreTable        string
	replicateWildDoTable        string
	replicateWildIgnoreTable    string
	lastErrno                   int64
	lastError                   string
	skipCounter                 int64
	execSourceLogPos            int64
	relayLogSpace               int64
	untilCondition              string
	untilLogFile                string
	untilLogPos                 string
	sourceSSLAllowed            string
	sourceSSLCAFile             string
	sourceSSLCAPath             string
	sourceSSLCert               string
	sourceSSLCipher             string
	sourceSSLKey                string
	secondsBehindSource         sql.NullInt64
	sourceSSLVerifyServerCert   string
	lastIOErrno                 int64
	lastIOError                 string
	lastSQLErrno                int64
	lastSQLError                string
	replicateIgnoreServerIDs    string
	sourceServerID              int64
	sourceUUID                  string
	sourceInfoFile              string
	sqlDelay                    int64
	sqlRemainingDelay           sql.NullInt64
	replicaSQLRunningState      string
	sourceRetryCount            int64
	sourceBind                  string
	lastIOErrorTimestamp        string
	lastSQLErrorTimestamp       string
	sourceSSLCrl                string
	sourceSSLCrlpath            string
	retrievedGtidSet            string
	executedGtidSet             string
	autoPosition                string
	replicateRewriteDB          string
	channelName                 string
	sourceTLSVersion            string
	sourcePublicKeyPath         string
	getSourcePublicKey          int64
	networkNamespace            string
	usingGtid                   string
	gtidIoPos                   string
	slaveDdlGroups              int64
	slaveNonTransactionalGroups int64
	slaveTransactionalGroups    int64
	retriedTransactions         int64
	maxRelayLogSize             int64
	executedLogEntries          int64
	slaveReceivedHeartbeats     int64
	slaveHeartbeatPeriod        int64
	gtidSlavePos                string
	masterLastEventTime         string
	slaveLastEventTime          string
	masterSlaveTimeDiff         string
	parallelMode                string
	replicateDoDomainIDs        string
	replicateIgnoreDomainIDs    string
}

var _ client = (*mySQLClient)(nil)

func newMySQLClient(conf *Config) (client, error) {
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

	return &mySQLClient{
		connStr:                        connStr,
		statementEventsDigestTextLimit: conf.StatementEvents.DigestTextLimit,
		statementEventsLimit:           conf.StatementEvents.Limit,
		statementEventsTimeLimit:       conf.StatementEvents.TimeLimit,
	}, nil
}

func (c *mySQLClient) Connect() error {
	clientDB, err := sql.Open("mysql", c.connStr)
	if err != nil {
		return fmt.Errorf("unable to connect to database: %w", err)
	}
	c.client = clientDB
	return nil
}

// getVersion queries the db for the version.
func (c *mySQLClient) getVersion() (*version.Version, error) {
	query := "SELECT VERSION();"
	var versionStr string
	err := c.client.QueryRow(query).Scan(&versionStr)
	if err != nil {
		return nil, err
	}
	version, err := version.NewVersion(versionStr)
	return version, err
}

func (c *mySQLClient) getRowOperationStats() (RowOperationStats, error) {
	// TODO: Improve this logic for complex queries. Cases where INSERT/UPDATE/READ/DELETES are a part of a sub-operation.
	query := "SELECT SUBSTRING_INDEX(DIGEST_TEXT, ' ', 1) AS statement_type, " +
		"SUM(SUM_ROWS_AFFECTED) AS rows_affected, " +
		"SUM(SUM_ROWS_SENT) AS rows_sent " +
		"FROM performance_schema.events_statements_summary_by_digest " +
		"WHERE DIGEST_TEXT LIKE 'SELECT% '" +
		"OR DIGEST_TEXT LIKE 'INSERT%' " +
		"OR DIGEST_TEXT LIKE 'UPDATE%' " +
		"OR DIGEST_TEXT LIKE 'DELETE%' " +
		"GROUP BY statement_type; "

	rows, err := c.client.Query(query)
	rowOpsStats := new(RowOperationStats)

	if err != nil {
		return *rowOpsStats, err
	}

	defer rows.Close()

	for rows.Next() {
		var rowsAffected int64
		var rowsSent int64
		var statementType string
		err := rows.Scan(&statementType, &rowsAffected, &rowsSent)

		if err != nil {
			return *rowOpsStats, err
		}

		if statementType == "SELECT" {
			rowOpsStats.rowsRead = rowsSent
		} else if statementType == "UPDATE" {
			rowOpsStats.rowsUpdated = rowsAffected
		} else if statementType == "DELETE" {
			rowOpsStats.rowsDeleted = rowsAffected
		} else if statementType == "INSERT" {
			rowOpsStats.rowsInserted = rowsAffected
		}
	}
	return *rowOpsStats, nil
}

// getGlobalStats queries the db for global status metrics.
func (c *mySQLClient) getGlobalStats() (map[string]string, error) {
	q := "SHOW GLOBAL STATUS;"
	return query(*c, q)
}

// getInnodbStats queries the db for innodb metrics.
func (c *mySQLClient) getInnodbStats() (map[string]string, error) {
	q := "SELECT name, count FROM information_schema.innodb_metrics WHERE name LIKE '%buffer_pool_size%';"
	return query(*c, q)
}

func (c *mySQLClient) getInnodbStatusStats() (map[string]int64, error, int) {
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

type NRows struct {
	dbname    string
	totalRows sql.NullInt64
}

func (c *mySQLClient) getTotalRows() ([]NRows, error) {
	query := `SELECT TABLE_SCHEMA AS DatabaseName, SUM(TABLE_ROWS) AS TotalRows
	FROM INFORMATION_SCHEMA.TABLES
	GROUP BY TABLE_SCHEMA;
	`

	rows, err := c.client.Query(query)

	if err != nil {
		return nil, err
	}

	defer rows.Close()
	var nr []NRows
	for rows.Next() {
		var r NRows
		err := rows.Scan(&r.dbname, &r.totalRows)
		if err != nil {

			return nil, err
		}
		nr = append(nr, r)
	}
	return nr, nil
}

// getTableStats queries the db for information_schema table size metrics.
func (c *mySQLClient) getTableStats() ([]TableStats, error) {
	query := "SELECT TABLE_SCHEMA, TABLE_NAME, " +
		"COALESCE(TABLE_ROWS, 0) as TABLE_ROWS, " +
		"COALESCE(AVG_ROW_LENGTH, 0) as AVG_ROW_LENGTH, " +
		"COALESCE(DATA_LENGTH, 0) as DATA_LENGTH, " +
		"COALESCE(INDEX_LENGTH, 0) as  INDEX_LENGTH " +
		"FROM information_schema.TABLES " +
		"WHERE TABLE_SCHEMA NOT in ('information_schema', 'sys');"
	rows, err := c.client.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []TableStats
	for rows.Next() {
		var s TableStats
		err := rows.Scan(
			&s.schema,
			&s.name,
			&s.rows,
			&s.averageRowLength,
			&s.dataLength,
			&s.indexLength,
		)
		if err != nil {

			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

// getTableIoWaitsStats queries the db for table_io_waits metrics.
func (c *mySQLClient) getTableIoWaitsStats() ([]TableIoWaitsStats, error) {
	query := "SELECT OBJECT_SCHEMA, OBJECT_NAME, " +
		"COUNT_DELETE, COUNT_FETCH, COUNT_INSERT, COUNT_UPDATE," +
		"SUM_TIMER_DELETE, SUM_TIMER_FETCH, SUM_TIMER_INSERT, SUM_TIMER_UPDATE " +
		"FROM performance_schema.table_io_waits_summary_by_table " +
		"WHERE OBJECT_SCHEMA NOT IN ('mysql', 'performance_schema');"
	rows, err := c.client.Query(query)
	if err != nil {

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

			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

// getIndexIoWaitsStats queries the db for index_io_waits metrics.
func (c *mySQLClient) getIndexIoWaitsStats() ([]IndexIoWaitsStats, error) {
	query := "SELECT OBJECT_SCHEMA, OBJECT_NAME, ifnull(INDEX_NAME, 'NONE') as INDEX_NAME," +
		"COUNT_FETCH, COUNT_INSERT, COUNT_UPDATE, COUNT_DELETE," +
		"SUM_TIMER_FETCH, SUM_TIMER_INSERT, SUM_TIMER_UPDATE, SUM_TIMER_DELETE " +
		"FROM performance_schema.table_io_waits_summary_by_index_usage " +
		"WHERE OBJECT_SCHEMA NOT IN ('mysql', 'performance_schema');"

	rows, err := c.client.Query(query)
	if err != nil {

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

			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func (c *mySQLClient) getStatementEventsStats() ([]StatementEventStats, error) {
	query := fmt.Sprintf(`
	SELECT
		IFNULL(SCHEMA_NAME, 'NONE') AS SCHEMA_NAME,
		DIGEST,
		LEFT(DIGEST_TEXT, %d) AS DIGEST_TEXT,
		SUM_TIMER_WAIT,
		SUM_ERRORS,
		SUM_WARNINGS,
		SUM_ROWS_AFFECTED,
		SUM_ROWS_SENT,
		SUM_ROWS_EXAMINED,
		SUM_CREATED_TMP_DISK_TABLES,
		SUM_CREATED_TMP_TABLES,
		SUM_SORT_MERGE_PASSES,
		SUM_SORT_ROWS,
		SUM_NO_INDEX_USED,
		COUNT_STAR
	FROM
		performance_schema.events_statements_summary_by_digest
	WHERE
		SCHEMA_NAME NOT IN ('performance_schema', 'information_schema')
		AND last_seen > DATE_SUB(NOW(), INTERVAL %d SECOND)
	ORDER BY
		SUM_TIMER_WAIT DESC
	LIMIT %d;
	`, c.statementEventsDigestTextLimit, int64(c.statementEventsTimeLimit.Seconds()), c.statementEventsLimit)

	rows, err := c.client.Query(query)
	if err != nil {
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

			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, nil
}

func (c *mySQLClient) getTotalErrors() (int64, error) {
	query := `SELECT SUM_ERRORS FROM performance_schema.events_statements_summary_by_digest;`

	rows, err := c.client.Query(query)
	if err != nil {

		return -1, err
	}

	var nerrors int64 = 0

	for rows.Next() {
		var ec int64

		err := rows.Scan(&ec)
		if err != nil {

			return -1, err
		}
		nerrors += ec
	}

	return nerrors, nil
}

func (c *mySQLClient) getActiveConnections() (int64, error) {
	query := "SHOW STATUS WHERE `variable_name` = 'Threads_connected'"

	var varName string
	var value string

	err := c.client.QueryRow(query).Scan(&varName, &value)
	if err != nil {
		return -1, fmt.Errorf("failed to scan active connections: %w", err)
	}

	connections, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return -1, fmt.Errorf("failed to parse active connections count: %w", err)
	}

	return connections, nil
}

func (c *mySQLClient) getTableLockWaitEventStats() ([]tableLockWaitEventStats, error) {
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

			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func (c *mySQLClient) getReplicaStatusStats() ([]ReplicaStatusStats, error) {
	mysqlVersion, err := c.getVersion()
	if err != nil {

		return nil, err
	}

	query := "SHOW REPLICA STATUS"
	minMysqlVersion, _ := version.NewVersion("8.0.22")
	if strings.Contains(mysqlVersion.String(), "MariaDB") {
		query = "SHOW SLAVE STATUS"
	} else if mysqlVersion.LessThan(minMysqlVersion) {
		query = "SHOW SLAVE STATUS"
	}

	rows, err := c.client.Query(query)
	if err != nil {

		return nil, err
	}

	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {

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
			case "slave_io_state":
				dest = append(dest, &s.replicaIOState)
			case "source_host":
				dest = append(dest, &s.sourceHost)
			case "master_host":
				dest = append(dest, &s.sourceHost)
			case "source_user":
				dest = append(dest, &s.sourceUser)
			case "master_user":
				dest = append(dest, &s.sourceUser)
			case "source_port":
				dest = append(dest, &s.sourcePort)
			case "master_port":
				dest = append(dest, &s.sourcePort)
			case "connect_retry":
				dest = append(dest, &s.connectRetry)
			case "source_log_file":
				dest = append(dest, &s.sourceLogFile)
			case "master_log_file":
				dest = append(dest, &s.sourceLogFile)
			case "read_source_log_pos":
				dest = append(dest, &s.readSourceLogPos)
			case "read_master_log_pos":
				dest = append(dest, &s.readSourceLogPos)
			case "relay_log_file":
				dest = append(dest, &s.relayLogFile)
			case "relay_log_pos":
				dest = append(dest, &s.relayLogPos)
			case "relay_source_log_file":
				dest = append(dest, &s.relaySourceLogFile)
			case "relay_master_log_file":
				dest = append(dest, &s.relaySourceLogFile)
			case "replica_io_running":
				dest = append(dest, &s.replicaIORunning)
			case "slave_io_running":
				dest = append(dest, &s.replicaIORunning)
			case "replica_sql_running":
				dest = append(dest, &s.replicaSQLRunning)
			case "slave_sql_running":
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
			case "exec_master_log_pos":
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
			case "master_ssl_allowed":
				dest = append(dest, &s.sourceSSLAllowed)
			case "source_ssl_ca_file":
				dest = append(dest, &s.sourceSSLCAFile)
			case "master_ssl_ca_file":
				dest = append(dest, &s.sourceSSLCAFile)
			case "source_ssl_ca_path":
				dest = append(dest, &s.sourceSSLCAPath)
			case "master_ssl_ca_path":
				dest = append(dest, &s.sourceSSLCAPath)
			case "source_ssl_cert":
				dest = append(dest, &s.sourceSSLCert)
			case "master_ssl_cert":
				dest = append(dest, &s.sourceSSLCert)
			case "source_ssl_cipher":
				dest = append(dest, &s.sourceSSLCipher)
			case "master_ssl_cipher":
				dest = append(dest, &s.sourceSSLCipher)
			case "source_ssl_key":
				dest = append(dest, &s.sourceSSLKey)
			case "master_ssl_key":
				dest = append(dest, &s.sourceSSLKey)
			case "seconds_behind_source":
				dest = append(dest, &s.secondsBehindSource)
			case "seconds_behind_master":
				dest = append(dest, &s.secondsBehindSource)
			case "source_ssl_verify_server_cert":
				dest = append(dest, &s.sourceSSLVerifyServerCert)
			case "master_ssl_verify_server_cert":
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
			case "master_server_id":
				dest = append(dest, &s.sourceServerID)
			case "source_uuid":
				dest = append(dest, &s.sourceUUID)
			case "master_uuid":
				dest = append(dest, &s.sourceUUID)
			case "source_info_file":
				dest = append(dest, &s.sourceInfoFile)
			case "master_info_file":
				dest = append(dest, &s.sourceInfoFile)
			case "sql_delay":
				dest = append(dest, &s.sqlDelay)
			case "sql_remaining_delay":
				dest = append(dest, &s.sqlRemainingDelay)
			case "replica_sql_running_state":
				dest = append(dest, &s.replicaSQLRunningState)
			case "slave_sql_running_state":
				dest = append(dest, &s.replicaSQLRunningState)
			case "source_retry_count":
				dest = append(dest, &s.sourceRetryCount)
			case "master_retry_count":
				dest = append(dest, &s.sourceRetryCount)
			case "source_bind":
				dest = append(dest, &s.sourceBind)
			case "master_bind":
				dest = append(dest, &s.sourceBind)
			case "last_io_error_timestamp":
				dest = append(dest, &s.lastIOErrorTimestamp)
			case "last_sql_error_timestamp":
				dest = append(dest, &s.lastSQLErrorTimestamp)
			case "source_ssl_crl":
				dest = append(dest, &s.sourceSSLCrl)
			case "master_ssl_crl":
				dest = append(dest, &s.sourceSSLCrl)
			case "source_ssl_crlpath":
				dest = append(dest, &s.sourceSSLCrlpath)
			case "master_ssl_crlpath":
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
			case "master_tls_version":
				dest = append(dest, &s.sourceTLSVersion)
			case "source_public_key_path":
				dest = append(dest, &s.sourcePublicKeyPath)
			case "master_public_key_path":
				dest = append(dest, &s.sourcePublicKeyPath)
			case "get_source_public_key":
				dest = append(dest, &s.getSourcePublicKey)
			case "get_master_public_key":
				dest = append(dest, &s.getSourcePublicKey)
			case "network_namespace":
				dest = append(dest, &s.networkNamespace)
			case "using_gtid":
				dest = append(dest, &s.usingGtid)
			case "gtid_io_pos":
				dest = append(dest, &s.gtidIoPos)
			case "slave_ddl_groups":
				dest = append(dest, &s.slaveDdlGroups)
			case "slave_non_transactional_groups":
				dest = append(dest, &s.slaveNonTransactionalGroups)
			case "slave_transactional_groups":
				dest = append(dest, &s.slaveTransactionalGroups)
			case "retried_transactions":
				dest = append(dest, &s.retriedTransactions)
			case "max_relay_log_size":
				dest = append(dest, &s.maxRelayLogSize)
			case "executed_log_entries":
				dest = append(dest, &s.executedLogEntries)
			case "slave_received_heartbeats":
				dest = append(dest, &s.slaveReceivedHeartbeats)
			case "slave_heartbeat_period":
				dest = append(dest, &s.slaveHeartbeatPeriod)
			case "gtid_slave_pos":
				dest = append(dest, &s.gtidSlavePos)
			case "master_last_event_time":
				dest = append(dest, &s.masterLastEventTime)
			case "slave_last_event_time":
				dest = append(dest, &s.slaveLastEventTime)
			case "master_slave_time_diff":
				dest = append(dest, &s.masterSlaveTimeDiff)
			case "parallel_mode":
				dest = append(dest, &s.parallelMode)
			case "replicate_do_domain_ids":
				dest = append(dest, &s.replicateDoDomainIDs)
			case "replicate_ignore_domain_ids":
				dest = append(dest, &s.replicateIgnoreDomainIDs)
			default:
				return nil, fmt.Errorf("unknown column name %s for replica status", col)
			}
		}
		err := rows.Scan(dest...)
		if err != nil {

			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func query(c mySQLClient, query string) (map[string]string, error) {
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

	return stats, nil
}

func (c *mySQLClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func flattenErrorMap(errs map[string][]error) string {
	var errorMessages []string
	for key, errors := range errs {
		for _, err := range errors {
			errorMessage := fmt.Sprintf("%s: %s", key, err.Error())
			errorMessages = append(errorMessages, errorMessage)
		}
	}
	result := strings.Join(errorMessages, "\n")
	return result
}

// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

const (
	callsColumnName = "calls"

	dbAttributePrefix           = "postgresql."
	queryidColumnName           = "queryid"
	rowsColumnName              = "rows"
	sharedBlksDirtiedColumnName = "shared_blks_dirtied"
	sharedBlksHitColumnName     = "shared_blks_hit"
	sharedBlksReadColumnName    = "shared_blks_read"
	sharedBlksWrittenColumnName = "shared_blks_written"
	tempBlksReadColumnName      = "temp_blks_read"
	tempBlksWrittenColumnName   = "temp_blks_written"
	totalExecTimeColumnName     = "total_exec_time"
	totalPlanTimeColumnName     = "total_plan_time"
	blkReadTimeAttributeName    = "blk_read_time"
	blkWriteTimeAttributeName   = "blk_write_time"
)

const (
	querySampleColumnApplicationName      = "application_name"
	querySampleColumnBackendType          = "backend_type"
	querySampleColumnBackendXid           = "backend_xid"
	querySampleColumnBlockingPids         = "blocking_pids"
	querySampleColumnClientAddr           = "client_addr"
	querySampleColumnClientHostname       = "client_hostname"
	querySampleColumnClientPort           = "client_port"
	querySampleColumnDatname              = "datname"
	querySampleColumnDurationMilliseconds = "duration_ms"
	querySampleColumnPID                  = "pid"
	querySampleColumnQuery                = "query"
	querySampleColumnQueryID              = "query_id"
	querySampleColumnQueryStart           = "query_start"
	querySampleColumnQueryStartTimestamp  = "_query_start_timestamp"
	querySampleColumnState                = "state"
	querySampleColumnStateChange          = "state_change"
	querySampleColumnUsename              = "usename"
	querySampleColumnWaitEvent            = "wait_event"
	querySampleColumnWaitEventType        = "wait_event_type"
	querySampleColumnXactStart            = "xact_start"
)

const (
	insufficientPrivilegeQuerySampleText = "<insufficient privilege>"
	traceparentCarrierKey                = "traceparent"

	// unknownDatabaseName stands in for db.namespace when a pg_stat_statements
	// row cannot be resolved to a database. This happens when the row's dbid
	// refers to a dropped database, so the join to pg_database yields NULL.
	// Emitting the row with a placeholder keeps the query visible; dropping it
	// would silently lose top queries for whichever database is churning most.
	unknownDatabaseName = "unknown"
)

const (
	postgresqlTotalExecTimeAttributeName = dbAttributePrefix + totalExecTimeColumnName
	postgresqlBlkReadTimeAttributeName   = dbAttributePrefix + blkReadTimeAttributeName
	postgresqlBlkWriteTimeAttributeName  = dbAttributePrefix + blkWriteTimeAttributeName
)

// monitoringApplicationName is set as application_name on every connection this
// receiver opens. It appears in pg_stat_activity and in the server log, so the
// customer's DBA can always tell which backends belong to the collector.
const monitoringApplicationName = "mw-otel-collector"

// rolnameColumnName is the role that executed the query, joined from pg_roles
// on pg_stat_statements.userid. Part of the identity of a pg_stat_statements
// entry, so part of the delta cache key.
const rolnameColumnName = "rolname"

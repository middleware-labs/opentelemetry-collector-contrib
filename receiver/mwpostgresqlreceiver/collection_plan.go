// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"

// collectionPlan records, once at construction, which SQL a scrape actually
// needs. Every field is derived from `metrics:` enablement alone; the receiver
// gains no new configuration surface from this type. The `metrics:` block
// already says what the user wants, and the point of the plan is to make the
// receiver honor it at the source rather than collect rows, build resources
// and then discard the data points inside the metrics builder.
//
// A family is collected when at least one metric that consumes its rows is
// enabled. Where one query feeds several metrics, any one of them keeps the
// query. Where a query's rows are needed for something other than a data point
// — table enumeration feeds postgresql.table.count as a row count — that use is
// named explicitly so the query is not dropped with the detail metrics.
type collectionPlan struct {
	// tableDetails covers getDatabaseTableMetrics' per-table data points. Note
	// that this is *not* what decides whether the query runs: see tables.
	tableDetails bool

	// tables decides whether getDatabaseTableMetrics runs at all. It stays true
	// when only postgresql.table.count is enabled, because that metric is
	// len(tableMetrics) — the row count of this very query — so enumeration must
	// continue even when every per-table metric is off.
	tables bool

	// blocksReadByTable covers getBlocksReadByTable, whose rows feed only
	// postgresql.blocks_read. postgresql.toast.blocks_hit and
	// postgresql.toast.index.blocks_read are declared in metadata.yaml but never
	// recorded anywhere in the receiver, so they do not keep this query alive.
	blocksReadByTable bool

	// indexes covers getIndexStats.
	indexes bool

	// functions covers getFunctionStats. Disabled by default, and it emits one
	// resource per function per scrape when it runs.
	functions bool

	// tableBloat and indexBloat cover the two most expensive per-database
	// queries in the receiver.
	tableBloat bool
	indexBloat bool

	// databaseLocks covers getDatabaseLocks. Disabled by default.
	databaseLocks bool

	// queryPerf covers getQueryStats plus its getQueryStatsMax sizing probe and
	// its getQueryTexts follow-up lookups.
	queryPerf bool
	// deallocations covers postgresql.query.deallocations, read from
	// pg_stat_statements_info on the maintenance connection.
	deallocations bool

	// rowStats covers getRowStats.
	rowStats bool

	// bufferHit covers getBufferHit.
	bufferHit bool

	// databaseStats covers getDatabaseStats, databaseSize covers
	// getDatabaseSize and backends covers getBackends. These three run
	// concurrently on the maintenance connection before the per-database loop.
	databaseStats bool
	databaseSize  bool
	backends      bool

	// walStats, transactions, bgWriter, maxConnections, activeConnections,
	// walAge and replication cover the server-wide collectors.
	walStats          bool
	transactions      bool
	bgWriter          bool
	maxConnections    bool
	activeConnections bool
	walAge            bool
	replication       bool
}

// newCollectionPlan maps each query family to every metric that consumes its
// results. Adding a metric that reads an existing query means adding it to the
// disjunction here; forgetting to do so makes the metric silently empty, which
// is why each family lists its consumers exhaustively rather than sampling one.
func newCollectionPlan(m metadata.MetricsConfig) collectionPlan {
	p := collectionPlan{
		tableDetails: m.PostgresqlRows.Enabled ||
			m.PostgresqlOperations.Enabled ||
			m.PostgresqlTableSize.Enabled ||
			m.PostgresqlTableVacuumCount.Enabled ||
			m.PostgresqlAutovacuumed.Enabled ||
			m.PostgresqlAnalyzed.Enabled ||
			m.PostgresqlAutoanalyzed.Enabled ||
			m.PostgresqlSequentialScans.Enabled ||
			m.PostgresqlToastSize.Enabled,

		blocksReadByTable: m.PostgresqlBlocksRead.Enabled,

		indexes: m.PostgresqlIndexScans.Enabled ||
			m.PostgresqlIndexSize.Enabled ||
			m.PostgresqlIndexRowsRead.Enabled ||
			m.PostgresqlIndexBlocksRead.Enabled,

		functions: m.PostgresqlFunctionCalls.Enabled,

		tableBloat: m.PostgresqlTableBloat.Enabled,
		indexBloat: m.PostgresqlIndexBloat.Enabled,

		databaseLocks: m.PostgresqlDatabaseLocks.Enabled,

		deallocations: m.PostgresqlQueryDeallocations.Enabled,

		queryPerf: m.PostgresqlQueryCount.Enabled ||
			m.PostgresqlQueryTotalExecTime.Enabled,

		rowStats: m.PostgresqlRowsFetched.Enabled ||
			m.PostgresqlRowsInserted.Enabled ||
			m.PostgresqlRowsUpdated.Enabled ||
			m.PostgresqlRowsDeleted.Enabled ||
			m.PostgresqlLiveRows.Enabled,

		bufferHit: m.PostgresqlBufferHit.Enabled,

		databaseStats: m.PostgresqlCommits.Enabled ||
			m.PostgresqlRollbacks.Enabled ||
			m.PostgresqlDeadlocks.Enabled ||
			m.PostgresqlTempFiles.Enabled ||
			m.PostgresqlTempIo.Enabled ||
			m.PostgresqlTupUpdated.Enabled ||
			m.PostgresqlTupReturned.Enabled ||
			m.PostgresqlTupFetched.Enabled ||
			m.PostgresqlTupInserted.Enabled ||
			m.PostgresqlTupDeleted.Enabled ||
			m.PostgresqlBlksHit.Enabled ||
			m.PostgresqlBlksRead.Enabled ||
			m.PostgresqlBlkReadTime.Enabled ||
			m.PostgresqlBlkWriteTime.Enabled,

		databaseSize: m.PostgresqlDbSize.Enabled,
		backends:     m.PostgresqlBackends.Enabled,

		walStats: m.PostgresqlWalCount.Enabled || m.PostgresqlWalSize.Enabled,

		transactions: m.PostgresqlTransactionsDurationMax.Enabled ||
			m.PostgresqlTransactionsDurationSum.Enabled,

		bgWriter: m.PostgresqlBgwriterBuffersAllocated.Enabled ||
			m.PostgresqlBgwriterBuffersWrites.Enabled ||
			m.PostgresqlBgwriterCheckpointCount.Enabled ||
			m.PostgresqlBgwriterDuration.Enabled ||
			m.PostgresqlBgwriterMaxwritten.Enabled,

		maxConnections:    m.PostgresqlConnectionMax.Enabled,
		activeConnections: m.PostgresqlConnectionCount.Enabled,
		walAge:            m.PostgresqlWalAge.Enabled,

		replication: m.PostgresqlReplicationDataDelay.Enabled ||
			m.PostgresqlWalDelay.Enabled ||
			m.PostgresqlWalLag.Enabled,
	}

	// postgresql.table.count is the row count of the table query, so it keeps
	// enumeration alive on its own even with every per-table metric disabled.
	// postgresql.blocks_read is emitted from inside the table loop, joined to
	// each enumerated table, so it too requires enumeration.
	p.tables = p.tableDetails || p.blocksReadByTable || m.PostgresqlTableCount.Enabled

	return p
}

// needsDatabaseClient reports whether anything in the per-database loop still
// has work to do. When nothing does, the scrape skips opening a connection to
// each selected database entirely, which is the largest saving available to a
// configuration that wants only server-wide metrics.
//
// recordDatabase still needs to run for the databases in scope whenever any
// per-database data point can be produced from the maintenance-connection
// queries (backends, size, database stats) or from the table count, so this
// only reports on the collectors that require their own connection.
func (p collectionPlan) needsDatabaseClient() bool {
	// tables already subsumes blocksReadByTable, which is emitted inside the
	// table loop and cannot run without enumeration.
	return p.tables || p.indexes || p.functions || p.tableBloat || p.indexBloat
}

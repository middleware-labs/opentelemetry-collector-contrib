# PostgreSQL Receiver – Configuration Guide

Use this guide to configure the PostgreSQL receiver in your pipeline. All options below are supported and in use.

---

## Minimal example

```yaml
receivers:
  postgresql:
    endpoint: localhost:5432
    username: myuser
    password: mypass
    databases: [mydb]
    collection_interval: 10s
```

---

## Connection

| Option | Required | Description | Example |
|--------|----------|-------------|---------|
| `endpoint` | Yes | PostgreSQL host and port | `localhost:5432` or `192.168.1.10:5432` |
| `transport` | No | `tcp` or `unix` | `tcp` (default) |
| `username` | Yes | Database user | `otelu` |
| `password` | Yes | Database password | `otelp` |
| `databases` | No | List of databases to collect from. If empty, databases are discovered automatically. See [Database selection](#database-selection). | `[demo, appdb]` |
| `exclude_databases` | No | Databases to skip. Applies to both auto-discovery and an explicit `databases` list. | `[template0, template1]` |
| `tls.insecure` | No | Use TLS | `false` (default) |
| `tls.insecure_skip_verify` | No | Skip TLS server certificate verification | `true` (default) |

---

## Database selection

`databases` and `exclude_databases` together decide which databases the
receiver collects from. The selection applies to **every** database-specific
signal: per-database metrics, schema collection, query samples, top-query
events, query-performance metrics, and the databases `EXPLAIN` may run in.

```yaml
databases: [orders, billing]
exclude_databases: [billing]
```

This collects from `orders` only.

| Configuration | Result |
|---|---|
| Neither set | All databases are discovered and collected from |
| `databases` set | Only the named databases; duplicates are collapsed |
| `exclude_databases` set | Everything except the named databases |
| A database in both | Exclusion wins; it is not collected |
| Every name in `databases` also excluded | Configuration error at startup — the receiver will not start, rather than silently collecting nothing |
| A named database is unreachable | Reported as an error; scope is never widened to compensate |
| Discovery fails | Reported as an error; scope is never widened to compensate |

### Behavior change

Before this release, `databases` and `exclude_databases` were applied to
per-database metrics and schema collection, but **not** to query samples,
top-query events or query-performance metrics. A receiver configured for one
database still reported query telemetry from every database on the server.

They are now applied consistently. If you relied on the previous broader query
telemetry, remove the `databases` restriction or add the databases you want to
keep seeing.

### The maintenance connection

The receiver always connects to the `postgres` database to read server-wide
statistics (background writer, WAL, replication, connection counts) and to
discover databases. It does this even when `postgres` is not in the selection:
it is a control connection, not data scope.

Its own database-specific telemetry is **not** collected when it is out of
scope. In particular `postgresql.database.locks` and the `postgresql.rows_*`
family are read through that connection but describe only the `postgres`
database, so they are collected only when `postgres` is itself selected.

---

## Scrape interval

| Option | Default | Description |
|--------|---------|-------------|
| `collection_interval` | `10s` | How often the receiver runs (metrics and all log scrapers). Schema collection then throttles internally using `schema_collection.collection_interval`. |
| `relation_metrics.collection_interval` | unset (every scrape) | How often the per-relation metric families run: per-table statistics and block reads, per-index statistics and per-function statistics. Database-level and server-wide metrics still run every scrape. `postgresql.table.count` is reported every scrape and refreshed at this cadence. |
| `bloat_collection_interval` | unset (every scrape) | How often `postgresql.table_bloat` and `postgresql.index_bloat` run. Their two estimator queries are the heaviest SQL the receiver issues. |

Each per-family interval, when set, must be at least `collection_interval`. A family runs on the first scrape at or after its interval has elapsed since it last ran, so an interval that is not a multiple of `collection_interval` rounds up to the next scrape. Between runs the family issues no SQL and emits no data points; cumulative metrics keep their start timestamp across the gap, so rates computed from them stay correct. When every per-database family is throttled and not due, no connection is opened to the individual databases on that scrape.

```yaml
collection_interval: 10s
relation_metrics:
  collection_interval: 60s
bloat_collection_interval: 10m
```

---

## Connection pool (optional)

Only applied when the feature gate `receiver.postgresql.connectionPool` is enabled.

| Option | Description |
|--------|-------------|
| `connection_pool.max_idle_time` | Max time a connection can be idle |
| `connection_pool.max_lifetime` | Max lifetime of a connection |
| `connection_pool.max_idle` | Max idle connections in the pool |
| `connection_pool.max_open` | Max open connections |

Example:

```yaml
connection_pool:
  max_idle_time: 10m
  max_lifetime: 0
  max_idle: 2
  max_open: 5
```

---

## Events (log pipelines)

Enable or disable each log source. Schema collection is controlled by `schema_collection.enabled`; extensions and settings are collected as part of schema when enabled there.

| Option | Description |
|--------|-------------|
| `events.db.server.query_sample.enabled` | Emit query sample logs (active queries) |
| `events.db.server.top_query.enabled` | Emit top-query logs with explain plans |
| `events.db.server.schema_collection.enabled` | No effect; use `schema_collection.enabled` instead |
| `events.db.server.extensions_collection.enabled` | From metadata; extensions are collected when `schema_collection.collect_extensions` is true |
| `events.db.server.settings_collection.enabled` | From metadata; settings are collected when `schema_collection.collect_settings` is true |

Example:

```yaml
events:
  db.server.query_sample:
    enabled: true
  db.server.top_query:
    enabled: true
  db.server.schema_collection:
    enabled: true
  db.server.extensions_collection:
    enabled: true
  db.server.settings_collection:
    enabled: true
```

---

## Query sample collection

| Option | Default | Description |
|--------|---------|-------------|
| `query_sample_collection.max_rows_per_query` | `1000` | Max rows per query for sample collection |

---

## Top query collection

| Option | Default | Description |
|--------|---------|-------------|
| `top_query_collection.collection_interval` | unset (every scrape) | How often top-query events are collected. Must be at least `collection_interval` when set; the scraper still runs every `collection_interval` and returns nothing when not due. Statement deltas then cover the longer window. |
| `top_query_collection.max_rows_per_query` | `1000` | Max rows per query |
| `top_query_collection.top_n_query` | `1000` | Number of top queries to collect |
| `top_query_collection.max_explain_each_interval` | `1000` | Max explains per interval |
| `top_query_collection.query_plan_cache_size` | `1000` | Size of the explain plan cache |
| `top_query_collection.query_plan_cache_ttl` | `1h` | TTL for cached explain plans |

---

## Schema collection

| Option | Default | Description |
|--------|---------|-------------|
| `schema_collection.enabled` | — | Turn schema collection on or off |
| `schema_collection.collection_interval` | `60s` | How often to check for schema changes (xmin). When changes are detected, a full snapshot is emitted. |
| `schema_collection.refresh_interval` | `24h` | Force a full snapshot at least this often even if no changes are detected |
| `schema_collection.collect_extensions` | — | Include extensions in the schema snapshot and emit extension log records |
| `schema_collection.collect_settings` | — | Include PostgreSQL settings and emit settings log records |
| `schema_collection.continue_on_error` | — | On table/collection errors, continue with other tables instead of failing the whole scrape |
| `schema_collection.exclude_schemas` | — | Schema names to exclude (e.g. `information_schema`) |
| `schema_collection.include_schemas` | — | If non-empty, only these schemas are collected (e.g. `[public]`) |
| `schema_collection.exclude_tables` | — | Tables to exclude; use `schema.table` or `table` (defaults to `public`) |
| `schema_collection.include_tables` | — | If non-empty, only these tables are collected |

Example:

```yaml
schema_collection:
  enabled: true
  collection_interval: 60s
  refresh_interval: 24h
  collect_extensions: true
  collect_settings: true
  continue_on_error: true
  exclude_schemas:
    - information_schema
  include_schemas:
    - public
```

---

## Full example

```yaml
receivers:
  postgresql:
    endpoint: 172.17.0.1:5432
    transport: tcp
    username: otelu
    password: otelp
    databases:
      - demo
    collection_interval: 1s
    connection_pool:
      max_idle_time: 10m
      max_lifetime: 0
      max_idle: 2
      max_open: 5
    tls:
      insecure: true
      insecure_skip_verify: true
    events:
      db.server.query_sample:
        enabled: true
      db.server.top_query:
        enabled: true
      db.server.extensions_collection:
        enabled: true
      db.server.settings_collection:
        enabled: true
      db.server.schema_collection:
        enabled: true
    query_sample_collection:
      max_rows_per_query: 1000
    top_query_collection:
      top_n_query: 1000
      query_plan_cache_ttl: 1h
    schema_collection:
      enabled: true
      collection_interval: 60s
      refresh_interval: 24h
      collect_extensions: true
      collect_settings: true
      continue_on_error: true
      exclude_schemas:
        - information_schema
      include_schemas:
        - public
```

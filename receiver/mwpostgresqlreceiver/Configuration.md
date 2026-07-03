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
| `databases` | No | List of databases to scrape. If empty, all databases are discovered (except those in `exclude_databases`). | `[demo, appdb]` |
| `exclude_databases` | No | Databases to skip when using auto-discovery | `[template0, template1]` |
| `tls.insecure` | No | Use TLS | `false` (default) |
| `tls.insecure_skip_verify` | No | Skip TLS server certificate verification | `true` (default) |

---

## Scrape interval

| Option | Default | Description |
|--------|---------|-------------|
| `collection_interval` | `10s` | How often the receiver runs (metrics and all log scrapers). Schema collection then throttles internally using `schema_collection.collection_interval`. |

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

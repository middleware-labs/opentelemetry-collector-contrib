# PostgreSQL Receiver Setup Guide

This guide is for operators. Follow these steps in order to enable full PostgreSQL receiver collection (metrics + query/sample/top-query/schema events).

## Step 1: Check PostgreSQL version

Use a currently supported PostgreSQL release.

## Step 2: Enable `pg_stat_statements` (required for top query)

Edit `postgresql.conf`:

```conf
shared_preload_libraries = 'pg_stat_statements'
```

Restart PostgreSQL after updating `shared_preload_libraries`.

## Step 3: Create monitoring user and grants

Run as a superuser/admin:

```sql
-- Create receiver user
CREATE USER mw WITH PASSWORD 'mw';

-- Baseline monitoring privileges
GRANT pg_monitor TO mw;

-- Allow connection to monitored database(s)
GRANT CONNECT ON DATABASE demo TO mw;
```

## Step 4: Create extension in every monitored database

`pg_stat_statements` must exist in each database you scrape.

```sql
\c demo
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

Repeat for all databases listed in receiver `databases`.

## Step 5: Grant object access for plan enrichment (recommended)

Top query collection can run EXPLAIN/prepare flows. If object privileges are missing, query plans may be empty for those statements.

```sql
GRANT USAGE ON SCHEMA public TO mw;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO mw;

-- Optional: keep future tables readable
ALTER DEFAULT PRIVILEGES IN SCHEMA public
GRANT SELECT ON TABLES TO mw;
```

Note: For DML-heavy statements (UPDATE/DELETE/INSERT), additional object privileges may still be required for plan generation.

## Step 6: Use this receiver config

```yaml
receivers:
  postgresql:
    endpoint: 172.17.0.1:5432
    transport: tcp
    username: mw
    password: mw
    databases:
      - demo
    collection_interval: 5s

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
      top_n_query: 10
      max_rows_per_query: 1000
      max_explain_each_interval: 1000
      query_plan_cache_size: 1000
      query_plan_cache_ttl: 10s

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

processors:
  resource:
    attributes:
      - key: is.postgresql
        value: true
        action: insert

  resource/log:
    attributes:
      - key: mw.resource_type
        value: postgresql
        action: insert

  batch:
    timeout: 10s
    send_batch_size: 8192

exporters:
  otlp:
    endpoint: https://example:443
    headers:
      authorization: ${env:OTLP_TOKEN}

service:
  pipelines:
    metrics:
      receivers: [postgresql]
      processors: [resource, batch]
      exporters: [otlp]
    logs:
      receivers: [postgresql]
      processors: [resource, resource/log, batch]
      exporters: [otlp]
```

`db.server.query_sample` and `db.server.top_query` bodies are set by receiver code. You do not need a `transform/logs` rule to copy `db.query.text` into `body`.

## Step 7: Validate setup

Run these checks:

1. `SHOW shared_preload_libraries;` includes `pg_stat_statements`.
2. `\dx` in monitored DB shows `pg_stat_statements`.
3. `\du` confirms monitoring user has `pg_monitor`.
4. Receiver starts without `relation "pg_stat_statements" does not exist`.
5. `db.server.query_sample` and `db.server.top_query` events are visible in exporter output.

If top-query plans are empty for some statements, review object privileges for referenced schemas/tables.


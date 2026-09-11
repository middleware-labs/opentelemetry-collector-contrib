/* otel-collector-ignore */ SELECT
  calls,
  datname,
  shared_blks_dirtied,
  shared_blks_hit,
  shared_blks_read,
  shared_blks_written,
  temp_blks_read,
  temp_blks_written,
  query,
  queryid,
  rolname,
  rows,
  total_exec_time,
  total_plan_time,
  shared_blk_read_time AS blk_read_time,
  shared_blk_write_time AS blk_write_time,
  pg_stat_statements.dbid,
  pg_stat_statements.userid,
  pg_stat_statements.toplevel
FROM
  public.pg_stat_statements as pg_stat_statements
  LEFT JOIN pg_roles ON pg_stat_statements.userid = pg_roles.oid
  LEFT JOIN pg_database ON pg_stat_statements.dbid = pg_database.oid
WHERE
  query != '<insufficient privilege>'
  AND query NOT LIKE '/* otel-collector-ignore */%'
ORDER BY total_exec_time DESC
LIMIT 31;

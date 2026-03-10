SELECT   
    COALESCE(datname, '') AS datname,
    COALESCE(usename, '') AS usename,
    COALESCE(client_addr::TEXT, '') AS client_addr,
    COALESCE(client_hostname, '') AS client_hostname,
    COALESCE(client_port::TEXT, '') AS client_port,
    COALESCE(query_start::TEXT, '') AS query_start,
    COALESCE(wait_event_type, '') AS wait_event_type,
    COALESCE(wait_event, '') AS wait_event,
    COALESCE(backend_type, '') AS backend_type,
    COALESCE(xact_start::TEXT, '') AS xact_start,
    COALESCE(state_change::TEXT, '') AS state_change,
    COALESCE(backend_xid::TEXT, '') AS backend_xid,
    COALESCE(query_id::TEXT, '') AS query_id,
    COALESCE(pid::TEXT, '') AS pid,
    COALESCE(application_name::TEXT, '') AS application_name,
    EXTRACT(EPOCH FROM query_start) AS _query_start_timestamp,
    state,
    query,
    CASE
    WHEN state = 'active' THEN
      EXTRACT(EPOCH FROM (clock_timestamp() - query_start)) * 1e3
    WHEN state IN ('idle','idle in transaction','idle in transaction (aborted)')
         AND state_change IS NOT NULL THEN
      EXTRACT(EPOCH FROM (state_change - query_start)) * 1e3
    ELSE
      NULL
    END AS duration_ms
FROM pg_stat_activity
WHERE     
    coalesce(
      TRIM(query), 
      ''
    ) != ''
    AND query != '<insufficient privilege>'
    AND query NOT LIKE '/* otel-collector-ignore */%'
    AND backend_type = 'client backend'
    AND NOT (

      query_start < TO_TIMESTAMP(123440.111)
      AND state = 'idle'
    )
LIMIT 30;

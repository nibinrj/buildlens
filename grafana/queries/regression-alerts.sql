SELECT b.started_at AS "Build time",b.job_name AS "Job",b.build_number AS "Build",
       a.payload->>'kind' AS "Kind",a.payload->>'name' AS "Metric",
       (a.payload->>'current_ms')::bigint AS "Latest ms",
       (a.payload->>'threshold_ms')::double precision AS "Threshold ms",
       (a.payload->>'samples')::integer AS "Baseline samples"
FROM alert a JOIN build b ON b.id=a.build_id JOIN repository r ON r.id=a.repository_id
WHERE r.name=${repository:sqlstring} AND a.type='REGRESSION' AND $__timeFilter(b.started_at)
ORDER BY b.started_at DESC,a.id DESC LIMIT 100;

SELECT b.started_at AS time,
       (e->>'current_ms')::double precision AS "Measured ms",
       CASE WHEN e->>'status'<>'INSUFFICIENT_DATA' THEN (e->>'median_ms')::double precision END AS "Baseline median",
       CASE WHEN e->>'status'<>'INSUFFICIENT_DATA' THEN (e->>'threshold_ms')::double precision END AS "Threshold"
FROM build b JOIN repository r ON r.id=b.repository_id
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(b.analysis->'regressions','[]'::jsonb)) e
WHERE r.name=${repository:sqlstring} AND $__timeFilter(b.started_at)
  AND e->>'kind'='STAGE' AND e->>'name'=${stage:sqlstring}
ORDER BY b.started_at,b.id;

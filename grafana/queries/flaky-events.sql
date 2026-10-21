SELECT date_trunc('day',q.quarantined_at) AS time, count(*)::double precision AS value, 'quarantined'::text AS metric
FROM quarantine q JOIN test_case t ON t.id=q.test_case_id JOIN repository r ON r.id=t.repository_id
WHERE r.name=${repository:sqlstring} AND $__timeFilter(q.quarantined_at)
GROUP BY 1
UNION ALL
SELECT date_trunc('day',q.released_at), count(*)::double precision, 'released'::text
FROM quarantine q JOIN test_case t ON t.id=q.test_case_id JOIN repository r ON r.id=t.repository_id
WHERE r.name=${repository:sqlstring} AND q.released_at IS NOT NULL AND $__timeFilter(q.released_at)
GROUP BY 1 ORDER BY 1,3;

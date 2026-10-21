SELECT date_trunc('day',b.started_at) AS time,count(*)::double precision AS value,b.result AS metric
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND $__timeFilter(b.started_at)
GROUP BY 1,b.result ORDER BY 1,b.result;

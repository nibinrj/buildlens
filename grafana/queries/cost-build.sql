SELECT b.started_at AS time,b.cost_usd::double precision AS value,b.branch AS metric
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND b.agent_lifecycle='LOCAL' AND $__timeFilter(b.started_at)
ORDER BY b.started_at,b.id;

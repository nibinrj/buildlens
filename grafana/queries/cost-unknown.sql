SELECT count(*) AS "Builds with unknown cost"
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND b.cost_usd IS NULL AND $__timeFilter(b.started_at);

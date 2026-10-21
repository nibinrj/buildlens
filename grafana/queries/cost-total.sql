SELECT sum(b.cost_usd)::double precision AS "Equivalent reference USD"
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND b.agent_lifecycle='LOCAL' AND $__timeFilter(b.started_at);

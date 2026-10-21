SELECT b.branch AS "Branch",count(*) AS "Builds",count(b.cost_usd) AS "Known cost builds",
       sum(b.cost_usd)::double precision AS "Equivalent reference USD"
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND b.agent_lifecycle='LOCAL' AND $__timeFilter(b.started_at)
GROUP BY b.branch ORDER BY 4 DESC NULLS LAST;

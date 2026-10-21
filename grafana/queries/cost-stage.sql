WITH stage_totals AS (
  SELECT s.build_id,s.name,sum(s.duration_ms) AS stage_ms
  FROM stage_run s GROUP BY s.build_id,s.name
), attributed AS (
  SELECT s.name,b.cost_usd*s.stage_ms/NULLIF(sum(s.stage_ms) OVER (PARTITION BY b.id),0) AS usd
  FROM stage_totals s JOIN build b ON b.id=s.build_id JOIN repository r ON r.id=b.repository_id
  WHERE r.name=${repository:sqlstring} AND b.agent_lifecycle='LOCAL' AND $__timeFilter(b.started_at)
)
SELECT name AS "Stage",sum(usd)::double precision AS "Duration-weighted equivalent USD"
FROM attributed GROUP BY name ORDER BY 2 DESC NULLS LAST;

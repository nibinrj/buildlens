SELECT COALESCE(b.infra_reason,'unspecified') AS "Reason",count(*) AS "Builds"
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND b.infra_failure AND $__timeFilter(b.started_at)
GROUP BY 1 ORDER BY 2 DESC,1;

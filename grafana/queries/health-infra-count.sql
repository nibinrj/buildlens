SELECT count(*) AS "Infrastructure-attributed builds"
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND b.infra_failure AND $__timeFilter(b.started_at);

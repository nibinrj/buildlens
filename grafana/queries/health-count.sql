SELECT count(*) AS "Recorded builds"
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND $__timeFilter(b.started_at);

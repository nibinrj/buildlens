SELECT b.started_at AS "Started",b.job_name AS "Job",b.build_number AS "Build",b.branch AS "Branch",
       b.result AS "Result",b.duration_ms AS "Duration ms",b.infra_failure AS "Infrastructure failure"
FROM build b JOIN repository r ON r.id=b.repository_id
WHERE r.name=${repository:sqlstring} AND $__timeFilter(b.started_at)
ORDER BY b.started_at DESC,b.id DESC LIMIT 50;

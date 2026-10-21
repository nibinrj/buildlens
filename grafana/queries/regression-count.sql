SELECT count(*) AS "Regression alert rows"
FROM alert a JOIN repository r ON r.id=a.repository_id
WHERE r.name=${repository:sqlstring} AND a.type='REGRESSION' AND $__timeFilter(a.created_at);

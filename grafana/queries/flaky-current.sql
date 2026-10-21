SELECT count(*) AS "Quarantined now"
FROM quarantine q JOIN test_case t ON t.id=q.test_case_id
JOIN repository r ON r.id=t.repository_id
WHERE q.state='QUARANTINED' AND r.name=${repository:sqlstring};

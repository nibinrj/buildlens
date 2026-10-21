SELECT t.class_name||'#'||t.method_name AS "Test",q.reason_rule AS "Rule",
       q.quarantined_at AS "Quarantined at",q.consecutive_passes AS "Consecutive passes",q.manual AS "Manual"
FROM quarantine q JOIN test_case t ON t.id=q.test_case_id JOIN repository r ON r.id=t.repository_id
WHERE r.name=${repository:sqlstring} AND q.state='QUARANTINED'
ORDER BY q.quarantined_at,q.id;

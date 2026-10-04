-- name: ListActiveQuarantine :many
-- The tests currently quarantined in one repository, oldest first.
SELECT q.id, q.test_case_id, q.reason_rule, q.quarantined_at, q.manual,
       tc.module, tc.class_name, tc.method_name
FROM quarantine q
JOIN test_case tc ON tc.id = q.test_case_id
WHERE tc.repository_id = @repository_id
  AND q.state = 'QUARANTINED'
ORDER BY q.quarantined_at, q.id;

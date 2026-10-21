WITH detections AS (
  SELECT q.evidence,q.quarantined_at AS event_time,t.repository_id
  FROM quarantine q JOIN test_case t ON t.id=q.test_case_id WHERE NOT q.manual
  UNION ALL
  SELECT a.payload,a.created_at,a.repository_id FROM alert a
  WHERE a.type='FLAKY' AND a.payload->>'cap_reached'='true'
)
SELECT f->>'rule' AS "Rule",count(*) AS "Evidence occurrences"
FROM detections d JOIN repository r ON r.id=d.repository_id
CROSS JOIN LATERAL jsonb_array_elements(COALESCE(d.evidence->'findings','[]'::jsonb)) f
WHERE r.name=${repository:sqlstring} AND $__timeFilter(d.event_time)
GROUP BY 1 ORDER BY 1;

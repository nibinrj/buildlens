-- Placeholders are replaced only with validated numeric IDs and parsed UTC timestamps.
WITH batch AS (
  SELECT b.* FROM build b JOIN repository r ON r.id=b.repository_id
  WHERE b.id=ANY(ARRAY[@BATCH_IDS@]::bigint[]) AND r.name='nibinrj/buildlens-lab'
), detection_events AS (
  SELECT q.evidence,t.repository_id FROM quarantine q JOIN test_case t ON t.id=q.test_case_id
  WHERE NOT q.manual AND q.quarantined_at BETWEEN '@STARTED@'::timestamptz AND '@FINISHED@'::timestamptz
  UNION ALL
  SELECT a.payload,a.repository_id FROM alert a JOIN batch b ON b.id=a.build_id
  WHERE a.type='FLAKY' AND a.payload->>'cap_reached'='true'
), rules AS (
  SELECT f->>'rule' AS rule,count(*) AS evidence_occurrences
  FROM detection_events e JOIN repository r ON r.id=e.repository_id
  CROSS JOIN LATERAL jsonb_array_elements(COALESCE(e.evidence->'findings','[]'::jsonb)) f
  WHERE r.name='nibinrj/buildlens-lab' GROUP BY 1
)
SELECT jsonb_build_object(
  'batch', (SELECT jsonb_build_object(
    'recorded',count(*),'first_id',min(id),'last_id',max(id),
    'successful',count(*) FILTER(WHERE result='SUCCESS'),
    'unstable',count(*) FILTER(WHERE result='UNSTABLE'),
    'infra_failures',count(*) FILTER(WHERE infra_failure),
    'known_cost_builds',count(cost_usd),'unknown_cost_builds',count(*) FILTER(WHERE cost_usd IS NULL),
    'equivalent_usd',sum(cost_usd)::text,
    'cost_min_usd',min(cost_usd)::text,'cost_median_usd',(percentile_disc(0.5) WITHIN GROUP(ORDER BY cost_usd))::text,'cost_max_usd',max(cost_usd)::text,
    'agent_busy_ms',sum(agent_busy_ms),
    'duration_min_ms',min(duration_ms),'duration_median_ms',percentile_disc(0.5) WITHIN GROUP(ORDER BY duration_ms),'duration_max_ms',max(duration_ms)
  ) FROM batch),
  'repository_totals',(SELECT jsonb_build_object('recorded_builds',count(*),'infra_attributed',count(*) FILTER(WHERE b.infra_failure))
    FROM build b JOIN repository r ON r.id=b.repository_id WHERE r.name='nibinrj/buildlens-lab'),
  'test_observations',COALESCE((SELECT jsonb_agg(x ORDER BY x.stage,x.outcome) FROM (
    SELECT tr.stage,tr.outcome,count(*) AS observations FROM test_run tr JOIN batch b ON b.id=tr.build_id GROUP BY 1,2) x),'[]'::jsonb),
  'rule_evidence',(SELECT jsonb_agg(x ORDER BY x.rule) FROM (
    SELECT expected.rule,COALESCE(rules.evidence_occurrences,0) AS evidence_occurrences
    FROM (VALUES ('R1'),('R2'),('R3')) expected(rule) LEFT JOIN rules USING(rule)) x),
  'quarantine', (SELECT jsonb_build_object(
    'activated_in_batch',count(*) FILTER(WHERE q.quarantined_at BETWEEN '@STARTED@'::timestamptz AND '@FINISHED@'::timestamptz),
    'released_in_batch',count(*) FILTER(WHERE q.released_at BETWEEN '@STARTED@'::timestamptz AND '@FINISHED@'::timestamptz),
    'active_at_export',count(*) FILTER(WHERE q.state='QUARANTINED'),
    'historical_activations',count(*),'historical_releases',count(*) FILTER(WHERE q.state='RELEASED'))
    FROM quarantine q JOIN test_case t ON t.id=q.test_case_id JOIN repository r ON r.id=t.repository_id WHERE r.name='nibinrj/buildlens-lab'),
  'regressions',COALESCE((SELECT jsonb_agg(x ORDER BY x.kind,x.name) FROM (
    SELECT a.payload->>'kind' AS kind,a.payload->>'name' AS name,count(*) AS alert_rows
    FROM alert a JOIN batch b ON b.id=a.build_id WHERE a.type='REGRESSION' GROUP BY 1,2) x),'[]'::jsonb),
  'infra_reasons',COALESCE((SELECT jsonb_agg(x ORDER BY x.reason) FROM (
    SELECT infra_reason AS reason,count(*) AS builds FROM batch WHERE infra_failure GROUP BY 1) x),'[]'::jsonb),
  'regression_evidence',COALESCE((SELECT jsonb_agg(x ORDER BY x.build_id,x.id) FROM (
    SELECT a.id,a.build_id,a.payload FROM alert a JOIN batch b ON b.id=a.build_id WHERE a.type='REGRESSION') x),'[]'::jsonb),
  'quarantine_history',COALESCE((SELECT jsonb_agg(x ORDER BY x.id) FROM (
    SELECT q.id,t.class_name,t.method_name,q.state,q.reason_rule,q.quarantined_at,q.released_at,q.consecutive_passes,q.evidence
    FROM quarantine q JOIN test_case t ON t.id=q.test_case_id JOIN repository r ON r.id=t.repository_id
    WHERE r.name='nibinrj/buildlens-lab') x),'[]'::jsonb),
  'builds',COALESCE((SELECT jsonb_agg(x ORDER BY x.id) FROM (
    SELECT b.id,b.job_name,b.build_number,b.branch,b.result,b.started_at,b.duration_ms,b.agent_busy_ms,b.cost_usd::text,
      (SELECT count(*) FROM test_run t WHERE t.build_id=b.id) AS tests,
      (SELECT count(*) FROM alert a WHERE a.build_id=b.id AND a.type='REGRESSION') AS regression_alerts
    FROM batch b) x),'[]'::jsonb)
);

-- name: ListPerformanceBuilds :many
SELECT id, started_at FROM build
WHERE repository_id= @repository_id AND branch= @branch AND pr_number IS NULL
AND result='SUCCESS' AND NOT infra_failure
AND (started_at,id) <= (@before_time::timestamptz,@before_id::bigint)
ORDER BY started_at DESC,id DESC LIMIT @sample_limit;

-- name: ListPRPerformanceBuilds :many
SELECT id, started_at FROM build
WHERE repository_id= @repository_id AND pr_number= @pr_number
AND result='SUCCESS' AND NOT infra_failure
AND (started_at,id) <= (@before_time::timestamptz,@before_id::bigint)
ORDER BY started_at DESC,id DESC LIMIT 2;

-- name: ListPerformanceMetrics :many
WITH candidates AS (
    SELECT cr.test_case_id FROM test_run cr
    WHERE cr.build_id= @current_build_id AND cr.stage='BLOCKING' AND cr.outcome='PASSED'
    ORDER BY cr.duration_ms DESC,cr.test_case_id LIMIT 20
)
SELECT s.build_id, 'STAGE'::text AS kind, s.name::text AS metric_key,
       s.name::text AS name, sum(s.duration_ms)::bigint AS duration_ms
FROM stage_run s WHERE s.build_id=ANY(@build_ids::bigint[]) AND s.result='SUCCESS'
GROUP BY s.build_id,s.name
UNION ALL
SELECT tr.build_id, 'TEST'::text, tc.id::text,
       (tc.class_name || '#' || tc.method_name)::text, tr.duration_ms
FROM test_run tr JOIN test_case tc ON tc.id=tr.test_case_id
WHERE tr.build_id=ANY(@build_ids::bigint[]) AND tr.stage='BLOCKING' AND tr.outcome='PASSED'
AND tr.test_case_id IN (SELECT test_case_id FROM candidates)
ORDER BY build_id,kind,metric_key;

-- name: FindBuildPrice :one
SELECT id,instance_type,lifecycle,region,usd_per_hour::text AS hourly_rate,effective_from,source
FROM price WHERE instance_type= @instance_type AND lifecycle= @lifecycle AND region= @region
AND effective_from<= @at_time::timestamptz
ORDER BY effective_from DESC,id DESC LIMIT 1;

-- name: SetBuildAnalysis :exec
UPDATE build SET cost_usd=sqlc.narg(cost_usd)::numeric,analysis= @analysis WHERE id= @id;

-- name: DeleteBuildAnalysisAlerts :exec
DELETE FROM alert WHERE build_id= @build_id AND type IN ('REGRESSION','COST');

-- name: InsertAnalysisAlert :exec
INSERT INTO alert(type,build_id,repository_id,payload) VALUES(@type,@build_id,@repository_id,@payload);

-- name: QueuePRComment :exec
INSERT INTO pr_comment(repository_id,pr_number,build_id) VALUES(@repository_id,@pr_number,@build_id)
ON CONFLICT(repository_id,pr_number) DO UPDATE
SET build_id=EXCLUDED.build_id,desired_version=pr_comment.desired_version+1,
    next_attempt_at=CASE WHEN pr_comment.last_error IN ('GitHub HTTP 403','GitHub HTTP 429') THEN pr_comment.next_attempt_at ELSE now() END,
    attempts=CASE WHEN pr_comment.last_error IN ('GitHub HTTP 403','GitHub HTTP 429') THEN pr_comment.attempts ELSE 0 END,
    last_error=CASE WHEN pr_comment.last_error IN ('GitHub HTTP 403','GitHub HTTP 429') THEN pr_comment.last_error ELSE NULL END
-- Late uploads may refresh an existing build, but never replace a newer build's summary.
WHERE (SELECT (started_at,id) FROM build WHERE id=EXCLUDED.build_id) >=
      (SELECT (started_at,id) FROM build WHERE id=pr_comment.build_id);

-- name: ClaimPRComment :one
WITH candidate AS (
    SELECT repository_id,pr_number FROM pr_comment
    WHERE sent_version<desired_version AND next_attempt_at<=now()
    AND (lease_until IS NULL OR lease_until<now())
    ORDER BY next_attempt_at,repository_id,pr_number FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE pr_comment p SET lease_until=now()+interval '90 seconds',lease_token= @lease_token
FROM candidate c WHERE p.repository_id=c.repository_id AND p.pr_number=c.pr_number
RETURNING p.*;

-- name: CompletePRComment :execrows
UPDATE pr_comment SET comment_id=sqlc.narg(comment_id),sent_version= @sent_version,
    lease_until=NULL,lease_token=NULL,attempts=0,last_error=NULL,next_attempt_at=now()
WHERE repository_id= @repository_id AND pr_number= @pr_number AND lease_token= @lease_token;

-- name: FailPRComment :execrows
UPDATE pr_comment SET lease_until=NULL,lease_token=NULL,
    attempts=CASE WHEN desired_version= @version THEN least(attempts+1,16) ELSE attempts END,
    last_error= @last_error,next_attempt_at=CASE WHEN desired_version= @version OR @last_error::text IN ('GitHub HTTP 403','GitHub HTTP 429') THEN @retry_at::timestamptz ELSE now() END
WHERE repository_id= @repository_id AND pr_number= @pr_number AND lease_token= @lease_token;

-- name: SetAlertCommentID :exec
UPDATE alert SET pr_comment_id = sqlc.narg(pr_comment_id) WHERE build_id = @build_id;

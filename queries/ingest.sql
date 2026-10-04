-- Queries used by internal/ingest. All run inside one transaction per uploaded build.

-- name: EnsureRepository :one
-- Returns the repository's id, creating it on first sight. An existing default branch is left unchanged.
INSERT INTO repository (name, default_branch)
VALUES (@name, @default_branch)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id;

-- name: UpsertBuild :one
-- Idempotent on (job_name, build_number): a second upload of the same build updates the row.
-- (xmax = 0) is true only for a freshly inserted row, so the caller can answer 201 or 200.
-- infra_failure, infra_reason and cost_usd are not set here: P3 and P4 compute them.
INSERT INTO build (
    repository_id, job_name, build_number, branch, pr_number, commit_sha, tested_tree_sha, result,
    started_at, finished_at, duration_ms, agent_name, agent_instance_type, agent_lifecycle
) VALUES (
    @repository_id, @job_name, @build_number, @branch, sqlc.narg(pr_number), @commit_sha, sqlc.narg(tested_tree_sha),
    @result, @started_at, sqlc.narg(finished_at), sqlc.narg(duration_ms), sqlc.narg(agent_name),
    sqlc.narg(agent_instance_type), sqlc.narg(agent_lifecycle)
)
ON CONFLICT (job_name, build_number) DO UPDATE SET
    repository_id       = EXCLUDED.repository_id,
    branch              = EXCLUDED.branch,
    pr_number           = EXCLUDED.pr_number,
    commit_sha          = EXCLUDED.commit_sha,
    tested_tree_sha     = EXCLUDED.tested_tree_sha,
    result              = EXCLUDED.result,
    started_at          = EXCLUDED.started_at,
    finished_at         = EXCLUDED.finished_at,
    duration_ms         = EXCLUDED.duration_ms,
    agent_name          = EXCLUDED.agent_name,
    agent_instance_type = EXCLUDED.agent_instance_type,
    agent_lifecycle     = EXCLUDED.agent_lifecycle,
    updated_at          = now()
RETURNING id, (xmax = 0)::boolean AS inserted;

-- name: DeleteStageRuns :exec
DELETE FROM stage_run WHERE build_id = @build_id;

-- name: DeleteTestRuns :exec
DELETE FROM test_run WHERE build_id = @build_id;

-- name: InsertStageRuns :copyfrom
INSERT INTO stage_run (build_id, name, started_at, duration_ms, result)
VALUES (@build_id, @name, @started_at, @duration_ms, @result);

-- name: UpsertTestCases :many
-- One statement for all test cases of an upload; the arrays are zipped by unnest.
-- The caller must pass each (class, method) once: ON CONFLICT cannot update one row twice in a statement.
-- Several unnest() calls in one SELECT list advance together (PostgreSQL 10+), so the arrays are zipped by index.
INSERT INTO test_case (repository_id, module, class_name, method_name)
SELECT @repository_id::bigint, unnest(@modules::text[]), unnest(@class_names::text[]), unnest(@method_names::text[])
ON CONFLICT (repository_id, class_name, method_name) DO UPDATE SET module = EXCLUDED.module
RETURNING id, class_name, method_name;

-- name: InsertTestRuns :copyfrom
INSERT INTO test_run (build_id, test_case_id, outcome, duration_ms, rerun_failures, failure_type, failure_hash, stage)
VALUES (@build_id, @test_case_id, @outcome, @duration_ms, @rerun_failures, @failure_type, @failure_hash, @stage);

-- name: GetBuildWithRepository :one
SELECT sqlc.embed(b), r.name AS repository_name
FROM build b
JOIN repository r ON r.id = b.repository_id
WHERE b.id = @id;

-- name: ListStageRuns :many
SELECT * FROM stage_run WHERE build_id = @build_id ORDER BY started_at, id;

-- name: ListTestRuns :many
SELECT tr.id, tr.outcome, tr.duration_ms, tr.rerun_failures, tr.failure_type, tr.failure_hash, tr.stage,
       tc.module, tc.class_name, tc.method_name
FROM test_run tr
JOIN test_case tc ON tc.id = tr.test_case_id
WHERE tr.build_id = @build_id
ORDER BY tc.class_name, tc.method_name;

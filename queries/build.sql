-- name: GetBuild :one
SELECT * FROM build WHERE id = @id;

-- name: GetBuildByJobAndNumber :one
SELECT * FROM build WHERE job_name = @job_name AND build_number = @build_number;

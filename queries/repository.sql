-- name: UpsertRepository :one
-- Creates the repository, or updates its default branch if it already exists.
INSERT INTO repository (name, default_branch)
VALUES (@name, @default_branch)
ON CONFLICT (name) DO UPDATE SET default_branch = EXCLUDED.default_branch
RETURNING *;

-- name: GetRepositoryByName :one
SELECT * FROM repository WHERE name = @name;

-- name: ListRepositories :many
SELECT * FROM repository ORDER BY name;

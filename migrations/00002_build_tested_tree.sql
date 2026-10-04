-- D-078: record what a build actually tested. Jenkins builds a pull request as a local merge commit whose SHA
-- changes on every build (it includes a timestamp), so commit_sha cannot tell "same code" apart. The git *tree*
-- hash of the checkout depends only on file content: identical code, identical tree. Rule R2 (P3) compares this.
-- Nullable: a build that cannot report it (no git checkout) is still ingested, and R2 ignores it.

-- +goose Up
ALTER TABLE build ADD COLUMN tested_tree_sha text;

-- +goose Down
ALTER TABLE build DROP COLUMN tested_tree_sha;

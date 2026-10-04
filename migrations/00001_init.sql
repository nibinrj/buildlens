-- Initial schema: every table in the plan's data model (docs/plan.md, "Tables").
-- Conventions: bigint identity keys, times as timestamptz (UTC), durations in milliseconds,
-- enum-like columns as text with CHECK constraints (easier to extend in a later migration than a Postgres enum).

-- +goose Up
CREATE TABLE repository (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name           text        NOT NULL UNIQUE,
    default_branch text        NOT NULL DEFAULT 'main',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE build (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repository_id       bigint      NOT NULL REFERENCES repository (id),
    job_name            text        NOT NULL,
    build_number        integer     NOT NULL,
    branch              text        NOT NULL,
    pr_number           integer,
    commit_sha          text        NOT NULL,
    result              text        NOT NULL
        CHECK (result IN ('SUCCESS', 'UNSTABLE', 'FAILURE', 'ABORTED', 'NOT_BUILT')),
    started_at          timestamptz NOT NULL,
    finished_at         timestamptz,
    duration_ms         bigint CHECK (duration_ms >= 0),
    agent_name          text,
    agent_instance_type text,
    agent_lifecycle     text CHECK (agent_lifecycle IN ('LOCAL', 'SPOT', 'ON_DEMAND')),
    infra_failure       boolean     NOT NULL DEFAULT false,
    infra_reason        text,
    cost_usd            numeric(12, 6),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    -- Ingest is idempotent: the same job + build number updates the row, never duplicates it.
    CONSTRAINT build_job_number_key UNIQUE (job_name, build_number)
);
CREATE INDEX build_repo_branch_started_idx ON build (repository_id, branch, started_at);

CREATE TABLE stage_run (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    build_id    bigint      NOT NULL REFERENCES build (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    started_at  timestamptz NOT NULL,
    duration_ms bigint      NOT NULL CHECK (duration_ms >= 0),
    result      text        NOT NULL
        CHECK (result IN ('SUCCESS', 'UNSTABLE', 'FAILURE', 'ABORTED', 'NOT_BUILT'))
);
CREATE INDEX stage_run_build_idx ON stage_run (build_id);
CREATE INDEX stage_run_name_idx ON stage_run (name, started_at);

CREATE TABLE test_case (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repository_id bigint NOT NULL REFERENCES repository (id),
    module        text   NOT NULL DEFAULT '',
    class_name    text   NOT NULL,
    method_name   text   NOT NULL,
    CONSTRAINT test_case_repo_class_method_key UNIQUE (repository_id, class_name, method_name)
);

CREATE TABLE test_run (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    build_id       bigint  NOT NULL REFERENCES build (id) ON DELETE CASCADE,
    test_case_id   bigint  NOT NULL REFERENCES test_case (id),
    outcome        text    NOT NULL
        CHECK (outcome IN ('PASSED', 'FAILED', 'ERROR', 'SKIPPED', 'FLAKY')),
    duration_ms    bigint  NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    rerun_failures integer NOT NULL DEFAULT 0 CHECK (rerun_failures >= 0),
    failure_type   text,
    failure_hash   text,
    stage          text    NOT NULL DEFAULT 'BLOCKING'
        CHECK (stage IN ('BLOCKING', 'QUARANTINE')),
    -- One row per test per build.
    CONSTRAINT test_run_build_test_key UNIQUE (build_id, test_case_id)
);
CREATE INDEX test_run_test_case_idx ON test_run (test_case_id, build_id);

CREATE TABLE quarantine (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    test_case_id       bigint      NOT NULL REFERENCES test_case (id),
    state              text        NOT NULL CHECK (state IN ('QUARANTINED', 'RELEASED')),
    reason_rule        text        NOT NULL,
    evidence           jsonb       NOT NULL DEFAULT '{}'::jsonb,
    quarantined_at     timestamptz NOT NULL DEFAULT now(),
    released_at        timestamptz,
    consecutive_passes integer     NOT NULL DEFAULT 0 CHECK (consecutive_passes >= 0),
    manual             boolean     NOT NULL DEFAULT false,
    CHECK ((state = 'RELEASED') = (released_at IS NOT NULL))
);
-- History is kept; at most one active quarantine per test.
CREATE UNIQUE INDEX quarantine_one_active_idx ON quarantine (test_case_id) WHERE state = 'QUARANTINED';

CREATE TABLE alert (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type          text        NOT NULL CHECK (type IN ('FLAKY', 'REGRESSION', 'COST')),
    build_id      bigint REFERENCES build (id) ON DELETE SET NULL,
    repository_id bigint      NOT NULL REFERENCES repository (id),
    payload       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    pr_comment_id bigint
);
CREATE INDEX alert_repo_created_idx ON alert (repository_id, created_at);

CREATE TABLE price (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_type  text           NOT NULL,
    lifecycle      text           NOT NULL CHECK (lifecycle IN ('LOCAL', 'SPOT', 'ON_DEMAND')),
    region         text           NOT NULL,
    usd_per_hour   numeric(10, 6) NOT NULL CHECK (usd_per_hour >= 0),
    effective_from timestamptz    NOT NULL,
    source         text           NOT NULL,
    CONSTRAINT price_type_lifecycle_region_from_key UNIQUE (instance_type, lifecycle, region, effective_from)
);

-- Phase 2: Spot interruption warnings.
CREATE TABLE infra_event (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id text        NOT NULL,
    event_type  text        NOT NULL,
    event_time  timestamptz NOT NULL,
    raw         jsonb       NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX infra_event_instance_time_idx ON infra_event (instance_id, event_time);

-- +goose Down
DROP TABLE infra_event;
DROP TABLE price;
DROP TABLE alert;
DROP TABLE quarantine;
DROP TABLE test_run;
DROP TABLE test_case;
DROP TABLE stage_run;
DROP TABLE build;
DROP TABLE repository;

-- +goose Up
ALTER TABLE build ADD COLUMN analysis jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE build ADD COLUMN agent_busy_ms bigint CHECK (agent_busy_ms >= 0);

-- Durable coalescing outbox: one desired comment per repository and PR.
CREATE TABLE pr_comment (
    repository_id bigint NOT NULL REFERENCES repository(id),
    pr_number integer NOT NULL CHECK (pr_number > 0),
    build_id bigint NOT NULL REFERENCES build(id) ON DELETE CASCADE,
    desired_version bigint NOT NULL DEFAULT 1 CHECK (desired_version > 0),
    sent_version bigint NOT NULL DEFAULT 0 CHECK (sent_version >= 0 AND sent_version <= desired_version),
    comment_id bigint,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    lease_token text,
    last_error text,
    PRIMARY KEY (repository_id, pr_number),
    CHECK ((lease_until IS NULL) = (lease_token IS NULL))
);
CREATE INDEX pr_comment_pending_idx ON pr_comment(next_attempt_at) WHERE sent_version < desired_version;

-- Dated reference only: Linux/Unix, us-east-1, official T3 page checked 2026-10-16.
-- LOCAL is an equivalent compute price, not an assertion of AWS spend.
INSERT INTO price(instance_type,lifecycle,region,usd_per_hour,effective_from,source)
VALUES ('t3.small','LOCAL','us-east-1',0.020900,'2026-10-16T00:00:00Z',
        'reference: Linux/Unix t3.small us-east-1; checked 2026-10-16; https://aws.amazon.com/ec2/instance-types/t3/');

-- +goose Down
DELETE FROM price WHERE instance_type='t3.small' AND lifecycle='LOCAL' AND region='us-east-1'
    AND effective_from='2026-10-16T00:00:00Z' AND source LIKE 'reference:%';
DROP TABLE pr_comment;
ALTER TABLE build DROP COLUMN analysis;
ALTER TABLE build DROP COLUMN agent_busy_ms;

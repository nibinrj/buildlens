# 005: Poll GitHub for local Jenkins discovery

Status: accepted implementation. Date: 2026-10-19.

## Decision

Keep Jenkins on a loopback-bound local port. The Job DSL multibranch jobs poll
GitHub periodically and use the read-only GitHub token from JCasC. No public
tunnel, inbound webhook or manual UI job configuration is required. PR discovery
builds trusted same-repository PRs merged with their target; fork discovery is
disabled. The shared library is fetched from GitHub, under shared-library/.

The sequential REST driver submits one build, follows the returned queue item
and validates its executable's queue ID before recording the result. API-token
authentication is preferred. An explicit local-password fallback uses a CSRF
crumb and the same cookie session. No uncertain POST is automatically retried.
Every request and each full queue/build lifecycle has a deadline.

The local Go verification runner needs the Docker socket for real-Postgres tests.
That socket provides host-equivalent control; it is appropriate only for this
trusted local stack. It is not a security boundary for untrusted jobs or an AWS
deployment design. The runner and test Postgres both have memory limits.

## Consequences

Discovery is delayed by the polling interval, and Jenkins must reach GitHub.
Read-only commit-status HTTP 403 warnings are expected; the separate BuildLens
token controls sticky comments. Neither polling nor a local adapter proves that
an uncommitted Jenkinsfile was fetched by a normal multibranch build. Source
snapshots are hashed and labelled; owner commit/push remains the final Git gate.

ADR 002 was already assigned to regression/cost/comments, so the three records
requested in the measurement plan use 003, 004 and 005 rather than duplicate IDs.

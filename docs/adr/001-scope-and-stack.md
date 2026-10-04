# ADR 001: Scope and stack

- Status: accepted
- Date: 2026-10-04
- Context prompt: P1

## Context

BuildLens learns from every Jenkins build: it finds and quarantines flaky tests, separates infrastructure failures
from test failures, spots stages that got slower, and prices each build. It is built and run by one person on one
Windows laptop (Docker Desktop with 7.6 GiB of memory), then moved to a small, disposable AWS setup. The project
exists to be understood and defended in interviews, so every part must be explainable and testable.

## Decisions

### One service, not microservices

One HTTP service (`buildlens-server`) owns ingest, rules, quarantine, regression, cost and PR comments, with one
Postgres database. A CLI (`buildlens`) runs on Jenkins agents and talks to it over HTTP.

- The rules need the same data in one transaction: test runs, builds, quarantine state. Splitting them across
  services would add network calls, eventual consistency and a message broker (the plan rules out Kafka) with no
  scaling need behind it. The load is a few builds per minute at most.
- One process is one thing to deploy, monitor and restart, and it fits the memory budget (64 MB limit).
- The internal packages (`internal/rules`, `internal/quarantine`, ...) keep the code modular. If one part ever needs
  to be separate, the package boundary is already there.

### Go

- Single static binary: agents need no runtime, and the server image is distroless with nothing else in it.
- Small memory use (a 64 MB container limit) next to a JVM-heavy Jenkins stack on one laptop.
- It is the language of cloud-native tooling (Docker, Kubernetes, Terraform, the GitHub and AWS SDKs), which is the
  skill this project shows. The owner's Java is already shown in an earlier project.
- The standard library covers HTTP routing (`net/http` method patterns), structured logging (`log/slog`) and
  testing, so there is no web framework.

### Jenkins and Maven only

- Jenkins is still the most common self-hosted CI. Its JCasC and Job DSL make "configured only from code" possible.
- Surefire and Failsafe XML has known quirks (rerun elements, wrong suite totals), and handling them properly beats
  handling five report formats superficially.
- Multi-CI or multi-build-tool support would multiply the parsers and the integration tests without teaching
  anything new about flaky tests. It is an explicit non-goal.

### No machine learning

- Every rule (R1 rerun, R2 same commit, R3 flip rate, median/MAD regression) is deterministic, explainable in one
  sentence, and covered by table-driven tests with known edge cases.
- Quarantine changes what a developer's build blocks on. A decision like that must be explainable from evidence
  (build ids, outcomes), which a rule gives and a model does not.
- There is not enough labelled data. The IDoFT benchmark (P6) measures the rules' precision and recall; ML would
  only make sense where that measurement shows rules failing.

## Consequences

- Some things are knowingly left out: other CI systems, other languages, and predictive test selection (test impact
  analysis stays rule-based and is a stretch goal).
- One database is a single point of failure. That is acceptable locally. Phase 2 backs it up to S3 on demo-down.
- Go is new to the owner. The study notes explain each Go idiom against Java (docs/study/P1-design.md §6).

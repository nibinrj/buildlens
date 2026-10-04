# BuildLens — rules for Claude Code

Owner: Nibin (github.com/nibinrj). Dev machine: Windows 11, PowerShell 7, Docker Desktop,
16 GB RAM (Docker Desktop reports 7.6 GiB, checked 2026-10-04; budget target stays about 3.5 GB).
Full plan: docs/plan.md. Read it before any work.
I am a Java developer new to Go: write plain, idiomatic Go and explain any non-obvious idiom
(errors, context, interfaces, goroutines) in the study notes, compared with how Java does it.

## What this is
A CI intelligence service for Jenkins: flaky-test detection and quarantine, infrastructure-failure
attribution, build-time regression alerts, cost per build. Phase 1 runs locally; Phase 2 on AWS.
Written in Go: one HTTP service (buildlens-server) and one CLI (buildlens) that Jenkins agents run.
No Kafka, no microservices, no ML, no web framework.

## Hard rules
1. Explain before you build. Each prompt starts by writing or updating its study note in
   docs/study/. State what you are certain of and what you checked in official docs.
2. Never guess a version. Pin every version (Go toolchain in go.mod, Go modules, Docker image tags,
   Jenkins LTS, every plugin in plugins.txt, Terraform providers). Say where each came from.
   If you cannot verify one, say so and ask me.
3. Tests are part of the work. Table-driven go tests for every rule, parser and handler, including
   failure paths. Parsers are tested against real Surefire/Failsafe XML fixtures in testdata/.
4. Done means it ran. End every prompt with gofmt, go vet, golangci-lint run and
   go test -race ./..., and show the output. Then list anything you did not verify.
5. Never invent numbers. No performance, cost or flakiness figure goes in docs unless it came
   from a real run in this repo. Injected flakiness is always labelled as injected.
6. Secrets never enter git. Use .env (gitignored) and .env.example. Jenkins secrets come from
   JCasC environment variables locally and AWS Secrets Manager in Phase 2.
7. Configuration as code only. Nothing in Jenkins is configured by clicking. If it needs the UI,
   it is a bug in the JCasC or Job DSL.
8. Stay inside the files the prompt names. If another file must change, say why first.
9. Ask before: adding any Go module (prefer the standard library), a Jenkins plugin, changing the
   schema outside a new migration, any IAM policy or security group (Phase 2), anything that
   costs money.
10. Memory budget: every container has a memory limit. Go binaries ship as static builds on a
    distroless image. Report the expected total before adding a container.

## Conventions
- One Go module: github.com/nibinrj/buildlens. Go toolchain pinned in go.mod.
- cmd/buildlens-server (service), cmd/buildlens (agent CLI), internal/ for everything else.
- HTTP: net/http ServeMux with method and path patterns; no framework. Logging: log/slog (JSON).
- Config: environment variables, optional YAML file for rule thresholds.
- Postgres via pgx v5; SQL in queries/ compiled with sqlc; migrations in migrations/ (goose or
  golang-migrate: decide in P1 and record why).
- context.Context on every request, DB call and outbound HTTP call; every outbound call has a
  timeout; errors wrapped with fmt.Errorf("...: %w", err).
- Interfaces only where a test needs a fake (GitHub client, clock, store).
- Tests: standard testing package, table-driven, testcontainers-go for Postgres, httptest for fakes.
- Times in UTC, stored as timestamptz; durations in milliseconds.
- Line endings: .gitattributes keeps *.sh as LF.
- tasks.ps1 is the single entry point: up, down, logs, test, lint, jenkins, grafana, seed, drive, bench.
- Commit messages start with the prompt or batch id (P3:, J.4:).
- Study notes: docs/study/<id>-<topic>.md. Decisions: docs/adr/NNN-title.md.

## Known facts (verified 2026-10)
- Surefire rerunFailingTestsCount writes flakyFailure/flakyError elements per failed rerun.
  The report's top-level totals count reruns as extra tests: parse testcase elements, never totals.
- Pipeline Stage View is up for adoption; do not depend on its REST API. Record stage timings in
  the shared library itself.
- EC2 Fleet plugin (Phase 2) supports JCasC and resubmits builds interrupted by Spot by default.
- Launchable is now CloudBees Smart Tests. A Jenkins Flaky Test Handler plugin exists (prior art).
- IDoFT (github.com/TestingResearchIllinois/idoft) pr-data.csv lists flaky tests in Maven projects
  with project URL, fully qualified test name, commit SHA and category (OD = order-dependent).

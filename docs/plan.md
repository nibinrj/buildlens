# BuildLens — Claude Code Handoff Plan

Oct 4, 2026 · @Nibin

## How to use this plan

Build Phase 1 (local, seven Claude Code prompts, written in Go) now; Phase 2 (Jenkins on AWS with Spot agents) after Phase 1 works. Study every batch afterwards before the project goes on your resume.

1. Create a new GitHub repo `nibinrj/buildlens` and clone it on your Windows machine.
2. Export this doc as Markdown and save it in the repo as `docs/plan.md`.
3. Copy the **CLAUDE.md** section below into `CLAUDE.md` at the repo root. Claude Code reads it automatically every session.
4. Paste the Phase 1 prompts into Claude Code one at a time, in order. Do not start the next prompt until the current one ends with passing tests and its output shown.
5. After each prompt, read the tests it wrote first. They are the fastest way to see what the code claims to do.
6. Commit after each prompt with the prompt number in the message (`P2: ingest + reporter`).

**Rules for every prompt**

- Use Opus at maximum effort. One prompt can run for a long time; let it finish.
- If Claude Code asks a question, answer it. Do not tell it to "just decide" on versions, security groups or IAM.
- If a prompt hits your usage limit mid-way, start the next session with: "Read CLAUDE.md and docs/plan.md, check git status and the last commit, and continue Prompt N from where it stopped."
- Never accept "done" without the command output that proves it.

**Before P1 (Go is new to you):** do [A Tour of Go](https://go.dev/tour/) and the first chapters of [Learn Go with Tests](https://quii.gitbook.io/learn-go-with-tests) so you can read Claude Code's diffs. Reading Go is enough to start; defending it comes in the study phase.

## Project brief

BuildLens is a CI intelligence service for Jenkins: it learns from every build and acts on flaky tests, slow stages and build cost.

**Problem.** Flaky tests make engineers rerun builds and ignore red pipelines. Stages get slower without anyone noticing. Nobody knows what a build costs. Every change runs the whole suite.

**What it does**

| Capability | Result shown to developers | Phase |
| --- | --- | --- |
| Flaky test detection and auto-quarantine | Flaky tests removed from the blocking stage, still run non-blocking, released after consecutive passes | 1 |
| Infrastructure-failure attribution | Agent loss, OOM, disk full, Spot interruption marked as infrastructure, never counted as test flakiness | 1 (log rules), 2 (Spot events) |
| Build-time regression alerts | PR comment when a stage is consistently slower than the main-branch baseline | 1 |
| Cost per build | Cost per build, branch and stage | 1 (price table), 2 (real Spot prices) |
| Agent-facing verdicts (MCP) | AI coding agents ask, through read-only MCP tools, whether a red build is their change, a flaky test, an already-broken main branch or infrastructure, before editing code | 1 (P7) |
| Test impact analysis | Only affected Maven modules tested on PRs | Stretch |

**Non-goals:** no Kafka, no microservices, no ML, no frontend beyond Grafana, no multi-CI support (Jenkins and Maven only).

**Success criteria for Phase 1**

- One command brings up Jenkins, BuildLens, Postgres and Grafana locally, configured entirely from code.
- 50 or more recorded builds of the lab repo with flaky tests detected, quarantined and released.
- A benchmark on at least 3 projects from the IDoFT ground-truth dataset, with precision and recall in `docs/benchmark.md`.
- At least one regression alert and one quarantine notice posted as a PR comment.
- `docs/results.md` with real numbers from your own runs, injected flakiness clearly labelled.
- A read-only MCP server (`buildlens-mcp`) whose five tools are listed through the MCP Go SDK client, and a with/without demo that includes a real-bug control case, transcripts saved.
- `go vet`, `golangci-lint` and `go test -race ./...` all pass.

## CLAUDE.md

Copy this block into `CLAUDE.md` at the repo root. It is the contract Claude Code follows in every session.

```markdown
# BuildLens — rules for Claude Code

Owner: Nibin (github.com/nibinrj). Dev machine: Windows 11, PowerShell 7, Docker Desktop,
16 GB RAM (about 4-5 GB usable by Docker). Full plan: docs/plan.md. Read it before any work.
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
```

## Architecture

The loop is the point: each build sends its results to BuildLens, and BuildLens decides which tests the next build blocks on.

&#91;embedded content: BuildLens architecture · Phase 1 local, Phase 2 additions dashed\]

On the agent, the Go buildlens CLI fetches the quarantine list before tests and uploads results after every build, and BuildLens writes everything to Postgres for Grafana and PR comments.

| Component | Phase 1 (local Docker) | Phase 2 (AWS) |
| --- | --- | --- |
| Jenkins controller | Container, JCasC from the repo | EC2 from a Packer + Ansible image, SSM access only |
| Agents | One inbound agent container | Spot instances from an Auto Scaling Group, min 0, via the EC2 Fleet plugin |
| BuildLens, Postgres, Grafana | Containers | On the controller instance; Postgres backed up to S3 on demo-down |
| Triggering | Periodic branch scan | Periodic branch scan (no public inbound for webhooks) |
| Infra signals | Log-tail rules | Log-tail rules plus Spot interruption events |
| Prices | Reference price table | Spot price history at build time |

## Data model and detection rules

Every rule is deterministic and configurable in `config.yaml`; defaults below are starting points to tune with real data, not facts.

### Tables (PostgreSQL, SQL migrations)

| Table | Key columns | Notes |
| --- | --- | --- |
| `repository` | id, name, default\_branch | One row per subject repo |
| `build` | id, repository\_id, job\_name, build\_number, branch, pr\_number, commit\_sha, result, started\_at, finished\_at, duration\_ms, agent\_name, agent\_instance\_type, agent\_lifecycle (LOCAL/SPOT/ON\_DEMAND), infra\_failure, infra\_reason, cost\_usd | Unique (job\_name, build\_number): ingest is idempotent |
| `stage_run` | id, build\_id, name, started\_at, duration\_ms, result | From the shared library's timed stages |
| `test_case` | id, repository\_id, module, class\_name, method\_name | Unique per repo + class + method |
| `test_run` | id, build\_id, test\_case\_id, outcome (PASSED/FAILED/ERROR/SKIPPED/FLAKY), duration\_ms, rerun\_failures, failure\_type, failure\_hash, stage (BLOCKING/QUARANTINE) | One row per test per build |
| `quarantine` | id, test\_case\_id, state (QUARANTINED/RELEASED), reason\_rule, evidence (jsonb), quarantined\_at, released\_at, consecutive\_passes, manual | History kept; one active row per test |
| `alert` | id, type (FLAKY/REGRESSION/COST), build\_id, repository\_id, payload (jsonb), created\_at, pr\_comment\_id | Drives PR comments and Grafana |
| `price` | id, instance\_type, lifecycle, region, usd\_per\_hour, effective\_from, source | Config table in Phase 1, Spot price history in Phase 2 |
| `infra_event` | id, instance\_id, event\_type, event\_time, raw (jsonb) | Phase 2: Spot interruption warnings |

### Parsing Surefire and Failsafe XML

- Read every `testcase` element; ignore the suite's top-level totals (known Surefire bug with reruns).
- `flakyFailure` or `flakyError` present and no `failure`/`error` → outcome FLAKY, `rerun_failures` = their count.
- `failure` → FAILED; `error` → ERROR; `skipped` → SKIPPED; otherwise PASSED.
- `failure_hash` = hash of exception type plus the first frame inside the subject's own packages, so the same failure is recognised across builds.

### Infrastructure attribution (runs before flaky detection)

A build is `infra_failure = true` when its log tail or events match one rule. Its test failures are excluded from all flakiness statistics.

| Rule | Signal |
| --- | --- |
| Agent lost | Remoting channel closed or agent disconnected during the build |
| Out of memory | Exit code 137, or OOMKilled in the log |
| Disk full | "No space left on device" |
| Docker unavailable | Cannot connect to the Docker daemon (Testcontainers) |
| Dependency fetch | Maven artifact resolution failure caused by a network timeout |
| Spot interruption (Phase 2) | An `infra_event` for the build's instance within the build window |

### Flaky detection

| Rule | Fires when | Default |
| --- | --- | --- |
| R1 Surefire rerun | Test is FLAKY in at least N builds within the window | N = 2 in the last 30 builds |
| R2 Same commit | Same test PASSED and FAILED on the same commit\_sha, in non-infra builds | Once is enough |
| R3 Flip rate | On the default branch, outcome changed between consecutive runs at least F times in W runs | F = 3, W = 30 |

### Quarantine loop

1. A rule fires → quarantine row created with the rule and its evidence (build ids, outcomes).
2. The pipeline asks `GET /api/v1/quarantine?repo=` before tests and excludes those tests from the blocking stage.
3. A non-blocking quarantine stage runs only quarantined tests; failures mark the stage UNSTABLE, never the build FAILED.
4. Release after K consecutive passes in the quarantine stage (default K = 10).
5. Safety cap: never quarantine more than 5% of a repo's tests or 10 tests, whichever is smaller. Above the cap, alert instead of quarantining.
6. Manual quarantine and release endpoints exist, recorded with `manual = true`.

### Regression detection

For each stage on the default branch, the baseline is the median and MAD of the last 20 successful builds; fewer than 10 samples → "insufficient data", no alert.

```latex
\text{threshold} = \text{median} + \max\left(3 \times 1.4826 \times \text{MAD},\; 0.2 \times \text{median}\right)
```

- Default branch: alert when 2 of the last 3 builds exceed the threshold (sustained, not a one-off).
- Pull request: alert when its latest build exceeds the threshold by 50% or more, or its last 2 builds both exceed it.
- The same check runs on the 20 slowest tests.

### Cost per build

Cost = agent busy seconds ÷ 3600 × `usd_per_hour` for the agent's instance type and lifecycle at build time. Phase 1 uses a reference price for a configured instance type so local builds show an equivalent cost, labelled as such. Phase 2 uses the Spot price history recorded when the build ran.

### Test impact analysis (stretch)

- Changed files from `git diff` against the merge base → owning Maven modules → `mvn -pl <modules> -amd`.
- Full run when the root POM, `.mvn/`, the Jenkinsfile or the shared library changes, or when mapping is uncertain.
- Full run nightly on the default branch. Report selected vs total tests, time saved, and escaped failures (tests that failed on the next full run but were skipped).

## Stack, versions and repo layout

Versions are not listed here on purpose: Claude Code pins each one at build time from official sources and records where it came from (CLAUDE.md rule 2).

| Area | Choice | Why |
| --- | --- | --- |
| Service | Go, net/http, log/slog, pgx v5, sqlc, goose or golang-migrate (P1 decides) | The language of cloud-native tooling; single binary; tens of MB of memory |
| Agent CLI | Go `buildlens` binary with `report` and `quarantine` subcommands, static build | How real CI reporters work; agents need no runtime; keeps the Groovy layer thin |
| Database | PostgreSQL, pinned image | jsonb for evidence, window functions for baselines |
| Tests | `go test` (table-driven, `-race`), testcontainers-go (Postgres), httptest fakes for the GitHub API, JenkinsPipelineUnit for the Groovy wrappers | Every rule tested without real Jenkins or GitHub |
| Code quality | gofmt, go vet, golangci-lint with a committed config | Consistent, idiomatic Go |
| CI server | Jenkins LTS image, plugins pinned in `plugins.txt` via jenkins-plugin-cli, Configuration as Code, Job DSL, Pipeline Graph View | Industry-standard Jenkins as code |
| Agents (Phase 1) | One inbound agent container with JDK 21, Maven, Go and the `buildlens` CLI; Docker socket mounted for Testcontainers (local only) | Builds the Java subject repos and BuildLens itself |
| Dashboards | Grafana with a Postgres datasource, provisioned from files | No clicking |
| PR comments | GitHub REST API via google/go-github, fine-grained token on the lab repo only | One sticky comment per PR, edited in place |
| Benchmark | IDoFT ground-truth dataset, Surefire random run order for order-dependent tests | Precision and recall against real flaky tests |
| Phase 2 | Terraform, Packer with the Ansible provisioner, EC2 Fleet plugin, Auto Scaling Group with Spot, EventBridge, Go Lambdas (aws-lambda-go, aws-sdk-go-v2), AWS FIS, SSM | See Phase 2 |

**Memory budget (Phase 1, target about 3.5 GB):** Jenkins controller 1 GB, agent 1.5 GB (Maven builds), buildlens-server 64 MB limit, Postgres 384 MB, Grafana 256 MB. Claude Code measures with `docker stats` in P5 and reports the real total. The Go service is what freed roughly 450 MB compared with the Spring Boot version.

### Repositories

- `nibinrj/buildlens`: the service, Jenkins as code, shared library, dashboards, docs.
- `nibinrj/buildlens-lab`: a small multi-module Maven project used as the build subject, with deliberately flaky and slow tests (labelled), so detection can be demonstrated on demand.
- Optional real subject: your Quiz repo, added later for honest real-world numbers.

### Layout of `buildlens`

```
buildlens/
  CLAUDE.md
  go.mod  go.sum  .golangci.yml  sqlc.yaml
  Dockerfile                   multi-stage: static Go build -> distroless
  docker-compose.yml  tasks.ps1
  .env.example  .gitattributes  .gitignore
  cmd/
    buildlens-server/          HTTP service (main.go)
    buildlens/                 agent CLI: report, quarantine
    buildlens-mcp/             read-only MCP server (stdio) for AI coding agents
  internal/
    api/                       handlers, auth middleware
    surefire/                  XML parser
    ingest/                    build ingestion, idempotency
    infra/                     infrastructure-failure attribution
    rules/                     flaky rules R1-R3
    quarantine/                quarantine loop, safety cap
    regression/                median/MAD baselines
    cost/                      cost per build
    github/                    sticky PR comments
    verdict/                   per-build verdict and failure tags (served to buildlens-mcp)
    store/                     sqlc-generated queries
  migrations/                  SQL migrations
  queries/                     SQL compiled by sqlc
  testdata/surefire/           real XML fixtures
  jenkins/
    controller/Dockerfile      Jenkins LTS + plugins
    controller/plugins.txt     exact plugin versions
    agent/Dockerfile           JDK 21 + Maven + Go + buildlens CLI
    casc/jenkins.yaml          JCasC: security, credentials, library, seed job
    jobs/seed.groovy           Job DSL: multibranch + benchmark jobs
  shared-library/
    vars/                      timedStage, quarantineAwareTests, reportBuild (call the CLI)
    test/                      JenkinsPipelineUnit tests (the only JVM code)
  grafana/
    provisioning/              datasource + dashboard providers
    dashboards/                flaky.json, regressions.json, cost.json, health.json
  bench/                       IDoFT import + benchmark scoring (Go)
  tools/
    drive-builds.ps1           triggers N builds through the Jenkins API
  docs/
    plan.md  results.md  benchmark.md  mcp.md
    study/  adr/  demo/
  infra/                       Phase 2 only: terraform/, packer/, ansible/, lambdas/ (Go), fis/
```

### Layout of `buildlens-lab`

```
buildlens-lab/
  Jenkinsfile          uses @Library('buildlens')
  pom.xml              parent, modules below
  core/                stable tests
  api/                 depends on core; one slow test
  flaky-lab/           LABELLED injected flakiness: random, time-based, order-dependent
  README.md            states clearly which flakiness is injected and how
```

## Phase 1: the seven prompts (local, Go)

Each prompt is a vertical slice that ends running. Paste each block into Claude Code exactly as written. P1–P5 build the system; P6 measures it against real flaky tests; P7 exposes its verdicts to AI coding agents through a read-only MCP server.

### P1: Design notes and Go skeleton

```text
Prompt P1. Read CLAUDE.md and docs/plan.md fully first.

Part A - study note (before any code): docs/study/P1-design.md covering:
- Surefire/Failsafe XML: testcase, failure, error, skipped, flakyFailure, flakyError, rerunFailure.
  Cite the official Surefire docs page you checked. Explain the known top-level totals bug.
- The exact Surefire syntax to (a) exclude specific test methods and (b) run only specific test
  methods, for current Surefire. Verify in the docs; say which you are certain of. Prefer an
  excludes file if method-level -Dtest negation is unreliable. The CLI will generate this format.
- How the shared library times stages itself (do not use Stage View's REST API).
- JCasC: configuring a global shared library from a folder in this repo (libraryPath or the
  equivalent). If unsupported, say so and propose the alternative.
- Go choices with reasons: sqlc + pgx vs plain pgx; goose vs golang-migrate; project layout.
- Go idioms this project uses, each compared with Java: error values and wrapping, context and
  cancellation, interfaces for test fakes, struct embedding, defer, table-driven tests.
- The flaky, quarantine, regression and cost rules from the plan, restated with edge cases.
- Memory budget per container and the expected total.

Part B - skeleton:
- go.mod (module github.com/nibinrj/buildlens, toolchain pinned), .golangci.yml, sqlc.yaml.
- cmd/buildlens-server: config from env, slog JSON logging, /healthz and /readyz (readyz checks the
  DB), graceful shutdown on SIGTERM with a timeout.
- migrations/: the first migration creates ALL tables from the plan's data model.
- queries/ + sqlc generation into internal/store with a few basic queries.
- Dockerfile: multi-stage, CGO_ENABLED=0 static build, distroless runtime, non-root.
- docker-compose.yml: Postgres, Grafana and buildlens-server (pinned tags, memory limits).
- tasks.ps1: up, down, logs, test, lint.
- A testcontainers-go test proving migrations apply and every table exists.
- docs/adr/001-scope-and-stack.md: why one service, why Go, why Jenkins + Maven only, why no ML.

Done when: gofmt, go vet, golangci-lint and go test -race ./... pass (show output);
docker compose up starts all three (show docker ps and docker stats); list what you did not verify.
```

### P2: Jenkins as code, CLI, ingest (first end-to-end slice)

```text
Prompt P2. Read CLAUDE.md, docs/plan.md and docs/study/P1-design.md first.

Study note first: docs/study/P2-jenkins-cli-ingest.md (JCasC, Job DSL, inbound agents, shared
library structure, multipart upload in Go, idempotent ingest, CLI design with the standard flag
package or one small library - ask before adding one).

Jenkins (jenkins/ only):
- controller/Dockerfile from a pinned Jenkins LTS tag; plugins.txt with exact versions installed by
  jenkins-plugin-cli. Minimum set: configuration-as-code, job-dsl, workflow-aggregator,
  pipeline-graph-view, git, github-branch-source, credentials-binding, junit, timestamper. Ask
  before any other plugin.
- agent/Dockerfile: inbound agent with pinned JDK 21, Maven and Go, plus the buildlens CLI copied
  from a Go build stage.
- casc/jenkins.yaml: local admin from .env, no setup wizard, agent node, credentials from env vars
  (GitHub token, BuildLens ingest key), global library 'buildlens' from shared-library/, seed job.
- jobs/seed.groovy: multibranch jobs for nibinrj/buildlens-lab and nibinrj/buildlens, periodic branch
  scan (no webhooks: Jenkins on localhost is not reachable from GitHub).
- Add jenkins and agent to docker-compose.yml with memory limits. tasks.ps1: jenkins, seed.

CLI (cmd/buildlens, Go):
- buildlens report: flags for server URL, key, job, build number, branch, PR, commit, result, agent,
  stages JSON file, log-tail file and report globs (target/surefire-reports, target/failsafe-reports).
  Multipart POST with retries and backoff. If the server is down it warns and exits 0, unless --strict.
- buildlens quarantine: stub for now (implemented in P3).
- Unit tests with httptest: success, server down, 401, retry then success.

Shared library (shared-library/ only, kept thin):
- vars/timedStage.groovy: wraps stage(), appends name, start, duration and result to stages.json.
- vars/reportBuild.groovy: called in post { always }; writes the last 500 log lines to a file and runs
  buildlens report with build metadata. Never fails the build.
- JenkinsPipelineUnit tests for both (a small test-only Maven or Gradle project; ask which).

Service (Go):
- POST /api/v1/builds (multipart: metadata JSON + XML files + log tail), API-key middleware.
- internal/surefire parser exactly as the plan's parsing rules; fixtures in testdata/surefire for:
  passed, failed, error, skipped, flakyFailure, flakyError, rerunFailure, wrong totals.
- Idempotent: the same job + build number twice updates, never duplicates (test it).
- GET /api/v1/builds/{id} for debugging.

Lab repo: create the buildlens-lab structure from the plan in ../buildlens-lab with a Jenkinsfile
using timedStage and reportBuild. No flaky tests yet. Print the git commands to push; do not push.

Done when: all lint and tests pass; JenkinsPipelineUnit tests pass; tasks.ps1 up brings everything up
with no UI clicks; one manual lab build appears in BuildLens with tests and stage timings (show the API
response and docker stats). List what you did not verify.
```

### P3: Infra attribution, flaky detection, quarantine loop

```text
Prompt P3. Read CLAUDE.md, docs/plan.md and the P1/P2 study notes first.

Study note first: docs/study/P3-flaky-and-quarantine.md (each rule, its false positives, why infra
attribution runs before flaky detection, the quarantine safety cap).

Service (Go):
- internal/infra: attribution rules from the plan, run on ingest from the log tail. Table-driven tests
  with real-looking log samples, including a near-miss that must NOT match.
- internal/rules: R1, R2, R3 with thresholds from config; failures from infra builds excluded.
  Table-driven tests including a genuine break then fix on main (not flaky) and a test failing only
  in infra builds (not flaky).
- internal/quarantine: create with evidence, safety cap, release after K consecutive quarantine-stage
  passes, manual quarantine/release endpoints. GET /api/v1/quarantine?repo= returns the active list.
  New migration if the schema changes.

CLI: buildlens quarantine --repo --format surefire-excludes writes the exclusion file in the syntax
verified in P1, and --format surefire-only writes the run-only list. If the server is unreachable it
writes an empty file and warns (all tests run). Tests for both formats and the unreachable case.

Shared library: vars/quarantineAwareTests.groovy calls the CLI, runs the blocking test stage with
exclusions, then a non-blocking quarantine stage with only those tests (catchError: stage UNSTABLE,
build result unchanged). JenkinsPipelineUnit tests: empty list, non-empty list, server down.

Lab repo: add flaky-lab/ tests, labelled as injected in code comments and README: one random (about
20% failure), one time-based, one order-dependent. Seeded so behaviour is reproducible.

Done when: all lint and tests pass; 20 lab builds show at least one test quarantined and then excluded
from the blocking stage (show the API output and Jenkins console lines). List what you did not verify.
```

### P4: Regression alerts, PR comments, cost per build

```text
Prompt P4. Read CLAUDE.md, docs/plan.md and earlier study notes first.

Study note first: docs/study/P4-regressions-cost-prs.md (median/MAD vs mean/stddev, sustained vs
one-off, minimum samples, cost attribution, sticky PR comments).

Service (Go):
- internal/regression for stages and the 20 slowest tests, exactly as the plan, configurable, with an
  insufficient-data state. Table-driven tests on synthetic duration series: noise only (no alert),
  one spike (no alert), sustained shift (alert), PR with a large jump (alert).
- internal/cost from the price table (migration seeds a reference instance type; source column says
  it is a reference price). Local builds labelled as equivalent cost.
- internal/github with google/go-github: one sticky comment per PR found by a hidden marker, edited on
  later builds; sections for quarantined tests, regressions and cost. httptest fakes including 403
  and 5xx, which must never break ingest. Run PR updates asynchronously with a bounded worker and
  explain the goroutine and context usage in the study note.
- Alerts written to the alert table.

Lab repo: in api/, add a test whose duration a system property can raise, to create a labelled,
controlled regression on a branch.

Done when: all lint and tests pass; a lab PR shows one sticky comment updated across two builds with a
regression and a quarantine section (show the GitHub API response). List what you did not verify.
```

### P5: Build driver, dashboards, results, docs

```text
Prompt P5. Read CLAUDE.md, docs/plan.md and all study notes first.

Study note first: docs/study/P5-measurement.md (how many builds are needed, what each dashboard
answers, what the numbers can and cannot claim).

- tools/drive-builds.ps1: triggers N builds of a job/branch through the Jenkins REST API with a token
  from .env, waits for each, prints a summary. tasks.ps1 drive.
- Grafana dashboards provisioned from files: Flaky (quarantined now, detections over time, by rule),
  Regressions (stage duration vs baseline band, alerts), Cost (per build, branch, stage), Health
  (builds per day, infra failures by reason).
- The buildlens multibranch job builds this repo too (gofmt check, go vet, golangci-lint,
  go test -race) using the same shared library: dogfooding.
- Run 50+ lab builds. Measure docker stats during a build and record the real memory total.
- docs/results.md: tables from real runs only (builds recorded, flaky detections per rule,
  quarantined and released, regressions, infra failures by reason, cost per build, peak memory) and
  a section "Injected vs real" stating exactly what was injected.
- docs/adr/002-detection-rules.md, 003-quarantine-safety.md, 004-local-jenkins-no-webhooks.md.
- README.md: what it is, how to run it, architecture, results link, limitations, prior art.
- tasks.ps1 demo-dump / demo-restore: pg_dump and restore so a demo shows full history instantly.

Done when: tasks.ps1 up then drive 50 produces the numbers in results.md, dashboards show data with no
manual setup, and you list every README claim not backed by results.md.
```

### P6: Ground-truth benchmark on IDoFT

```text
Prompt P6. Read CLAUDE.md, docs/plan.md and all study notes first.

Study note first: docs/study/P6-benchmark.md (what IDoFT is, OD vs non-OD flakiness, why reruns find
only part of the known flaky tests, precision vs recall, what counts as a false positive).

- bench/ (Go): import pr-data.csv from the IDoFT repo pinned to one commit (record the commit). Filter
  to Maven projects and pick 3-5 small modules whose listed SHA builds with an available JDK; show me
  the shortlist with reasons and wait for my approval before running anything.
- Agent images for the JDK versions the shortlist needs (one at a time, memory limits).
- Job DSL: a parameterised idoft-benchmark job (repo URL, SHA, module, JDK label, run order default or
  random, number of runs). It builds only the given module, runs tests, and reports through the CLI
  under repo name idoft/<project>. Add a Surefire random run order option (verify the property).
- Scoring: for each project, compare BuildLens's flagged tests with the dataset's listed tests for that
  module and SHA: true positives, missed, and flagged-but-unlisted. Unlisted flags are NOT counted as
  false positives automatically: list them for my manual check. Report precision and recall overall
  and split by OD vs non-OD.
- docs/benchmark.md generated from the data: dataset commit, projects and SHAs, runs per project, run
  order used, results table, and limitations. tasks.ps1 bench.

Done when: at least 3 projects x 30 runs are scored and docs/benchmark.md is generated from real runs
(show the table and the commands used). List what you did not verify.
```

### P7: Read-only MCP server for AI coding agents

```text
Prompt P7. Read CLAUDE.md, docs/plan.md and all study notes first.

CONTEXT
BuildLens should answer the question an AI coding agent must ask before "fixing" a red build:
is this failure caused by my change, or is it a flaky test, a failure already on the default
branch, or broken infrastructure?
Build buildlens-mcp: a small Go program that exposes BuildLens's existing data to AI coding
agents (such as Claude Code) as read-only MCP tools. Claude Code starts it as a subprocess and
talks to it over stdin/stdout. Existing P1-P6 behaviour does not change: the server only gains
read endpoints and a read-only key scope, both additive. Update CLAUDE.md's "What this is" and
Conventions for the third binary and say why first (rule 8).

TOOLS (all read-only)
1. get_build_verdict(job, build_number)
   Returns: build result; infra failure and reason; each failed test tagged exactly one of
   QUARANTINED, KNOWN_FLAKY (a rule fired but not quarantined, e.g. safety cap),
   FAILING_ON_DEFAULT_BRANCH (same failure_hash already failing on the default branch),
   NEW_FAILURE; any regression alerts; and an overall verdict:
   INFRA | LIKELY_NOT_THIS_CHANGE | LIKELY_THIS_CHANGE | MIXED | INSUFFICIENT_DATA.
   A quarantined or flaky test whose failure_hash does not match its recorded flaky evidence
   is tagged NEW_FAILURE, not QUARANTINED: a known-flaky test can still break for real.
   Agent uses it to: decide whether a red build is its fault before editing code.
2. get_test_history(repo, test_name)
   test_name is the fully qualified class#method, as stored by the parser.
   Returns: last N outcomes (with commit and branch), flaky rule evidence, quarantine state.
   Agent uses it to: judge one failing test.
3. explain_failure(job, build_number, test_name)
   Returns: failure type, failure hash, first seen, how often the same failure hash appeared on
   the default branch, and whether those were infra builds.
   Agent uses it to: spot a recurring failure that predates the change.
4. list_quarantine(repo)
   Returns: active quarantined tests with reason rule and since when.
   Agent uses it to: know which tests to distrust.
5. get_stage_trend(repo, stage)
   Returns: baseline median and MAD, recent durations, active alert, or "insufficient data"
   when below the regression minimum sample.
   Agent uses it to: check whether its change made the build slower.

DESIGN RULES
- Read-only by design. No quarantine or release through MCP: an agent must never be able to
  hide a failing test. Those actions stay with humans.
- Thin client over the BuildLens REST API, not direct database access: one source of truth,
  one auth path, a read-only API key.
- Read-only scope: keys come from env vars (ingest key, read key); the middleware maps a key to
  a scope; read keys get 403 on every write route. No new table needed; if you think one is,
  ask.
- Agent-friendly output: structured JSON plus a one-line plain summary. Summaries state evidence
  and hedge, never certainty, for example: "Likely not this change: test quarantined since
  3 Oct (rule R1), same failure in 4 of 30 runs on unchanged code".
- Results are size-bounded: history N defaults to 30, hard max 100; every string field
  truncated to a fixed length; the cap is reported in the result when it applies.
- Untrusted text: build output (failure messages, log lines) is written by the code under test
  and can contain instructions aimed at the agent. Return no raw log text; return failure
  type and hash, and at most a truncated message in a field named untrusted_message.
- Tool descriptions are written for the agent: each says when to call it and what NOT to
  conclude from it (e.g. "QUARANTINED does not prove this change is safe; a quarantined test
  can still catch a real bug; check the tag, not just the summary").
- Every API call has a timeout and a context; API down returns a tool error the agent can read,
  never a crash or an empty "all clear".

STUDY NOTE FIRST
docs/study/P7-mcp.md: what MCP is (tools, transports, the stdio model), how the official Go SDK
(github.com/modelcontextprotocol/go-sdk) defines a server and typed tools, why this server is
read-only, how tool descriptions steer an agent, and prompt injection through tool results and
how this server limits it. Pin the SDK version and cite its docs. Ask before adding the SDK
module (CLAUDE.md rule 9).

BUILD
- cmd/buildlens-mcp: an MCP server over the stdio transport using the official Go SDK, with
  the five tools above. Typed inputs and outputs; every result has structured JSON plus a
  one-line plain summary; history capped at a configurable N. Logs go to stderr only: stdout
  is the protocol.
- It calls the BuildLens REST API (base URL and a read-only API key from env vars). Add any
  read endpoints the tools need to the server (e.g. look up a build by job + build number),
  with a read-only key scope that cannot call write endpoints (test that it cannot, for every
  write route).
- Verdict logic lives in the server (internal/verdict), not in the MCP binary, so the same
  answer is available to the API, PR comments and Grafana later. The MCP binary formats only.
- No write tools. If you think one is needed, stop and ask.
- Tests: table-driven verdict tests (each tag, the hash-mismatch case, infra build, main already
  red); the SDK's in-memory transports to call each tool end to end; an httptest fake for the
  BuildLens API; cases for unknown build, unknown test, API down, and oversized history.
- docs/adr/006-mcp-read-only.md.
- docs/mcp.md: how to build the binary and register it with Claude Code (check the current
  Claude Code docs for the exact command), a project-scoped config example for the lab repo
  with the key taken from an environment variable (never written into a committed file),
  plus the demo protocol below.

DEMO PROTOCOL (I run it; definitions fixed before any run)
- Unnecessary code edit = any change to production code, test code or build config, or any
  disabling/skipping of a test, when the build's only failures are ones BuildLens tags
  QUARANTINED or INFRA.
- False reassurance = the agent declares a failure not its fault when the failure was
  caused by the change.
- Cases: (a) a lab PR whose only failure is a quarantined injected flaky test; (b) a NEGATIVE
  CONTROL: a lab PR with a real, deliberate bug that breaks a stable test.
- Each case twice: without and with the MCP server. Same model, same prompt
  ("the build failed, fix it"), fresh session each time, same failing console log given in
  both runs. Save all transcripts under docs/demo/ (scrub keys).
- Optional: 10 failing lab builds (mix of quarantined-only, infra, and real-bug cases, mix
  written down before running), with and without MCP; count unnecessary edits and false
  reassurances. Report only real numbers, with n, in docs/results.md under "MCP demo".

DONE WHEN
gofmt, go vet, golangci-lint and go test -race ./... pass (show output); a test builds the
binary, starts it over stdio through the SDK's client and lists its five tools; a read key gets
403 on every write route; docs/mcp.md is complete. List what you did not verify.
```

## Phase 2: Jenkins on AWS with Spot agents

Start only after Phase 1's results.md exists. Each batch is one Claude Code prompt; you run every `terraform apply`, `packer build` and FIS experiment yourself. Nothing billable runs between demos.

**Cost model:** controller and BuildLens on one small on-demand EC2 instance, only during demos; agents on Spot from a mixed-instance Auto Scaling Group with minimum 0; no NAT gateway; logs kept 1–3 days. Claude Code checks every price in the AWS Pricing Calculator in J.1; none are assumed here.

### J.0: You, first (manual)

- Region: the same one you chose for Buzzer.
- AWS Budgets alerts active (reuse Buzzer's); a separate cost-allocation tag `Project=buildlens`.
- Install AWS CLI, Session Manager plugin, Terraform, Packer, Ansible, Go (WSL2 is fine for Ansible).

### J.1: Explain first (no code)

```text
Batch J.1. No code. Write docs/study/J.1-aws-design.md: EC2 Fleet plugin vs EC2 plugin and why ASG;
Spot allocation strategies and capacity-rebalance; how the plugin resubmits interrupted builds and
what that means for BuildLens's infra attribution; SSH agents vs inbound agents on AWS; reaching
the controller only through SSM port forwarding (no public inbound); why that means SCM polling
instead of GitHub webhooks; Packer with the Ansible provisioner; JCasC with AWS Secrets Manager
credentials; VPC with public subnets and no NAT, security groups; pg_dump to S3 so build history
survives demo-down. Price every resource for my region in the Pricing Calculator and flag anything
you cannot confirm.
```

### J.2: Terraform foundation

```text
Batch J.2. infra/terraform only. Remote state in S3 with use_lockfile (new bootstrap folder, survives
demo-down: state bucket + an S3 bucket for pg_dump backups). envs/dev: VPC across 2 AZs, public
subnets, S3 gateway endpoint, no NAT. Security groups: controller has NO inbound rules (SSM only);
agents accept SSH only from the controller SG. IAM: controller role limited to describing and
updating the one agent ASG, terminating instances tagged for it, reading its secrets, writing to the
backup bucket; agent role with SSM only. Launch template (IMDSv2 required, no key pair, AMI from a
variable). ASG: mixed instances, Spot with capacity-optimized allocation, on-demand base 0, min 0,
max from a variable (cost guard). Tags Project=buildlens everywhere. fmt + validate + tflint +
checkov; explain every IAM permission. Print apply commands; do not run them.
```

### J.3: Images with Packer and Ansible

```text
Batch J.3. infra/packer and infra/ansible only. Ansible roles: common (updates, users, time sync,
SSM agent, hardening, IMDSv2 checks), docker, jdk, maven, jenkins_agent (agent user, workspace on a
separate volume, disk cleanup timer), jenkins_controller (pinned LTS + plugins.txt from jenkins/,
JCasC from this repo), buildlens (Docker + compose for BuildLens, Postgres, Grafana). Two Packer
templates using the Ansible provisioner: agent AMI and controller AMI, both pinned base images.
ansible-lint and packer validate must pass. Print build commands; do not run them.
```

### J.4: Controller, JCasC on AWS, EC2 Fleet cloud

```text
Batch J.4. Terraform + jenkins/casc only. Controller EC2 from the controller AMI in a public subnet
with a public IP and no inbound rules. A JCasC overlay for AWS: credentials from Secrets Manager,
EC2 Fleet cloud bound to the agent ASG (label, min 0, max idle minutes, executors), resubmission
enabled. BuildLens records agent instance type and lifecycle per build. tasks.ps1: demo-up
(apply, wait for Jenkins healthy, restore the latest pg_dump from S3, print the SSM port-forward
command), demo-down (pg_dump to S3, destroy, fail loudly if anything tagged Project=buildlens is left
outside bootstrap).
```

### J.5: Spot events and cost guard

```text
Batch J.5. infra/lambdas (Go) + Terraform only. Two Go Lambdas using aws-lambda-go and aws-sdk-go-v2
on the provided.al2023 runtime, each built as a static bootstrap binary, with table-driven go tests
and interface-based fakes for every AWS call:
(1) EventBridge rule for EC2 Spot interruption warnings -> Lambda in the VPC -> POST to BuildLens
/api/v1/infra-events (new endpoint + migration) so builds on that instance are marked infra_failure;
(2) scheduled cost guard: terminates agent instances older than a max age and alarms if running
agents exceed the cap. BuildLens records Spot price history for each build's instance type at build
time (DescribeSpotPriceHistory via aws-sdk-go-v2) into the price table. Explain every IAM permission.
```

### J.6: Chaos, rebuild, runbooks

```text
Batch J.6. docs + infra/fis only. AWS FIS experiment template that sends Spot interruption notices to
agents tagged for this ASG, with a stop condition. docs/chaos.md (PowerShell 7): start a 10-build
burst, run the experiment, show the plugin resubmitting builds and BuildLens marking them as
infrastructure, not flaky. Controller rebuild drill: demo-down, demo-up, time it, confirm jobs, history
and quarantine list are back. Runbooks: agent not connecting, Spot capacity unavailable, disk full,
controller unhealthy. One postmortem template filled from a real drill.
```

### J.7: Platform CI and AWS measurement

```text
Batch J.7. A Jenkins pipeline (in the buildlens multibranch job) that runs tflint, checkov,
ansible-lint, packer validate, gofmt, go vet, golangci-lint, go test -race and the JenkinsPipelineUnit
tests on every PR. Then a measurement run I execute: 50+ lab builds on Spot agents. Add an AWS section
to docs/results.md: cost per build (Spot vs the on-demand price for the same type), agent start time
p50/p95, interruptions observed, controller rebuild time. Real numbers only.
```

**Gate:** docs/adr/005-aws-topology.md (no NAT, SSM-only access, polling instead of webhooks, Spot strategy) and a 5-minute recorded explanation without notes.

## Verification and honesty rules

The project's value is its measured numbers, so every claim must trace back to a run you can repeat.

**After every prompt, you check:**

- [ ] Build and tests ran, and the output was shown, not described.
- [ ] Claude Code listed what it did not verify, and you read that list.
- [ ] No new dependency, plugin or version appeared without a stated source.
- [ ] No secret in the diff (`git diff --cached` before every commit).
- [ ] You read the new tests and can say in one sentence what each proves.

**Results rules**

- Every number in README.md and results.md comes from `docs/results.md`, and every number there comes from a run you can name (date, build range, branch).
- Injected flakiness and controlled regressions are always called injected or controlled. Real findings, if any, go in a separate column.
- Local cost figures are "equivalent cost at reference price", never presented as AWS spend.
- If a rule produced false positives during measurement, report them. A tuned threshold with a known false-positive count is more credible than a perfect-looking table.

**Results table template for `docs/results.md`**

| Metric | Value | Source (run, builds, date) |
| --- | --- | --- |
| Builds recorded |  |  |
| Flaky tests detected (R1 / R2 / R3) |  |  |
| Quarantined / released |  |  |
| False positives found |  |  |
| Infra failures by reason |  |  |
| Regression alerts (true / false) |  |  |
| Cost per build (reference or Spot) |  |  |
| Peak memory, local stack |  |  |
| Benchmark: known flaky tests detected / listed (IDoFT, OD and non-OD) |  |  |
| Benchmark: flagged but unlisted (manually checked) |  |  |
| MCP demo: unnecessary code edits, without / with MCP (n) |  |  |
| MCP demo: false reassurances on real-bug cases, without / with MCP (n) |  |  |

## Prior art, interview prep, study map

The idea is established commercially and uncommon in fresher portfolios; present it as a build-to-understand project, never as novel.

### Prior art to study before interviews

| Tool | What it does | What BuildLens adds or does differently |
| --- | --- | --- |
| [Jenkins Flaky Test Handler plugin](https://plugins.jenkins.io/flaky-test-handler/) | Uses Surefire reruns, reruns failed tests at the failed revision, aggregates pass/fail/flake stats | Auto-quarantine with release, infra attribution, regressions, cost, PR comments |
| [Trunk Flaky Tests](https://trunk.io/compare/trunk-vs-buildpulse) | Detects and quarantines flaky tests from uploaded results | Self-hosted, Jenkins-native, rule-based and explainable |
| [BuildPulse](https://buildpulse.io/products) | Flaky detection from JUnit XML, ranked by cost | Same idea at small scale, plus cost per build |
| [CloudBees Smart Tests](https://www.cloudbees.com/capabilities/cloudbees-smart-tests) (formerly Launchable) | AI test selection, flaky analysis, failure triage | Deterministic rules first; test impact analysis by module graph |
| Gradle Develocity | Build scans, flaky detection, predictive test selection | Free, Maven + Jenkins scope |

### Questions to be ready for

- Why not use one of the tools above? (Paid or hosted; you built the core to understand it, scoped to Jenkins and Maven.)
- Why Go? (The language of cloud-native tooling, a single static binary for agents, tens of MB of memory; your Java is already shown in Buzzer.)
- How do you know it works on real flaky tests? (IDoFT benchmark: precision and recall against a published ground-truth dataset.)
- How do you avoid quarantining a real bug? (Infra attribution first, R2 needs the same commit, safety cap, quarantined tests still run.)
- Why median and MAD instead of mean and standard deviation? (Robust to one-off spikes in CI timings.)
- Why no ML? (Rules are explainable and testable; ML only where rules measurably fail.)
- Why is the MCP server read-only? (An agent must never be able to quarantine a test to turn its own build green; quarantine and release stay with humans.)
- Could a malicious test trick the agent through your MCP tools? (Build output is untrusted: no raw logs returned, messages truncated and labelled untrusted, read-only key.)
- What happens when a Spot agent is reclaimed mid-build? (Plugin resubmits; BuildLens marks it infrastructure, not flaky.)
- Why is the controller disposable? (JCasC, Job DSL and plugins.txt in git; history restored from S3.)

### Study-later map

| Batch | Study before claiming it |
| --- | --- |
| P1 | Go basics: errors, context, interfaces, modules, defer, table-driven tests; Surefire reports; SQL migrations; testcontainers-go |
| P2 | Jenkins controller/agent model, JCasC, Job DSL, shared libraries; multipart upload in Go; CLI design; idempotency |
| P3 | Flaky test causes (time, order, concurrency, environment); each rule's false positives |
| P4 | Median, MAD, robust statistics; goroutines and bounded workers; GitHub REST API; cost attribution |
| P5 | Grafana provisioning; what your numbers do and do not prove |
| P6 | IDoFT and iDFlakies methodology, OD vs non-OD flakiness, precision vs recall |
| P7 | MCP (tools, stdio transport, the Go SDK), API key scopes, prompt injection through tool results, designing a fair with/without experiment |
| J.1–J.2 | VPC, subnets, routing, security groups, IAM least privilege, Terraform state |
| J.3 | Linux: systemd, users, permissions, disks, cloud-init; Ansible roles; Packer |
| J.4–J.5 | Auto Scaling and Spot, EventBridge, Go Lambdas in a VPC, SSM port forwarding |
| J.6–J.7 | FIS, runbooks, postmortems, CI for infrastructure code |

### Sources (checked 2026-10)

- [Surefire: rerun failing tests](https://maven.apache.org/surefire/maven-surefire-plugin/examples/rerun-failing-tests.html)
- [SUREFIRE-1627: wrong totals with reruns](https://issues.apache.org/jira/browse/SUREFIRE-1627)
- [Jenkins Plugin of the Month: Pipeline Graph View](https://www.jenkins.io/blog/2026/04/06/plugin-of-the-month/)
- [EC2 Fleet plugin](https://plugins.jenkins.io/ec2-fleet/) and [its configuration options](https://github.com/jenkinsci/ec2-fleet-plugin/blob/master/docs/CONFIGURATION-OPTIONS.md)
- [International Dataset of Flaky Tests (IDoFT)](https://github.com/TestingResearchIllinois/idoft)
- [iDFlakies paper (ICST 2019)](https://mir.cs.illinois.edu/marinov/publications/LamETAL19iDFlakies.pdf)

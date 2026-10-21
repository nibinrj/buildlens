# Build driver, dashboards and measurement

Started 2026-10-19 before implementation. Read the plan, prior study notes and
existing SQL, Compose, Job DSL, shared-library and local verification scripts.

## What is established

The service stores build/test/stage observations, quarantine history, regression
evidence and reference cost in Postgres. The live two-build PR comment gate is
verified. Grafana is already installed with a pinned image and a 384 MiB limit,
but has no provisioned datasource or dashboards. `tasks.ps1 drive` currently runs
the earlier snapshot experiment; it is not yet a general Jenkins build driver.
The configured stack limits total 3392 MiB. No new service, plugin or Go module
is required for this work. Retain every existing version pin.

## Official sources checked

Checked on 2026-10-19:

- [Jenkins remote API](https://www.jenkins.io/doc/book/using/remote-access-api/)
  and [scripted authentication](https://www.jenkins.io/doc/book/system-administration/authenticating-scripted-clients/):
  authenticate with user/API token, submit a build once, follow its queue item
  to the actual executable, then poll that executable. A POST with an uncertain
  outcome must not be retried blindly, because it can create duplicate builds.
- [Grafana provisioning](https://grafana.com/docs/grafana/latest/administration/provisioning/)
  and [Postgres provisioning](https://grafana.com/docs/grafana/latest/datasources/postgres/configure/):
  provision datasource secrets through environment variables, use a read-only
  database role, and load dashboard JSON from mounted files. UI edits are disabled.
- [Postgres query editor](https://grafana.com/docs/grafana/latest/datasources/postgres/query-editor/):
  time-series queries return a `time` column and are ordered by time. Execute each
  real dashboard target through Grafana's query API to verify field types and SQL.
- PostgreSQL 18 [pg_dump](https://www.postgresql.org/docs/18/app-pgdump.html)
  and [pg_restore](https://www.postgresql.org/docs/18/app-pgrestore.html):
  custom archives support restoration into a separate database. Validate there
  before offering an owner-controlled replacement of the active demo database.

## Measurement design

The Jenkins check runner reuses the Go 1.27.1 test image and golangci-lint 2.14.0;
the latter's upstream image index digest was verified through the registry as
`sha256:ad862ba6b3798cbe0fd9fd7408d498fd74fbd2623a92406b2fd3898faf0bf98f`.
The agent gains the Docker CLI from official `docker:29.8.0-cli-alpine3.24`,
documented on Docker Hub and registry-verified as
`sha256:eccaacfeed644c7de222ff047483568cb988dde95476fbaaf10ea2d04921bb66`.
No floating tag is used. The local Docker socket mount supplies host-equivalent
control and is limited to trusted local jobs; fork discovery remains disabled.
The temporary CI runner is limited to 1536 MiB and a test Postgres to 384 MiB,
bringing combined configured limits to 5312 MiB while checks run. Run these after
the measured batch. Neither CLI image is added as a long-running service.

Fifty builds demonstrate repeated execution and an observable quarantine loop;
they do not establish production precision/recall or a universal latency estimate.
Preserve batch start/end, queue IDs, actual executable numbers, upload IDs, source
identity and failures. Keep historical totals separate from the new batch.
No race tests run concurrently with the measured lab batch. Sample all five stack
containers during builds and report the maximum observed sum, sampling interval
and any gaps. A sampled peak is a lower bound on an instantaneous peak; configured
limits are separate numbers. Exclude unrelated Docker stacks explicitly.

| Dashboard | Question answered |
| --- | --- |
| Flaky | Which tests are quarantined now, and which rules generated detections? |
| Regressions | How do measured stage durations compare with their stored baseline/threshold? |
| Cost | What reference cost was recorded per build and branch, and how is it attributed to stages? |
| Health | How many builds occurred each day, what results occurred, and what infrastructure reasons were recorded? |

Stage cost is a duration-weighted attribution of a build's known cost to recorded
stages, not independently metered agent usage. Unknown build cost stays unknown.
Queries must avoid join multiplication, preserve missing data and restrict dates
and repository explicitly. Current quarantine is labelled current rather than a
historical count selected by the dashboard time range.

## Go and Java comparison

The driver and provisioning use PowerShell and existing SQL rather than adding a
Go framework. Existing Go handlers continue to use explicit context/deadline and
error values (similar to passing cancellation and checked exceptions in Java).
The driver uses a single sequential loop rather than goroutines or a Java thread
pool: the local Jenkins agent has one executor, so extra concurrency adds queueing
without increasing capacity. Each request and each queued/running build has a
deadline, like an HTTP timeout plus an overall `Future.get(timeout)` deadline.

## Planned verification

Live execution exposed a wrapper prerequisite: the inbound image has no `unzip`.
The checked wrapper switches its ZIP URL to TAR.GZ when unzip is missing, but
still compares with the pinned ZIP hash. An independent ZIP download matched
both the configured SHA-256 and Maven Central's SHA-512, so validation was not
disabled or changed. Jenkins uses the already checksum-pinned Maven 3.9.16 in
the agent image for library tests; Windows local checks keep the Maven wrapper.
The first archive run also lost its executable bit; using the agent's pinned
Maven resolves that portability issue without changing Git file modes.

Exercise successful and failed driver paths without external writes; provision
Grafana without UI configuration; execute every panel query; run 50 actual lab
builds; export real result tables and sampled memory; test dump/restore into a
separate database and compare contents; run the required formatter, vet, lint,
race suite and shared-library tests. Uncommitted Jenkinsfile work can be exercised
through a reviewed local Job DSL adapter; the normal GitHub multibranch checkout
can only verify that new file after an owner commit/push. Do not claim that live
gate passed from local execution.

## Verified local outcome

On 2026-10-19, the final token-authenticated batch completed fifty successful
Jenkins builds, IDs 105–154. Every queue/executable/upload tuple also matched
the stored job, number and result. The earlier fifty-build run is retained
separately. The exact source manifest, UTC interval and SQL exports are in
`docs/evidence/P5/`; generated tables are in `docs/results.md`.

The maximum sampled simultaneous total was 1626.11 MiB across 286 complete
samples, with no sample errors. This is an observed peak, not a guarantee that
no shorter peak occurred. The fixed 300 ms delay still produced 52 regression
alert rows, including incidental timing shifts and comparisons with prior
history. This is evidence of sensitivity to local timing variation, not a
production accuracy result. No new quarantine activation or release occurred
in this batch; the previously quarantined injected test remained flaky.

All four provisioned dashboards rendered in the signed-in browser, and all
22 panel/variable queries passed. The reader's attempted UPDATE was denied.
The default Git-backed lab job also passed a separate one-build driver smoke
test. A custom dump restored into a separate database with matching row counts
and content fingerprints for all eleven tables. Live replacement was not run.

The exact new Jenkinsfile passed through a hashed local Jenkins adapter,
including static binaries, formatting, vet, lint, Go race tests and 29 pipeline
tests. The normal BuildLens GitHub multibranch check remains open until the
owner commits and pushes the new file. The closing local test outputs are
recorded separately from the measured batch so their containers do not alter
its memory results. Credentials, private consoles and dumps remain ignored.

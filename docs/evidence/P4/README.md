# Regression, reference cost and PR summary verification

Verified locally on 2026-10-16. The controlled slow test and the pre-existing
flaky lab behavior are **injected**. These are local demonstration results,
not production performance or precision measurements.

Implementation and local verification are complete. The plan's live GitHub PR
acceptance gate remains open: no PR comment was published in this run.

## Real Jenkins runs

[builds.csv](builds.csv) contains 19 successful Jenkins builds and their actual
BuildLens IDs, busy durations, cost snapshots and controlled-test observations.
[database-audit.csv](database-audit.csv) independently exports the stored cost and
alert counts. All 19 builds have one COST record; the combined run contains 13
REGRESSION records, which are alert rows rather than distinct affected tests.

| Job | Build range | BuildLens IDs | Input and purpose |
| --- | --- | --- | --- |
| buildlens-regression-local-20261016-145208 | 1–15 | 30–44 | 13 baseline builds at injected 300 ms, then two at injected 3000 ms |
| buildlens-regression-local-20261016-150224 | 1–2 | 45–46 | Reuse stored history; verify baseline durations and build IDs in evidence |
| buildlens-regression-local-20261016-150934 | 1–2 | 47–48 | Final deployed source and isolated per-job evidence directories |

The last two runs reuse repository history; they are not fresh baselines. Existing
successful main-branch history also contributes where an older comparable metric
exists. The newly introduced controlled test supplies its own new samples.
Exports include only responses whose job matches the corresponding source manifest.

Observed progression for `lab.api.ControlledSlowTest#controlledWork`:

| Initial job build | Actual duration ms | Baseline samples | Threshold ms | Result |
| --- | ---: | ---: | ---: | --- |
| 12 | 303 | 9 | no baseline yet | INSUFFICIENT_DATA |
| 13 | 302 | 10 | 363 | NORMAL |
| 14 | 3001 | 11 | 362.4 | NORMAL: one spike |
| 15 | 3004 | 12 | 363 | REGRESSION: sustained |

Full authenticated response bodies are preserved in initial-build-12.json through
initial-build-15.json, with authorization headers omitted. The final run's two
responses are [final-build-1.json](final-build-1.json) and
[final-build-2.json](final-build-2.json). They include exact baseline/recent build
IDs and duration samples. The final controlled-test threshold was 363.6 ms with
16 baseline samples. Its observed duration was 6263 ms. The final Test stage also
alerted at 11443 ms against a 9890.0269 ms threshold.

Requested sleep duration and reported test duration are separate fields. In the
final run Surefire reported 6325 and 6263 ms despite the requested 3000 ms delay;
the console corroborates the slower elapsed test time. The cause of that extra
elapsed time was not isolated. Local compilation and verification shared the host
with Jenkins, so this is not an isolated performance benchmark.

## Noise and limitations observed

[regressions.csv](regressions.csv) preserves every REGRESSION result across these
runs. Seven alert rows concern unchanged fast tests outside the controlled target:
CalculatorTest#divideRejectsZero, SlugsTest#makesSlugs(String, String)[2], and
OrderTest#polluter. The other six rows comprise five controlled-test alerts and
one Test-stage alert. Stable inputs do not prevent environmental timing variation;
these extra alerts expose sensitivity in the starting thresholds, especially for
short tests. No production false-positive rate is inferred or claimed. The plan's
formula was retained rather than tuned to make this experiment look cleaner.

## Source identity and cost

Each run used a hashed local lab/library snapshot and actual Jenkins execution.
Sources remained uncommitted. The snapshot SHA-1 used as tested_tree_sha is an
**archive content identifier, not a Git tree object**. The snapshot SHA-256 and
base commits are in the three source manifests. The first run predates the final
provenance fields; the final manifest covers the deployed final source. Every
recorded service/query/test source hash in the final manifest matched the checkout
when this evidence was collected.

Cost is calculated from an explicit allocated-agent busy timer. It is labelled
**equivalent cost at reference price**, using the dated reference source recorded
in each API response. For example, final build ID 48 recorded 24073 busy ms and
$0.000140 equivalent cost. This excludes upload time after measurement, storage,
networking, idle capacity and burst CPU credit charges. It is not AWS spend.

[console-excerpts.txt](console-excerpts.txt) shows the actual Maven property, test
duration and successful report upload. [memory-snapshot.txt](memory-snapshot.txt)
is one observation after the builds, not a peak-memory claim. Configured stack
limits remain 3392 MiB. The Go service remains a static binary in distroless.

## HTTP/Postgres/comment simulation

[http-comment-test.txt](http-comment-test.txt) records a separate local automated
test with real Postgres and simulated GitHub HTTP endpoints. It ingests builds
through the authenticated multipart API, loads the durable outbox, creates one
comment with HTTP 201, then updates the same comment with HTTP 200 after the next
build. Both comments contain quarantine, regression and reference-cost sections.
The comment ID 91 and its durations are **synthetic test data**, not a GitHub PR.

The [comment preview](comment-preview.md) was extracted from that test's actual
rendered output. Client and integration tests also cover 403/429/5xx, cancellation,
uncertain POST recovery, pagination, copied markers, bounded responses, corrected
uploads, concurrent claims, expired leases and cooldowns surviving new uploads.

## Fresh checks

[lint.txt](lint.txt): formatting clean, vet success, pinned golangci-lint reports
0 issues. [tests.txt](tests.txt): every tested Go package passes
`go test -race -count=1 -p 1 ./...`; JenkinsPipelineUnit reports 25 tests with zero
failures and errors. Migration tests apply, roll back and reapply the real schema.

`./tasks.ps1 up` was verified with sequential image builds, retaining all existing
version pins and memory limits. GitHub publication was explicitly disabled during
local Jenkins verification. No agent Git staging, commit or push occurred.

## Owner steps to finish the live PR gate

The repository rules reserve staging, committing and pushing to the owner. Review
the changes and the comment preview first. The following commands are suggestions
for the owner to run; they have not been executed by the assistant.

Publish the service/shared-library changes:

```powershell
cd D:\dev\BuildLens
git add -A -- . ':(exclude)AGENTS.md' ':(exclude)PROJECT_GUIDE.md' ':(exclude)docs/study/project-guide-review.md'
git diff --cached
git commit -m "Add duration regression alerts, reference cost, and durable PR summaries"
git push
```

Publish the prepared lab change on a demonstration branch and create its PR:

```powershell
cd D:\dev\buildlens-lab
git switch -c controlled-duration-regression
git add -- Jenkinsfile README.md api/pom.xml api/src/test/java/lab/api/ControlledSlowTest.java
git diff --cached
git commit -m "Add a controlled duration regression demonstration"
git push -u origin controlled-duration-regression
gh pr create --repo nibinrj/buildlens-lab --base main --head controlled-duration-regression --title "Demonstrate controlled duration regression" --body "Adds an explicitly injected slow test with a validated delay parameter and measured agent busy time."
```

Add a separate fine-grained token in the ignored `.env` as BUILDLENS_GITHUB_TOKEN,
restricted to nibinrj/buildlens-lab with Metadata read and Pull requests read/write.
Keep Jenkins's GITHUB_TOKEN read-only. Never paste either token in chat.

Provide the PR URL and authorize the sticky-comment demonstration there. Then
recreate the service with the token enabled, index the lab through the existing
Job DSL setup, and run two PR builds with SLOW_MILLIS=10000. Existing local main
history supplies a dated reference; for a fresh database, first collect at least
13 comparable main builds with the controlled test present. Capture both comment
API responses and confirm the comment ID is unchanged and both required sections
are present. These steps remain outstanding.

## Other unverified paths

The final source has not run through normal GitHub multibranch/global-library
checkout, because it is uncommitted. Naturally occurring duration regressions,
production threshold quality, sustained load, real AWS billing, multi-agent cost
attribution, and a real GitHub permission/rate-limit incident are unverified.
Local HTTP simulations do not substitute for the outstanding live PR gate.

Reproduce a fresh local demonstration with `./tasks.ps1 regression-demo`; it appends
real builds and stored history. `./tasks.ps1 regression-demo 0` reuses existing
history for two slow builds. Neither command resets the database or publishes PR
comments. See [the study note](../../study/P4-regressions-cost-prs.md) and
[ADR 002](../../adr/002-regression-cost-comments.md) for design and limitations.

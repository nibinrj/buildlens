# Regression alerts, reference cost, and pull request comments

## Live PR verification follow-up (2026-10-19)

Before execution: the owner published BuildLens commit 70f1fc6, opened lab PR #1,
saved the separate comment token locally and explicitly authorized creating and
updating its sticky comment. Verify token presence without displaying its value.
Use the existing Job DSL multibranch job and Git-backed shared library, retaining
all container limits. Run two PR builds with the labelled injected 10000 ms delay;
save Jenkins, authenticated BuildLens and GitHub comment response snapshots after
each build, and compare the real comment IDs and required sections. The existing
local main history is a dated baseline, not a fresh performance benchmark.
The official GitHub issue-comment API and fine-grained-token instructions were
checked during token setup. No new dependencies or versions are needed here.

Started 2026-10-16 before implementation.

## Established requirements

The plan requires median/MAD baselines from the last 20 successful default-branch
builds, at least 10 baseline samples, sustained stage and slow-test regression
checks, reference cost per build, and one sticky GitHub comment per pull request.
These defaults are policy thresholds, not measured production results.

The previous milestone's schema already includes stages, test durations, prices,
alerts and pull request numbers. Inspect the SQL and ingest transaction before
deciding what additional migration is needed. Preserve transactional ingest,
retry safety, quarantine behavior and bounded network calls.

## Checks before implementation

Read the earlier study notes and existing decisions. Verify the approved GitHub
client version, official GitHub comment API and permissions, reference-price
source, and Go cancellation/concurrency documentation. Record checked sources
below; an unchecked version or price must never be treated as verified.

## Design to explain and test

Median is the middle sorted duration; MAD is the median absolute distance from
that median. A few very slow builds influence these less than mean and standard
deviation. Exclude the build under evaluation from its baseline and retain the
sample count and threshold in alert evidence. Missing history is insufficient
data, not evidence that performance is normal.

In Go, a goroutine runs a function concurrently, while a bounded channel limits
queued work. Together they play a role similar to a bounded Java executor.
Use service lifetime cancellation rather than an HTTP request context for work
that must outlive ingest. Database state must retain pending work when the
in-memory queue is full or the process exits. Tests must cover shutdown, retry,
duplicate uploads, HTTP 403/5xx, and failure isolation from ingest.

Local cost must be labelled equivalent cost at a reference price. Missing prices
must remain unknown. A stored zero must not conceal a missing rate.

## Sources checked and implementation choices

Checked on 2026-10-16:

- [Official go-github v89.0.0 release](https://github.com/google/go-github/releases/tag/v89.0.0)
  and its pinned [module file](https://github.com/google/go-github/blob/v89.0.0/go.mod).
  Existing D-016 approves this named dependency. The SDK uses NewClient options
  and returns an error; its go-querystring dependency is v1.2.0.
- [GitHub comments API](https://docs.github.com/en/rest/issues/comments): PR
  conversation comments use the issue-comment endpoints. The fine-grained
  BuildLens token needs Pull requests read/write and Metadata read on the lab.
  It is separate from Jenkins's read-only token. HTTP 403, 429 and 5xx are deferred
  work, never grounds to roll back ingest.
- [AWS T3 product table](https://aws.amazon.com/ec2/instance-types/t3/): the checked
  Linux/Unix us-east-1 t3.small reference rate is $0.0209/hour. The migration dates
  its applicability from 2026-10-16. This is a cited price input, not a measured
  cloud bill or a claim about earlier prices.
- Go [context](https://pkg.go.dev/context) and [HTTP client](https://pkg.go.dev/net/http#Client):
  request contexts and Client.Timeout bound outbound calls. The worker derives
  cancellation from service lifetime; a request disconnect cannot discard its
  already committed outbox row.

The full choices and limitations are in [ADR 002](../adr/002-regression-cost-comments.md).
Evidence snapshots include the exact baseline durations and baseline/recent build
IDs, rather than relying on whatever corrected history happens to exist later.
In particular, the evaluation cohort is excluded from the baseline, which means
13 comparable main builds provide 10 baseline samples and three held-out runs.
The test checks rank the current build's 20 slowest passed blocking tests.
Failed, skipped, flaky and quarantine observations are not comparable duration
samples. A missing metric breaks consecutive evidence. Noise, insufficient
history, one spike, sustained shifts and PR jump boundaries have explicit tests.

An allocated agent starts its busy timer immediately inside node(), excluding
the earlier queue. reportBuild sends that duration through --agent-busy-ms.
Older uploads without it retain unknown cost. For this single-allocation lab,
the interval ends just before the report command executes, so report upload time
is excluded; this is not complete multi-agent capacity accounting.

## Additional Go idioms for a Java reader

math/big.Rat avoids converting USD to binary floating point. It resembles Java
BigDecimal for the purpose of exact money calculations, but stores a numerator
and denominator; quotient/remainder rounding is explicit and happens once.
The regression calculations use float64 because median/MAD thresholds are
statistical quantities rather than stored monetary values.

The worker's buffered channel contains wake signals, not the authoritative work.
A non-blocking select/default coalesces repeated notifications. Postgres keeps
the desired comment generation, so a full channel cannot lose a build. A goroutine
runs the loop; a done channel lets shutdown wait for its exit, like joining an
executor task. context.WithTimeout bounds each attempt and each persistence step.

A database lease is different from holding a transaction open during an HTTP
call. Claim commits first, then network work runs without blocking ingest's row
updates. A random token rejects stale completion after another worker reclaims
an expired lease. A generation number prevents completion of an older request
from swallowing a newer summary, similar to optimistic version checks in JPA.
The real-Postgres tests exercise competing claims and crash-style lease recovery.

The SDK's error responses are converted into a small status/timing value before
logging. Remote response text is intentionally absent because it can echo private
data. Comment search also checks the author, so an uploaded/copied marker cannot
make BuildLens edit someone else's comment.

## Verification

On 2026-10-16 the final formatter, vet and pinned lint checks passed, and all Go
packages passed under the Linux race detector. JenkinsPipelineUnit ran 25 tests,
with zero failures and errors. Real-Postgres coverage includes migration rollback,
baseline eligibility, retry correction, exact price selection, durable outbox
claims, stale acknowledgements and rate-limit cooldown across newer uploads.

Nineteen actual local Jenkins builds succeeded across three explicitly bounded
jobs. The initial 13 baseline builds used an injected 300 ms delay; the next two
used 3000 ms. The controlled test transitioned from insufficient data to normal,
then one non-alerting spike and a sustained regression. Four additional slow
builds verified evidence provenance and final source/reproduction behavior.
Seven incidental alert rows on unchanged fast tests are retained as a sensitivity
limitation. They are not hidden or converted into a production precision claim.

The [reviewed evidence](../evidence/P4/README.md) includes actual API responses,
all build/alert counts, source hashes, console excerpts, cost values and full
check output. A separate HTTP/Postgres test creates and updates one simulated
GitHub comment, with a rendered preview. It does not publish a real PR comment.

Publication was disabled for the October 16 local Jenkins runs. On October 19,
after owner commits/pushes, token setup and explicit publication authorization,
normal Git-backed multibranch PR builds #3 and #4 succeeded (BuildLens IDs 50/51).
They created and updated the same real comment, ID 6261856291, with regression,
quarantine and reference-cost sections. Outbox delivered generations advance
from 1 to 2. The injected controlled test reports 10001/10003 ms against 363.6 ms.
See the [live evidence](../evidence/P4/live-20261019/README.md) for GitHub GET
responses, console logs, exact source identity and successful delivery assertions.

The retained baseline comes from older local snapshot jobs. Comparing their
snapshot-copy Checkout stage with Git-backed checkout causes an incidental
Checkout alert; it is not a source-code regression. Another short unchanged test
alerts once and returns to NORMAL. Production threshold quality, sustained load,
real AWS cost and multi-agent accounting remain unverified. The live PR gate
is verified; no merge was performed.

# Duration evidence, reference cost and durable PR summaries

Date: 2026-10-16. Status: accepted for local implementation.

## Context

Build ingest must remain transactional. Retrying an upload must replace its
analysis without duplicating alerts. GitHub may reject or time out a request;
those failures must not undo an otherwise valid build upload.

## Decisions

- Use the plan's median/MAD formula and configurable default window of 20, minimum
  10 samples, MAD multiplier 3, minimum relative increase 0.2 and PR jump 0.5.
  These are policy defaults rather than production measurements.
- Baselines use successful non-infrastructure default-branch builds strictly
  outside the evaluation cohort. Default-branch evaluation holds out its latest
  three eligible builds; PR evaluation holds out its latest two builds. No future
  build can enter an older build's baseline. Ordering is started_at, then build ID.
  A first complete main-branch cohort therefore needs 13 eligible builds.
- Compare successful stages and passed blocking tests. Combine repeated same-name
  stages within one build. Evaluate the current build's 20 slowest eligible tests,
  with deterministic ties by test case ID. Missing observations break consecutive
  evidence; insufficient baseline samples produce an explicit state.
- Persist normal, insufficient and alert evidence in build.analysis.
  Baseline durations and the baseline/recent build IDs accompany the statistics
  so a later upload correction cannot erase the explanation of an earlier alert.
  Replace REGRESSION and COST records for that build on retry. Quarantine history retains
  its existing lifecycle semantics. Historical corrections do not automatically
  re-evaluate every later build.
- Measure allocated-agent busy milliseconds explicitly, beginning inside node()
  and ending when the report command is assembled. Never substitute total Jenkins
  build duration. The current lab has one agent allocation. Multi-agent accounting
  requires additional interval attribution and is outside this local model.
- Calculate money with exact rational arithmetic and round once to six USD
  decimal places. Snapshot the applicable rate, source and effective time.
  Missing duration/rate stays null; unrepresentable amounts stay unavailable.
- Seed only a dated LOCAL reference rate: Linux/Unix t3.small in us-east-1,
  $0.0209/hour, checked on the official AWS T3 page on 2026-10-16. Its effective
  time is that date, not an invented historical price. Equivalent compute cost
  excludes storage, networking, idle capacity and burst CPU credit charges.
- Store one durable desired summary per repository and PR. Coalesce wakes into a
  one-slot channel; one worker processes at most 16 jobs per batch, polls every
  two seconds, and gives each attempt a 20-second deadline. Database leases expire
  after 90 seconds; random lease tokens fence stale acknowledgements. A newer
  generation cannot be acknowledged by an older upload's completion.
- Retry remote failures with bounded exponential backoff and respect server
  rate-limit timing, including when newer uploads arrive during a cooldown.
  Preserve the outbox across restart, queue saturation and
  failed acknowledgements. Never log tokens or GitHub response bodies.
- Find the sticky marker only on comments authored by the authenticated identity,
  including subsequent pages. Search at most 20 pages of 100 comments before
  refusing to create a possible duplicate. Limit response reads to 2 MiB and
  rendered comment bodies to the service's bounded text budget.
- Allow publication only to explicitly configured repositories, using a separate
  BuildLens token. Closed PRs are acknowledged without a write. Summaries name
  the tested commit, which may be a Jenkins merge commit. A summary can describe
  the previous head until the next build finishes; it does not assert that the
  current PR head was tested. Uploaded labels are escaped as text.

## Sources and dependencies

The existing dependency decision permits google/go-github. Pin v89.0.0 from its
[official release](https://github.com/google/go-github/releases/tag/v89.0.0);
its [go.mod](https://github.com/google/go-github/blob/v89.0.0/go.mod) requires
go-querystring v1.2.0. Existing toolchain and infrastructure pins are retained.
This selects a verified stable release without claiming it is the newest.

GitHub's [issue comment API](https://docs.github.com/en/rest/issues/comments)
supports PR conversation comments and accepts Pull requests write permission.
The Jenkins token remains read-only. Use a fine-grained BuildLens token restricted
to the lab, with Metadata read and Pull requests read/write.

The reference rate is from [AWS T3 product details](https://aws.amazon.com/ec2/instance-types/t3/).
Go [context](https://pkg.go.dev/context) and [HTTP client](https://pkg.go.dev/net/http#Client)
documentation establish cancellation and request timeout behavior. Sources were
checked on 2026-10-16.

## Consequences and limits

An ingest response does not wait for GitHub. Outbox age, attempts and last_error
are inspectable in Postgres. Remote creation followed by a lost acknowledgement
is recovered by searching for the existing marker; pathological remote read
consistency or overlapping attempts beyond a lease deadline can still require
human duplicate review. No distributed transaction spans Postgres and GitHub.

Short histories, changed stage composition, environmental changes and test
selection changes can affect comparisons. Test-duration reports describe the
attempt Surefire reports, not necessarily the cumulative cost of reruns. Reference
cost is not AWS spend. Passing synthetic tests is not a production precision,
throughput or reliability measurement.

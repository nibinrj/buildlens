# Local measurement results

Measured on 2026-10-19. Lab flakiness and controlled delays are **injected**. These are actual local Jenkins runs, not production precision or performance benchmarks.

## New batch

Job: `buildlens-regression-local-20261016-150934`. UTC interval: 2026-10-19T07:39:30.8290540Z to 2026-10-19T08:01:17.3960692Z. Driver requested 50 builds and verified every queue/executable pair and BuildLens upload. The initial failed preflight is separate from this completed batch.

| Observation | Measured result |
| --- | ---: |
| Recorded batch builds | 50 |
| SUCCESS | 50 |
| UNSTABLE | 0 |
| BuildLens ID range | 52-101 |
| Infrastructure-attributed builds | 0 |
| Known / unknown cost builds | 50 / 0 |
| Equivalent reference USD | 0.004501 |
| Equivalent USD per build / min, median, max | 0.000079, 0.000090, 0.000115 |
| Agent busy milliseconds / sum | 775779 |
| Stored build duration ms / min, median, max | 13740, 15679, 20367 |
| Quarantine activations during interval | 0 |
| Quarantine releases during interval | 0 |
| Active quarantine at export | 1 |

Historical lab total at export: **100** stored builds; **4** quarantine activations and **3** releases. These totals include earlier experiments and are separate from the new batch.

## Test observations and rule evidence

| Stage | Outcome | Observations |
| --- | --- | ---: |
| BLOCKING | PASSED | 650 |
| QUARANTINE | FLAKY | 50 |

| Rule | Evidence occurrences during batch |
| --- | ---: |
| R1 | 0 |
| R2 | 0 |
| R3 | 0 |

Rule counts combine automatic activations and cap alerts, counting each retained rule finding. They are not distinct tests or accuracy scores. A release can concern a quarantine activated before this batch.

## Regression and infrastructure observations

| Kind | Metric | Alert rows in new batch |
| --- | --- | ---: |
| STAGE | Build | 6 |
| STAGE | Checkout | 8 |
| STAGE | Test | 1 |
| TEST | lab.api.ControlledSlowTest#controlledWork | 1 |
| TEST | lab.api.PriceServiceTest#emptyOrderIsZero | 2 |
| TEST | lab.core.CalculatorTest#addRejectsOverflow | 2 |
| TEST | lab.core.SlugsTest#makesSlugs(String, String)[1] | 2 |
| TEST | lab.core.SlugsTest#makesSlugs(String, String)[2] | 5 |
| TEST | lab.core.SlugsTest#makesSlugs(String, String)[3] | 7 |

The batch retained a fixed requested delay of 300 ms yet generated **34** regression alert rows. These include incidental timing shifts and comparisons with older history. No new deliberate slowdown was introduced in this batch, and no production accuracy claim is inferred. Exact per-alert baseline/current values are retained in the evidence.

No infrastructure failure reason was recorded in this batch. Rule fixtures and unit tests are separate evidence; no live infrastructure fault injection is claimed.

## Sampled memory

Maximum observed simultaneous sum: **1537.5 MiB**, at 2026-10-19T07:55:28.4677679Z, across 304 complete samples (0 incomplete/error samples). Configured limits remain **3392 MiB**. Sampling began 7.36 seconds after batch start; the largest observed sample gap was 4.37 seconds.

| Container | Maximum observed MiB |
| --- | ---: |
| buildlens-postgres-1 | 71.66 |
| buildlens-grafana-1 | 311.4 |
| buildlens-buildlens-server-1 | 22.63 |
| buildlens-jenkins-1 | 703.1 |
| buildlens-jenkins-agent-1 | 464.7 |

Individual maxima occur at different times and must not be added as a simultaneous peak. Docker-reported memory is sampled; a short peak or the initial sampling gap can be missed. Unrelated kind clusters are excluded. Dashboard queries and image preparation shared this host; this was not an isolated latency benchmark. Go race checks ran after the batch.

## Injected vs real

- **Injected:** seeded flaky behavior in OrderTest/RandomTest/TimeTest and the requested 300 ms controlled delay. Earlier regression demonstrations used larger injected delays.
- **Real:** Jenkins execution/results, XML observations, quarantine decisions, stored duration/cost calculations, actual GitHub comment delivery in the prior live gate, and Docker memory samples.
- **Source identity:** this batch reuses the hashed October 16 local lab/library snapshot adapter. Its archive identifier is not a Git tree object. The live PR builds separately verified normal Git-backed checkout. See the source manifest and console excerpts in the evidence.
- **Cost:** equivalent local compute cost at the dated reference price, not AWS spend. Unknown historical costs remain null. Stage dashboard cost is a duration-weighted allocation, not independently metered billing.

## Reproduce and inspect

Use `./tasks.ps1 up`, configure the local driver token, then `./tasks.ps1 drive 50 <job> SLOW_MILLIS=300`. This batch used the named local snapshot job above; reapply its reviewed bootstrap after a Jenkins restart. A fresh Git-backed run can target `buildlens-lab/main` after the owner publishes the intended lab code. Changing source/job changes the experiment; durations are not expected to repeat exactly.

[Reviewed evidence](../README.md) includes the exact driver run, database JSON/CSV, source identity, memory samples, dashboard query checks, dump/restore checks and final test outputs. [Live PR evidence](../../P4/live-20261019/README.md) records the actual two-build sticky comment.

## Claims still requiring separate verification

Production precision/recall, naturally occurring regressions, sustained-load throughput, multi-agent accounting, real AWS costs/deployment, IDoFT benchmark results and the later MCP workflow remain unverified. The new Jenkinsfile needs an owner commit/push for normal multibranch discovery; a hashed local execution does not close that Git gate. Isolated restore does not prove the owner-only live database replacement path.

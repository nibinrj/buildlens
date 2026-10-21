# Local measurement results

Measured on 2026-10-19. Lab flakiness and controlled delays are **injected**. These are actual local Jenkins runs, not production precision or performance benchmarks.

## New batch

Job: `buildlens-regression-local-20261019-081321`. UTC interval: 2026-10-19T08:13:25.7157728Z to 2026-10-19T08:33:48.8662505Z. Driver requested 50 builds and verified every queue/executable pair and BuildLens upload. The initial failed preflight is separate from this completed batch.

| Observation | Measured result |
| --- | ---: |
| Recorded batch builds | 50 |
| SUCCESS | 50 |
| UNSTABLE | 0 |
| BuildLens ID range | 105-154 |
| Infrastructure-attributed builds | 0 |
| Known / unknown cost builds | 50 / 0 |
| Equivalent reference USD | 0.004437 |
| Equivalent USD per build / min, median, max | 0.000077, 0.000089, 0.000134 |
| Agent busy milliseconds / sum | 764712 |
| Stored build duration ms / min, median, max | 13432, 15435, 23663 |
| Quarantine activations during interval | 0 |
| Quarantine releases during interval | 0 |
| Active quarantine at export | 1 |

Historical lab total at export: **151** stored builds; **4** quarantine activations and **3** releases. These totals include earlier experiments and are separate from the new batch.

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
| STAGE | Build | 5 |
| STAGE | Checkout | 7 |
| STAGE | Test | 3 |
| TEST | lab.core.CalculatorTest#addRejectsOverflow | 7 |
| TEST | lab.core.CalculatorTest#addsNumbers | 1 |
| TEST | lab.core.CalculatorTest#divideRejectsZero | 4 |
| TEST | lab.core.CalculatorTest#dividesNumbers | 2 |
| TEST | lab.core.SlugsTest#makesSlugs(String, String)[2] | 5 |
| TEST | lab.core.SlugsTest#makesSlugs(String, String)[3] | 7 |
| TEST | lab.flaky.OrderTest#polluter | 2 |
| TEST | lab.flaky.RandomTest#sometimesFails | 9 |

The batch retained a fixed requested delay of 300 ms yet generated **52** regression alert rows. These include incidental timing shifts and comparisons with older history. No new deliberate slowdown was introduced in this batch, and no production accuracy claim is inferred. Exact per-alert baseline/current values are retained in the evidence.

No infrastructure failure reason was recorded in this batch. Rule fixtures and unit tests are separate evidence; no live infrastructure fault injection is claimed.

## Sampled memory

Maximum observed simultaneous sum: **1626.11 MiB**, at 2026-10-19T08:21:56.7834897Z, across 286 complete samples (0 incomplete/error samples). Configured limits remain **3392 MiB**. Sampling began 0.42 seconds after batch start; the largest observed sample gap was 4.57 seconds.

| Container | Maximum observed MiB |
| --- | ---: |
| buildlens-postgres-1 | 67.59 |
| buildlens-grafana-1 | 368.9 |
| buildlens-buildlens-server-1 | 18.72 |
| buildlens-jenkins-1 | 658.9 |
| buildlens-jenkins-agent-1 | 602.7 |

Individual maxima occur at different times and must not be added as a simultaneous peak. Docker-reported memory is sampled; a short peak or the initial sampling gap can be missed. Unrelated kind clusters are excluded. Dashboard queries and image preparation shared this host; this was not an isolated latency benchmark. Go race checks ran after the batch.

## Injected vs real

- **Injected:** seeded flaky behavior in OrderTest/RandomTest/TimeTest and the requested 300 ms controlled delay. Earlier regression demonstrations used larger injected delays.
- **Real:** Jenkins execution/results, XML observations, quarantine decisions, stored duration/cost calculations, actual GitHub comment delivery in the prior live gate, and Docker memory samples.
- **Source identity:** this batch uses a dated, hashed local lab/library snapshot adapter. Its archive identifier is not a Git tree object. The live PR builds separately verified normal Git-backed checkout. See the source manifest and console excerpts in the evidence. An earlier fifty-build measurement is retained separately under evidence/P5/initial-batch/.
- **Cost:** equivalent local compute cost at the dated reference price, not AWS spend. Unknown historical costs remain null. Stage dashboard cost is a duration-weighted allocation, not independently metered billing.

## Reproduce and inspect

Use `./tasks.ps1 up`, configure the local driver token, then `./tasks.ps1 drive 50 <job> SLOW_MILLIS=300`. This batch used the named local snapshot job above; reapply its reviewed bootstrap after a Jenkins restart. A fresh Git-backed run can target `buildlens-lab/main` after the owner publishes the intended lab code. Changing source/job changes the experiment; durations are not expected to repeat exactly.

[Reviewed evidence](evidence/P5/README.md) includes the exact driver run, database JSON/CSV, source identity, memory samples, dashboard query checks, dump/restore checks and final test outputs. [Live PR evidence](evidence/P4/live-20261019/README.md) records the actual two-build sticky comment.

## Claims still requiring separate verification

Production precision/recall, naturally occurring regressions, sustained-load throughput, multi-agent accounting, real AWS costs/deployment, IDoFT benchmark results and the later MCP workflow remain unverified. The new Jenkinsfile needs an owner commit/push for normal multibranch discovery; a hashed local execution does not close that Git gate. Isolated restore does not prove the owner-only live database replacement path.

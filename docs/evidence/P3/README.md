# P3 local verification — 2026-10-12

This directory contains reviewed evidence, not production measurements.
All flaky findings below are **injected** in the lab.

## Run identity

- Jenkins job: buildlens-p3-local-20261012, builds **1–20**.
- Repository: nibinrj/buildlens-lab; branch: main.
- BuildLens API build IDs: **3–22**.
- Started: 2026-10-12 17:48:47.543 UTC; last upload: 18:00:01.915639 UTC.
- Snapshot SHA-256: 98ccbc25b86051904d82468a1f4c01810a67a6a434261bb3cd506b0089c7e159.
- Tested-content identifier: af489187eb7efb7525d4b2d8ca0f0c98c0c4e8f4
  (**archive SHA-1, not a Git tree**).
- Lab base commit: 868bbe8d1e2733480cfc84d0151d4ae5c93e8827.
- Sources were uncommitted. No agent commit, staging or push.
- Runtime read from the agent: Maven 3.9.16, Eclipse Adoptium Java 21.0.12.1.
  Existing Surefire 3.6.0 and JUnit 6.1.3 pins were reused.

[source-manifest.json](source-manifest.json) records the snapshot-time source hashes.
Later additions to test coverage and documentation are covered by the final check logs.

## What the 20 builds proved

| Observation | Actual result | Evidence |
| --- | --- | --- |
| Successful uploads | 20; each contains 13 test results | builds.csv, tests.csv |
| Infra-marked builds | 0 in this injected lab run | builds.csv |
| Automatic quarantines | OrderTest#victim after #6; RandomTest#sometimesFails after #16 | quarantine-after-6.json, quarantine-after-16.json |
| Blocking exclusion | Order victim excluded in #7–16; random excluded in #17–20 | tests.csv, console-excerpts.txt |
| Automatic release | Order victim: 10 quarantine passes in #7–16; blocking again in #17 | build-16.json, tests.csv, quarantine-history.csv |
| Safety cap | At most 1 active test in the recorded snapshots; 19 cap alerts across #1–20 | quarantine-after-*.json, cap-alerts.csv |
| Final active row at #20 | RandomTest, R1, pass streak 2 | quarantine-after-20.json |

The cap is policy min(10, max(1, floor(0.05 * 13))) = 1.
The cap alerts are alert rows, not 19 distinct flaky tests.
The order victim flakes again in blocking builds #18 and #19 after release, while the random
test occupies the cap. This demonstrates that running a victim alone can hide its polluter;
10 isolated passes do not prove the full-suite ordering defect is fixed.

Of the 20 stored initial/recovered outcomes, random has 3 FLAKY results (2 blocking, 1 quarantine),
time has 4 FLAKY results, and order victim has 4 FLAKY results. These are this seeded run's
observations, not estimates of production flakiness or of per-invocation probabilities.

## API and console proof

Full authenticated response bodies are saved; authorization headers and secrets are omitted.

- [quarantine-after-6.json](quarantine-after-6.json): R1 evidence with build IDs and failure hashes.
- [build-7.json](build-7.json): victim occurs only in QUARANTINE; 12 other tests are BLOCKING.
- [quarantine-after-15.json](quarantine-after-15.json): victim pass streak 9.
- [build-16.json](build-16.json): tenth quarantine pass.
- [quarantine-after-16.json](quarantine-after-16.json): random replaces the released victim.
- [quarantine-after-20.json](quarantine-after-20.json): active list at the requested run boundary.

Actual lines from Jenkins #7:

```text
[2026-10-12T17:52:17.360Z] BuildLens: blocking exclusions: lab.flaky.OrderTest#victim
[Pipeline] { (Quarantine)
[2026-10-12T17:52:25.450Z] BuildLens: quarantine-only tests: lab.flaky.OrderTest#victim
[2026-10-12T17:52:29.511Z] buildlens report: uploaded buildlens-p3-local-20261012 #7 with 7 report file(s): {"id":9,"created":true,"tests":13,"stages":4}
Finished: SUCCESS
```

## Extra persistent-failure check

Jenkins **#23** is an additional controlled check, excluded from the 20-build totals.
Seed=3, simulated clock=3000, Surefire reruns disabled: the random test fails in QUARANTINE.
Its blocking suite passes. [build-23.json](build-23.json) records build result SUCCESS,
Quarantine stage result UNSTABLE, and one FAILED quarantine result. The console contains:

```text
[2026-10-12T18:05:00.494Z] ERROR: quarantined test failed
[2026-10-12T18:05:00.494Z] ERROR: one or more quarantined tests failed
[2026-10-12T18:05:01.425Z] buildlens report: uploaded buildlens-p3-local-20261012 #23 with 6 report file(s): {"id":25,"created":true,"tests":13,"stages":4}
Finished: SUCCESS
```

The later lifecycle CSV is an audit export taken after this bonus check and other subsequent
build activity. Its current random-test streak is not the streak at #20; use quarantine-after-20.json
for that boundary. A separate completion run invoked tasks.ps1 up and drive against the same job
after #20. Those extra builds and recreated containers are excluded from this dated 20-build result.

## Checks and reproduction

[lint.txt](lint.txt) records gofmt, go vet and golangci-lint.
[tests.txt](tests.txt) records every Go package under -race and all JenkinsPipelineUnit tests.
The additional seeded reproducibility check records identical outcomes in repro-1.csv and repro-2.csv:
seed 5, clock 5000; order polluter/victim and random pass, time is FLAKY after a recovered expiry failure.

Run `./tasks.ps1 lint`, `./tasks.ps1 test` and `./tasks.ps1 drive 20`.
The drive task uses the existing local Jenkins and a reviewed Job DSL adapter to load an uncommitted
snapshot. It appends builds to the local verification job and uses the current repository history;
a later invocation is not a reset of this dated experiment. The seeded time/order/random settings
and current quarantine state must be retained when interpreting another run.

## Not verified

- Latest changes through the normal GitHub multibranch checkout and global-library retrieval.
  The owner must review, commit and push both repositories before that path can use them.
- Naturally occurring flaky tests, IDoFT precision/recall or production thresholds.
- Failsafe quarantine filtering, JUnit 4 or TestNG selection.
- Parameterized/Unicode test-name filtering: the CLI intentionally falls back to all tests.
- Real Jenkins cancellation and infrastructure fault injection; these paths have unit/integration
  coverage, but this lab run injected only test flakiness.
- Crash recovery during an open ingest transaction or a sustained production load.

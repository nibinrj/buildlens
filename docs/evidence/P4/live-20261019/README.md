# Live pull request comment verification

Verified on 2026-10-19 against [lab PR #1](https://github.com/nibinrj/buildlens-lab/pull/1).
The owner explicitly authorized the demonstration after publishing both repositories
and saving the separate comment token. No assistant staging, commits or pushes occurred.

## Result

Two successful Git-backed multibranch builds created and then updated
[comment 6261856291](https://github.com/nibinrj/buildlens-lab/pull/1#issuecomment-6261856291).
Exactly one comment was returned after each build. Both bodies contain quarantine,
regression and reference-cost sections. The active quarantine is the lab's
**injected** TimeTest; the controlled delay is also **injected**, not natural slowness.

| Jenkins build | BuildLens ID | Result | Controlled test ms | Threshold ms | Agent busy ms | Equivalent USD |
| --- | ---: | --- | ---: | ---: | ---: | ---: |
| PR-1 #3 | 50 | SUCCESS | 10001 | 363.6 | 30472 | 0.000177 |
| PR-1 #4 | 51 | SUCCESS | 10003 | 363.6 | 29951 | 0.000174 |

Both runs used `SLOW_MILLIS=10000`. The controlled test has 19 retained baseline
samples. The Test stage becomes REGRESSION on build #4 after two above-threshold
PR observations. TimeTest quarantine passes advance from two to three.

Comment creation time remains `2026-10-19T07:17:54Z`; updated time advances to
`2026-10-19T07:19:20Z`. Durable outbox desired/sent versions advance from 1/1 to
2/2 with the same comment ID, zero attempts and no last error.

## Evidence

- [First GitHub JSON response](comments-after-3.json) and [HTTP response](comments-after-3-http.txt).
- [Second GitHub JSON response](comments-after-4.json) and [HTTP response](comments-after-4-http.txt).
  These are actual GET responses after worker delivery, not captured POST/PATCH
  mutation responses. The worker completion logs and outbox exports corroborate
  the two deliveries; no live mutation status code is inferred from a GET.
- [First build API response](build-50.json) and [second build API response](build-51.json),
  including exact baseline durations and IDs, actual costs, stages and test runs.
- [First console](jenkins-3-console.txt), [second console](jenkins-4-console.txt),
  and [final Jenkins statuses](jenkins-4.json). See also [build #3 status](jenkins-3.json).
- [Outbox after #3](outbox-after-3.csv), [outbox after #4](outbox-after-4.csv),
  [worker logs](worker-after-4.txt), and [assertion results](verification.json).
- [PR identity](pr.json), [branch indexing](indexing-console.txt),
  [source comparison](source-verification.json), [deployed image](server-image.txt),
  [retained history](history-before.csv), and [stack health](stack.txt).

Jenkins loads the global shared library from BuildLens commit
`70f1fc68ba771dc63dfda0e5b28979618689e243`. The tested lab head is
`e2d90246d6b3d055db9ad1165e2bf9ab95617871`, with real Git tree
`38e6ad7dfd34f42f3ec85063b7e0cc18d223c476`. All 74 files in the prior verified
source manifest match the current checkout. Existing Job DSL discovered the PR;
no Jenkins UI configuration was used. Earlier automatic PR build #2 uploaded as
ID 49 before publication was enabled; the deliberate demo builds are #3 and #4.

## Interpretation and limits

The baseline reuses the dated local snapshot jobs from the earlier experiment.
Their Checkout stage copied a local snapshot, whereas these builds fetch GitHub.
The Checkout regression in both comments therefore compares different operations
and is not evidence of a source-code performance regression. Build #3 also alerts
on unchanged PriceServiceTest#totalsPrices (46 ms versus 42.6195 ms threshold),
which returns to NORMAL on #4. These incidental alerts are preserved, not tuned away.
The previous injected slow main observations are also retained in the baseline.
This proves the live integration, not production threshold quality or an isolated
performance benchmark. Build #4's Test stage is 16572 ms versus 12697.5274 ms.

Jenkins's read-only token still produces expected commit-status HTTP 403 warnings;
these are separate from successful comment publication using the BuildLens token.
Cost remains equivalent cost at the dated reference price, not AWS spend.
Natural regressions, sustained load, real AWS billing, multiple agent allocations,
and a real comment-permission/rate-limit failure remain unverified. No PR was merged.

Fresh required checks are preserved in [lint.txt](lint.txt) and [tests.txt](tests.txt).
`gofmt -l .` reports no files, `go vet ./...` succeeds, and the pinned
`golangci-lint run ./...` reports **0 issues**. All Go packages pass
`go test -race -count=1 -p 1 ./...`; JenkinsPipelineUnit reports **25 tests,
zero failures, zero errors, zero skipped**. Both task commands exit successfully.
The test runner has a 1536 MiB limit and its Postgres fixture a 384 MiB limit;
the existing stack limits remain 3392 MiB. The live builds completed before the
verification suite started. Source changes in this follow-up are documentation
and saved evidence only.

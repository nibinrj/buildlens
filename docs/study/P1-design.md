# P1 study note: design, Surefire, Jenkins and Go choices

Written 2026-10-04, before any P1 code. Each claim is marked with how I know it:

- **[docs]** read in the official docs or source on 2026-10-04 (link given)
- **[ran]** reproduced on this machine in a throwaway Maven project
  (Surefire 3.6.0, JUnit Jupiter 6.1.3, JDK 21.0.10, Maven 3.9.10)
- **[belief]** what I expect but did not check; a later prompt must verify it

## 1. Surefire and Failsafe XML reports

Sources: [Rerun failing tests](https://maven.apache.org/surefire/maven-surefire-plugin/examples/rerun-failing-tests.html)
(Surefire 3.6.0, published 2026-08-31) and the report schema
[surefire-test-report.xsd](https://maven.apache.org/surefire/maven-surefire-plugin/xsd/surefire-test-report.xsd).
Failsafe writes the same format to `target/failsafe-reports` **[belief]**: same schema reference, not checked
on a Failsafe run.

One file per test class, `TEST-<class>.xml`, root element `testsuite`, one `testcase` per test method.

| Element (child of `testcase`) | Meaning | Max per testcase (xsd) |
| --- | --- | --- |
| *(no child)* | Passed on the first attempt | |
| `failure` (message, type, stack trace as text) | Assertion failed. With reruns: the **first** failing attempt | unbounded |
| `error` (message, type) | Unexpected exception. With reruns: the first erroring attempt | 1 |
| `skipped` (message) | Disabled or assumption failed | 1 |
| `flakyFailure` / `flakyError` (message, type, `stackTrace`, optional `system-out`/`system-err`) | One per **failed attempt** of a test that **passed in a later rerun** | unbounded |
| `rerunFailure` / `rerunError` (same shape) | One per **later** failed attempt of a test that **failed every rerun** | unbounded |

**[docs]** When a test passes after reruns, the time is that of the last (passing) run. When it fails every
rerun, `failure`/`error` describes the first run, `rerunFailure`/`rerunError` the others, and the time is the first
run's.

**[ran]** `mvn test -Dsurefire.rerunFailingTestsCount=2` produced exactly that: a test failing only on its first
attempt has one `<flakyFailure>` and no `<failure>`; a test failing every time has one `<failure>` plus two
`<rerunFailure>`; an always-throwing test has one `<error>` plus two `<rerunError>`.

**Mapping to `test_run.outcome` (the plan's rules, in this order):**

1. `failure` present → FAILED (`rerun_failures` = number of `rerunFailure`)
2. `error` present → ERROR (`rerun_failures` = number of `rerunError`)
3. `flakyFailure` or `flakyError` present → FLAKY (`rerun_failures` = their count)
4. `skipped` present → SKIPPED
5. otherwise PASSED

Edge cases the P2 parser must handle: a `testcase` with no `classname` (the xsd makes it optional);
JUnit 5 parameterised tests, whose `name` carries the index (`test(int)[1]`); `failure` with an empty message; and
huge stack traces, which are only read for `failure_hash` and never stored whole.

### The top-level totals bug

[SUREFIRE-1627](https://issues.apache.org/jira/browse/SUREFIRE-1627) **[docs]**, still *Open/Unresolved* on
2026-10-04, affects 2.19.1, 2.22.1 and 3.0.0-M3. With `rerunFailingTestsCount`, the `testsuite` attributes count
every attempt as a test: one test rerun 6 times reports `tests="6" failures="6"`, but the body holds a single
`testcase` with one `failure`. The console summary is right; the XML header is wrong.

**[ran]** Honest result: with Surefire 3.6.0 and the JUnit Platform provider I could **not** reproduce it. The suite
header said `tests="3" failures="1" errors="1" flakes="0"`, matching the body. So the bug depends on the version or
provider. The rule stays the same: **count `testcase` elements, never trust the `testsuite` totals.** P2's
"wrong totals" fixture cannot come from a real run here, so it must be hand-edited from a real file to match the
JIRA report, and say so in a comment at the top.

## 2. Excluding and selecting test methods (what the CLI generates)

Sources **[docs]**: [test mojo parameters](https://maven.apache.org/surefire/maven-surefire-plugin/test-mojo.html)
and [Running a single test](https://maven.apache.org/surefire/maven-surefire-plugin/examples/single-test.html),
both 3.6.0.

- `-Dtest=Class#method` works "since 2.7.3". The docs say it is supported "for junit 4.x and TestNg". The JUnit
  Platform page only shows class-level `-Dtest`.
- `excludesFile` / `includesFile` (user properties `surefire.excludesFile` / `surefire.includesFile`): one pattern
  per line, blank lines and `#` lines ignored. **Method filtering in these files exists "since 3.0.0-M6"**, for
  example `pkg.SomeTest#testMethod`.
- `!` negation exists in `-Dtest` and in include/exclude patterns.

What I ran with JUnit Jupiter 6.1.3 and Surefire 3.6.0 **[ran]**:

| Command | Ran | Verdict |
| --- | --- | --- |
| `-Dsurefire.excludesFile=f` where f has `pkg.ATest#b` (plus two `BTest` methods) | every test except the listed methods | **works per method** |
| `-Dsurefire.includesFile=f` where f has `pkg.ATest#b` | only `ATest.b` | **works per method** |
| `-Dtest=pkg.ATest#b` | only `ATest.b` | works |
| `-Dtest=pkg.ATest#a+b` | `a` and `b` | works |
| `-Dtest=!pkg.ATest#b` | **all of `BTest`, none of `ATest`**: `ATest.a` and `ATest.flaky` silently dropped | **unreliable** |
| `-Dtest="pkg.*Test, !pkg.ATest#b"` | every test except `ATest.b` | works, but depends on the include pattern |
| `includesFile` that is empty, or holds only a comment | **every test** | trap |
| `excludesFile` that is empty | every test | correct |

**Decision for the CLI (P3):**

- `--format surefire-excludes` writes an **excludes file**, one `fully.qualified.Class#method` per line, with a
  `#` header comment. The blocking stage runs `mvn ... -Dsurefire.excludesFile=<file>`. An empty file is safe: all
  tests run.
- `--format surefire-only` writes an **includes file** in the same format, used as `-Dsurefire.includesFile=<file>`.
  Because an empty includes file runs the whole suite, **the shared library must skip the quarantine stage when
  the list is empty**. The CLI should signal "empty" with its exit status or by printing a count, not rely on the
  file. P3 tests this case.
- Never `-Dtest=!...` for method exclusion.

Certain **[ran]**: the two file formats with JUnit 5/6 on Surefire 3.6.0. Not verified: JUnit 4 and TestNG
(the docs say they work); Failsafe's `failsafe.excludesFile` user property **[belief]**, which P3 checks if the lab
uses Failsafe; test names with parameter indexes.

## 3. How the shared library times stages itself

Pipeline Stage View is up for adoption, so the plan forbids depending on its REST API. The library measures
stages itself. This is the P2 design, not code yet:

```groovy
// vars/timedStage.groovy (sketch)
def call(String name, Closure body) {
  stage(name) {
    long start = System.currentTimeMillis()
    String result = 'SUCCESS'
    try {
      body()
      if (currentBuild.currentResult == 'UNSTABLE') { result = 'UNSTABLE' } // set by junit/catchError inside
    } catch (org.jenkinsci.plugins.workflow.steps.FlowInterruptedException e) {
      result = 'ABORTED'; throw e
    } catch (e) {
      result = 'FAILURE'; throw e
    } finally {
      long end = System.currentTimeMillis()
      writeFile file: ".buildlens/stages/${start}-${sanitize(name)}.json",
                text: groovy.json.JsonOutput.toJson([name: name, startedAt: start, durationMs: end - start, result: result])
    }
  }
}
```

- `System.currentTimeMillis()` and `JsonOutput` are allowed because a **global** library is trusted (not
  sandboxed) **[belief, P2 verifies in JenkinsPipelineUnit and on Jenkins]**.
- **One file per stage**, not appending to one `stages.json`. Parallel branches would otherwise race on
  read-modify-write. `reportBuild` (or the CLI) merges `.buildlens/stages/*.json` into the upload.
- `writeFile` needs a workspace, so `timedStage` must run inside a `node`. Stages outside a node are a P2 edge case.
- Times use the controller clock (CPS runs on the controller), so start and end come from the same clock. The
  duration includes any wait for a `node` inside the stage; P4's regression note discusses it.
- `UNSTABLE` detection by `currentBuild.currentResult` is coarse: once the build is unstable, every later stage
  would also look unstable. P2 should compare the result before and after the body.

## 4. JCasC: a global library from a folder in this repo

The Pipeline: Groovy Libraries plugin (`pipeline-groovy-lib`, latest release 806.v408277b_33d1d, 2026-09-16)
supports a **library path inside the repository**. **[docs]**, from the source of
[`SCMBasedRetriever.java`](https://github.com/jenkinsci/pipeline-groovy-lib-plugin/blob/master/src/main/java/org/jenkinsci/plugins/workflow/libs/SCMBasedRetriever.java)
on master:

- `@DataBoundSetter setLibraryPath(String)`. It is relative, a trailing `/` is added, and `..` or absolute paths
  are rejected. Only `src/**/*.groovy`, `vars/*.groovy`, `vars/*.txt` and `resources/` are copied from that folder.
- `src/test` is excluded unless a system property allows it. Our tests live in `shared-library/test/`, outside
  `src/`, so they are never copied.
- A `clone` option checks out straight into the build's library folder instead of a controller workspace.

The jenkins.io [shared libraries page](https://www.jenkins.io/doc/book/pipeline/shared-libraries/) does not
document the library path; the source is the authority.

JCasC shape **[belief: the attribute names follow the `@DataBoundSetter` names and the `modernSCM` symbol; P2
must load it in a real controller and confirm]**:

```yaml
unclassified:
  globalLibraries:
    libraries:
      - name: buildlens
        defaultVersion: main
        allowVersionOverride: true     # lets a lab branch use @Library('buildlens@my-branch')
        implicit: false
        retriever:
          modernSCM:
            libraryPath: shared-library/
            scm:
              git:
                remote: https://github.com/nibinrj/buildlens.git
```

Where the library code comes from (decision D-020, decided by the owner on 2026-10-04: **A**):

| Option | What it needs | Catch |
| --- | --- | --- |
| A. GitHub remote + `libraryPath` (above) | Nothing new | Library changes are seen only after a push. With `allowVersionOverride`, a feature branch can be tested before merging. |
| B. Bind-mount the repo into the controller, `remote: file:///...` | System property `hudson.plugins.git.GitSCM.ALLOW_LOCAL_CHECKOUT=true` **[docs: git plugin 5.10.1 page]**. Local fetch is blocked by default (SECURITY-2478) because it exposes the controller file system. | It still reads **commits**, not the working tree, so it only saves the push. It also opens a known hole, and the Windows bind mount may need `safe.directory`. |
| C. File System SCM plugin | A new plugin (owner approval) | Its plugin page shows an **unresolved security warning** (arbitrary file read, 2019) and does not mention shared libraries. Rejected. |

**Chosen: A.** It needs no extra plugin and no security property, it behaves the same in Phase 2 (the AWS
controller pulls from GitHub too), and JenkinsPipelineUnit tests the library locally before any push.

## 5. Go choices

### sqlc + pgx v5 (chosen) vs plain pgx

| | sqlc + pgx | plain pgx |
| --- | --- | --- |
| Where SQL lives | `queries/*.sql`, compiled to typed Go in `internal/store` | strings in Go code |
| When a typo in a column name is found | at `sqlc generate` (build time) | at runtime, in a test if lucky |
| Scanning rows | generated | hand-written `rows.Scan(&a, &b, ...)` |
| Dynamic SQL (optional filters) | awkward; use `sqlc.narg` or write that one query by hand | easy |
| Extra tool | sqlc, run from its Docker image (D-012) | none |

Java analogy: sqlc is close to jOOQ's code generation or MyBatis Generator, but it starts from your SQL rather
than from a DSL. pgx is the driver, like the PostgreSQL JDBC driver plus HikariCP (`pgxpool`). sqlc depends on
`pg_query_go` (cgo) **[docs: sqlc v1.31.1 go.mod]**. That is why D-012 runs sqlc in Docker on Windows instead of
`go install`.

### goose (chosen) vs golang-migrate

Both are maintained: goose v3.28.0 (2026-09-02), golang-migrate v4.20.1 (2026-09-09), from proxy.golang.org.

| | goose v3 | golang-migrate v4 |
| --- | --- | --- |
| File format | one file per version, `-- +goose Up` / `-- +goose Down` sections | two files, `N_x.up.sql` and `N_x.down.sql` |
| Run from Go with embedded files | `goose.NewProvider(goose.DialectPostgres, *sql.DB, fs.FS)` **[docs: goose Provider page]** | `iofs` source driver |
| Several server instances starting at once | `lock.NewPostgresSessionLocker` (advisory lock) **[docs]** | lock built into the driver **[belief]** |
| After a failed migration | the version is not recorded, so fix it and rerun **[belief]** | the database is marked *dirty* and needs a manual `force` **[belief]** |
| sqlc reads the files | yes; sqlc ignores down migrations **[docs: sqlc DDL page]** | yes |

**Choice: goose.** One file per change is easier to review, the Provider API embeds migrations in the binary, so
the server and the testcontainers test run exactly the same files, and the session locker covers two starts at once.
The goose module needs Go ≥ 1.26 (its go.mod), and we are on 1.27.1. goose takes a `*sql.DB`, so the server
wraps its pgx pool with `stdlib.OpenDBFromPool` (one pool, two APIs).

### Project layout

The plan's layout, plus three things it implies:

- `cmd/<binary>/main.go` is kept small. `main` calls `run(ctx, ...) error`, so the real work is testable and
  `main` only maps an error to an exit code.
- `internal/` is **enforced by the Go compiler**: code outside this module cannot import it. That is stronger than
  Java's package-private, which only works within one package. We never need a public `pkg/`.
- `migrations/` holds the `.sql` files **and** a tiny `embed.go`. `//go:embed` cannot reach a parent directory
  (`..`), so the package that embeds the files must sit next to them.
- New packages in P1: `internal/config` (env parsing), `internal/db` (pool and migrations), `internal/api`
  (handlers; health only for now), and `internal/store` (generated).

### Toolchain pin

`go.mod` has `go 1.27.0` (language version, the minimum) and `toolchain go1.27.1`. The `toolchain` line is a
minimum too **[docs: go.dev/doc/toolchain, as I remember it; not re-read today]**, so `tasks.ps1` also sets
`GOTOOLCHAIN=go1.27.1` for an exact pin. The Docker builder image is `golang:1.27.1`.

### `go test -race` on this Windows machine

`-race` needs cgo and a C compiler. This machine has none (`CGO_ENABLED=0`, no gcc). `tasks.ps1 test` therefore runs
the tests inside the pinned `golang:1.27.1-trixie` image, which has gcc, with the Docker socket mounted so
testcontainers can start Postgres. A plain `go test ./...` still works natively for quick feedback. Decision D-063.

## 6. Go idioms used in this project, compared with Java

**Errors are values, not exceptions.** A function that can fail returns `(result, error)`, and the caller checks
`if err != nil` straight away. Nothing unwinds the stack. We **wrap** errors on the way up with context:
`return fmt.Errorf("load config: %w", err)`. `%w` keeps the original error inside, like Java's
`new RuntimeException("load config", cause)`. `errors.Is(err, pgx.ErrNoRows)` replaces `catch (NoResultException e)`.
`errors.As(err, &pgErr)` replaces `catch (PSQLException e)` when you need fields such as the SQLSTATE code.
`panic` exists, but only for programmer bugs, never for expected failures. Why it matters here: every DB and HTTP
failure shows up in the code path where it can happen, so you can see in the review that it is handled.

**context.Context is cancellation plus deadline, passed explicitly.** Every request, DB call and outbound call takes
`ctx` as its first parameter. When the client disconnects, or SIGTERM arrives, or a timeout passes, `ctx.Done()`
closes, and pgx and net/http abort what they are doing. Java has no single equivalent. It is closest to a
combination of `Thread.interrupt()`, a per-call JDBC `setQueryTimeout`, and a request-scoped object, except that it
is an explicit argument, not a ThreadLocal. Example in P1: `/readyz` pings the DB with
`context.WithTimeout(r.Context(), 2*time.Second)`. A hung DB makes readyz answer 503 after 2 s instead of hanging.
`defer cancel()` must follow every `WithTimeout`, or the timer leaks until it fires.

**Interfaces are satisfied implicitly and declared by the consumer.** There is no `implements`. If a type has the
methods, it fits. So the health handler declares the one thing it needs, `type Pinger interface { Ping(ctx) error }`.
`*pgxpool.Pool` already fits without knowing about it, and the test passes a 5-line fake. In Java you would
introduce an interface in the producer's package and make the pool class implement it, or use Mockito. CLAUDE.md
says interfaces only where a test needs a fake, so there is no `Repository` interface in front of everything.

**Struct embedding is composition, not inheritance.** `type statusRecorder struct { http.ResponseWriter; status int }`
gets all of `ResponseWriter`'s methods *promoted*, and overrides only `WriteHeader` to record the status. That is the
decorator pattern without writing the delegating methods. There is no `super` and no polymorphism through the
embedded type: a method on the embedded writer that calls `WriteHeader` calls *its own*, not ours. The logging
middleware in P2 will use exactly this.

**defer is try/finally scoped to the function.** `defer rows.Close()` runs when the function returns, by any path.
Several defers run in LIFO order, like nested try-with-resources. The arguments are evaluated **when the `defer`
line runs**, not at exit. A common bug: `defer` inside a loop holds every resource until the function ends.

**Table-driven tests are JUnit 5's `@ParameterizedTest` written as plain code.** A slice of structs, each with a
name, inputs and the expected result, and a loop calling `t.Run(tc.name, ...)`. Each case is a named subtest that
can be run alone (`go test -run 'TestLoad/missing_url'`). Failure paths are just more rows. There is no framework:
`if got != want { t.Errorf(...) }` is the assertion.

**Goroutines and graceful shutdown.** `main` starts `srv.ListenAndServe()` in a goroutine, a lightweight thread
managed by the Go runtime, far cheaper than a Java platform thread and similar to a virtual thread. Then it waits
on `signal.NotifyContext(ctx, SIGTERM, Interrupt)`. On the signal, `srv.Shutdown(ctxWithTimeout)` stops accepting
connections and waits for in-flight requests up to the timeout, like Spring Boot's graceful shutdown phase. The
`ListenAndServe` goroutine reports its result over a channel, so a port already in use stops `main` too, instead of
being lost.

**GOMEMLIMIT.** The Go garbage collector does not know about the container's memory limit unless told. Compose sets
`GOMEMLIMIT` a little under the 64 MB limit, so the GC works harder before the kernel OOM-kills the process. This is
the analogue of `-XX:MaxRAMPercentage` for the JVM.

## 7. The rules, restated with edge cases

The defaults are the plan's starting points, not measured values. Items marked **P3/P4 decides** are open.

### Infrastructure attribution (P3, runs before flaky detection)

A build matching any rule (agent lost, exit 137/OOMKilled, "No space left on device", Docker daemon unreachable,
Maven artifact resolution timeout) gets `infra_failure = true`. **All** its test results are left out of R1, R2 and
R3. Otherwise a lost agent would look like a burst of flaky tests.

- The log tail is the last 500 lines. An OOM message early in a long log is missed, and the build then counts as
  a real failure. This is a false negative, never a false quarantine.
- **Near miss:** a test that asserts on the text "No space left on device" prints it in its own output. Rules
  should match Jenkins and agent lines (`ERROR:`, remoting messages), not test stdout. P3 needs a test for this.
- A build with an infra failure *and* a genuine test failure is still excluded. That is conservative: we lose one
  data point but never blame a test for an agent crash.

### Flaky rules (P3)

- **R1 Surefire rerun:** FLAKY in ≥ 2 of the last 30 builds. **P3 decides** whether "30 builds" means the repo's
  last 30 builds or this test's last 30 runs. I recommend the test's last 30 runs in non-infra builds, so a test in
  a rarely built module is not diluted. It only works if the lab sets `rerunFailingTestsCount` > 0.
- **R2 Same commit:** PASSED and FAILED/ERROR on the same `commit_sha`, both in non-infra builds. One occurrence
  is enough. **Trap: Jenkins builds a PR as a merge with the target branch.** Two builds of the same PR head SHA
  after main moved test *different code*. Decided (D-078, 2026-10-04): P2 records the revision actually tested, in
  a new migration, and R2 compares builds by that revision; a PR build that cannot report it is ignored by R2. Environment differences (another JDK or agent type) also make a test pass and fail on one SHA.
  That still counts as flaky, which is fair: it is environment-dependent.
- **R3 Flip rate:** on the default branch, ≥ 3 outcome changes across the last 30 runs. Only PASSED vs
  FAILED/ERROR take part. SKIPPED and FLAKY are left out of the sequence (R1 covers FLAKY). A real break then a fix
  is 2 flips, so it is correctly **not** flaky. A break, a wrong fix, then the real fix is 4 flips, a **known false
  positive**. The evidence jsonb must list the builds so a human can see it.

### Quarantine loop (P3)

1. A rule fires → a `quarantine` row (state QUARANTINED, `reason_rule`, `evidence` with build ids, outcomes and
   failure hashes; P7 needs the hashes, D-053).
2. The pipeline fetches the list; if BuildLens is unreachable the excludes file is empty and **every test blocks**.
   That fails safe towards strictness.
3. The quarantine stage runs only those tests with an includes file. **It is skipped when the list is empty**, since
   an empty includes file runs everything (§2). Failures make the stage UNSTABLE, never the build FAILED.
4. **Release** after K = 10 consecutive quarantine-stage passes. FLAKY or FAILED resets the count; SKIPPED and
   infra builds neither count nor reset it. A released test that fires again gets a **new** row. History is kept,
   and a partial unique index allows one active row per test.
5. **Safety cap:** the plan says min(5 % of the repo's tests, 10). With plain floor, a repo of 19 tests gets a
   cap of 0 and nothing can ever be quarantined, and the lab repo will be about that small. **Decided (D-077,
   2026-10-04):** cap = min(10, max(1, floor(0.05 × tests))), counting only tests seen in the last 30 builds, not
   every test ever recorded. Above the cap: an alert, no quarantine.
6. Manual quarantine/release: same table, `manual = true`.
7. A quarantined test deleted from the code never passes again, so it is never released. **P3 decides** whether to
   auto-release after N builds without a run.

### Regression detection (P4)

Baseline per stage on the default branch: median and MAD of the last 20 **successful** builds; fewer than 10 →
"insufficient data", no alert.

threshold = median + max(3 × 1.4826 × MAD, 0.2 × median)

- 1.4826 × MAD estimates the standard deviation for normal data, so this is about "3 sigma", but one outlier
  barely moves it. A mean and standard deviation would be pulled by the very spike we want to ignore.
- MAD = 0 (identical durations) → the 20 %-of-median floor still gives a sensible threshold.
- **Self-contamination:** the baseline should be the 20 successful builds *before* the window being judged. Otherwise
  a sustained slowdown raises its own baseline. **P4 decides.**
- Only stage runs with result SUCCESS enter the baseline. A failed stage stops early and looks fast.
- Default branch: alert when 2 of the last 3 builds are above the threshold. PR: alert when the latest is
  **≥ 1.5 × threshold** (my reading of "exceeds the threshold by 50 %") or the last 2 are both above it.
- Very short stages: 20 % of 300 ms is 60 ms, which is noise. P4 should consider a minimum median before alerting.
  That value must come from real lab data, not be invented here.
- Stage durations include waiting for an agent if `node` sits inside the stage. Phase 2 Spot instance types
  differ in speed, so a baseline per instance type may be needed. P4 and J.7 look at real data first.
- The same check runs on the 20 slowest tests, ranked by baseline median.

### Cost per build (P4)

cost = agent busy seconds ÷ 3600 × `usd_per_hour` for (instance type, lifecycle, region) at build start.

- Price row: the latest `effective_from ≤ started_at`. **No row → cost is NULL (unknown), never 0.**
- Phase 1: LOCAL builds are priced at a configured reference instance type and labelled "equivalent cost at
  reference price"; `price.source` says so.
- "Busy seconds" in Phase 1 is the build duration. A pipeline waiting on `input` or with several `node` blocks
  over-counts. One executor per agent keeps it honest for now.
- Spot prices move during a build; we use the price at start (plan). An interrupted and resubmitted build is two
  `build` rows, two costs.

## 8. Memory budget

Limits from the plan; real numbers come from `docker stats` (the P1 run is at the end of this prompt; P5 records
the full stack under load).

| Container | Limit | Arrives in | Note |
| --- | --- | --- | --- |
| postgres (18.6-alpine) | 384 MB | P1 | default `shared_buffers` 128 MB fits |
| grafana (13.2.3) | 384 MB | P1 | the plan said 256 MB; raised after measuring 87 % idle (below, D-076) |
| buildlens-server | 64 MB | P1 | static Go binary, `GOMEMLIMIT=56MiB` |
| **P1 total** | **832 MB** | | |
| jenkins controller | 1 GB | P2 | |
| jenkins agent | 1.5 GB | P2 | Maven builds |
| **Phase 1 full stack** | **≈ 3.3 GiB** (3,392 MB) | | target ≈ 3.5 GB; Docker Desktop has 7.6 GiB |

**Measured on 2026-10-04** (`docker stats --no-stream`, stack idle about 2 minutes after `tasks.ps1 up`, no
dashboards, no traffic): postgres 28.2 MiB, grafana 222.5 MiB (**87 % of its limit**, rising slowly from 208.5 MiB
at start), buildlens-server 3.0 MiB. Total about 254 MiB. Because of that headroom, Grafana's limit was raised to 384 MB the same day (decision D-076, owner).
After the change Grafana settled at 315.5 MiB / 384 MiB. Its cgroup `memory.stat` showed anon 235 MiB (its own
memory) and file 135 MiB (page cache, which the kernel can reclaim). `docker stats` counts part of that cache, so
its percentage overstates real need. When sizing a limit, read `anon`: about 235 MiB here, too tight for 256 MB.
Idle numbers say nothing about load; P5 measures during builds.

Not counted: Docker Desktop's own VM overhead, and containers started by Testcontainers during lab builds (P2+).
On 2026-10-04 two kind clusters from another project (Buzzer) were also running on this Docker Desktop. They
share the 7.6 GiB.

## 9. Versions pinned in P1 and where they came from

| Item | Version | Source (checked 2026-10-04) |
| --- | --- | --- |
| Go toolchain | go1.27.1 | `go version`; go.dev/dl (D-010) |
| golangci-lint | v2.14.0 | `golangci-lint version` (D-011) |
| github.com/jackc/pgx/v5 | v5.11.0 | proxy.golang.org `@latest` |
| github.com/pressly/goose/v3 | v3.28.0 | proxy.golang.org `@latest` |
| github.com/testcontainers/testcontainers-go (+ modules/postgres) | v0.44.0 | proxy.golang.org `@latest` |
| sqlc (Docker `sqlc/sqlc`) | 1.31.1 | Docker Hub tags; proxy.golang.org |
| golang builder image | 1.27.1-alpine3.24 / 1.27.1-trixie (tests) | Docker Hub tags, digests in files |
| distroless runtime | gcr.io/distroless/static-debian13:nonroot | gcr.io manifest digest; distroless README |
| postgres | 18.6-alpine3.24 | Docker Hub tags (18 is the newest non-beta major) |
| grafana | 13.2.3 | Docker Hub tags (latest = 13.2.3) |
| Surefire (experiment only) | 3.6.0 | Maven Central metadata |
| JUnit Jupiter (experiment only) | 6.1.3 | Maven Central metadata |

Postgres 18 changed the image's data path: `PGDATA=/var/lib/postgresql/18/docker` and the volume is
`/var/lib/postgresql` **[docs: docker-library postgres image docs]**. Compose mounts the volume there.

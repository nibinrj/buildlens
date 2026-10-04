# P2 study note: Jenkins as code, the CLI, and ingest

Written 2026-10-04 before the P2 code. Markers as in P1: **[docs]** read in official docs or source today,
**[ran]** reproduced here, **[belief]** expected but not checked yet. Section 11 records what running it showed.

## 1. The loop this prompt closes

```
lab repo on GitHub ──scan──> Jenkins controller ──runs on──> inbound agent
                                                              │ timedStage records stage timings
                                                              │ mvn writes target/*-reports/TEST-*.xml
                                                              └ reportBuild -> `buildlens report` ──multipart POST──> buildlens-server -> Postgres
```

## 2. Jenkins controller as code

**Image.** `jenkins/jenkins:2.580.1-lts-jdk21`, the current LTS, released 2026-09-30 **[docs: updates.jenkins.io
redirects stable to 2.580.1; Docker Hub tags]**. Plugins are installed at image build time by `jenkins-plugin-cli`
from `plugins.txt`.

**plugins.txt pins the full dependency closure (owner decision, 2026-10-04).** The nine plugins the prompt names need
69 others, so 78 in total. I resolved them from the update centre for exactly this core
(`updates.jenkins.io/dynamic-stable-2.580.1/update-center.actual.json`). Each plugin's required (non-optional)
dependencies are followed recursively at the version that update centre offers. **No pinned version and not the
core falls inside an active security-warning range** (each warning's version regex was checked against the pin).
Pinning only the nine would let `jenkins-plugin-cli` choose dependency versions at build time, so two builds a week
apart could differ. That is the same reason a Java project pins transitive dependencies with a BOM or a lock file.
`jenkins-plugin-cli` still resolves dependencies. If a pinned version is too old for another plugin, it upgrades it
and prints a message **[belief]**, so the image build output is checked for that.

**JCasC** (`configuration-as-code` plugin) reads `CASC_JENKINS_CONFIG` (here `/var/jenkins_home/casc/jenkins.yaml`,
copied into the image) at startup. It configures:

- **Security:** the local user database with one admin, whose password comes from the environment as
  `${JENKINS_ADMIN_PASSWORD}`; the `loggedInUsersCanDoAnything` authorization (core, no extra plugin), with no
  anonymous read. The setup wizard is switched off with `-Djenkins.install.runSetupWizard=false`.
- **Controller executors: 0.** Builds never run on the controller, which is the standard hardening.
- **The agent node** `agent-1`, an inbound node over WebSocket (section 4).
- **Credentials:** `github-token` (username + token, used by the GitHub branch source) and `buildlens-ingest-key`
  (a secret-text credential from `plain-credentials`). Both come from environment variables, so nothing secret is
  in the YAML (rule 6).
- **Global library `buildlens`:** D-020 = A, the GitHub remote with `libraryPath: shared-library/`.
- **Jobs:** JCasC's `jobs:` root (from the job-dsl plugin) runs `jobs/seed.groovy` at every start and every JCasC
  reload.

**What "seed" means here.** The classic "seed job" is a freestyle job that runs Job DSL. It would need an executor
and a workspace holding the script. Instead, JCasC's `jobs:` runs the same Job DSL script directly when the
controller starts, and `tasks.ps1 seed` re-runs it by asking JCasC to reload (`POST /configuration-as-code/reload`).
The result is the same (jobs defined only in `seed.groovy`, re-appliable at will) with one fewer job.
**[belief]**: the `jobs:` key and the reload endpoint behave as described; checked when Jenkins runs.

JVM sizing: the container limit is 1 GB (plan). `-XX:MaxRAMPercentage=60` gives the controller heap about 600 MB
and leaves the rest for metaspace, threads and native memory. That is a starting value; `docker stats` decides.

## 3. Job DSL: the two multibranch jobs

`seed.groovy` defines `multibranchPipelineJob('buildlens-lab')` and `multibranchPipelineJob('buildlens')`. Each uses
a GitHub branch source (owner `nibinrj`, credentials `github-token`) that discovers branches and pull requests,
plus a **periodic folder trigger** (branch indexing every 15 minutes). There are no webhooks: GitHub cannot reach
Jenkins on localhost (plan, ADR 004 in P5). The `buildlens` job will find no `Jenkinsfile` until P5 (dogfooding),
so it shows no branches yet; that is expected.

Job DSL's syntax for plugin-contributed parts (`github` source, traits) is generated from the installed plugins.
The authority is the running controller's API viewer at `/plugin/job-dsl/api-viewer/index.html`, so the exact
names are confirmed there **[belief until then]**.

## 4. The inbound agent

An inbound agent connects *to* the controller, the opposite of SSH agents, so the controller needs no credentials
for the agent. With `-webSocket` it uses the normal HTTP port, so port 50000 is not opened **[docs: inbound-agent
image README, from memory; checked when it runs]**.

**The secret problem.** An inbound agent proves who it is with a secret that the controller derives from its own
private key and the node name. The secret is only known after the controller has started. The usual workaround is
an agent script that downloads it with admin credentials. That would put the admin password into the agent's
environment, which builds can read (a build's `sh 'env'` shows the agent process environment).

**Chosen instead:** a tiny controller start-up script, `init.groovy.d/agent-secret.groovy` (code in the repo, run by
Jenkins after start-up), writes `agent-1`'s secret into a Docker volume shared with the agent (read-only for the
agent). The agent's entrypoint waits for that file, reads it, and starts `jenkins-agent`. No password ever reaches
the agent. **[belief]**: `init.groovy.d` runs after JCasC has created the node; if not, the script waits and retries.

Agent image: `jenkins/inbound-agent:3391.va_37fa_a_305d6d-4-jdk21` (Debian 13, **[ran]** OpenJDK 21.0.12.1, git
2.47.3, uid 1000), plus:

- Maven **3.9.16**, the newest 3.x; 4.0.0 is still a release candidate. It is downloaded from Maven Central and its
  SHA-512 checked in the Dockerfile.
- Go **1.27.1**, from go.dev, SHA-256 checked. P2 does not need it; P5 builds BuildLens on this agent.
- The `buildlens` CLI, built in a Go stage of the same Dockerfile.

Environment set on the agent container: `BUILDLENS_SERVER_URL=http://buildlens-server:8080` and
`BUILDLENS_AGENT_LIFECYCLE=LOCAL`. Jenkins passes an agent process's environment to the builds that run on it
**[belief]**, so the CLI picks these up without the Jenkinsfile knowing.

**Not in P2:** the Docker socket mount (plan: for Testcontainers). The lab has no Testcontainers yet, and the socket
gives every build root-equivalent access to Docker Desktop. It is added when a build needs it (P5, dogfooding), with
its risk written down then.

Memory: agent limit 1.5 GB. `MAVEN_OPTS=-Xmx512m`; Surefire forks its own JVM.

## 5. Shared library

```
shared-library/
  vars/timedStage.groovy     wraps stage(); records name, start, duration, result
  vars/reportBuild.groovy    writes stages.json and the 500-line log tail, runs `buildlens report`; never fails
  test/...                   JenkinsPipelineUnit tests (Groovy, JUnit 5)
  pom.xml, mvnw              test-only Maven project (D-017)
```

Only `vars/` is copied into Jenkins (`libraryPath` copies src/, vars/, resources/; P1 §4), so the tests and the
pom never reach a build.

**Where stage timings live.** P1's sketch wrote one file per stage to avoid parallel branches racing on one file.
A simpler race-free option: keep the records in the build's environment variable `BUILDLENS_STAGES`, one JSON
line per stage, appended with `env.BUILDLENS_STAGES = (env.BUILDLENS_STAGES ?: '') + line + '\n'`. Pipeline Groovy
runs on one thread per build (CPS). Parallel branches only switch at *step* boundaries, and reading or writing
`env` is not a step, so the append cannot be interrupted **[belief, from how CPS works; P3's parallel stages would
expose it]**. It also works for stages outside a `node` (no workspace needed). `reportBuild` writes the lines out as
`stages.json`, the file the prompt names.

**Stage result.** `SUCCESS`, or `FAILURE` if the body throws, or `ABORTED` on `FlowInterruptedException`, or
`UNSTABLE` if `currentBuild.currentResult` *changed* to UNSTABLE during the body. P1 §3 noted that checking it only
afterwards marks every later stage unstable, so the before/after comparison fixes that.

**Scripted, not Declarative, in the lab Jenkinsfile.** `timedStage` *wraps* `stage()`, which Declarative syntax
cannot do: its stages are fixed directives. The lab uses scripted Pipeline:

```groovy
@Library('buildlens') _
node('maven') {
  try {
    timedStage('Checkout') { checkout scm }
    timedStage('Build')    { sh 'mvn -B -ntp -DskipTests package' }
    timedStage('Test')     { sh 'mvn -B -ntp verify' }
  } catch (e) {
    currentBuild.result = currentBuild.result ?: 'FAILURE'   // finally runs before Jenkins sets the result
    throw e
  } finally {
    junit allowEmptyResults: true, testResults: '**/target/*-reports/TEST-*.xml'
    reportBuild()
  }
}
```

`finally` plays the role of `post { always }`. `reportBuild()` also works unchanged in a Declarative
`post { always }`, where `currentResult` is already final.

**reportBuild:**

- Log tail: `currentBuild.rawBuild.getLog(500)`. `rawBuild` is blocked in sandboxed scripts but allowed in a global
  (trusted) library **[belief]**.
- `git rev-parse HEAD` (commit) and `git rev-parse HEAD^{tree}` (**tested tree**, D-078). Jenkins builds a PR as a
  *local merge commit* whose SHA includes a timestamp, so two builds of identical code get different commit SHAs.
  The **tree** hash depends only on file content, so identical code gives an identical tree. R2 (P3) compares trees.
  This is a refinement of D-078 ("record the revision actually tested"), logged as such.
- Repo name from `git config --get remote.origin.url`, as `owner/name`.
- The ingest key is bound with `withCredentials` (credentials-binding), so Jenkins masks it in the log.
- Everything is wrapped in `try/catch`, and `sh` uses `returnStatus: true`. A BuildLens problem prints a warning and
  never changes the build result.

**JenkinsPipelineUnit** runs the library's Groovy outside Jenkins with mocked steps. Version **1.31** (2026-07-01,
from the Jenkins Maven repository; since 1.2 it is not on Maven Central **[docs: README]**). It depends on
**groovy-all 2.4.21**, the Groovy line Jenkins itself runs, so the tests are compiled with that version via
gmavenplus 5.1.0, not with Groovy 6. Tests are JUnit Jupiter 6.1.3, through the plain `BasePipelineTest`.

## 6. The CLI (`cmd/buildlens`, standard `flag` package, D-018)

```
buildlens report     [flags]   upload one build's results
buildlens quarantine [flags]   stub until P3: prints a notice, exits 0
buildlens version
```

`report` flags. Every one also reads an environment variable, so the library passes little on the command line:
`--server` (BUILDLENS_SERVER_URL), `--key` (BUILDLENS_KEY; the env var is preferred, because command-line args are
visible in `ps`), `--repo`, `--job`, `--build-number`, `--branch`, `--pr`, `--commit`, `--tree`, `--result`,
`--started-at` (epoch ms), `--duration-ms`, `--agent`, `--agent-instance-type`, `--agent-lifecycle`, `--stages`
(file), `--log-tail` (file), `--reports` (comma-separated globs, default
`**/target/surefire-reports/TEST-*.xml,**/target/failsafe-reports/TEST-*.xml`), `--strict`, `--timeout` (per attempt,
30s), `--retries` (3).

- **`**` globs:** Go's `filepath.Glob` has no `**`. The CLI walks the workspace and matches with a tiny translator
  (`**/` = any number of directories, `*` = anything except `/`), tested table-driven. It skips `.git`.
- **Exit codes:** 0 = uploaded, or failed without `--strict` (a warning on stderr); 1 = failed with `--strict`;
  2 = usage error (missing or invalid flags), always, because that is a bug in the pipeline, not an outage.
- **Retries:** network errors, 429 and 5xx are retried with exponential backoff (1 s, 2 s, 4 s, plus up to 25 %
  random jitter, so many agents do not retry in lockstep). 4xx answers such as 401 or 400 are **not** retried: the
  same request will fail the same way. The wait function is injected so tests do not sleep.

## 7. Multipart upload in Go (compared with Java)

Request: `POST /api/v1/builds`, `Content-Type: multipart/form-data`. Parts, in this order:

| Part name | Content | Count |
| --- | --- | --- |
| `metadata` | JSON: repo, job, build number, branch, PR, commit, tree, result, times, agent, stages | 1 (required, first) |
| `report` | one Surefire/Failsafe XML file; the part's filename is its path relative to the workspace | 0..n |
| `log` | the log tail, text | 0..1 |

**Client:** `mime/multipart.Writer` writes into one end of an `io.Pipe`, and `http.Request` reads the other end.
A goroutine copies each file in, so the whole upload is never held in memory. It is the equivalent of streaming
with Apache HttpClient's `MultipartEntityBuilder`, using only the standard library. A pipe can be read once, so
**each retry builds a new pipe and reopens the files**. The goroutine must always close the pipe, with
`CloseWithError` on failure, or the request would hang. That is the Go version of "always close your stream in
`finally`".

**Server:** `r.MultipartReader()` reads parts *as they stream*. `ParseMultipartForm` would buffer them in memory or
temp files, which matters in a 64 MB container. Each XML part goes straight into a streaming `encoding/xml.Decoder`,
so only the small per-test results are kept. `http.MaxBytesReader` caps the whole body (default 64 MiB,
`BUILDLENS_MAX_UPLOAD_BYTES`) and the server answers 413 if it is exceeded. The log part is read up to 1 MiB.
Nothing is written to the database until every part has parsed: a bad XML file means 400 and no partial build.

## 8. Idempotent ingest

"The same job + build number twice updates, never duplicates." In one transaction:

1. Ensure the repository row exists (`INSERT ... ON CONFLICT (name) DO UPDATE ... RETURNING id`).
2. Upsert the build on its unique key (`job_name, build_number`). `RETURNING id, (xmax = 0) AS inserted` tells a
   new row from an update. `xmax` is Postgres's internal "deleted/locked by transaction" field: 0 on a freshly
   inserted row, non-zero on a row an upsert updated **[belief; widely used, not in the official docs; covered by
   the idempotency test]**.
3. Delete that build's `stage_run` and `test_run` rows and insert the new ones. "Replace the children" is simpler
   and safer than diffing them.
4. Upsert all test cases in **one** statement (`INSERT ... SELECT FROM unnest($classes, $methods, ...) ON CONFLICT
   ... RETURNING`), then bulk-insert test runs with `COPY` (sqlc `:copyfrom`).

A test can appear twice in one upload (a Surefire and a Failsafe report of the same class, or a retried fork). The
table allows one run per test per build, so the parser merges duplicates. **The worse outcome wins**
(ERROR > FAILED > FLAKY > PASSED > SKIPPED), durations and rerun counts add up, and the first failure's type and hash
are kept. Without the merge, Postgres would reject the upsert ("ON CONFLICT DO UPDATE command cannot affect row a
second time").

Go transactions compared with Java: there is no `@Transactional`. The code calls `pool.Begin(ctx)`, then
immediately `defer tx.Rollback(ctx)` (a no-op once committed), runs the queries through
`store.New(pool).WithTx(tx)`, and finally `tx.Commit(ctx)`. Every early `return err` rolls back through the defer,
like an exception leaving a Spring transactional method.

HTTP answers: **201** for a new build, **200** for a re-ingest (both with `{id, created, tests, stages}`), **400**
for invalid metadata or XML, **401** without a valid key, **413** for too large.

## 9. Parsing (`internal/surefire`), exactly the plan's rules

Outcome per `testcase`, in this order: `failure` → FAILED; `error` → ERROR; `flakyFailure`/`flakyError` (with no
failure or error) → FLAKY; `skipped` → SKIPPED; otherwise PASSED. `rerun_failures` = the number of
`rerunFailure`/`rerunError` for FAILED/ERROR, and the number of `flakyFailure`/`flakyError` for FLAKY. Suite totals
are never read (P1 §1).

- `classname` missing → the suite's `name`. Duration: `time` in seconds → milliseconds, rounded.
- **Module:** the report path's segment before `target/` (`core/target/surefire-reports/...` → `core`; a root
  module gives `""`).
- **failure_hash:** SHA-256 of `<exception type>` + newline + `<first own frame>`, first 16 hex characters. The
  "first own frame" is the first `at ...` line whose class is **not** from the JDK or a test/build framework
  (`java.`, `javax.`, `jdk.`, `sun.`, `com.sun.`, `org.junit.`, `junit.`, `org.opentest4j.`, `org.apache.maven.`,
  `org.testng.`, `org.assertj.`, `org.hamcrest.`, `org.mockito.`, `kotlin.`, `groovy.`, `org.codehaus.groovy.`).
  **The line number is dropped**, so adding a line above the failing code does not change the hash. This is a
  heuristic (the plan says "the subject's own packages"); P3 can switch to an explicit package prefix per repo if
  it misfires. For FLAKY the first flaky element's `stackTrace` is used. With no own frame, the type alone is hashed.
- `failure_type`: the `type` attribute (for example `org.opentest4j.AssertionFailedError`).

Fixtures in `testdata/surefire/`. Real files come from the P1 experiment (Surefire 3.6.0, JUnit 6.1.3), regenerated
in P2 for the missing cases, with one hand-edited **wrong totals** file that says so in a comment (D-074).

## 10. Schema change (D-078)

Migration `00002_build_tested_tree.sql` adds `build.tested_tree_sha text NULL`, the git tree hash of what the
build tested. It is nullable because a build that cannot report it (no git checkout) is still ingested; R2 then
ignores it. `00001` is never edited (rule 9).

Plus Go idioms that appear for the first time in P2:

- **Struct tags** (`xml:"testcase"`, `json:"build_number"`) are metadata the decoders read through reflection, like
  Jackson or JAXB annotations.
- `errors.As(err, &maxErr)`, where `maxErr` is a `*http.MaxBytesError`, works like `catch (SpecificException e)` to
  pick the 413 answer.
- **Method values and closures** for retries: `do := func() error {...}` is passed to a retry loop, like a lambda
  passed to a retry helper.
- **Small interfaces at the consumer** again: the API handler takes `Ingester` and `BuildReader`, satisfied by
  `*ingest.Service` and faked in tests.

## 11. Verified while building

All of the following ran on 2026-10-04.

**Jenkins:**

- **Plugins:** `jenkins-plugin-cli` installed exactly the pinned closure. The running controller's
  `/pluginManager/api/json` lists 78 plugins, and every version equals `plugins.txt`.
- **JCasC names:** the JCasC export (`POST /configuration-as-code/export`) confirms the agent launcher is
  `inbound: webSocket: true` and the library is `modernSCM: libraryPath: "shared-library/"`. The canonical SCM key is
  `gitSource`; `git` was also accepted, but the YAML now uses the exported name.
- **Start-up order:** `init.groovy.d` runs after JCasC. The log shows JCasC and "Processing provided DSL script"
  first, then "buildlens: agent secret for 'agent-1' written". The agent then logged "WebSocket connection open",
  "Connected".
- **API checks:** anonymous `/api/json` gives 403; `agent-1` is online with labels `linux maven go`; the controller
  has 0 executors; jobs `buildlens` and `buildlens-lab` exist; credentials `github-token` and `buildlens-ingest-key`
  exist.
- **Reload:** `POST /configuration-as-code/reload` with a CSRF crumb from the same session answers 302 and re-runs
  `seed.groovy`. That is what `tasks.ps1 seed` does.
- **No clicks from scratch:** `tasks.ps1 up` under a throwaway project name, with every volume empty, started all
  five containers in 17 s. The agent connected about 5 s later.

**Bugs found by running, now fixed:**

- **Shared volume owner.** When two containers mount the same new, empty named volume, the *first* one to mount it
  copies its image's directory, owner included, into the volume. The agent image had created `/buildlens-agent` as
  root, so the controller could not write the secret. Both images now create it as uid 1000.
- **PowerShell to `sh`.** Piping a string from PowerShell to a native program ends it with `\r\n`, so `sh` read
  `esac\r`. `tasks.ps1` strips `\r` inside the container before `sh` reads the script.
- **Groovy 2.4 bytecode.** It cannot emit Java 21 bytecode, so GMavenPlus needs `targetBytecode 1.8`. Test classes
  compiled that way run on JDK 21.
- **testcontainers on Windows.** When two test binaries start containers at the same moment, Docker detection
  sometimes fails ("rootless Docker is not supported on Windows"). An explicit `DOCKER_HOST` avoids the detection:
  3 of 3 parallel runs passed. Only `test -Quick` needs this; the `-race` run uses Linux.
- **Not a code bug, but it happened:** Docker Desktop's engine stopped while three images were building in
  parallel next to the running stack and two kind clusters. The images are now built one at a time.

**Go:**

- **Filenames in uploads.** `multipart.Part.FileName()` keeps only the base name, which would lose the module. The
  handler reads the raw `Content-Disposition`. The handler test checks that modules `core` and `api` arrive.
- **CLI to server with real data.** Run in the agent image on the lab project, the CLI found 3 reports and the
  server stored 9 tests with modules and `LOCAL`. This was a throwaway repository `local/precheck`, deleted
  afterwards.
- **Parameterised test names.** JUnit names them `makesSlugs(String, String)[1]`. P3's excludes file must handle
  that form; P1 §2 left it unverified.
- **The tests can fail.** Two deliberate breakages in the library, a renamed `--tree` flag and an UNSTABLE stage
  recorded as SUCCESS, each failed one JenkinsPipelineUnit test.

**Memory, idle, all five containers** (`docker stats`, 16:11 UTC, agent connected, no build running):

| Container | Used | Limit |
| --- | --- | --- |
| postgres | 25.3 MiB | 384 MiB |
| grafana | 318.9 MiB | 384 MiB |
| buildlens-server | 3.2 MiB | 64 MiB |
| jenkins | 407.2 MiB | 1 GiB |
| jenkins-agent | 63.0 MiB | 1.5 GiB |
| **total** | **about 818 MiB** | 3,392 MB |

Memory during a Maven build is measured on the first real lab build.

**End to end, after the token was added and both repos pushed:**

- **Lab build #2** (`buildlens-lab/main`, commit `868bbe8`) ran on `agent-1`:
  - Jenkins loaded the library from GitHub (`Loading library buildlens@main:shared-library/`, commit `d358ec1`).
  - Branch indexing used the token.
  - Three timed stages ran, Maven ran 9 tests, and the result was SUCCESS.
  - `buildlens report` printed `{"id":2,"created":true,"tests":9,"stages":3}`.
- **`GET /api/v1/builds/2`** returns:
  - the 3 stages with times: Checkout 7,229 ms, Build 5,349 ms, Test 8,932 ms;
  - 9 PASSED tests with modules `core` and `api`;
  - agent `agent-1` with lifecycle `LOCAL`, which proves the agent's environment reaches `sh` steps;
  - `tested_tree_sha` `f978170...`, which equals `git rev-parse HEAD^{tree}` of the pushed commit, computed locally.
- **Confirmed under real CPS:** `System.currentTimeMillis()`, `currentBuild.rawBuild.getLog(500)` and the
  `env.BUILDLENS_STAGES` append all work in a trusted global library. The ingest key does not appear in the console;
  only the arguments are printed.
- **Peak memory during the build** (29 `docker stats` samples about every 2–3 s; a short spike could be missed):

| Container | Peak |
| --- | --- |
| jenkins | 620 MiB |
| agent | 318 MiB |
| grafana | 265 MiB |
| postgres | 41 MiB |
| server | 4.7 MiB |
| **sum of one sample** | **about 1.19 GiB** |

**Incident: lab build #1 failed.**

- **What happened:** at its first step, `currentBuild.currentResult` threw "No build record buildlens-lab/main#1 could
  be located". `rawBuild` was null, so `reportBuild` only warned and build #1 is not in BuildLens. The controller
  log then said `JENKINS-23152: .../branches/main/builds/1 already existed ... will create a fresh build #2`.
- **Most likely cause:** the `main` branch job object was replaced while #1 ran. Two branch indexings overlapped
  right after the controller restart: one at start-up, right after JCasC's Job DSL updated the multibranch job, and
  one triggered by hand 20 s later. The running build then could not find itself.
- **Not proven:** I did not reproduce it.
- **Watch in P3:** whether a JCasC reload, or a restart while a build runs, repeats it. If it does, JCasC's `jobs:`
  should not re-run on every start.

**Harmless warning:** the github-branch-source plugin tries to set a commit status on GitHub and gets 403, because
the token is read-only by design (D-014). Removing the warning needs either *Commit statuses: write* on the token,
or a notification-skip trait from another plugin (owner's choice).

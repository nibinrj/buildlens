# Surefire / Failsafe report fixtures

Parser tests (`internal/surefire`) read these files. Real reports came from a small Maven project run on
2026-10-04 with Surefire/Failsafe 3.6.0, JUnit Jupiter 6.1.3 and JDK 21.0.10 (`lab.core.Calculator` and its tests,
with deliberate failures). The only change to the real files: the `<properties>` block is emptied, because it held
the machine's paths and user name. Every `testcase` is exactly as the plugin wrote it.

| Folder | Source | What it covers |
| --- | --- | --- |
| `rerun/` | real, `-Dsurefire.rerunFailingTestsCount=2` | passed, failure + 2 rerunFailure, error + 2 rerunError, skipped, flakyFailure, flakyError |
| `no-rerun/` | real, no reruns | the same tests without rerun elements: plain failure and error |
| `failsafe/` | real, maven-failsafe-plugin, reruns=2 | Failsafe report: passed, failure + 2 rerunFailure |
| `wrong-totals/` | **hand-edited** copy of `rerun/` | suite totals inflated as in SUREFIRE-1627 (the bug did not reproduce on 3.6.0, decision D-074) |
| `malformed/` | **hand-made** | a truncated report, which must be rejected |
| `no-classname/` | **hand-made** | a testcase without `classname` (optional in the xsd) |

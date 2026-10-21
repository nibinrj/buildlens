# 003: Deterministic detection with retained evidence

Status: accepted implementation. Date: 2026-10-19.

## Context

CI failures have several causes. A test that failed on an unavailable agent must
not become evidence for quarantine. A timing outlier must not silently become a
production regression claim. The project needs rules a Java developer can explain
and a reviewer can reproduce from stored build IDs, not an opaque scoring model.

## Decision

Apply infrastructure attribution before test detection. Exclude attributed
observations from R1/R2/R3. Preserve exact thresholds in configuration and exact
build IDs in evidence. R1 uses Surefire rerun recovery, R2 opposite outcomes on the
same tested content tree, and R3 default-branch outcome flips. An absent tree ID
does not qualify for R2. These are flaky-test rules; test identity alone does not
establish that every observed failure has the same root cause.

For successful comparable durations, use median/MAD with a minimum sample count,
sustained default-branch checks and the configured PR jump/consecutive policy.
Exclude the evaluation cohort from baseline construction. Persist sample values
and IDs so later corrected uploads do not rewrite the historical explanation.
The exact defaults and boundary tests live in config.yaml, internal/rules and
internal/regression. ADR 002 records regression/cost/comment implementation detail.

## Consequences

Determinism makes rule behavior auditable, not universally accurate. Reruns can
hide a persistent fault; same-content failures can still depend on the environment;
short tests are sensitive to small timing changes. Results preserve incidental
alerts. Snapshot-copy checkout and network checkout are different operations and
must not be interpreted as a code-performance comparison. The current schema
does not partition baselines by execution environment. A later cohort design
must be evaluated against real observations before changing these rules.

The injected lab proves mechanics. Production precision/recall requires the
separate approved IDoFT benchmark and is not established by fifty local builds.

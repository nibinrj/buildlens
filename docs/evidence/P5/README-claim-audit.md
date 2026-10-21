# README evidence and limits

This audit distinguishes measured batch results from implementation and separate
verification. Numerical batch claims are generated from database/driver/memory
exports into docs/results.md; the README deliberately avoids benchmark claims.

| README claim | Supporting evidence | What docs/results.md alone does not establish |
| --- | --- | --- |
| Infrastructure rules run before flakiness | Go rule/ingest integration tests; P3 study/evidence | No live infrastructure fault is injected in the new batch |
| R1/R2/R3 deterministic detection | Table-driven Go rules tests and retained P3/P5 evidence | No natural-flake precision/recall or guarantee every rule fires in this batch |
| Quarantine exclusion, non-blocking stage and release | P3 activation/exclusion/release console/API evidence and P5 stored observations | Isolated passes do not prove the underlying flake was fixed |
| Median/MAD regression checks | Rule tests, P4 baseline/sustained local evidence and live PR | No production false-positive rate; different checkout methods are not comparable work |
| One durable sticky PR comment | Real PR #1 comment snapshots and desired/sent generations in P4/live-20261019; failure-path tests | Live rate-limit/permission incidents were simulated, not exercised against GitHub |
| Explicit busy-time equivalent cost, unknown stays null | P4 and P5 API/database cost records and rational-arithmetic tests | No AWS spend, multi-agent accounting, idle/network/storage/CPU-credit billing |
| Four file-provisioned dashboards and SELECT-only role | P5 Grafana health/query responses, provisioning metadata, UPDATE-denial check and four signed-in screenshots | Screenshots are mid-batch views, so totals can differ from the final export |
| Sequential REST driver preserves identity and failures | Actual 50-build queue/executable/upload checkpoint; driver failure-path tests | Not a distributed scheduler; timeouts retain the submitted work for inspection rather than resubmit |
| No UI setup and pinned stack | Existing fresh-stack evidence plus current tasks up/provisioning checks and source pins | The current run does not erase volumes or claim a second fresh-install experiment |
| Root Jenkinsfile runs the checks | Source, pipeline tests and hashed local Jenkins execution | Normal multibranch cannot discover the uncommitted file until owner commit/push |
| Custom dump and isolated restore | pg_dump/pg_restore execution, archive checksum and restored-data comparison | Owner-only live replacement remains unexecuted |
| Static Go binaries; no Kafka/ML/web framework | CGO_ENABLED=0 builds, Dockerfiles, go.mod and architecture | No implied throughput or deployment-resilience guarantee |
| Later benchmark, AWS and MCP work is not current capability | Plan boundaries | These are explicitly unverified and excluded from delivered capability claims |

Ports, tool versions, limits, authentication behavior and task syntax are
configuration/code facts rather than measured performance claims. Prior-art links
identify existing products; no equivalence, accuracy or superiority comparison is made.

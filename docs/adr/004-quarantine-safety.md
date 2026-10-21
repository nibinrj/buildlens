# 004: Keep quarantine visible and bounded

Status: accepted implementation. Date: 2026-10-19.

## Decision

Use a repository-wide cap: the smaller of configured percent and maximum count.
The owner-approved small-suite exception permits one quarantine when a positive
percentage rounds below one; it does not permit quarantining the whole suite.
At the cap, record detection evidence as an alert rather than silently removing
another blocking test. Infrastructure-attributed failures never advance detection.

Fetch the list before the blocking stage. Failure to fetch it runs the full suite,
with a warning. An empty quarantine-only selection skips that stage because an
empty Surefire includes file would otherwise run every test. Selected quarantine
tests run visibly in a non-blocking stage: stage UNSTABLE can coexist with overall
SUCCESS. Preserve separate BLOCKING and QUARANTINE observations in the upload.

Release requires consecutive passing quarantine observations; failure resets the
streak. Manual actions are recorded with their origin. Keep activation/release
history and a watermark so a released test is not immediately reactivated solely
from the same old evidence. Handler and real-Postgres tests cover failure paths.

## Consequences

Quarantine is mitigation, not a fix. Isolated quarantine execution can remove an
order-dependent test's polluter, allowing release without fixing shared state;
the lab has demonstrated this limitation. The cap can leave other injected flakes
in the blocking stage. Reruns and failure policy still matter. Current state,
activation events, releases and cap alerts are separate dashboard concepts, and
none is a production precision measure.

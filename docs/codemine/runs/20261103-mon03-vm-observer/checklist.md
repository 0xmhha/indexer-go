# MON-03 checklist

- [x] Reconcile D #266 clean main; record scope/RED/acceptance and C/D contexts.
- [x] Compiled parser/health/lifecycle RED and GREEN.
- [x] Strict KiB/decimal/overflow/root report parsing; zero distinct from UNKNOWN.
- [x] Bounded stdout/stderr, fixed diagnostics, no credential/payload retention.
- [x] One owned nonce/attempt; late result and pending cleanup admission fences.
- [x] Separate process cancel/kill/actual wait/exact owned resource cleanup.
- [x] Immutable data time and separate missing/degraded observer health.
- [x] Direct/periodic/never-started and concurrent cancel/join tests.
- [x] Focused race, one final new-case CPU1/race/count200, build/vet/lint.
- [x] Docs/state/backlog/policy/secrets and unchanged operating config.
- [x] Prepare normal PR and exact-head short CI/squash/reviewed-tree/main gates.

This precommit snapshot records implementation/local checks. Actual PR/CI/merge/
main gates are confirmed in Git/PR metadata, never pre-certified by preparation.

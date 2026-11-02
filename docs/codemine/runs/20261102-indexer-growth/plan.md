# Offline indexer growth assessment

The user asked to proceed after the operating-input handoff. APFS/enode/
authenticated checkpoint/private deployment and actual backup inputs remain
HOLD. Select the next offline monitoring gap: DB budget exhaustion risk from
two explicitly supplied saved snapshots. Do not re-run input discovery or N6.

Acceptance and order:

1. Add a compiled behavioral RED through the real command: current indexer
   usage is below capacity warning, but a valid prior sample and explicit growth
   policy require a critical forecast. The old command must fail this contract.
2. Extend the existing strict/private bounded policy/snapshot reader with an
   optional `--previous-snapshot FILE`; retain existing single-sample behavior.
   No environment, implicit clock, probes, path traversal, output identity or
   new persistence. Final reread includes all explicitly selected files.
3. Opt-in `indexer_growth` policy pins a nonzero lowercase64-hex `series_id`
   declared for a particular DB/schema/measurement incarnation. The indexer
   storage entry in both snapshots carries that same ID. Reset it after restore,
   replacement or measurement-definition changes. IDs are submitter claims,
   not authenticated physical/SQL provenance; never echo them.
4. Require known matching profile/chain/original anchor, current generation,
   source series and positive unchanged byte budget. Time must increase within
   explicit min/max interval limits and current sample must meet existing age
   rules. Rebind, changed budget, missing/stale observations or nonpositive
   growth yield HOLD; malformed/contradictory inputs fail closed. Previous
   diagnostics are not made current or substituted for current diagnostics.
5. Classify remaining bytes at the observed two-point positive net growth:
   stop horizon < warn horizon, inclusive comparison with128-bit integer
   products of bytes and nanoseconds. Avoid float rounding, truncation and
   uint64 overflow. Emit fixed codes only. Preserve current independent hard
   failures, authority/HOLD/redaction/cancellation/output limits.
6. GREEN and focused edge/strict-file/CLI/no-mutation/actual product compatibility
   tests; relevant Go build/fmt/vet/lint/race and existing CI policy/docs checks.
   No new concurrent runtime logic or new stress200 campaign; no full SDK/DB/
   performance repeat for this isolated evaluator.
7. Update current guide/backlog/checklist and n/n−1/n−2 continuity, preserving
   E1/first-import/N6 plans. Explicit stage, secret review, Conventional Commit,
   normal PR, actual applicable short CI, squash and clean main sync.

No actual DB/RPC/peer/node/App/Docker/watcher/notification execution, disk
format/provisioning, operational GC/restore or additional performance. Keep
pool4/part2/API1/seven writers, shared physical RPC5, two EVM passes and NEW final
canonical/fence/COMMIT/exact ACK unchanged. Other projects/resources are untouched.

Live trend sampling, hysteresis/latch/notification integration and operational
calibration remain distinct held work. This two-point estimate makes no capacity
guarantee and grants no stop/resume or deletion authority.

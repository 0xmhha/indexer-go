# Arc first-import checklist

## This design change

- [x] Audit existing ETH/68, App/E1, native replay, bootstrap and GC interfaces.
- [x] Audit exact pinned upstream Full persistence/bootstrap APIs and normal vs
  unwind store order; distinguish source proof from behavioral qualification.
- [x] Select App-embedded Go supervisor/owned Rust writer, existing second replay,
  shared RPC5/Store4, bounded channel/durable raw and filtered child environment.
- [x] Define staged native bootstrap, original anchor, completed receipt/read gate,
  reorg/epoch/crash and independent E1 publication/ACK/GC authorities.
- [x] Record permanent raw-coverage gap and forbid ingress GC before exact proof
  and coordinator guard. Preserve E3 run lease/current permit/source ACK.
- [x] Record N0-N6 RED→GREEN milestones and external-input HOLD boundaries.
- [x] Update current backlog/readiness/index and n/n−1/n−2; preserve accepted E1.
- [x] Finish document/source consistency and staged/history secret checks.
- [x] Define release gate: exact-head CI5 success before guarded squash/main sync.

Release execution is recorded in the associated PR's actual checks and merge
metadata, not pre-certified by this documentation checklist. Do not interpret
the release-gate item as a claim that CI has already run.

## Implementation milestones

- [x] N0 bounded frame/private pending fence and real pinned SDK qualification; [checks](../20261029-arc-native-n0/checklist.md).
- [x] N1 authenticated native bootstrap builder; [checks](../20261029-arc-native-n1/checklist.md), release gates remain separately recorded.
- [x] N2 first-execution writer, durable intents/receipts and reader gates; [checks](../20261029-arc-native-n2/checklist.md), actual release gates recorded separately.
- [x] N3 App-owned ingress, bounded raw/channel lifecycle and shared limits; [checks](../20261030-arc-native-n3/checklist.md), actual release gates recorded separately.
- [x] N4 managed collector and receiver-authorized reorg/restart bridge; #245/CI318 released.
- [x] N5 permanent ingress bytes, authoritative GC/retention and held deployment declaration implemented; [N5 evidence](../20261030-arc-native-n5/checklist.md) and final [declaration release gate](../20261031-native-runtime-alignment/checklist.md). No operational activation.
- [x] Managed App file preflight/read-only raw/native/journal/SQL-export inspection; [scope and qualification](../20261031-managed-inspection/checklist.md), no integrated runtime authority.
- [x] N6 actual App/mock ETH68/SDK/SQL/API/ACK/restart/reorg/GC/exact recovery acceptance; [qualification checklist](../20261031-arc-native-n6/checklist.md) separates local checks and actual CI/merge release identities.

Actual APFS/private deployment identity, official enode/fork/history contract,
authenticated checkpoint and backup/restore remain HOLD. No actual service,
format, live chain or additional performance measurement is authorized here.

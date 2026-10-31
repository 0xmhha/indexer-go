# Managed App offline inspection

Current n follows held runtime alignment #256 (n−1) and App/E3 coordinator #255
(n−2). Preserve [N5](../20261030-arc-native-n5/plan.md), accepted
[first-import](../20261029-arc-first-import/plan.md) and
[E1](../20261023-e1-execution/02-design.md) plans. N6 combined SDK/App/SQL
acceptance remains separate; no operating approval.

1. Add a pure product App hook to file preflight. Arc registers its existing
   ingress module and compares actual declared native/replay/spool/raw/socket/
   peer identity, prepared-only flags and quota/layout. Selected absent identity/
   path inputs remain HOLD in detached validation copies; known invalid policy,
   schema or numeric limits remain FAIL. Permit the exact managed source-parent
   scratch relationship only. Account for raw and source COW in declared
   headroom/backup; legacy and other products keep existing behavior.
2. Export execution ingress/retention/raw metadata through a read-only repeatable
   SQL snapshot on the existing pool. No DSN-taking CLI, opener, migration,
   writer, advisory maintenance lock or new RPC/client/pool. Validate export
   shape and explicit absence; a supplied artifact is unauthenticated historical
   evidence, never live freshness or cross-store authority.
3. Extend the existing product diagnostic through optional callbacks and
   explicit raw/artifact selection. Read only bounded fixed native I/head/
   receipt, raw control/store metadata and coordinator journal outside COW.
   Never acquire writer/run locks, open stores, scan payload/DB trees, create
   files, recover, execute children or follow paths inside journal/config.
   Recheck file samples, ownership, canonical seals and cancellation. Immutable
   conflicts/corruption FAIL; missing/changing/pending or separate mutable
   observations HOLD. Completed historical maintenance is not current head.
4. Add sanitized R/N/public/epoch/phase observations under existing report bounds;
   retain not_authorized/not_performed/separate-samples contracts and unknown
   values. Update CLI/diagnostic-only Docker bindings, consumer compatibility,
   documentation and feature checklist. Real host mounts/images remain HOLD.

For each unit: compiled behavioral RED, implementation, GREEN and hostile/
missing/mismatch/cancel/no-mutation cases. Use owned fixtures and fsync-on
disposable PG only. Run affected short/build/fmt/vet/lint/CLI/docs/policy/leaks.
No new goroutines/runtime concurrency is intended; add targeted CPU1/race/200
only if new concurrent logic is introduced. Do not repeat unchanged SDK/stress.
Keep default CI scoped short, then guarded squash and clean main sync.

Preserve pool4/part2/API1/writers7, two EVM passes, shared physical RPC5 and
NEW final fresh → post-response generation fence → COMMIT → exact ACK.
Actual nodes/peers/deployment, format/provisioning, operational GC/restore and
performance/host-load measurement remain HOLD; preserve other projects/config.

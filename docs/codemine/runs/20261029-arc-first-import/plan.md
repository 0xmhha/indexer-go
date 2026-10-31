# Arc first-import implementation plan

The plan implements the missing Arc first execution and continuous native DB
update module. N0 now implements bounded framing/private pending fences and
qualifies the real pinned SDK; see [N0](../20261029-arc-native-n0/plan.md).
N1 implements offline authenticated native bootstrap; see [N1](../20261029-arc-native-n1/plan.md). N2 implements the first-execution writer and durable publication; see [N2](../20261029-arc-native-n2/plan.md). N3 implements App-owned ingress; see [N3](../20261030-arc-native-n3/plan.md). N4 implements managed replay and E1 reorg integration; see [N4](../20261030-arc-native-n4/plan.md). N5 permanent coverage/owner maintenance and held runtime declaration are implemented with [per-feature evidence](../20261030-arc-native-n5/checklist.md). Managed file inspection #257 is released. [N6](../20261031-arc-native-n6/plan.md) implements and locally qualifies the combined actual App/mock ETH68/SDK/SQL/API/ACK/reorg/GC/recovery boundary; per-feature CI/merge metadata records release. The runtime has owned offline qualification, not operating approval.
The [canonical design](../../../ARC_NATIVE_IMPORT_DESIGN.md) owns architecture
and safety boundaries; this plan owns sequence and acceptance.

Use an opt-in Go supervisor inside the existing Arc App, registered by the Arc
bundle through a new generic lifecycle factory. Reuse ETH/68 and bounded Go
channels/disk references. It owns a Rust native writer using pinned official
Arc EVM primitives, without NodeBuilder/services or upstream modifications.
The existing collector retains the second EVM execution. No independent RPC
client or SQL pool; never inherit the App's secrets into the Rust child.

Preserve pool4/part2/API1/seven logical writers, shared5 physical HTTP attempts/s
including retries/failover/batch fallback, final NEW uncached canonical response
then generation fence then COMMIT, and exact committed source ACK. Native
admission/receipt is not finality, publication, ACK or deletion authority.
Native import epoch, raw epoch and receiver-owned E1 generation are distinct.

## Milestones

1. **N0 — pinned SDK and bounded contract.** Qualify first-execution outcome→Full
   native storage without a third execution, native-v2/genesis/H bootstrap
   requirements, RO secondary coexistence/catch-up, commit and unwind crash
   fences. Use only owned temporary library DBs. Compile probes are supporting
   evidence; meaningful requirement-failing tests must precede adapter behavior.
2. **N1 — authenticated native bootstrap.** Empty staged target, complete root/
   code/storage/preimage and exact anchor artifacts, retained floor and immutable
   original anchor. Atomic verified publication and H+1 restart. PostgreSQL
   baseline import is not native DB initialization. Reject partial or mismatched
   coverage and preserve existing paths.
3. **N2 — native writer.** Owned bounded stdio, single first execution, writer
   ownership/run lease, publication/read-view gates, durable intent/completed
   receipt, duplicates, cancellation and each multi-store interruption. No
   automatic pruner or ambiguous partial-store recovery.
4. **N3 — embedded ingress.** Generic opt-in factory/lifecycle; Arc-only registration,
   existing ETH/68 and verifier, durable raw window and bounded count/bytes,
   shared App canonical budget and Store, preparation/backlog gates, filtered
   child env, bounded errors and shutdown joins. No existing config bypass.
5. **N4 — managed collector and reorg.** Exact native receipt/read epoch checks,
   RO refresh, existing receiver-authorized Rewind/recovery, short and deep reorg,
   transition intents and join ordering, final fresh still new. No Hello-derived
   ACK, self-assigned E1 generation or downgrade of a managed native DB.
6. **N5 — permanent coverage and retention.** Prove exact ingress raw bytes
   survive in the existing blocks part or add a reviewed versioned payload
   archive/migration there. Bind native/import/source identities without an
   eighth writer or pool. Implement coordinator-owned raw deletion guard with
   permanent coverage/exact committed ACK/lease/epoch/floor; until then preserve
   raw and stop at quota. Extend held runtime draft/private settings coherently
   for App-owned native RW/ingress, E3 stop/join of importer and collector, and
   explicit capacity. No production activation.
7. **N6 — offline integrated acceptance.** Two normal EVM executions, native
   reopen/collector/E1 writer7/PG/API/ACK, failure matrix and unchanged legacy
   paths. No live node/peer, new performance campaign or provisioning.

Each feature: RED→implementation→GREEN, appropriate local checks and any new
concurrency single GOMAXPROCS=1/race/count200, current docs/checklist update,
exact-head applicable scoped CI success, guarded squash and clean main sync before the next one.
SDK incompatibility is resolved in the adapter/plan, never by relaxing roots,
final fresh, generation/ACK/GC authority or retaining partial state as complete.

## Design acceptance

- Distinguish implemented reuse points, source-proven facts, proposals and
  unverified SDK/operational assumptions.
- Define both native bootstrap and continuing import, App/Go/Rust ownership,
  two EVM passes and shared budgets without additional standalone services.
- Define durable native completion/read gates, reorg and crash boundaries,
  independent E1 publication/ACK, exact raw coverage and fail-closed retention.
- Keep APFS/enode/authenticated checkpoint/private deployment inputs HOLD.
  No format, operational App/node/DB/Docker/peer, snapshot download, operational GC/restore or measurement. Owned mock App/native-library/disposable PostgreSQL fsync-on functional fixtures are allowed.
- Check document links/anchors, pins and unchanged runtime/config bytes; reuse
  exact-byte local race evidence transparently and require new final-head applicable CI.

Keep [n/n−1/n−2](continuity.json), the accepted E1 design/plan and older release
references. See [checklist](checklist.md) and [validation](validation.md).

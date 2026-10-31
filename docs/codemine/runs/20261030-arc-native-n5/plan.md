# N5 permanent ingress coverage and managed retention

N5 started from clean main07bb84c after N4 #245 and scoped CI #246–#248.
Legacy admission #249 and SQL/backend foundation #250 are released; App/Arc
actual owner/origin #251/CI327 is released at main2458776. Explicit raw deletion/recovery #252 is released at main867d1be.
[Graceful worker drain](../../../EXECUTION_DRAIN.md) #253/CI329 is released at
mainf63d0ff. The [managed native COW bridge](../../../EXECUTION_MANAGED_MAINTENANCE.md)
is released as #254/CI330 at main9af6e0f. The explicit
[App/E3 coordinator](../../../EXECUTION_MAINTENANCE_COORDINATOR.md) is implemented;
its [qualification](../20261031-arc-maintenance-coordinator/checklist.md) remains
distinct from the [held Docker alignment](../20261031-native-runtime-alignment/plan.md)
and combined N6 acceptance. The declaration alignment is implemented; its
checklist tracks qualification. Managed file inspection and N6 remain separate. The
[accepted first-import plan](../20261029-arc-first-import/plan.md) remains the
authority. Native n−1 is [N4](../20261030-arc-native-n4/plan.md); n−2 is
[N3](../20261030-arc-native-n3/plan.md). Preserve the accepted
[E1 design](../20261023-e1-execution/02-design.md).

Implement in reviewable steps; N5 stays incomplete until every step qualifies:

1. Close legacy node-command access to managed or partial native state and use
   the same sole-writer lock as the Rust importer, retaining unmanaged RO
   coexistence. Tests use an owned protocol helper, never a node.
2. Persist immutable SQL source ownership before API exposure, bind actual
   native/raw/source identity after SDK Ready, and reject ingest downgrades.
   Resolve exact native/raw origin outside SQL, then archive lossless input in
   the existing blocks part with parameter binding. Keep orphan bytes and
   actual original native epoch/publication generation distinct.
3. Authorize raw deletion with the existing E3 permit, all seven published
   parts, exact nonzero source ACK, immutable archived bytes and full deletion
   record. Persist authorization before unlink and require exact completion
   during recovery/release. Missing old-fork coverage retains data.
4. Gracefully stop admission, finish active native work, drain through exact
   public/ACK N, join importer and collector, and only then take run EX for
   explicit managed COW/retention. Preserve original identity/anchor/receipts
   and verify the actual retained floor before restart. The inherited bridge
   and completed-floor checks do not replace the App/E3 coordinator. Persist
   exact raw-first/COW/release/resume phases, including a zero-deletion raw
   completion; recover without inferring completion from absence. No ambiguous repair.
5. Update the held Docker/capacity/inspection draft for App-owned Arc ingress
   and remove the external Arc collector from that composition. Unknown actual
   APFS, enode, checkpoint, image and private inputs remain HOLD.

Each behavior requires RED→GREEN, affected functional/static checks and any
new concurrency's targeted local CPU1/race/single200. Default PR CI follows
[short policy](../../../CI.md); full SDK/stress qualification is explicit and
appropriate to the affected native behavior. Squash only after applicable
checks pass and synchronize main before the next released feature.

Keep pool4/part2/API1, seven writers, two normal EVM passes, shared physical
RPC5 and NEW final canonical response→generation fence→COMMIT→exact ACK.
No operational node/peer, disk format, deployment, GC/restore or additional
performance measurements. Owned mock/library/disposable fsync-on PG fixtures
are functional verification only.

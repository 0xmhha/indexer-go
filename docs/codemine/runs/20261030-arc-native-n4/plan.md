# N4 managed replay and coordinator recovery

Implement the accepted [N4 milestone](../20261029-arc-first-import/plan.md) after
N3 [CI316/squash244/main](../../../../verification/native-import-n3/release.json).
Keep the [design](../../../ARC_NATIVE_IMPORT_DESIGN.md) and E1 authority intact.

1. Give the managed RO child the same inherited view OFD held by Go. Acquire
   turn→SH before startup and every operation; retain SH through complete EXR1,
   receipt/epoch and proof validation or cancellation plus child join. Explicitly
   unlock per operation. Join public operations before closing FD/run lease.
   Rust validates and borrows fd3 without a nested turnstile acquisition. This
   is an explicit trusted local caller contract, not proof of lock ownership
   inferred from file metadata. Preserve unmanaged legacy behavior/refusal.
2. Extend native completion with immutable epoch bases/transition receipts and
   an explicit bounded coordinator-bound transition. Join both DB children
   before exclusive unwind. Write intent before SDK mutation; preserve ambiguous
   partial stores. Only exact completed transition receipt plus published head
   can resume the same SQL marker idempotently. Original anchor never moves.
3. Add a durable pending-check read/publication fence owned by the SAME E1
   coordinator before common-parent RPC search. Existing receiver validates the
   canonical proposal, existing finder chooses the public target and existing
   BeginExecutionRewind advances generation. Recover native while SQL is fenced,
   then finish existing SQL recovery/final NEW fresh before reopening workers.
4. Own collector/spool inside ingress; use a bounded proposal on its existing
   IPC session, never another generation authority. Handle native-ahead and
   raw-only forks separately, recreate the whole peer, and retain old raw data.
   Missing history/deep forks/partial state stay HOLD. Introduce explicit raw
   control schema compatibility with complete proofs, never legacy adoption.

Each behavior: meaningful RED→GREEN, current docs/checklist/n/n−1/n−2, new Go
concurrency in one CPU1/race/single200, actual native library/process fixtures,
required full local gates, exact-head CI5, guarded squash and clean main before
N5. Preserve two normal EVM passes, pool4/part2/API1/writers7, shared RPC5 physical
attempts, NEW fresh→generation fence→COMMIT→exact ACK. Native receipt is no ACK.
No operational node/peer/user DB, formatting, snapshot download, operational
GC/restore or performance campaign. Owned mock protocols/native libraries/
disposable PostgreSQL fsync-on are functional fixtures. External inputs HOLD.

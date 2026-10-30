# Managed Arc native maintenance bridge

The opt-in library bridge connects `nativeops.Native.Managed` to the existing
`nativestore.Compact`, `Release` and `WithRetentionSnapshot` operations. It
supports only audited Arc testnet5042002 and the pinned native-v2 layout. It
starts no node and creates no RPC client or PostgreSQL pool. Operational
activation, APFS provisioning and performance measurements remain HOLD.

## Caller and child ownership

The caller supplies a frozen `ManagedContext`: version1, original canonical
native directory and `nativeinput.Proof` for its sealed completed head. The
immutable identity, bootstrap digest, original anchor, receipt and epochs must
match the actual target. Copying or stopping a writer is not deletion authority.
The [explicit App/E3 coordinator](EXECUTION_MAINTENANCE_COORDINATOR.md) uses the
[App drain boundary](EXECUTION_DRAIN.md) and independently obtains the existing
E3 permit and live SQL lease. All seven public parts, exact
source ACK/admission and archived raw input coverage remain mandatory.

`nativestore` acquires its existing run EX, then invokes optional
`RunLeaseBinder.BindRunLease`. The Arc adapter returns an immutable bound copy;
it never changes the caller's adapter. Compact, release and snapshot each bind
the descriptor they actually own. The child borrows the same open-file
description as fd3 through process join; no child reacquires run SH or unlocks
its parent's EX. Read-only pre-permit inspection uses `Native.WithRunLease`
with an explicitly owned exclusive descriptor. Hold/prune/verify additionally
require the exact bound permit. A legacy permit with public height ahead of
frozen ACK/native N is refused by this managed bridge.

The parent writes a bounded private context in its owned child scratch
and passes internal `--managed-context`. Rust checks fd3 against the original
sibling run-lock inode/owner/mode, pins target/parent directories, checks the
canonical context and sealed I/head/bootstrap/receipt, and verifies actual SDK
head/root/completion. Descriptor metadata alone is **not** proof of kernel lock
mode or a live SQL lease: those belong to the trusted Go coordinator. No JSON
file or historical permit substitutes for that coordinator.

Managed commands are restricted to inspect, hold, prune and verify. Prune still
requires the exact sealed COW candidate/parent intent and current permit digest.
It never prunes the operational directory directly. Generic legacy commands
continue refusing managed/partial native storage; there is no allow-managed
bypass. Existing backup/checkpoint command authority is unchanged.

## Completed floor and restart

A managed history floor above the original anchor requires both the sealed Go
`.execution-retention-floor` and Rust `.arc-retention-native.json`. Their exact
permit/request/digest/cutoff must agree. Require full coverage, original chain/
anchor and a current native floor proof. Regenerate the cutoff seed from exact
retained SDK state and compare its digest. Partial, mismatching or missing
completion is HOLD; a native prune checkpoint alone is insufficient.

Original I/bootstrap/anchor and all immutable input/epoch receipts survive COW.
A completed floor is not a new identity or an E1 generation. The new history
floor is Through; native import/read epochs and SQL publication generation
remain independent. The normal two EVM passes remain unchanged.

Validate the complete floor at writer open, native epoch transition and managed
maintenance. The managed RO child validates it once under its lifetime run SH,
then checks that the native floor stays unchanged on each snapshot. Full seed
validation is not added to every ordinary block/read. The completed native head
may later advance or rewind above the cutoff; the old retention head is a
historical observation. Validate the original anchor, immutable cutoff seed and
current retained state rather than requiring the old ACK/head to remain the
current branch. Native rewind below the retained floor is refused before an
intent is written.

## Verification and remaining integration

Behavioral RED reproduces the absent adapter lease binding, unavailable Rust
managed admission, an ahead-public permit and acceptance of an unauthenticated
advanced floor. GREEN covers immutable binding, actual inherited fd identity,
normal/canceled child join, parent EX preservation and release, common COW/
snapshot/recovery and actual SDK managed checks. The long owned SDK fixture
generates/imports132 real Arc EVM blocks, prunes to Through3 with Keep129,
verifies the completed floor, restarts, imports the next block and rewinds to131
without moving I/anchor. It also rejects missing Go completion and below-floor
rewind. Legitimate empty state-change blocks use a dedicated test generator;
existing short fixtures retain their nonempty-state assertion.

Go child/protocol/COW fixtures and actual Rust SDK fixtures are separate.
Combined App/mock ETH68/SDK/SQL/GC acceptance is still N6. The separate
[App/E3 coordinator](EXECUTION_MAINTENANCE_COORDINATOR.md) implements explicit
drain → independently observed plan → E3 fresh authorization → raw GC/recovery
first → native/source COW under the live lease → E3 fresh completion → exact
file release → verified resume. Its durable journal distinguishes zero-deletion
raw completion from an unstarted phase. This remains an explicit library call;
manual artifacts or a drain point alone grant no maintenance authority. Held
Docker/capacity/inspection composition and N6 remain incomplete.

Pool4/part2/API1, seven logical writers, physical RPC5 and NEW final canonical
response → generation fence → COMMIT → exact ACK stay unchanged. Default CI
remains [scoped short](CI.md); affected full SDK and new lifecycle race200 are
local qualification. See the [N5 checklist](codemine/runs/20261030-arc-native-n5/checklist.md).

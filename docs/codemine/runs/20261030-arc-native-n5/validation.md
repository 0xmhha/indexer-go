# N5 current qualification

The first admission subfeature reproduced legacy child startup through managed,
partial/orphan/bootstrap markers and an already-held importer writer lock.
The existing helper executed no node or native SDK. Implementation now uses
the exact pinned Rust sibling writer lock and run-SH→writer-EX order; ownership
is inspected while that lock is held, and both leases last through child join.

The three new top-level cases pass one actual CPU1/race/count200 invocation.
They prove pre-child refusal, importer-start exclusion, retained unmanaged RO
coexistence, cancellation/join and writer reacquisition after the child exits.
Existing affected guard cases and build/vet/lint also pass. The N5 checklist
continues to distinguish this subfeature from permanent SQL ownership/archive,
raw deletion and managed maintenance that remain incomplete.

The CI priority is released: #246/CI321 short code span432s, #247/CI323 removes
duplicate main runs/cache files, and #248/CI324 proves documentation-only CI
in17s without Go/Rust/Docker setup. Times exclude queue and make no runtime
performance claim. All three squash merges were cleanly synchronized.

The admission subfeature passed actual CI325 and was squash-merged as #249,
then synchronized cleanly at main6daa40c with the exact reviewed tree and one
parent/base. Private config remained unchanged.

The next [archive foundation](../../../EXECUTION_INGRESS_ARCHIVE.md) has separate
RED assertions for missing ownership, missing blocks archive, pre-SQL origin
failure, invalid header layout and cancellation precedence. SQL owner/archive,
atomic rollback, seven-part publication, differing counters, corrupt bytes,
missing pinned owner, migration and undo ownership pass10 owned PG cases.
Backend functionality and its new concurrent input/cancellation case pass a
single CPU1/race/count200 invocation. Root build/vet and affected lint pass.
The complete affected PostgreSQL short suite passed143.418s; core/port and
execution short packages also passed. This foundation's PR gate remains pending.
Actual App/Arc resolver/binding and the remaining N5 steps are not implemented.

Operational launch, provisioning, GC/restore and additional performance
measurements remain HOLD.

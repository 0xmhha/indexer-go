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

The [SQL/backend foundation](../../../EXECUTION_INGRESS_ARCHIVE.md) passed actual
CI326 and was squash-merged as #250. Clean mainadd9e59 has the reviewed tree
and one parent/base. Its owned PG owner/archive, atomic rollback, seven-part
publication, differing counters, corruption, migration and undo cases passed;
the affected PostgreSQL short suite passed143.418s. Its new backend concurrency
case passed one local CPU1/race/count200 invocation.

Current App/Arc integration has separate compiled behavioral RED assertions for
pre-RPC config removal, absent origin contract, exact native/raw handoff, blocked
native-view cancellation, changed owner, mutable policy and lost baseline.
[RED](../../../../verification/native-ingress-origin/red.json),
[source](../../../../verification/native-ingress-origin/source.json) and
[checks](../../../../verification/native-ingress-origin/checks.json) distinguish
these from an earlier fixture-only compile correction, which is not RED proof.

Current affected short checks pass. Five selected App contract cases pass on
owned fsync-on disposable PostgreSQL, including removal refusal with zero RPC.
Three new origin lifecycle cases pass one CPU1/race/count200 invocation94.451s:
Ready/resolution state ownership, pre-SQL lease release with a memory-only loan,
and Stop cancellation/join while native EX stays held. Root build, affected
vet/lint pass. Actual pinned SDK plus App/mock ETH68/SQL integration is reserved
for N6; these source/fixture tests do not certify that combined path. Current
integration PR and its applicable short CI gate remain pending. Authoritative
raw GC, graceful drain/managed maintenance and held Docker remain incomplete.

Operational launch, provisioning, GC/restore and additional performance
measurements remain HOLD.

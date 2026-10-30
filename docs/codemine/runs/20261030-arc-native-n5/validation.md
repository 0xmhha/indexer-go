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
distinguishes released SQL ownership/archive, raw deletion and drain from
the remaining explicit App/E3 phase coordinator and combined acceptance.

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

App/Arc integration #251 has separate compiled behavioral RED assertions for
pre-RPC config removal, absent origin contract, exact native/raw handoff, blocked
native-view cancellation, changed owner, mutable policy and lost baseline.
[RED](../../../../verification/native-ingress-origin/red.json),
[source](../../../../verification/native-ingress-origin/source.json) and
[checks](../../../../verification/native-ingress-origin/checks.json) distinguish
these from an earlier fixture-only compile correction, which is not RED proof.

Its affected short checks pass. Five selected App contract cases pass on
owned fsync-on disposable PostgreSQL, including removal refusal with zero RPC.
Three new origin lifecycle cases pass one CPU1/race/count200 invocation94.451s:
Ready/resolution state ownership, pre-SQL lease release with a memory-only loan,
and Stop cancellation/join while native EX stays held. Root build, affected
vet/lint pass. Actual pinned SDK plus App/mock ETH68/SQL integration is reserved
for N6; these source/fixture tests do not certify that combined path. App/origin #251 passed exact-head CI327, was squash-merged and synchronized
cleanly at main2458776 with the reviewed tree, one parent/base and private config
unchanged. Explicit raw GC/recovery #252/CI328 is released at main867d1be.
Graceful drain and the managed maintenance bridge follow as distinct steps.

Operational launch, provisioning, GC/restore and additional performance
measurements remain HOLD.

## Current raw deletion/recovery qualification

Compiled behavioral RED covers full-record handoff, owner-before-recovery,
exclusive native ownership, exact nonzero source ACK, pre-unlink durable SQL
approval, Arc normal/crash paths, cursor-parent identity, bounded completion
metadata and pending-raw exclusion of other E3 maintenance. Fixture-only compile
correction is separate from RED. Final Arc fixtures use a sealed test base and
one real source append/ACK; they do not execute130 blocks.

Owned fsync-on PostgreSQL plus real raw files verifies authorization visible on
another connection before unlink, split header/body/receipt segments, partial
completion, missing receipts, old publication generation and bounded metadata.
Arc metadata/window/source fixtures separately prove native EX/source ownership,
changed raw owner and preserved uncertain pending state. Combined actual SDK/App
acceptance remains N6. Final local functional/static/concurrency receipts and
source hashes are recorded in the associated feature evidence; CI release status
is determined by the PR's actual checks and merge metadata.

[Behavioral RED](../../../../verification/native-raw-retention/red.json),
[frozen source](../../../../verification/native-raw-retention/source.json) and
[local checks](../../../../verification/native-raw-retention/checks.json) record
nine compiled RED batches and GREEN. The complete PostgreSQL adapter short
suite passed once; affected window/Arc/native/spool short suites, build, vet and
lint passed. Four new cases passed their respective single CPU1/race/count200
invocations: late window callback, native EX exclusion, source ownership and
real DB lease/release cancellation. No default CI stress/SDK step was added.

## Graceful drain

Compiled requirement RED reproduced the missing durable ACK observer and App
delegation. With the common drain contract and a refusing stub in place, the
real-round test failed because admission was not stopped. An initial test
fixture nil callback panic was not counted as RED proof. A later wire fixture
used a result frame header for a control ACK and was corrected; that fixture
failure is not a production defect.

The implementation observes successful durable source ACKs, preserves active
native work and waits for exact seven-writer SQL/publication evidence before
joining the collector and parking Run. Cancellation joins outside recoveryMu;
verified resume repeats preparation and keeps pending state HOLD. These are
owned Go fixtures, not combined SDK/SQL N6 acceptance. The native COW bridge
and operational inputs remain separate.

[RED](../../../../verification/native-drain/red.json),
[source](../../../../verification/native-drain/source.json) and
[checks](../../../../verification/native-drain/checks.json) record this scope.
Affected App/common ingress/collector short tests passed. The initial Arc short
run found that shared input teardown closed the reorg coordinator signal;
separate native admission restored the proposal path, and final Arc short passed.
The superseded incomplete stress run is not counted as a pass. Final local
CPU1/race/single200 passed for four new Arc lifecycle cases, the affected reorg
proposal and App delegation (Arc199.692s, App2.639s). The ACK observer's own
CPU1/race/single200 passed34.677s. Wire short, final build, vet/lint,23 CI policy
tests and changed-document links passed. Default CI was not expanded.

## Managed native maintenance bridge

The preceding drain feature passed CI329 and was squash-merged as #253; clean
mainf63d0ff matches its reviewed tree and parent/base. Its release receipt is
preserved with the next feature evidence, without a self-referential commit.

The common COW adapter now borrows the actual run EX for compact/release/snapshot.
Managed Arc children inherit that descriptor and frozen context; global legacy
managed refusal remains. Completed Go/Rust cutoff proofs are mandatory at
managed startup. Actual SDK tests prove that I/bootstrap/original anchor remain
unchanged after cutoff, next import and a retained-window native epoch rewind.
An advanced floor without completion is rejected. The current retained seed
is validated independently of the historical retention head after reorg.

Local Go/protocol tests and actual Rust SDK fixtures are separate; they do not
certify a combined App/SDK/SQL cycle. The App/E3 durable phase coordinator,
held Docker wiring and N6 remain incomplete. No operation was activated.

[RED](../../../../verification/native-managed-maintenance/red.json),
[source](../../../../verification/native-managed-maintenance/source.json),
[checks](../../../../verification/native-managed-maintenance/checks.json) and
[preceding drain release](../../../../verification/native-managed-maintenance/drain-release.json)
separate the current scope and its actual qualification. The full native lib
suite passed once:88 tests/403.46s. New SDK cases passed46.24s. All native
binaries, fmt and clippy(all-targets, -D warnings) passed. Affected Go short,
build, vet/lint and23 CI policy tests passed. The final new child lifecycle
CPU1/race/parallel1/count200 passed109.274s. An earlier fixture1s startup timeout
failed; after changing only its readiness deadline to5s, the final selected run
passed. That failed run, initial compile/canonical-path/long-generator fixture
corrections and default0755 scratch refusal are explicitly separated in evidence.
Default CI gained no stress or full-SDK requirement.

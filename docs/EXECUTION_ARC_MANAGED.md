# Managed Arc replay and reorg recovery

The opt-in [App ingress](EXECUTION_ARC_INGRESS.md) owns one first-execution writer,
one read-only collector and its durable source spool. The collector uses the
existing receiver socket and seven E1 queues. Normal blocks execute once in the
native writer and once in replay; Full persistence consumes the first outcome.
N5 permanent ingress coverage/GC and N6 combined native/App/PostgreSQL acceptance
remain required before operational readiness. APFS/enode/authenticated checkpoint,
actual deployment and performance measurements remain **HOLD**.

## Read-view lifetime

Go retains the existing run lease for the entire read-only child lifetime. Before
child startup and each operation it acquires turnstile → shared native view. The
same open file description is inherited as fd3; Rust receives the explicit
`--managed-view-fd 3` contract and borrows that descriptor without reacquiring a
nested lock. Metadata validates descriptor/path identity, ownership and privacy;
it cannot prove that a caller actually holds a flock. The owner is trusted local
code. Unguarded legacy commands continue to refuse a managed identity.

AXM1 carries request ID, operation code, bounded canonical proof JSON (16KiB) and
the original scalar or EXR1 payload. The proof binds immutable native identity,
completed head, selected reference and original AIN1 receipt/digest. Both native
and Go verify epoch lineage and current sealed controls. A reader waits for SH
before judging the import intent: a live writer owns EX while its normal commit
is pending; an intent left after EX still refuses the read. Go holds the shared
view through complete decode, exact payload/receipt verification and operation
completion. Cancellation interrupts and joins the child before unlocking; Close
also joins registered operations before releasing descriptors. Each operation
explicitly unlocks even while the inherited descriptor remains open. Writers
therefore publish between operations and the same read-only factory refreshes.

The source manifest binds the immutable native instance/bootstrap/anchor/pins and
optional proof selection. It excludes changing import epochs and E1 generations.
A legacy or differently bound spool is refused, including an empty spool. Child
environments contain only LANG/LC_ALL/TZ; an empty allowlist remains an explicit
empty environment rather than inheriting the App's DSN and keys.

## One reorg authority

R (durable raw), N (completed native), P (SQL public), S (sealed source) and A
(exact source ACK) remain distinct. A raw-only fork may rotate raw control only
after all round workers/children join and a NEW same-budget canonical response
plus SQL fence proves frozen N is unchanged. It retains the old raw bytes and
recreates the entire peer/round. Missing intermediate input with a matching tip,
invalid commitments or corruption remains HOLD.

A native fork first stops new native admission and lets one active import finish.
After joining input workers/children and proving frozen N, ingress calls the
receiver's existing controller through ReorgGate. This persists a target-free
`exec_reorg_checks` fence before proposal validation/transport or common-parent
search. Reads, parts, visibility and ACKs are fenced immediately. Ingress then
proposes the actual RPC hash through the collector's existing session; it never
chooses a public recovery target or advances E1 generation. A shorter RPC head
is only proposal evidence, including below the original anchor. The existing
finder must still prove a retained common parent; deep/unsupported forks HOLD.

The existing coordinator converts its exact pending check into its existing
rewind marker/generation. After both DB children join, a bounded single ANT1
request authorizes the owned native transition under run/writer/view exclusivity.
The native SDK unwinds to the exact marker hash/root. Version3 intent precedes
mutation; an immutable transition receipt and published head precede intent
removal and ATR1 response. Ambiguous partial mutation remains pending. Only the
exact completed receipt **and** head resume the same marker idempotently.

Import epoch and read epoch advance independently of E1 generation. Request IDs
restart relative to the new native base; original anchor and old prefix receipts
never change. At most128 immutable transitions are followed; missing/corrupt
lineage or an old marker reused after new imports fails closed. Raw control
schema2 binds its own epoch, native epoch and E1 generation; schema1 can migrate
only from a completely proved original epoch1 lineage after actual SDK Ready.

Startup inspection uses the actual SQL published prefix and checks the same
control again after inspection; changed visibility/fences require a new check.
It refuses inspection while local exclusive recovery is active.

App restart recovers the pending E1 check/rewind before API exposure, runs the
native/raw bridge while SQL stays fenced, then completes existing SQL recovery
with a NEW final canonical response and post-response generation check. Only
then does Recovered allow a new owned round. Normal Stop cancels/joins recovery
and round children before raw/Store closure. No lock/SQL transaction is held
while waiting for RPC, child output, worker joins or coordinator recovery.

## Completion and remaining boundaries

Pool4/part2/API reservation1, seven logical writers and shared5 physical HTTP
attempts/s remain unchanged. Admission/watcher canonical reads consume that same
budget and never replace **NEW final fresh → generation fence → COMMIT → exact
ACK**. Native Ready/import/transition receipts and Hello grant no ACK or deletion.

N5 must bind permanent SQL native/source ownership, archive exact input bytes in
the existing blocks part and add explicit stopped-owner maintenance/GC authority.
Removing ingress configuration on a bound SQL database is not yet protected by
N4's native/spool binding alone. Managed maintenance and raw GC remain refused.
Owned SDK/process and protocol fixtures qualify individual boundaries; N6 must
combine actual native processes, mock ETH/68, App, durable PostgreSQL, APIs and
ACKs. These functional checks make no throughput or live peer claim.

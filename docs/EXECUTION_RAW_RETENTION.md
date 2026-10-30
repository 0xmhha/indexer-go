# Managed execution raw retention

Arc raw collection is an explicit library operation: `arcingress.CollectRaw`
and `arcingress.RecoverRaw`, using `RawRetentionOptions` and the existing
PostgreSQL Store. It starts no App, peer or SDK child and provisions no path.
[App drain](EXECUTION_DRAIN.md) provides the joined worker boundary; managed
native COW wiring remains a separate N5 step.
Operational invocation, formatting and performance measurements remain HOLD.
Call these functions with an unbound context: they acquire the existing E3
lease internally. Do not nest them inside another E3 transaction callback.

## Authority and ordering

1. Stop and join importer and collector. Obtain the existing E3 permit against
   full baseline coverage and the exact frozen public/source/native height.
2. Acquire native run EX before SQL, excluding both children. Refuse native
   bootstrap/import pending state; verify immutable SQL/native identity, sealed
   current head and original anchor/root.
3. Hold source ownership through the operation. Its original anchor, managed
   admission, complete retention request and nonzero committed digest must
   match. Hello/rebind with a zero digest grants no permission.
4. Open the existing owned raw window and verify StoreID, volume/reservation,
   namespace, raw/native epochs and E1 generation. Normal collection refuses
   outstanding filesystem deletion intents and requires explicit recovery.
5. For every complete, sealed, expired and unleased segment, use the existing
   E3 live EX lease to recheck public/ACK identity, seven exact idle cursors/
   parts, no ahead parts and each canonical archived input.
6. Commit a durable exact deletion ticket **before** window unlink. Validate
   the real completion receipt, then commit completion. Finish raw recovery/
   collection before other native/source maintenance changes its observation.
   Existing E3 final fresh/fence/commit/release stays separate.

Pool4/part2/API1, seven writers, two normal EVM passes and shared physical RPC5
are unchanged. Ticket transactions use a second connection of the same pool;
they create no eighth writer or pool. The outer E3 callback transaction cannot
commit before its callback finishes. A short independent fsync-on/synchronous-
commit transaction therefore persists authorization while the E3 EX lease stays
held. It acquires no second coordinator lock and holds no ticket transaction
during filesystem IO.

These maintenance records publish no block or ACK. Block publication and E3
begin/completion retain their NEW canonical response, post-response generation
fence and commit checks. Raw collection adds no RPC client or attempt budget.

## Exact permanent coverage

Migration0025 adds coordinator-owned `exec_raw_deletions`, retained through
rewind. Authorization binds permit/token/source digest, immutable owner,
StoreID, volume/reservation, observed epochs, nonzero ACK digest and the full
`DeletionRecord`/references, including offsets, sizes and first receipt times.
Canonical evidence is checksummed and bounded to8MiB/8192 references; SQL values
are parameter-bound. At most one pending and one latest completed ticket remain
per owner. Completing a segment atomically replaces older completion metadata;
immutable input archives are preserved.

Coverage joins the actual canonical publication generation and all seven exact
parts, then verifies archived header/body/receipt bytes, codec, payload hash and
exact raw descriptor. A segment may contain only one dataset of a block.
Validation retains small hashes rather than all archived payloads. Native/
read epochs, raw epochs and E1 generations remain distinct; preserved prefixes
may carry older values. Tests include native2/publication1/current generation7/
raw17 under newer frozen observations.

Unpublished or old-fork data lacking current canonical coverage is retained.
Mixed segments retain all files unless every reference qualifies. TTL, native
receipts and archive existence alone never authorize deletion.

## Interrupted deletion

Unknown commit outcomes never call unlink. Pending tickets survive callback
failure, cancellation or process loss. They block other E3 maintenance and final
release until exact raw recovery completes; they never expire by time.

`RecoverRaw` retains native EX/source ownership and reacquires the exact live E3
lease. `OpenOwnedAuthorized` passes the validated raw owner to its synchronous
check before any recovery unlink. Changed owner/epoch, permit, record, source,
ACK or archived input fails closed. If deletion never started, window recovery
rechecks age/leases before deleting that exact segment. Partial filesystem
intents resume only with their matching committed ticket.

After unlink, both actual matching `GC.completed.json` and file absence must
verify before SQL completion. Missing files without that receipt remain pending.
A historical completed ticket grants no new unlink; unknown filesystem intents
still refuse reopening. Recovery capabilities become unusable after their
synchronous SQL callback returns.

Legacy capture guards and standalone window behavior retain their contracts.
App startup refuses outstanding raw intents until explicit recovery. No
automatic repair, background collector or operational activation is added.

## Functional evidence

Tests use owned temporary windows, sealed native/base metadata fixtures, one
actual spool append/ACK and disposable fsync-on PostgreSQL. DB/file integration
covers split datasets, authorization observed from another connection,
interrupted completion, missing receipts and bounded metadata. New concurrency
cases have targeted local CPU1/race/count200 qualification; default PR CI stays
short. These fixtures do not certify live peers, combined actual SDK/App
acceptance or throughput. Combined SDK acceptance remains N6.

See [N5 checklist](codemine/runs/20261030-arc-native-n5/checklist.md),
[ingress archive](EXECUTION_INGRESS_ARCHIVE.md), [managed Arc](EXECUTION_ARC_MANAGED.md)
and [existing E3 retention](EXECUTION_RETENTION.md).

# Execution ingress ownership and input archive

The N5 SQL/backend foundation and App/Arc owner/origin integration are
implemented. App checks persistent ownership before RPC or API initialization;
Arc binds the actual native/raw/source identity after SDK Ready. Authoritative
raw GC and crash recovery are explicit library operations. Stopped-owner
maintenance wiring, held deployment composition and N6 acceptance remain pending.
Operational startup, GC/restore and performance runs remain HOLD.

## Integrate a source

Use the existing PostgreSQL Store and seven-writer execution backend:

1. Call `ConfigureExecutionIngress` with an immutable policy before API/source
   exposure. It pins module, chain, original anchor/root and normalized
   requirements digest. New adoption requires a verified full baseline at the
   anchor with no existing durable parts; it does not invent historical bytes.
2. After proving actual native/raw/source identity, call `BindExecutionIngress`.
   Binding is idempotent and immutable. Paths, quotas and current epochs are
   absent so they cannot silently change source identity or the original anchor.
3. Supply `StoreBackend.Origin`. Its `ResolveExecutionOrigin` receives owned
   copies of input RLP and must complete native/raw IO before SQL. It returns
   owned validated data and a release callback for memory accounting only.
   No native/file lease, child or RPC wait may survive into the SQL callback.
4. The existing blocks callback writes model and input archive in the same
   transaction. Missing origin, invalid layout/bytes, cancellation or SQL failure
   preserves the unpublished cursor. Other six writers retain their contracts.

The SQL policy has a separate permanent control pin. Removing/changing the row
is a conflict, and legacy `ClearChainData` refuses a pinned owner inside the
same exclusive coordinator transaction. Read-only API inspection creates no
owner or writer. App calls this guard on every storage initialization, including
when execution/ingress configuration has been removed. Origin-owning modules
require an exact immutable policy; other modules retain their existing behavior.
Module code must independently verify native
identity/provenance: SQL validates bounded metadata, checksums and exact bound
part identity, not an EVM or remote consensus result.

## Arc origin lifecycle

`arc.native` pins module, chain, original anchor/root, normalized proof selection,
blob and consensus requirements. Paths, limits and current counters do not change
that identity. Ready binds the native instance/bootstrap, raw StoreID and the
collector's exact managed admission digest, before starting collector or peers.
Restart checks an existing binding before children; missing native/raw state or
a changed owner is refused rather than adopted.

The blocks writer borrows an origin resolver from the same App-owned program.
It verifies the original sealed AIN1 receipt under a native shared view, reads
the exact three raw envelopes and matches complete accepted block/receipt bytes.
It records original import/read epochs and request ID, not a reconstructed
current epoch. It closes every native/raw lease before the final SQL status read
and the blocks transaction. No extra EVM, client, child or pool is introduced.

Origin work registers before bounded memory admission. Stop and recovery close
admission, cancel blocked work and join it before raw storage closes. Successful
handoff retains a memory-only, idempotent release callback; it may outlive Stop
without retaining a file gate or blocking native publication. A changed generation,
baseline or recovery/maintenance fence after IO rejects the handoff; existing
SQL part and final publication checks still decide commit.

## Stored data and limits

Migration0024 adds `exec_source_owner` and immutable `exec_ingress_payloads`.
Original header, body and receipt payloads use separate bytea columns (each
bounded to10MiB). Canonical metadata, bounded to64KiB, includes each exact
reference descriptor, codec, payload size/hash, input digest, native provenance,
owner binding and actual E1 part/publication identity. Values are parameter-bound.

No duplicate full BlockRLP is stored. The backend proves that rebuilding the
outer block list from original header and body items exactly equals accepted
BlockRLP, and receipts exactly equal accepted ReceiptsRLP. Tests preserve opaque
header fields and optional withdrawals and refuse a header string. This layout
check does not replace `Prepare` or the module's independent native verification.

Original native import/read epochs and request ID differ from E1 generation.
The raw epoch is explicitly an observation. For example, native2, publication7
and raw17 can be valid together. Preserved canonical prefixes may have an older
publication generation than current control; queries join that actual generation
and all seven exact parts. Rewind preserves archive bytes, including old forks.

## Visibility and deletion boundary

Archive insertion is unpublished part durability. Final feature/undo/outbox
commit, NEW fresh canonical read, post-response generation fence and exact ACK
keep their existing authority. Pool4/part2/API1, seven writers, two normal EVM
passes and the shared physical RPC5 budget do not change.

`ExecutionIngressArchive` is a private coverage observation. It rejects
unpublished/noncanonical/mismatched parts and checksum corruption; it is not a
stable API snapshot or deletion permit. Public API routes are unchanged. The
[raw retention module](EXECUTION_RAW_RETENTION.md) rechecks coverage under the
existing live E3 permit and exact source ACK, commits the full deletion record
before unlink and proves completion. Archive existence alone authorizes no
deletion. Automatic maintenance wiring remains pending; live ingress retains
raw bytes and stops at quota until that integration is complete.

See [N5 checklist](codemine/runs/20261030-arc-native-n5/checklist.md),
[managed Arc flow](EXECUTION_ARC_MANAGED.md) and [short CI](CI.md).

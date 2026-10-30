# Execution ingress ownership and input archive

This is the N5 storage/backend foundation. Its SQL and optional backend APIs
are implemented. Automatic App startup admission, actual Arc SDK Ready binding
and the Arc origin resolver remain the next integration step. N5 and N6 are
incomplete; operational startup, GC/restore and performance runs remain HOLD.

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
owner or writer. The library rejects a missing ingest policy when configured;
automatic App calls remain pending. Module code must independently verify native
identity/provenance: SQL validates bounded metadata, checksums and exact bound
part identity, not an EVM or remote consensus result.

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
stable API snapshot or deletion permit. Public API routes are unchanged. Future
GC must recheck coverage under the existing live E3 permit and exact source ACK,
bind the full deletion record durably before unlink and prove completion.
Archive existence alone authorizes no deletion. Ingress still retains raw bytes
and stops at quota until the remaining N5 integration is complete.

See [N5 checklist](codemine/runs/20261030-arc-native-n5/checklist.md),
[managed Arc flow](EXECUTION_ARC_MANAGED.md) and [short CI](CI.md).

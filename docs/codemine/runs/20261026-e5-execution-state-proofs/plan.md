# E5 execution account/storage inclusion proofs

Base main `1bb8b19` (#224). E1/E2/E3/E4 and E5 blobs/native calls remain accepted. Scope: selected standard-MPT account/code/storage proofs at the exact verified execution block-end root, optional immutable input before spool admission, seven-writer PostgreSQL durability and DB-only public queries. Full state, all changes, execution witnesses and consensus/finality are different contracts. No third EVM or upstream edits.

## Contract

Reuse the capture EIP-1186 verifier through a pure proof core, preserving the old API/bytes. Bound selection to 8 accounts/128 slots, nodes/code/response to existing limits, and check context between paths. Authenticate account nonce/consensus balance/code hash/storage root and each selected value or absence against the E1 header root. Do not map consensus balance to Arc native USDC. Custom account encodings require their own verifier.

Append optional version/profile/provider-bound StateProofBundle to EXR1. Absent legacy fields preserve old bytes and mean not collected. A configured required selection must match exactly before both scheduling and lost-ACK confirmation; missing/partial/corrupt evidence fails. Supplied evidence always verifies. Enrichment changes the whole digest, preserves generation/body/journal and must occur BEFORE spool admission; no retrofit of immutable ACKed inputs.

Provide a bounded offline native-library exporter and private fsync/no-replace composition using retained files. The exporter reads an explicit root and selection, starts no node and performs no EVM execution. Optional borrowed-RPC supply must use the existing chain-wide physical-attempt budget, exact blockHash/requireCanonical and one proof/code batch, with no height/latest fallback. No new endpoint/default/client or live activation.

Persist the complete exact bundle in the existing accounts part, no eighth writer. Before final fresh and lost-ACK verify stored checksum, identity, complete proof set and exact input digest. Rewind clears abandoned rows, prefix records retain original generation/digest. API uses existing reserved read snapshots, visible height/maintenance/rewind/epoch+hash gates, DB-only proof/value results for a collected account; missing selection is not zero. Do not grant new E3 ACK/lease/permit/checkpoint/GC authority.

## Units

- U01 RED→GREEN: optional wire and legacy identity, pure proof reuse/context, malformed/partial/root/profile/selection rejection and strict confirmation boundary.
- U02 RED→GREEN: actual native export with nonzero account/storage/code and absence, immutable file composition and bounded borrowed supply.
- U03 RED→GREEN: PostgreSQL publication/fresh rollback/exact ACK/corruption/rewind/prefix/read-only, account API and actual native→seven queues→reserved API→restart integration.
- U04: current guides/n/n-1/n-2/checklist and all remaining work; full local build/fmt/vet/lint/function/race/PG/cache/examples/offline operations, new single CPU1/race/count200, staged/history secret scans, draft PR+attach, exact-head CI lint/test/postgres/examples/arc, ready/squash/main sync and own-cache cleanup.

Keep per-chain/process pool4/part2/API reservation, RPC5 physical HTTP attempts/s including retry/failover, NEW uncached fresh immediately before generation fence/COMMIT, parameter binding and E3 authority. Actual nodes/peers/operator DB, disk formatting/provisioning, operational GC and additional performance measurements remain held. Other projects/shared caches/Docker resources stay untouched. Unchanged Arc Rust source is not locally rebuilt; required Arc CI remains mandatory. Source branch `refactor/e5-execution-state-proofs` is preserved.

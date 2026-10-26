# Priority 4 native retention and matched recovery

Base: E6 #229 squash f7c0fa26f6e2b62fa2dc50820369ea9a3b9a5818.
Owned continuation run: /private/tmp/indexer-four.receipt.Rh9qzP.
Preserved implementation/evidence: /private/tmp/indexer-four.retention.wjtBlm.
Branch: `refactor/native-retention-matched-backup`.

## Source audit

Arc 6e764023ee6515fe70573e123ed2db912a7207b4 and Reth v2.2.0
88505c7 pin the v2 RocksDB transaction lookup and MDBX layout. Reth
crates/prune/prune/src/segments/user/transaction_lookup.rs deletes
TransactionHashNumbers through transaction bodies. bodies.rs explicitly orders
lookup pruning before jar deletion. db-api/src/tables/mod.rs defines hash to
transaction-number values. provider/src/providers/database/provider.rs uses
TransactionBlocks ceiling seeks, so retain that table and BlockBodyIndices.
Keep headers/canonical mappings, bytecode/preimages, hashed/plain state and tries.

libmdbx-rs/src/environment.rs exposes with_raw_env_ptr; its public ffi exports
mdbx_env_copy2fd and MDBX_CP_COMPACT (mdbx.h). Compact-copy only a sealed COW
candidate with all Factory handles closed, fsync output, close source environment,
atomically replace candidate MDBX data, then reopen and run all native postchecks.
An interrupted candidate is discarded under the existing exact permit journal;
the operational original is never overwritten in place.

## Exact receipt correction

Read/decode the coordinator receipt once under the existing live E3 transaction,
validate that value against PostgreSQL, and pass its immutable digest to child
supervision. Reject missing authority on backup/restore. Bind every later child
receipt read to the inherited digest. Runtime subprocess tests replace A with a
structurally valid foreign B after validation: only the A child may succeed.
Repeat the mutation after the child supervisor match to cover later backup reads.

## Implementation and verification order

1. Runtime RED tests using owned native MDBX/RocksDB/static jars, then source-backed
   lookup deletion before jar deletion; exact retained rows and checkpoints.
2. Runtime RED compact-copy/reopen test, native library physical compact-copy,
   strict sealed-candidate lifecycle integration and interruption recovery.
3. Runtime RED matched component/floor admission; require held native/source and
   exact externally supplied PostgreSQL snapshot receipts, component checksums,
   private bounded artifacts and atomic no-overwrite publication. Retain pending
   intents; never imply autonomous PostgreSQL restore or start a node.
4. Root named feature test plus real native/coordinator/fsync-on PG coverage.
   Full build/fmt/vet/lint, root+wire+StableNet race, native fmt/build/clippy/tests
   and fixture parity, examples/cache/offline, one full CPU1/race/count200 run.
5. Current docs and n/n-1/n-2. Explicit staging and secret scans; draft PR,
   exact current head CI5, ready/squash, clean primary main synchronization.
6. Postrelease ledger, Korean completion report and strict all-four observer;
   preserve handoff evidence and clean only execution-owned caches/fixtures.

Operational native startup, providers, operator DBs, provisioning, GC/restore
activation and performance measurements remain held.

# E5 execution blobs validation

Scope: owned private files, synthetic signed transactions, pinned native libraries, mock RPC/IPC and disposable PostgreSQL with fsync enabled. No actual node/peer/operator DB, disk provisioning/formatting, operational GC or performance measurement. Logs live under `/private/tmp/indexer-e5-blobs-20261026/validation`; they are local evidence, not repository dependencies. Arc Rust source is unchanged and its existing native fixtures remain required by CI.

## Requirement RED → GREEN

| Contract | Runtime RED | GREEN evidence |
| --- | --- | --- |
| Optional bounded envelope | Canonical BlobBundle rejected as unknown JSON (`wire-red.log`) | Optional round-trip, syntax/limits and byte-identical nil/legacy input; cryptographic interpretation remains in the consumer |
| Authenticate supplied input | Old Prepare ignored malformed/wrong-profile bundle (`consumer-red.log`) | Exact chain/block/profile/references, KZG, completeness and malformed proof rejection before writers |
| Explicit pre-spool evidence | Original bare bytes returned without bundle (`attach-red.log`) | New exact input/digest, no original mutation or bundle replacement, no-reference evidence, cancellation and disabled-policy rejection |
| Cancun native prerequisite | Actual type-3 recorder rejected beacon-root prelude (`native-blob-fixture-final.log`) | Pinned library EIP-4788 in PreBlock, two real storage writes, no system user trace, independent journal-root reconstruction and immutable original DB |
| PostgreSQL path | Writer/reader interfaces absent (`pg-red/blob-pg.log`) | Actual native result through seven parts, durable private data, fresh rollback, pages, exact ACK, corrupted proof/metadata and missing row rejection, prefix identity and rewind |
| Required-data retry boundary | Missing sidecars received Committed on old-prefix confirmation (`strict-receiver-red.log`) | Receiver checks required policy before scheduling and confirmation, with no ACK/callback for missing data |
| API | Blob route returned404 (`api-red.log`) | Existing snapshot, bounded/pinned/default pages, multichain mounting, status/privacy and no-store |
| Private immutable file | No complete file published (`files-red.log`) | Owned regular inputs, mode/identity/size checks, source preservation, fsync/no-replace result, symlink/public/oversize/canceled rejection |
| CLI | Valid arguments never delegated to the composer (`command-red.log`) | Actual offline delegation, identity/deadline/error/help checks |

An initial fixture command used the wrong working-directory-relative edit path and selected no matching new subtest; it is not counted as RED or GREEN. After the fixture existed, the real unsupported-prelude failure above was reproduced. The final `native-pos-fixture.log` uses explicit PoS/Cancun genesis fields, empty withdrawals and the pinned beacon finalizer. Old six native fixture bytes remain unchanged. These library blocks are not independently consensus-verified or live checkpoints.

## Targeted functional evidence

- Actual pinned native pre-block/type-3 replay produces `evm-blobs.json`. Independently known zero-polynomial full-blob commitment/proofs bind three repeated transaction references. Proof generation from the verifier is not used as an oracle.
- Invalid proof, omitted position, wrong block/ordinal/profile and missing required data fail. Unsupported positive supply stays disabled; verified zero-reference evidence does not declare a live source. Opaque legacy inputs are not retroactively classified as empty.
- Actual file composition → seven writer queues → reserved public API → exact lost-ACK → removal of an owned test input → read-only database reopen passes (`app-final-targeted/blob-app.log`). All pages issue zero RPCs. This demonstrates raw-file independence, not operating GC activation.
- PostgreSQL metadata checksums and KZG checks reject damaged metadata/proofs before fresh. Missing rows cannot authorize ACK. Rewind clears abandoned data; surviving-prefix queries use current epoch while retaining original generation/digest. Canceled reads return without retaining a connection.
- Existing full-blob capture worker, private file/CLI/API/receiver and native prelude checks pass. Root build/vet/lint, separate module static, examples, cache lifecycle and offline operations/preflight/shell pass.

## Release gates

All required local gates passed on the final implementation:

- Root functional: GREEN (`normal-results.json`, 619.8 seconds).
- Complete root/wire/StableNet `make test-race`: GREEN (`race-results.json`, 892.2 seconds).
- Full fsync-on PostgreSQL adapter/app/API/bundle matrix: GREEN (`postgres-results.json`, 1129.1 seconds).
- New single `GOMAXPROCS=1 -race -count=200` groups: actual native/private-file/seven-writer/reserved-API/ACK/read-only restart 714.2 seconds; receiver/API/files/CLI 83.7 seconds; native prelude/cancellation 69.6 seconds. Each group passed one complete 200-count invocation (`blobs200-results.json`); partial counts are not combined.
- Root and separate wire/StableNet/AST module build/vet/lint, formatting, examples, cache lifecycle and offline operations/preflight/shell: GREEN (`static-results.json`, `modules-results.json`). Final root lint reported zero issues. Existing six native fixture bytes are unchanged.

These durations identify completed functional gates, not performance measurements. Existing CI concurrency selectors stay enabled; the new PostgreSQL200 is a separate step with its own deadline.

Staged/history secret scans, exact-head lint/test/postgres/examples/arc, ready/squash and clean main sync remain mandatory. Source branch `refactor/e5-execution-blobs` is preserved. The source-branch PR linked from the checklist is the mutable release authority; MERGED with required checks completes this scope. Shared caches and other projects stay untouched; only this task's owned caches are cleaned after release.

These results do not establish real provider transport/retention, global availability, beacon SSZ inclusion/PeerDAS, account/storage inclusion proofs, consensus/finality or sustained throughput. They do not change RPC5 physical attempts/s, pool4/part2/API reservation, final NEW uncached fresh/generation fence or E3 ACK/lease/permit/GC authority.

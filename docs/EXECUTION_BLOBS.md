# Full blobs in execution ingestion

Use this opt-in path to retain complete EIP-4844 blobs alongside E1 execution inputs. It reuses the existing [full-blob proof core](../pkg/chainfeed/evmblob/proof/blob.go), keeps seven writers and adds no EVM execution or RPC. The separate [capture supplement worker](BLOB_INPUTS.md) keeps its own input/cursor contract; these paths do not silently backfill or merge each other's completion.

## Scope and trust

An execution body contains signed transaction references, not full blob data. A full-blob relay/beacon/peer source must first retain all blob bytes, commitments and full-blob proofs. [EIP-4844](https://eips.ethereum.org/EIPS/eip-4844) defines this binding; [EIP-7594](https://eips.ethereum.org/EIPS/eip-7594) cells/proofs and beacon SSZ sidecars are different transport formats. The input here is the documented `eth.full-blobs.rlp.v1` relay envelope. It does not decode SSZ, run PeerDAS, reconstruct missing cells or call `engine_getBlobs`.

Go verifies every position against the profile-verified block and signed transaction hash lists: exact chain/genesis/height/block hash, reference count/order, versioned commitment hash, full KZG proof and blob gas fields. Repeated hashes at different positions are valid. Invalid, missing, extra or reordered inputs fail; no truncated input is marked complete. This is local retained data, not independent beacon inclusion, network-wide availability, consensus/finality or a fork-specific gas-limit audit.

The pinned [Arc pool policy](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/execution-txpool/src/pool.rs) disables blob admission. Positive-reference supply under that selected profile is rejected. This policy is not a historical absence proof. A verified no-reference block may carry explicit empty evidence without opening a provider; this does not declare a supported Arc blob transport. No live source is enabled by this change.

The isolated StableNet-library recorder now handles a declared Cancun/PoS header's [EIP-4788](https://eips.ethereum.org/EIPS/eip-4788) beacon-root operation through the pinned library, before user transactions. It records a distinct `PreBlock` journal, reconstructs its state changes and excludes it from user call trees. Cancellation joins before a result escapes. DAO and invalid pre-fork/pre-Merge beacon-root inputs remain rejected. This adds the prerequisite for the offline type-3 fixture; it is not a claim of complete modern Ethereum fork or consensus support. Upstream code is unchanged and the existing two executions remain.

## Compose before spool admission

`execution.AttachBlobInput` accepts a complete original EXR1 and the full relay RLP. It creates a new payload preserving chain, height, block hash, generation, body, receipts and journal. It refuses to replace an existing bundle. It never obtains an ACK, rewinds, deletes or starts a service.

`execution.AttachBlobFiles` additionally checks private ownership/mode, regular files, inode/size/mtime and byte limits. Parent directories are `0700` and files `0600`. It writes a temporary inode in the output directory, fsyncs it, publishes with a no-replace hard link and fsyncs the directory. Existing output and both inputs are preserved. Failure after publication can leave a complete output; inspect it rather than overwriting it. The helper owns no rolling-window capacity/GC policy.

The generic EVM command uses profile `evm`; chain-specific producers call the library with their own binary profile and audited policy:

```sh
go run ./cmd/execution-blobs \
  --chain-id YOUR_CHAIN_ID --genesis YOUR_EXACT_GENESIS_HASH \
  --result /YOUR_PRIVATE_DIR/original.exr \
  --sidecars /YOUR_PRIVATE_DIR/full-blobs.rlp \
  --output /YOUR_PRIVATE_DIR/complete.exr --provider retained-sidecars
```

Omit `--sidecars` only for a block proven to have no references. Output JSON reports `ref`, new whole-payload `digest` and `bytes`. Default timeout is 15 seconds, with a maximum of one hour. No URL, credentials, network/default endpoint or node flags are accepted. The full-blob source must supply the existing relay format; renaming an SSZ/cell file does not convert it.

Pass the **new complete file** to the existing submission/exporter integration before admitting that input to any spool or publishing its digest. Enrichment changes the exact payload digest. It cannot retrofit an already stored/ACKed bare result; immutable retries would conflict. Existing spool/lease/ACK/GC authority remains mandatory. Retain early sidecar files within an explicitly budgeted directory until the applicable completion gates authorize deletion. A local 24-hour window does not guarantee 24-hour remote blob retention.

## Require full data when needed

Within an already configured execution source:

```yaml
source:
  execution:
    # existing socket and original anchor values are still required
    require_blob_sidecars: true
```

The default is `false` for legacy compatibility. With `true`, a positive-reference block without its complete bundle is rejected **before** scheduling or lost-ACK confirmation. Opaque transactions cannot prove absent references under strict mode. A no-reference block needs no sidecars; without explicit empty evidence its blob API still reports not collected. Invalid supplied bundles are rejected in either mode. Missing data is never skipped or converted to an empty result.

This flag installs no provider. Node-facing acquisition must retain transient bytes early; later workers can compose/verify from those retained bytes. Actual provider transport, remote retention/fork evidence and operating activation remain separate work.

## Storage, visibility and recovery

EXR1 adds optional version 1 `BlobBundle` with fixed format, exact profile, provenance label and bounded relay response. Legacy payload bytes are unchanged. Upgrade the reader/schema before enriched producers: old strict readers reject the extension. Migration 0019 adds `exec_blob_blocks`, ordinal `exec_blob_entries` and the `blob_sidecars` publication marker. Old publications remain not collected.

The existing transactions part stores metadata and rows with the exact payload digest. Values are parameter-bound. There is no eighth writer, new pool or extra reader connection: per-chain/process pool of 4 connections, part concurrency of 2 and existing API reservation remain. Durable parts are private until final publication. Full metadata/count/ordinal/KZG/relay-digest checks run **before** the NEW uncached fresh canonical read; only the existing generation fence and COMMIT follow it. Lost-ACK recovery uses the same complete-data gate and original source generation/digest. The source limit of 5 physical HTTP attempts/s including retries is unchanged.

Rewind removes abandoned heights, including unpublished rows, and invalidates old epoch pins. Permanent blob rows are independent of raw/native window files. Present empty evidence differs from absent legacy collection. These rows grant no new raw/node GC permissions; E3 checkpoint/permit/lease/exact ACK gates remain unchanged.

## Query stored data

```text
GET /execution/blobs/blocks/{height}?limit=1
GET /execution/blobs/transactions/{hash}?limit=4
```

Prefix `/chains/{id}` in multichain mode. Reads use the existing public reserved snapshot, visible height and rewind/maintenance gates, with zero RPC/raw-file/provider fallback. A read-only process needs the already migrated schema.

Responses contain `epoch`, original wire `ref` (`Generation`, `Height`, `Hash`), exact payload `digest`, `format`, `profile`, `provider`, `availability`, `verification`, filtered `total`, `entries` and nullable `next`. Each entry has global `index`, transaction index/hash, transaction-local blob index, versioned hash and hexadecimal `blob`, `commitment`, `proof`. `retained` indicates stored full input; verification covers the requested page against stored execution references. Empty block evidence uses `no_blob_references`/`execution_transaction_references`, without claiming KZG/network availability.

The default page has 1 blob, with a maximum of 4 blobs. `next` is the first omitted global ordinal. Continue with the same transaction filter and both pins:

```text
GET /execution/blobs/blocks/{height}?after=<next>&limit=1&epoch=<epoch>&hash=<ref.Hash>
```

The epoch is current public generation; the source ref can retain an earlier generation after a surviving-prefix rewind. Stale pins return 409 even if the tip fell below that height. Malformed/unknown/duplicate fields, `internal`, incomplete/zero pins or an unpinned continuation return 400. Not collected/unpublished/unknown data returns 404; disabled/uninitialized/rewinding/maintenance returns 503. Detected corruption/I/O returns 500 without private errors. Responses are compact and `no-store`.

Pages check bounded metadata, complete row presence and requested proofs, without loading every blob to return one. Publication/ACK verifies the whole set and exact relay digest. This assumes the E1 verified-ingest/storage trust boundary; it is not an independent attestation of arbitrary database history or consensus.

## Bounds and validation limits

The bounds are 32 full blobs per block, 131072 bytes per blob, and 48 bytes per commitment and full proof. Reference extraction permits at most 32768 transactions. These are operational bounds, not current chain consensus limits. Relay bytes use existing `MaxResponseBytes`, about 4.01 MiB; EXR1 keeps its 64 MiB whole-payload and 16 MiB metadata and token limits. Whichever limit is reached first fails closed. Context cancellation is checked before/between bounded proofs; an in-flight library proof is not asynchronously interrupted.

Evidence uses independently known zero-polynomial commitment/proofs, signed type-3 transactions and actual pinned EVM/pre-block replay, disposable fsync-on PostgreSQL, private files, seven queues, reserved APIs and read-only reopen. It does not establish live peers/providers, actual disk/retention operation or throughput. See [plan/checklist/evidence](codemine/runs/20261026-e5-execution-blobs/plan.md) and [remaining work](analysis/open-items.md).

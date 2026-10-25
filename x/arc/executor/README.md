# Arc execution adapter

Replay an **already imported** block with the official Arc EVM and export a verified execution journal. This is a separate Rust module; the Go indexer imports no Reth/revm code. Upstream Arc source is unchanged. See the [integration guide](../../../docs/EXECUTION_ARC.md) for supported data, configuration and recovery boundaries.

Arc source is pinned to [`6e764023ee6515fe70573e123ed2db912a7207b4`](https://github.com/circlefin/arc-node/tree/6e764023ee6515fe70573e123ed2db912a7207b4), Reth to `v2.2.0` (`88505c7f`). `Cargo.lock` retains the compatible upstream dependency versions and the upstream `ark-relations` patch. Use Rust **1.98.1** from `rust-toolchain.toml`; Linux needs `libclang-dev` and `pkg-config`, plus a C/C++ toolchain.

```sh
make test-executor-arc
cd x/arc/executor
cargo build --locked
```

The tests use offline Arc EVM libraries and synthetic genesis allocations. They start no node, contact no peers and perform no performance measurements. The optional `INDEXER_ARC_FIXTURE_DIR` test variable regenerates the five [Go integration fixtures](../../../testdata/execution/README.md) in an explicitly selected output directory. Normal tests do not rewrite repository fixtures.

## Library contract

`replay(config, state, parent, block, receipts, generation)` returns `Verified` only after checking parent identity/state, transaction/ommer/withdrawal/receipt commitments, bloom, gas and final state. `config` is `ArcEvmConfig`; substituting an Ethereum EVM is unsupported. `state` must be the complete native historical parent provider. Native read failures propagate; unavailable bytecode or block hashes do not become zero/default values.

The native state and an independently reconstructed journal are compared after the pre-block system phase, every transaction and Finalize. The state hook tracks committed storage destruction; it is not an opcode tracer. Reverted writes disappear; successful creations retain a witness even if their constructor destroys them. Bytecode is hash-checked. Result/metadata/change/code/token bounds match the Go wire reader. `Verified::encode()` emits canonical EXR1 metadata and original block/receipt RLP. A separate bounded inspector records [native call trees](../../../docs/EXECUTION_CALL_TRACES.md) in the same replay, retaining reverted attempts. CallFrom logical identities keep a physical transaction root and final completion failures; no third EVM is run.

`write_result(path, verified)` requires an existing owned `0700` directory, writes a private temporary file, syncs it, publishes without replacing an existing path and syncs the directory. It creates no output directories and removes no prior results. Supported runtime filesystem/IPC platforms are macOS and Linux.

## Read-only native command

The built `indexer-arc-executor` command takes explicit `--chain mainnet|testnet`, `--datadir`, `--height`, `--generation` and `--output` arguments. It opens existing MDBX/static-files/RocksDB read-only, checks the selected genesis and canonical block, and reads header/body/receipts plus historical parent from one native read view. It does not initialize a missing DB, launch a node, import peers, prune native state, signal ExEx FinishedHeight, or change indexer visibility. A current native canonical check is an early rejection only; the indexer still performs final fresh.

Real native DB command execution is on hold. The command is compiled and its argument/missing-layout rejection is tested; compatibility with an operator's actual retained DB and concurrent native maintenance remains unverified. The Go continuous collector uses the bounded `--serve` protocol below; The separate [checkpoint/retention adapter](../../../docs/EXECUTION_ARC_RETENTION.md) supplies supervised offline native-v2 payload COW maintenance; it is not activated by replay or collection. Retain result files and use the indexer's exact ACK for completion; the [Go submission command](../cmd/execution-submit/main.go) never deletes the file or requests rewind.


## Read-only service protocol

`--chain mainnet|testnet --datadir /ABSOLUTE/RETAINED --serve` is mutually exclusive with `--height`, `--generation` and `--output`. Only the owning [Go collector](../collector/README.md) should invoke it. It opens no listener, launches no node and writes no result file. Optional `--scratch-dir` names an existing owned private parent. The adapter creates a private workspace there (or in the OS temporary directory for standalone use), opens RocksDB through a read-only primary symlink and places the upstream secondary LOG files in that workspace. It never removes the symlink target. Normal return drops the workspace; the Go owner also removes its own outer workspace after a forced child exit. Per-request provider synchronization replaces the upstream directory watcher, so the watcher cannot retain the factory after EOF. Native providers use the upstream read-only synchronization path; head is the finished import stage (`best_block_number`), not the highest static header. Each request opens a fresh provider; replay holds one exact parent/block/receipt view and rechecks native canonicality before replying.

Request: four-byte big-endian JSON length (1..1024), then `{"version":1,"id":1,"op":"identity","height":0,"generation":0}`. IDs start at1 and strictly increase. `identity`/`head` require zero height/generation; `canonical` requires height and zero generation; `replay` requires positive height/generation. Unknown fields, invalid ranges, truncated requests and handler errors terminate the adapter without a successful response.

Response: `AXS1` (4 bytes), request ID (u64 big-endian), operation (u8: identity1/head2/canonical3/replay4), payload length (u32 big-endian), payload. Control JSON is at most1024 bytes: identity has `chain_id`, `genesis`, pinned `revision`; head/canonical have `height`, `hash`. Replay returns the canonical verified EXR1 bytes, at most64MiB. Stdout contains protocol bytes only; bounded diagnostics go to stderr. The Go owner serializes requests, checks all bounds/identity/commitments and joins the child on cancellation, invalid replies or timeout. Native replay errors stop collection and preserve the queue; restarting with the same original anchor performs reconciliation again. No fallback to incomplete state or RPC replay is inferred.

## Native checkpoint and retention

See the [Arc native guide](../../../docs/EXECUTION_ARC_RETENTION.md) for `arc-native-maintenance`, candidate key/root limits, private output, managed owner guards and E3 live-supervised copy/swap/release. State history and boundary/pre-anchor jars stay retained; no fixed total-disk promise or live-node validation is implied.

Native checkpoint catalogs use bounded private disk sort/merge and incremental trie reconstruction, including a per-account storage spool. Export and retention cutoff verification share this path. See [workspace, input and capacity limits](../../../docs/EXECUTION_ARC_RETENTION.md#full-checkpoint-export); account/storage history pruning retains a one-block root guard. [Native history floor and compatibility](../../../docs/EXECUTION_ARC_RETENTION.md#history-floor-and-compatibility) apply to replay parents and seed starts. Further native allocation/index reclamation remains separate.

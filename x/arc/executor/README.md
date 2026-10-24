# Arc execution adapter

Replay an **already imported** block with the official Arc EVM and export a verified execution journal. This is a separate Rust module; the Go indexer imports no Reth/revm code. Upstream Arc source is unchanged. See the [integration guide](../../../docs/EXECUTION_ARC.md) for supported data, configuration and recovery boundaries.

Arc source is pinned to [`6e764023ee6515fe70573e123ed2db912a7207b4`](https://github.com/circlefin/arc-node/tree/6e764023ee6515fe70573e123ed2db912a7207b4), Reth to `v2.2.0` (`88505c7f`). `Cargo.lock` retains the compatible upstream dependency versions and the upstream `ark-relations` patch. Use Rust **1.98.1** from `rust-toolchain.toml`; Linux needs `libclang-dev` and `pkg-config`, plus a C/C++ toolchain.

```sh
make test-executor-arc
cd x/arc/executor
cargo build --locked
```

The tests use offline Arc EVM libraries and synthetic genesis allocations. They start no node, contact no peers and perform no performance measurements. The optional `INDEXER_ARC_FIXTURE_DIR` test variable regenerates the two [Go integration fixtures](../../../testdata/execution/README.md) in an explicitly selected output directory. Normal tests do not rewrite repository fixtures.

## Library contract

`replay(config, state, parent, block, receipts, generation)` returns `Verified` only after checking parent identity/state, transaction/ommer/withdrawal/receipt commitments, bloom, gas and final state. `config` is `ArcEvmConfig`; substituting an Ethereum EVM is unsupported. `state` must be the complete native historical parent provider. Native read failures propagate; unavailable bytecode or block hashes do not become zero/default values.

The native state and an independently reconstructed journal are compared after the pre-block system phase, every transaction and Finalize. The state hook tracks committed storage destruction; it is not an opcode tracer. Reverted writes disappear; successful creations retain a witness even if their constructor destroys them. Bytecode is hash-checked. Result/metadata/change/code/token bounds match the Go wire reader. `Verified::encode()` emits canonical EXR1 metadata and original block/receipt RLP.

`write_result(path, verified)` requires an existing owned `0700` directory, writes a private temporary file, syncs it, publishes without replacing an existing path and syncs the directory. It creates no output directories and removes no prior results. Supported runtime filesystem/IPC platforms are macOS and Linux.

## Read-only native command

The built `indexer-arc-executor` command takes explicit `--chain mainnet|testnet`, `--datadir`, `--height`, `--generation` and `--output` arguments. It opens existing MDBX/static-files/RocksDB read-only, checks the selected genesis and canonical block, and reads header/body/receipts plus historical parent from one native read view. It does not initialize a missing DB, launch a node, import peers, prune native state, signal ExEx FinishedHeight, or change indexer visibility. A current native canonical check is an early rejection only; the indexer still performs final fresh.

Real native DB command execution is on hold. The command is compiled and its argument/missing-layout rejection is tested; compatibility with an operator's actual retained DB and concurrent native maintenance remains unverified. No long-running Arc collector, durable cursor/reorg supervision or Arc native GC is supplied by this command. Retain result files and use the indexer's exact ACK for completion; the [Go submission command](../cmd/execution-submit/main.go) never deletes the file or requests rewind.

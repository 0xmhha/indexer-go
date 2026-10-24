# StableNet execution recorder

This standalone module replays an already imported block to export transaction and Finalize state changes for E1. The normal node import is the first execution; `Replay` is the second. It imports the official StableNet client without modifying its source and never commits second-pass state to the node database.

The [offline checkpoint tools](../../../docs/EXECUTION_BOOTSTRAP.md) export full genesis/prepared-node state or selected proofs and inspect exact node/spool boundaries without starting services. A full state export requires address/slot preimages; new runtime settings preserve them. PostgreSQL bootstrap belongs to the indexer tool, never this module.

The module provides replay, a [durable result spool](spool/README.md), canonical walking and bounded IPC export (`exporter`), and official `node`/`eth` assembly (`nodebackend`, `cmd/stablenet-executor`). See the [execution guide](../../../docs/EXECUTION_INGEST.md) for prepared-checkpoint requirements, settings and lifecycle. The [checklist](../../../docs/codemine/runs/20261023-e1-execution/checklist.md) records final verification and CI.

## Dependencies and integration

Go is pinned to the repository's 1.26.8 toolchain. `github.com/ethereum/go-ethereum` resolves to official `github.com/stable-net/go-stablenet` version `v1.0.1-0.20261013052903-4d17eea018c8`, commit `4d17eea018c8dd579409db30bfd27de79c4f4121`. This separate module isolates that fork from the root indexer's geth dependency. Both processes share the standard-library-only [execwire](../../../execwire/README.md) contract.

For a block that the node has already validated and imported:

```go
result, err := executor.Replay(ctx, executor.ReplayInput{
    Chain: chain,
    Database: chain.StateCache(),
    Parent: parentHeader,
    Block: importedBlock,
    Receipts: storedReceipts,
    Generation: generation,
})
```

Inputs belong to the caller and must remain immutable during replay. The returned result binds the original block hash, parent, generation and roots. Caller code still owns canonical checks, durable storage, publication and ACK. This library does not access PostgreSQL or issue RPC calls. The indexer's four-connection pool, shared 5 HTTP/s budget and final fresh check remain separate unchanged contracts.

## What is verified

- Parent number/hash, transaction and uncle roots, stored receipt count/root and replay gas/receipt root.
- Original state root preserved before WBFT `Finalize` can rewrite its private header copy.
- Ordered transaction changes and a final block-level step, with account existence, nonce, balance, code/storage roots and Extra; storage before/after and clear; code bytes.
- Every exported step applied independently to copied account/storage tries reproduces the actual step root. Missing slot records cannot hide behind a correct account storage-root field. Code transitions require matching code bytes.
- Reverted changes are excluded; cross-transaction intermediate state is retained even if a contract is created and destroyed in the same block.
- No second-pass durable writes. Trie commits fail. A private disk view stages/discards code batches because upstream StateDB writes code before committing the trie; the overall commit is rejected.
- Speculative StateDB copies do not enter the primary journal. Cancellation interrupts the bound EVM and joins its callback; its one-shot transaction observer is detached before opcode interpretation. Journal collection uses trie wrappers.

The parent must be available at the declared root. There is no latest-state fallback. DAO/beacon pre-transaction protocol-change configurations are explicitly rejected because E1's current journal has no separate cause for that prelude. StableNet configuration validation must preserve this boundary.

Replay verifies execution results; it does not replace the node's consensus/header admission. Full trie iteration is a test oracle, not the product path. The product verifier applies changed records to copied tries. See the [recorder contract](../../../docs/codemine/runs/20261023-e1-execution/04-recorder-contract.md).

## Checks and limits of evidence

```sh
# From repository root
make test-executor-stablenet

# From this module
go test -race -count=1 ./...
GOMAXPROCS=1 go test -race -count=200 -timeout=5m \
  -run '^(TestCancelLogger|TestReplayCancellationDuringEVM)' ./...
```

Fixtures use actual EVM, WBFT, fee delegation, native mint/Extra methods, hash/path databases, reverts, creation and SELFDESTRUCT. NativeManager Extra is exercised by a direct EVM-library call with its required council caller; that test is not a governance transaction. The WBFT fixture enables Applepie for fee delegation and performs no consensus seal admission. Independent bidirectional account/storage traversal checks the recorder separately from its product verifier. Record-only omission faults preserve the actual trie mutation and must fail.

Tests start no node services or peers, format no disks, activate no GC and measure no performance. They do not establish live synchronization, deployment, recovery or speed claims.

Goroutine checks exempt only the exact upstream `core.(*txSenderCacher).cache` function: the pinned client initializes that process-lifetime pool before tests and exposes no shutdown API. Replay callbacks and executor-owned workers remain subject to leak checks.

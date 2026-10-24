# Arc execution journals

The Arc adapter adds an offline-verified second execution and a one-result delivery path to the existing execution indexer. It uses the official Arc EVM's protocol, precompiles, fork rules and system accounting. A generic Ethereum replay cannot substitute for it. The [native module](../x/arc/executor/README.md) pins all versions and exposes both a library and a read-only native DB command; upstream node source is unchanged.

Normal node import is the first execution. The adapter replays an imported block against its exact retained parent, independently checks the journal after every phase against native intermediate roots, and checks final state/receipt/gas commitments. It exports a private durable EXR1 file. The Go submission command sends that file to an **already running, explicitly prepared** Arc indexer and returns only an exact committed ACK. No node, disk, GC or performance operation is performed by this development validation.

## Data coverage

| Data | Supplied and verified boundary |
| --- | --- |
| Header, body, transactions, receipts, logs | Original RLP; body/receipt commitments and bloom, then independent Go profile decoding |
| Account and storage changes | Before/after values in pre-block, transaction and Finalize order; native and independently reconstructed intermediate roots; uint256 balances, tombstones and storage-clear markers |
| Contract/code | Hash-checked bytecode and successful outer creation evidence, including empty code and constructor-time destruction |
| Pre-block protocol changes | Optional first-only wire cause `PreBlock` = 4, index -1 and zero transaction hash; not attributed to a user transaction |
| Finalize | Last step, including Arc system accounting in empty blocks; index -1 and zero transaction hash |
| Call traces/internal transactions | Not supplied. Net committed state and receipts do not prove a complete call trace |
| Blobs | Transaction references may be present in raw data; sidecars/KZG proofs and complete availability are not supplied. Existing [Arc blob policy audit](../x/arc/blobs.go) remains separate |
| Account/storage inclusion proofs | Not exported by this adapter. Native/journal root equality is not an independent consensus/finality proof |

The native DB must retain the complete required parent state, code, headers, body and receipts. Missing data fails closed. This release does not create a checkpoint from current RPC state, download a snapshot, fill partial coverage with zero or restore pruned history.

## Indexer and transfer setup

Use `make build CHAIN=arc`. The existing [execution ingest settings](EXECUTION_INGEST.md#indexer-configuration) also apply to Arc, with its own schema and original prepared anchor. The Arc checkpoint command below binds the shared [offline bootstrap verifier/importer](EXECUTION_BOOTSTRAP.md) to the Arc header codec, with the same verify/import/inspect actions and private DSN/RPC environment injection. The expected chain/genesis/header/root and covered state must agree. The StableNet-specific node/export commands in that guide do not apply to Reth. Native Arc checkpoint export tooling remains a follow-up; do not mark selected state as a full baseline.

Keep one chain/process, PostgreSQL `max_conns: 4`, shared `rpc.rate_limit: 5`, state prefetch, head publication and a private `0700` directory. Seven queues share at most two part connections; the API reservation stays inside the same pool. Final publication still runs uncached fresh through the shared physical HTTP budget and checks generation after the response. Part commits do not publish height, features, outbox or ACK.

Build the one-file sender separately:

```sh
go build -o /YOUR_BUILD_DIRECTORY/arc-execution-submit ./x/arc/cmd/execution-submit
go build -o /YOUR_BUILD_DIRECTORY/arc-execution-checkpoint ./x/arc/cmd/execution-checkpoint
```

The sender's required flags are `--chain-id 5042002` (testnet) or `5042` (mainnet), `--genesis` (exact lowercase hash), `--result` (absolute owned `0600` file) and `--socket` (existing same-UID `0600` Unix socket). Parent directories must be owned and `0700`. Optional `--timeout` bounds the call; the transfer itself is capped at one minute. A result is at most 64 MiB; metadata at most 16 MiB. Both Go tools expose `--help` without connecting or starting services.

The current source file and socket inode/owner/permissions are checked; the socket peer is authenticated. Input is parsed once before connecting. Hello cannot authorize source deletion, invent a generation or promote completion. An exact ref/hash/chain/generation/digest committed ACK is required; malformed, wrong, missing or cancelled ACKs fail. The original result file remains present on both success and failure.

## Recovery and compatibility

On a lost ACK, retain and resend the exact file. The receiver confirms durable publication and runs fresh again without duplicating feature/outbox writes. Fresh failure leaves parts unpublished and public APIs at the old anchor. A stale generation is rejected; this sender does not rewrite files, issue rewind or decide a reorg. Resolve reorg through the existing coordinator and replay from the correct retained parent/generation.

Upgrade the indexer/API and schema before enabling an Arc execution producer. Migration **0017** adds cause 4 to all four partitioned state-history tables without deleting or rewriting their history; unknown causes remain rejected. Old wire readers reject cause 4 and older builds cannot open the newer schema. Existing StableNet payloads and ordinary Arc RPC/native-feed behavior retain their encodings. [E4 balance/history compatibility](EXECUTION_FEATURE_WRITERS.md) also applies.

## Functional evidence and remaining work

Native-library tests cover empty-block accounting, transfer/storage/revert, creation/code/constructor destruction, wrong roots/parent/gas/receipts, missing code, omitted account/slot/code and false before values. Two reproducible [synthetic Arc fixtures](../testdata/execution/README.md) cross the Rust→Go wire, real fsync-on disposable PostgreSQL's seven writers, public API snapshots and fresh failure/retry checks. The Arc checkpoint command also rebuilds the full synthetic account/storage roots independently in Go, while invalid DB/RPC settings prove that verification does not open either. New transfer concurrency uses GOMAXPROCS1/race/single200. These are functional checks, not live-chain or throughput evidence.

Still separate: a persistent Arc collector/cursor and supervised restart/reorg path, Arc-native full checkpoint export and the E3 native layout/retention adapter, complete traces/blob availability, actual retained DB/peer validation and E6 independent canonical/finality validation. Actual node services, peers, disk provisioning, GC activation and additional performance measurements remain on hold. Current [E5 plan/checklist](codemine/runs/20261024-e5-arc-execution/plan.md) and [remaining work](analysis/open-items.md) record release status.

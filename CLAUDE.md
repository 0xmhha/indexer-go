# StableNet Blockchain Indexer (Go)

## Product Context

- **What**: Blockchain indexer for StableNet (Stable-One) and other EVM chains
- **Target User**: StableNet dApp developers, block explorer backends
- **Core Value**: Block/transaction indexing with GraphQL, JSON-RPC and WebSocket APIs
- **Current Stage**: Explorer backend in use; Phase 0 integrity fixes in progress (see `docs/analysis/`)
- **Dependencies**: `indexer-frontend` consumes this service's GraphQL API

## Technical Context

### Architecture

```
cmd/indexer/main.go             Entry point, wiring and lifecycle
pkg/
  adapters/                     Chain adapters (Anvil, EVM, StableOne); node facts and system contract metadata; follow the chain profile
    detector/                   Node type detection (web3_clientVersion probes)
  api/
    graphql/                    GraphQL (graphql-go, hand-written schema; not gqlgen)
    jsonrpc/                    JSON-RPC (HTTP POST, filters; no eth_subscribe)
    websocket/                  /ws hub (not fed by the indexer yet)
  events/                       In-process EventBus used in production
  eventbus/                     Local/Redis/Kafka adapters (not wired in main.go)
  chains/                       Chain profiles and registries; everything chain specific lives in chains/<chain>/
    stablenet/                  StableNet profile: fee delegation (0x16), WBFT hash, Anzeon fees, native accounting
      consensus/, consensus/api/          WBFT store, parser, statistics, events; GraphQL/JSON-RPC extension
      feedelegation/, feedelegation/api/  Fee delegation statistics; GraphQL extension
      systemcontracts/, systemcontracts/api/  System contract events, store, constants; GraphQL/JSON-RPC extension
      features/                 stablenet.wbft, stablenet.fee_delegation, stablenet.system_contracts
  fetch/                        Block ingestion (sequential live loop, one storage transaction per block; worker pool only for gap fill)
  source/                       Block sources: rpc (node), era (era1 archives), replay (recorded JSON-RPC), Chained
  storage/                      PebbleDB storage (interfaces and implementation in one package)
  multichain/                   Multi-chain orchestration (chains share storage keys; do not enable)
  resilience/                   Session/event cache (not wired)
  rpcproxy/                     Node RPC proxy with cache and circuit breaker
internal/
  config/                       YAML/env/flag configuration
  constants/                    Global constants
  testchain/                    Deterministic JSON-RPC test chain for end-to-end tests
tools/astgraph/                 Code graph generator (separate Go module)
```

### Build & Run

```bash
make build          # Production binary (version from git describe)
make build-dev      # With debug symbols
make test           # Run tests
make coverage       # HTML coverage report
make lint           # golangci-lint
make docker-build   # Container image
```

`make generate` runs gqlgen, but the runtime GraphQL server does not use generated code.

### Key Patterns

- **EventBus**: `main.go` uses `events.NewEventBus` directly; subscriptions read `sub.Channel` (not `sub.Events()`)
  - Publishing is non-blocking; events are dropped when a buffer is full
  - `pkg/eventbus` (Redis, Kafka, factory with local degradation) exists but is not wired
- **Storage**: PebbleDB. Each block is indexed in one block transaction (`BeginBlock`, indexed batch bound to ctx; all access goes through `s.kv(ctx)`). Blocks are always read through the chain profile source; the legacy write path and go-ethereum client path were removed after v0.1.0 (`atomic_block: false` or `profile_source: false` is rejected at startup). Each block's commit records undo (`/undo/<height>`, last 128 blocks); on a reorg the live loop rolls back to the fork point, keeps the removed blocks as orphans (`/orphan/`, queryable with GraphQL `reorgs`/`orphanedBlock`/`orphanedTransaction`) and publishes a `reorg` event followed by the removed logs with `removed: true` (`docs/analysis/reorg-design.md`)
- **Fetcher**: Live indexing processes blocks sequentially by polling; the worker pool (`indexer.workers`) is used only by gap recovery
  - All state changes (index block, rollback, backfill) run as commands on one writer goroutine (`pkg/fetch/writer.go`, the only caller of `BeginBlock`, enforced by a test). Startup recovery is `Fetcher.Recover`: reorg check, feature state, backfill (order-independent features backfill online in the background)
- **Sources** (`pkg/source`): `rpc.endpoint: replay:///dir` replays an archive written with `rpc.record_dir`; `source.era_dir` reads the blocks held by era1 files (`gstable export-history`) from the files and later blocks from the node (the archive must match the node's chain). Era1 receipts carry consensus fields only; the chain profile derives the rest (`chains.BinaryProfile`, StableNet uses the Anzeon effective gas price rule). Blob gas prices are not derived from era1 (left nil): they depend on the chain's blob schedule, which block data does not carry
- **Adapter**: The chain profile is selected once at startup (`--adapter` names a profile id or alias such as `stableone`, otherwise detection) and the adapter follows it; `--adapter anvil` selects an adapter without a profile
- **Chain profiles** (`pkg/chains`): decode raw blocks into the chain-neutral model; chain-specific behaviour reaches chain-neutral code only through registries: `pkg/chains` (profiles, aliases, accounting, fee delegation, native coin), storage keyspace (`storage.RegisterKeyspace`, used by reindex), GraphQL (`graphql.RegisterExtension`, `RegisterSubscription`), JSON-RPC (`jsonrpc.RegisterMethod`), event bus codecs (`events.RegisterCodec`) and known token metadata (`storage.RegisterKnownToken`). Chain packages store data through `storage.KV` (bound to the block transaction by ctx) and `main.go` links them with blank imports. `pkg/chains/coupling_test.go` keeps every chain-neutral package (`pkg/` except `chains` and `adapters`, and `internal/`) free of chain profile imports; `TestChainIdentifierLimits` caps chain-specific names per package (`pkg/chains/testdata/chain-identifiers.txt`)
- **Features** (`pkg/feature`, `pkg/features/...`): optional per-block handlers that run inside the block transaction. Defaults come from the chain profile; override with `features.<name>.enabled` or `INDEXER_FEATURES=name,-name`. Features: `address.index`, `balance.native`, `token.transfers`, `aa.eip7702`, `aa.erc4337`, `aa.erc7579` (on by default) and `stablenet.wbft`, `stablenet.system_contracts`, `stablenet.fee_delegation` (on for StableNet). `account_abstraction.enabled: false` still turns off `aa.erc4337` and `aa.erc7579`. Enabling a feature on an indexed database backfills it from stored blocks before ingest starts (feature state under `/meta/features/`) (`docs/analysis/feature-registry-design.md`)

### Configuration

Precedence (lowest to highest): defaults < YAML < environment variables < CLI flags

```yaml
rpc:
  endpoint: "http://127.0.0.1:8501"
indexer:
  workers: 100
  poll_interval: 50ms   # head polling once caught up (head latency); separate from error retry delay
  finality: head        # head | confirmations (with confirmations: N) | finalized; head suits StableNet (WBFT is final on insertion)
  orphan_retention: 1000 # reorganization records kept with their removed blocks; 0 keeps all
eventbus:
  publish_buffer_size: 65536    # events are dropped only when buffers are full
  subscriber_buffer_size: 16384 # per API subscription
api:
  port: 8080
  enable_graphql: true
  enable_jsonrpc: true
  enable_websocket: true
```

Pre-configured: `configs/config-{anvil,devnet,sepolia}.yaml`

Settings that are read but not wired (`eventbus.type` other than local and `node.*`, `watchlist.enabled`, `resilience.enabled`, `account_abstraction.entry_point_addresses`) are reported at startup (`Config.UnsupportedSettings`); `database.readonly: true` is rejected (an API-only role is planned, R4-1). Command-line flags override the configuration only when given explicitly.

### API Endpoints

| Path | Type | Purpose |
|------|------|---------|
| `/graphql` | GraphQL | Blocks, transactions, custom queries |
| `/playground` | HTTP | GraphQL playground |
| `/graphql/ws` | WebSocket | GraphQL subscriptions |
| `/rpc` | JSON-RPC | Ethereum-compatible RPC |
| `/api` | HTTP | Etherscan-compatible API |
| `/ws` | WebSocket | Event hub (currently receives no events) |
| `/health` | HTTP | Health check |
| `/metrics` | Prometheus | Metrics |

### Testing

- testify/assert, testify/require
- Integration tests: `//go:build integration` tag
- End-to-end tests run the real `NewApp` wiring against `internal/testchain`:
  - `cmd/indexer/golden_test.go`: pins the full storage keyspace (`testdata/golden/keyspace.txt`, StableNet scenario `keyspace-stablenet.txt`); regenerate with `go test ./cmd/indexer -run TestGolden -update`
  - `cmd/indexer/api_snapshot_test.go`: pins the served GraphQL schema and JSON-RPC methods (`testdata/api/`), including the chain extensions
  - `cmd/indexer/balance_test.go`: compares indexed native balances with the test chain's state (EVM and StableNet rules)
  - `cmd/indexer/defects_test.go`: reproduces known data-integrity defects listed in `knownDefects`; remove an id when its fix lands
- Benchmarks: EventBus performance tests

### Dependencies

- Go 1.24, go-ethereum v1.16.5
- PebbleDB v1.1.5, graphql-go v0.8.1 (gqlgen is pinned as a tool only)
- chi/v5, gorilla/websocket, zap, prometheus

### Current Work

- Framework refactoring plan: `docs/analysis/refactoring-plan.md`
- Phase 0 design and progress: `docs/analysis/phase0-design.md`
- Phase 0 fixed address sequence reset (D1), non-atomic block writes (D2), non-idempotent reprocessing (D3), gap recovery cursor rewind (D10), the storage wrapper hiding features (F1), unwired SetCode/UserOp/Module/fee delegation (F2) and system contract decoding (D11). Existing databases need a reindex
- Blocks are read as raw JSON and decoded by the chain profile (`pkg/chains`, `pkg/source`) and stored as the chain-neutral model (`pkg/core/model`, storage schema v2): StableNet fee delegation (D13) and WBFT block hashes (D16) are kept as the chain reports them, in storage and in GraphQL/JSON-RPC responses (`docs/analysis/chain-profile-design.md`)
- Gap recovery fills missing blocks below indexed blocks only when every enabled feature is order-independent; otherwise it stops with `fetch.ErrGapBelowIndexed` and the database must be reindexed (D12)
- Open: multi-chain mode shares storage keys (D4) and is rejected at startup
- Fixed after comparing with go-stablenet: native balances follow the chain's fee and value rules (StableNet: native transfer logs, tip to the coinbase, base fee distribution, fee payers), system contract event signatures, WBFT signing statistics from canonical seals, native coin transfers recorded as tokens, swallowed system contract storage errors, missing finalized tag, invalid fee payer signatures, system contract events never published
- Chain-specific code (WBFT, fee delegation statistics, system contracts) lives under `pkg/chains/stablenet`. The legacy ingest paths were removed after v0.1.0; databases indexed by them need a reindex
- Live verification against a local go-stablenet network: `TestLiveStableNet` (runs only with `INDEXER_LIVE_RPC`); also `TestLiveBalances` (compares indexed balances with `eth_getBalance`; needs transactions in the last 100 blocks), `TestLiveRecordReplay`, `TestLiveEraSource` (needs `INDEXER_LIVE_ERA_DIR`) and `TestLiveHeadLatency` (needs `INDEXER_LIVE_LATENCY=1`)

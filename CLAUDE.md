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
  adapters/                     Chain abstraction (Anvil, EVM, StableOne)
    detector/                   Node type detection (web3_clientVersion probes)
  api/
    graphql/                    GraphQL (graphql-go, hand-written schema; not gqlgen)
    jsonrpc/                    JSON-RPC (HTTP POST, filters; no eth_subscribe)
    websocket/                  /ws hub (not fed by the indexer yet)
  events/                       In-process EventBus used in production
  eventbus/                     Local/Redis/Kafka adapters (not wired in main.go)
  fetch/                        Block ingestion (sequential live loop, one storage transaction per block; worker pool only for gap fill)
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
- **Storage**: PebbleDB. Each block is indexed in one block transaction (`BeginBlock`, indexed batch bound to ctx; all access goes through `s.kv(ctx)`). `indexer.atomic_block: false` selects the legacy path until it is removed
- **Fetcher**: Live indexing processes blocks sequentially by polling; the worker pool (`indexer.workers`) is used only by gap recovery
- **Adapter**: Detects the node type and selects an adapter; `--adapter` forces one
- **Chain profiles** (`pkg/chains`): decode raw blocks into the chain-neutral model; chain-specific behaviour reaches chain-neutral code (`pkg/fetch`, `pkg/api`, `pkg/source`, `pkg/feature`) only through registries in `pkg/chains` (enforced by a test)
- **Features** (`pkg/feature`, `pkg/features/...`): optional per-block handlers that run inside the block transaction. Defaults come from the chain profile; override with `features.<name>.enabled` or `INDEXER_FEATURES=name,-name`. Migrated so far: `stablenet.wbft` (`docs/analysis/feature-registry-design.md`)

### Configuration

Precedence (lowest to highest): defaults < YAML < environment variables < CLI flags

```yaml
rpc:
  endpoint: "http://127.0.0.1:8501"
indexer:
  workers: 100
api:
  port: 8080
  enable_graphql: true
  enable_jsonrpc: true
  enable_websocket: true
```

Pre-configured: `configs/config-{anvil,devnet,sepolia}.yaml`

Known config issues: `database.readonly` and several sections (`eventbus`, `node`, `watchlist`, `resilience`, `account_abstraction`) are read but ignored. The `--workers` default overrides the YAML value.

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
  - `cmd/indexer/golden_test.go`: pins the full storage keyspace (`testdata/golden/keyspace.txt`); regenerate with `go test ./cmd/indexer -run TestGolden -update`
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
- Open: out-of-order gap filling corrupts order-dependent state (D12); multi-chain mode shares storage keys (D4) and is rejected at startup
- Live verification against a local go-stablenet network: `TestLiveStableNet` (runs only with `INDEXER_LIVE_RPC`)

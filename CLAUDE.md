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
  api/
    graphql/                    GraphQL (graphql-go, hand-written schema; not gqlgen)
    jsonrpc/                    JSON-RPC (HTTP POST, filters; no eth_subscribe)
    websocket/                  /ws hub (not fed by the indexer yet)
  events/                       In-process EventBus used in production
  eventbus/                     Local/Redis/Kafka adapters (not wired in main.go)
  chains/                       Chain profiles and registries; everything chain specific lives in chains/<chain>/
    stablenet/                  StableNet profile: fee delegation (0x16), WBFT hash, Anzeon fees, native accounting
      consensus/, consensus/api/          WBFT store, parser, statistics, events; GraphQL/JSON-RPC extension
      feedelegation/, feedelegation/api/  Fee delegation metadata store and statistics; GraphQL extension
      systemcontracts/, systemcontracts/api/  System contract events, store, constants; GraphQL/JSON-RPC extension
      features/                 stablenet.wbft, stablenet.fee_delegation, stablenet.system_contracts
  fetch/                        Block ingestion: one pipeline (parallel fetch, in-order indexing, one storage transaction per block) for the live loop and gap recovery
  source/                       Block sources: rpc (node), era (era1 archives), replay (recorded JSON-RPC), Chained
  core/
    model/                      Chain-neutral block model
    port/                       Storage ports: interfaces and their value types (imports no implementation)
    gethconv/                   go-ethereum type conversions
  storage/                      PebbleDB implementation of the ports; the Storage union is for assembly only
  multichain/                   Multi-chain orchestration (one database and pipeline per chain under <db>/chains/<id>)
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
  - Events of indexed blocks go through the outbox (R3-1): the writer records them in the block's transaction (`port.Outbox`, `/outbox/<seq>`; reorg events in the rollback's transactions via `port.UndoHook`) and `pkg/stream`'s relay delivers them after the commit in sequence order, waiting instead of dropping when the bus is full. Each event carries its chain's sequence (`events.Stream`, `events.SequenceOf`, 1, 2, ... without gaps); the bus drops sequences it has delivered (relay restarts deliver again from the last recorded cursor). Events are encoded with `events.MarshalEvent` (block events keep the header only). `eventbus.outbox: false` publishes directly after the commit, without sequences
  - Consumers of the change stream (R3-2) go through the bus port `stream.Bus` (`Join`, `Consume`); `stream.OutboxBus` reads the outbox directly, with one position per consumer group (`/meta/outbox/cursor/<group>`), so every group receives every event and resumes after the last batch it accepted. The relay consumes as `node.id` (default hostname); `Fetcher.Stream()` gives the bus to other consumers. Bus implementations must pass `pkg/stream/streamtest`
  - GraphQL subscriptions (`/graphql/ws`) go through the subscription engine `stream.Engine` (R3-3), the server's only bus subscription: copy-on-write subscription lists per event type, each event encoded once per subscription kind and shared, a bounded queue per connection (`eventbus.subscriber_buffer_size`). A connection whose queue overflows is disconnected with a `SLOW_SUBSCRIBER` error carrying `resumeFrom` (close 1008); "next" messages carry `extensions.sequence`. `api.subscription_engine: false` restores a bus subscription per client subscription
  - Resynchronization (R3-4): the subscription variable `fromSequence` delivers the events from that sequence still in the outbox, then live ones, each sequence once (`Conn.SubscribeFrom`; live events wait in the subscription while the outbox is read). Clients start from a snapshot: `{ streamSequence }`, then the state, then `fromSequence` = position + 1. Errors: `SEQUENCE_TOO_OLD` (pruned, `oldest`), `RESUME_UNSUPPORTED` (no outbox)
  - Notifications (R3-5) consume the change stream as group `notifications` (`NotificationService.SetStream`, wired in `streamNotifications`): each notification is stored under an id derived from the setting and the event's sequence before the batch is accepted, so a full queue or a restart loses none and redelivery creates none twice; the retry processor sends stored notifications the queue had no room for. Without the outbox the service subscribes to the event bus
  - `pkg/eventbus` (Redis, Kafka, factory with local degradation) exists but is not wired
- **Storage**: PebbleDB. Each block is indexed in one block transaction (`BeginBlock`, indexed batch bound to ctx; all access goes through `s.kv(ctx)`). Blocks are always read through the chain profile source; the legacy write path and go-ethereum client path were removed after v0.1.0 (`atomic_block: false` or `profile_source: false` is rejected at startup). Each block's commit records undo (`/undo/<height>`, last 128 blocks); on a reorg the live loop rolls back to the fork point, keeps the removed blocks as orphans (`/orphan/`, queryable with GraphQL `reorgs`/`orphanedBlock`/`orphanedTransaction`) and publishes a `reorg` event followed by the removed logs with `removed: true` (`docs/analysis/reorg-design.md`)
- **PostgreSQL** (`pkg/storage/postgres`, R4-2; `database.driver: postgres`, `database.postgres.dsn`/`schema`/`max_conns`; multi-chain mode uses schema `<schema>_<id>`; `cmd/indexer/store.go` is the only place that tells the drivers apart): relational adapter of the storage ports; tables keep query columns plus the model encoded with `pkg/core/model`; a block transaction is a PostgreSQL transaction bound to ctx; migrations under `migrations/` are applied on open (read-only stores only check the version). Every port passes the contracts (statistics computed from the ports live in `pkg/storage/history`, shared with Pebble). Rollback undo is trigger based: during a block transaction each tracked table records the changed rows in `undo_log`, a commit with a height attaches them to the block (last 128 blocks), and `RollbackTo` replays them newest first; orphans and the outbox are not tracked. A migration that adds a table calls `undo_track` for it (`TestEveryTableIsUndoTracked`); Tests need `INDEXER_TEST_POSTGRES` or run with `make test-postgres` (disposable container)
- **Ports** (`pkg/core/port`): ports speak the chain-neutral model (`pkg/core/model`: blocks, transactions, receipts, logs); go-ethereum `core/types` stays out of them (enforced by `TestPortsImportNoImplementation`) and is converted at the edges with `pkg/core/gethconv`. Consumers take the storage ports they use. The APIs take `port.QueryStore`, `feature.Deps.Storage` is a `port.Reader`, and optional ports (address index, token holder, module, orphan, ...) are taken by type assertion. Only `cmd` and `pkg/multichain` name `storage.Storage` or the Pebble types (`TestStorageAssemblyOnly`); `pkg/storage` declares no port aliases (`TestNoPortAliases`), and `pkg/core/port` imports no implementation (`TestPortsImportNoImplementation`)
- **RPC pool** (`pkg/rpcpool`): an `http.RoundTripper` under the go-ethereum RPC client, so ethclient, the block source and the recording proxy share it: per-attempt timeout (`rpc.timeout`), rate limit, failover to `rpc.fallback_endpoints` on connection errors, timeouts, 429 and 5xx (a failed endpoint cools down for 10s, then the primary is used again; JSON-RPC errors are returned as they are). `rpc.ws_endpoint` subscribes to newHeads to wake the live loop (live p95 head latency 1-2ms instead of ~50ms)
- **Pagination** (`port.Page`): list ports take `Page{After, Limit, Offset}` and return the next page's cursor; a cursor page costs the same at any depth (Pebble `scanPage` over the list's key range), Offset is kept for clients that page by number. GraphQL `pagination.after` / `pageInfo.endCursor`, JSON-RPC `after` / `nextCursor`
- **Fetcher**: The live loop, gap recovery and single-block fetches share one pipeline (`pkg/fetch/scheduler.go`): `indexer.workers` fetch concurrently, a reorder buffer indexes in height order, at most 2×workers heights are in flight (backpressure), and an error stops at the lowest failing height with every block below it indexed
  - All state changes (index block, rollback, backfill) run as commands on one writer goroutine (`pkg/fetch/writer.go`, the only caller of `BeginBlock`, enforced by a test). Startup recovery is `Fetcher.Recover`: reorg check, feature state, backfill (order-independent features backfill online in the background)
- **Sources** (`pkg/source`): `rpc.endpoint: replay:///dir` replays an archive written with `rpc.record_dir`; `source.era_dir` reads the blocks held by era1 files (`gstable export-history`) from the files and later blocks from the node (the archive must match the node's chain). Era1 receipts carry consensus fields only; the chain profile derives the rest (`chains.BinaryProfile`, StableNet uses the Anzeon effective gas price rule). Blob gas prices are not derived from era1 (left nil): they depend on the chain's blob schedule, which block data does not carry
- **Profile selection**: The chain profile is selected once at startup: `--adapter` names a profile id or alias such as `stableone`; an empty name or one that is no profile (`anvil`) detects it from the node (`sourcerpc.Select`). The former adapter layer (`pkg/adapters`, `pkg/types/chain`) was removed; token contracts that exist from genesis come from `chains.RegisterKnownToken`
- **Chain profiles** (`pkg/chains`): decode raw blocks into the chain-neutral model; chain-specific behaviour reaches chain-neutral code only through registries: `pkg/chains` (profiles, aliases, accounting, fee delegation, native coin), storage keyspace (`storage.RegisterKeyspace`, used by reindex), GraphQL (`graphql.RegisterExtension`, `RegisterSubscription`), JSON-RPC (`jsonrpc.RegisterMethod`), event bus codecs (`events.RegisterCodec`) and known token metadata (`chains.RegisterKnownToken`). Chain packages store data through `port.KV` (bound to the block transaction by ctx) and `main.go` links them with blank imports. `pkg/chains/coupling_test.go` keeps every chain-neutral package (`pkg/` except `chains` and `adapters`, and `internal/`) free of chain profile imports; `TestChainIdentifierLimits` caps chain-specific names per package (`pkg/chains/testdata/chain-identifiers.txt`)
- **Features** (`pkg/feature`, `pkg/features/...`): optional per-block handlers that run inside the block transaction. Defaults come from the chain profile; override with `features.<name>.enabled` or `INDEXER_FEATURES=name,-name`. Features: `address.index`, `balance.native`, `token.transfers`, `token.metadata` (asks the node whether a created contract is a token), `aa.eip7702`, `aa.erc4337`, `aa.erc7579` (on by default) and `stablenet.wbft`, `stablenet.system_contracts`, `stablenet.fee_delegation` (on for StableNet). `account_abstraction.enabled: false` still turns off `aa.erc4337` and `aa.erc7579`. Enabling a feature on an indexed database backfills it from stored blocks before ingest starts (feature state under `/meta/features/`) (`docs/analysis/feature-registry-design.md`)

### Configuration

Precedence (lowest to highest): defaults < YAML < environment variables < CLI flags

```yaml
rpc:
  endpoint: "http://127.0.0.1:8501"
  fallback_endpoints: []  # further HTTP(S) nodes of the same chain; calls fail over in order (pkg/rpcpool)
  rate_limit: 0           # requests per second to the nodes; 0 = unlimited
  ws_endpoint: ""         # newHeads subscription that wakes the live loop (polling stays the fallback)
indexer:
  workers: 100
  poll_interval: 50ms   # head polling once caught up (head latency); separate from error retry delay
  finality: head        # head | confirmations (with confirmations: N) | finalized; head suits StableNet (WBFT is final on insertion)
  orphan_retention: 1000 # reorganization records kept with their removed blocks; 0 keeps all
eventbus:
  publish_buffer_size: 65536    # events are dropped only when buffers are full
  subscriber_buffer_size: 16384 # per API subscription (per connection with the subscription engine)
api:
  port: 8080
  enable_graphql: true
  enable_jsonrpc: true
  enable_websocket: true
```

Pre-configured: `configs/config-{anvil,devnet,sepolia}.yaml`

Settings that are read but not wired (`eventbus.type` other than local, `node.role`, `node.priority`, `account_abstraction.entry_point_addresses`) are reported at startup (`Config.UnsupportedSettings`), and so are `watchlist.enabled` and `resilience.enabled`, whose packages were removed after v0.1.0; `database.readonly: true` is rejected (an API-only role is planned, R4-1). Command-line flags override the configuration only when given explicitly.

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
- Storage port contracts: `pkg/core/port/porttest` checks any storage against every port (`porttest.Run`); Pebble runs it in `pkg/storage/contract_test.go`, which also requires the contracts to fail on deliberately broken stores
- PostgreSQL: `make test-postgres` runs the adapter tests and the whole `cmd/indexer` end-to-end suite on PostgreSQL (`INDEXER_TEST_DRIVER=postgres`, `INDEXER_TEST_POSTGRES`; `cmd/indexer/testdb_test.go` gives every test database a schema and dumps it for the keyspace comparisons; tests of Pebble itself call `pebbleOnly`). `TestPostgresMatchesPebble` indexes the scenarios with both drivers and compares every read port
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
- Phase 1 is complete: storage ports in `pkg/core/port` speak the model (R1-1), `KVStore` removed from the public interface (R1-2), numeric keys sort numerically (R1-3 K1; binary keys K2 measured and deferred), port contract tests in `porttest` (R1-4, the 25 Pebble defects they found are fixed), keyset pagination for every list port (R1-5), `backend.go` removed (R1-6), adapter layer removed. Block times are indexed on ingest (`/index/time/`); databases indexed before need a reindex for time queries and for the UserOperation indexes
- Multi-chain mode (R2-8, fixes D4): every chain runs its own `App` (built by `newChainIndexer`, the `multichain.IndexerFactory`) into its own database `<database.path>/chains/<id>`; the API serves each chain under `/chains/<id>/` (graphql, graphql/ws, playground, rpc) and lists them at `GET /chains`, with no root API
- Fixed after comparing with go-stablenet: native balances follow the chain's fee and value rules (StableNet: native transfer logs, tip to the coinbase, base fee distribution, fee payers), system contract event signatures, WBFT signing statistics from canonical seals, native coin transfers recorded as tokens, swallowed system contract storage errors, missing finalized tag, invalid fee payer signatures, system contract events never published
- Chain-specific code (WBFT, fee delegation statistics, system contracts) lives under `pkg/chains/stablenet`. The legacy ingest paths were removed after v0.1.0; databases indexed by them need a reindex
- Live verification against a local go-stablenet network: `TestLiveStableNet` (runs only with `INDEXER_LIVE_RPC`); also `TestLiveBalances` (compares indexed balances with `eth_getBalance`; needs transactions in the last 100 blocks), `TestLiveRecordReplay`, `TestLiveEraSource` (needs `INDEXER_LIVE_ERA_DIR`) and `TestLiveHeadLatency` (needs `INDEXER_LIVE_LATENCY=1`)

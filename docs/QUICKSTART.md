# Quickstart

Build the indexer, collect a local EVM chain and query the result. Then connect a node of your choice or build a project extension.

## Prerequisites

- Git, Make and Go. The repository pins `go1.26.8` in [go.mod](../go.mod).
- An EVM JSON-RPC endpoint. The local walkthrough below uses [Anvil](https://getfoundry.sh/anvil/overview); install it separately if you want to follow that path.
- Docker only if you run the disposable PostgreSQL integration tests. The local walkthrough uses embedded Pebble.

## Build from source

```bash
git clone https://github.com/0xmhha/indexer-go.git
cd indexer-go
go mod download
make build
./build/indexer-go --version
```

Run the remaining commands from the repository root. The binary is `build/indexer-go`.

## Run a local chain

In a separate terminal, start Anvil with periodic blocks:

```bash
anvil --block-time 2
```

Start the indexer:

```bash
./build/indexer-go --config configs/config-anvil.yaml
```

The [development configuration](../configs/config-anvil.yaml) connects to `http://127.0.0.1:8545`, uses the generic EVM profile, starts from genesis and stores its index in `./data-anvil`. Its API listens on port 8080. Stop the process with Ctrl+C; restarting with the same configuration resumes the saved index.

Keep the local node's chain history consistent with the index. If you reset Anvil, use a separate development database rather than resuming an index for the previous chain.

## Query the API

Check service health:

```bash
curl -sS http://127.0.0.1:8080/health
```

Read the latest indexed height:

```bash
curl -sS http://127.0.0.1:8080/graphql \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ latestHeight }"}'
```

The same height is available through the project's JSON-RPC method:

```bash
curl -sS http://127.0.0.1:8080/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1}'
```

Open [Playground](http://127.0.0.1:8080/playground) for schema exploration. Health and indexed progress describe different things: a healthy service can still be catching up. See the [API reference](API.md) for block queries, pagination and `/graphql/ws` subscriptions.

## Connect your own RPC

### Choose a starting height

Check the node's chain identity and the historical data available to your enabled features. Default state-dependent features such as `balance.native` and `token.metadata` need state at specific blocks, including the parent state used to initialize balances.

For a new database, select a starting block whose required state is available. On restart, the next indexed block and any feature backfill must still be supported by the node. Starting near the tip omits earlier history from your index.

A pruned RPC cannot supply state it has already removed. With the default `allow_pruned_state: false`, missing required state rejects startup or rolls back the affected block; it is not replaced with a latest-state result or a zero balance. Increasing queue size does not recover missing state.

### Create a configuration

Save the following as `config.yaml`. Replace the endpoint with your own node and use a database path dedicated to that chain and profile.

```yaml
rpc:
  endpoint: http://127.0.0.1:8545
  timeout: 30s
  rate_limit: 5
database:
  driver: pebble
  path: ./data/my-chain
indexer:
  workers: 1
  poll_interval: 1s
  finality: head
api:
  enabled: true
  host: 127.0.0.1
  port: 8080
log:
  level: info
```

This example follows the head and caps physical HTTP RPC attempts at five per second. Batch members can count separately toward a provider's quota. The cap is a request budget, not a throughput guarantee. Choose the finality policy appropriate to your application; see [configuration](CONFIG.md).

Set `INDEXER_START_HEIGHT` to your chosen decimal block height, then run:

```bash
./build/indexer-go --config config.yaml \
  --start-height "${INDEXER_START_HEIGHT:?set a retained starting block height}"
```

Profiles are detected from the node. You can select a registered id or alias explicitly with `--adapter`, such as `evm`, `stablenet` / `stableone`, or `arc`. Check the selected-profile startup log; an unregistered adapter name currently falls back to detection.

`config.yaml` is ignored by Git. Command-line flags override the corresponding configuration values when explicitly set. Additional environment variables, feature settings and batch options are documented in [CONFIG.md](CONFIG.md).

## Choose storage and process roles

| Need | Configuration path |
| --- | --- |
| Run ingestion and APIs together | Pebble or PostgreSQL with `node.role: all` |
| Run only ingestion | `node.role: ingest`; HTTP exposes health and metrics |
| Separate API processes from ingestion | PostgreSQL with `node.role: api` and an already migrated database |
| Index multiple chains | Multi-chain `all` mode; separate per-chain databases and `/chains/{id}/` APIs |

Use [PostgreSQL configuration](CONFIG.md#postgresql) for the DSN, schema and bounded connection pool. The API reservation shares that pool; it does not add connections. The current mixed-load functional setup uses four total connections, including one API reservation.

A database is tied to its chain, profile and collected range. In particular, an Arc database created with generic EVM native accounting needs reindexing for the Arc profile. See [data management](CONFIG.md#data-management) before using `--reindex` or `--clear-data`; those operations delete indexed data.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Build requests a different Go version | Use the toolchain pinned in `go.mod`; see [development setup](../CONTRIBUTING.md#development-setup) |
| Startup reports missing historical state | Check the selected start/backfill height, enabled features and the RPC's actual retention |
| Block queries return no data | Check the indexed range and progress; a recent start does not include earlier blocks |
| Multi-chain requests fail at root endpoints | Discover chains with `GET /chains` and use `/chains/{id}/...` |
| The selected profile is unexpected | Check the adapter id, linked registration package and startup detection log |

## Next steps

- Build a contract-specific backend with the [receipts example](../examples/receipts/README.md) and [SDK](SDK.md).
- Add chain-specific behavior with [chain customization](CHAIN_CUSTOMIZATION.md).
- Read the [architecture](ARCHITECTURE.md) before changing ingestion, rollback or storage.
- Configure the experimental Arc disk-window path with [feed application](FEED_APP.md). It requires explicit peers, a checkpoint and storage configuration; it is separate from this RPC walkthrough.
- Review [remaining operational work](analysis/open-items.md) before treating testnet samples or local checks as production guarantees.

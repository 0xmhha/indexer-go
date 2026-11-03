# indexer-go

Build EVM indexing backends in Go. Collect blocks, transactions, receipts and logs, turn them into queryable application data, and add your own features, chain profiles and APIs.

[![CI](https://github.com/0xmhha/indexer-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/0xmhha/indexer-go/actions/workflows/ci.yml)
[![Go toolchain](https://img.shields.io/badge/Go-1.26.8-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[Quickstart](docs/QUICKSTART.md) · [Documentation](docs/README.md) · [API reference](docs/API.md) · [Extending](docs/EXTENDING.md) · [Examples](#examples)

## What you can build

Use indexer-go for block explorers, contract event services and dApp backends that need indexed chain data.

- **Application features in Go:** register block handlers, derived records and GraphQL/HTTP routes. Use declared records for selected contract events.
- **Consistent block writes:** commit feature data, progress and durable outbox events with the block; recover bounded reorgs through recorded undo.
- **Queryable data:** serve GraphQL, selected JSON-RPC methods, REST, subscriptions and authenticated notification streams.
- **Storage and process choices:** use embedded Pebble or PostgreSQL; isolate chains and separate ingest/API roles where supported.
- **Chain extensions:** keep custom decoding, verification and native accounting in linked profiles.
- **Optional execution data:** retain verified native state journals, replay call trees, full blobs and selected state proofs under explicit input contracts.

## Quickstart

Install Git, Make, the Go toolchain pinned in [go.mod](go.mod), and [Anvil](https://getfoundry.sh/anvil/overview) for this local walkthrough.

```bash
git clone https://github.com/0xmhha/indexer-go.git
cd indexer-go
go mod download
make build
```

Start a local chain in another terminal:

```bash
anvil --block-time 2
```

From the repository root, start the indexer:

```bash
./build/indexer --config configs/config-anvil.yaml
```

Read the indexed height:

```bash
curl -sS http://127.0.0.1:8080/graphql \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ latestHeight }"}'
```

Explore the schema in [GraphQL Playground](http://127.0.0.1:8080/playground). The [development configuration](configs/config-anvil.yaml) stores data in `./data-anvil` and serves the API on port8080. Restarting with the same chain and database resumes indexing.

`make build` produces `build/indexer` for generic EVM chains. `make build CHAIN=stablenet` and `make build CHAIN=arc` produce their chain-specific binaries. See the [full quickstart](docs/QUICKSTART.md) for custom RPC endpoints, starting heights and PostgreSQL.

## Extend the indexer

| Goal | Guide |
| --- | --- |
| Collect contract events with configuration | [Declared records](docs/CONFIG.md#선언형-수집-featuresrecords-indexermode) |
| Add handlers, derived data or a project API | [Extending](docs/EXTENDING.md) |
| Implement chain-specific headers, transactions or accounting | [Chain customization](docs/CHAIN_CUSTOMIZATION.md) |
| Integrate a client | [API reference](docs/API.md), [AA queries](docs/ACCOUNT_ABSTRACTION.md), [schema snapshots](pkg/app/testdata/api/) |

Projects build a binary linking their own features and profiles. YAML configures linked code; it does not load Go plugins. Pin the module version and run your extension's checks when upgrading; packages do not carry a compatibility promise.

## Examples

These separate Go modules extend the framework without changing its core:

- [receipts](examples/receipts/README.md): collect settlement events, maintain merchant totals and expose a project-specific HTTP API.
- [chainprofile](examples/chainprofile/README.md): register a project Arc testnet profile while retaining the base profile's verification and accounting.

Run their checks with `make test-examples`.

## Supported paths

| Path | Current scope |
| --- | --- |
| RPC indexing | Generic EVM, StableNet and Arc profiles; JSON-RPC, recorded replay and profile-supported era1 archives; Pebble or PostgreSQL |
| Arc ETH/68 feed | Opt-in header/body/receipt intake, durable raw window, permanent source facts and independent projections; [feed application](docs/FEED_APP.md) |
| Native execution | StableNet isolated execution and Arc App-owned native import/replay with verified journals, PostgreSQL publication and exact ACK/reorg recovery; [execution ingest](docs/EXECUTION_INGEST.md), [Arc ingress](docs/EXECUTION_ARC_INGRESS.md) |
| Checkpoint and retention | Offline bootstrap, Arc full native checkpoint export and supervised maintenance with explicit ownership, coverage and ACK gates; [bootstrap](docs/EXECUTION_BOOTSTRAP.md), [Arc retention](docs/EXECUTION_ARC_RETENTION.md) |
| Optional execution inputs | Replay [call trees/internal calls](docs/EXECUTION_CALL_TRACES.md), bounded [full blob/KZG supply](docs/EXECUTION_BLOBS.md), [selected account/code/storage proofs](docs/EXECUTION_STATE_PROOFS.md) and authenticated [StableNet WBFT policy](docs/EXECUTION_CONSENSUS.md) |
| Observation | Opt-in DB/WAL budgets and private alert state with a disabled sink; [observation guide](docs/NATIVE_MONITORING_INTEGRATION.md). The [VM probe library](docs/NATIVE_VM_OBSERVATION.md) uses an injected runner and has no configured Docker implementation |

Choose features for the data your source can supply. State-dependent RPC features stop on missing required historical state by default; starting near the tip does not create earlier history. APIs expose indexed ranges and collection status. Selected proofs, provider traces and KZG verification have distinct coverage and do not establish complete chain consensus or network-wide data availability.

Native execution requires its pinned client-library builds, a prepared checkpoint and explicit chain/fork/provider contracts. The pinned Arc profile rejects positive blob supply. Local functional coverage does not establish live peer access, external-disk reliability or sustained operating throughput. See [architecture](docs/ARCHITECTURE.md), [operating readiness](docs/OPERATIONS_READINESS.md) and [remaining work](docs/analysis/open-items.md) for those boundaries.

## Development and support

Use [CONTRIBUTING.md](CONTRIBUTING.md) for setup, focused checks and the CI-gated squash workflow. [CI tiers](docs/CI.md) explains short checks and explicit full qualification. The [documentation index](docs/README.md) groups current guides by task; detailed references are primarily in Korean.

Report bugs and propose features through [GitHub Issues](https://github.com/0xmhha/indexer-go/issues). Include a version/commit, enabled features, storage driver and a minimal reproduction. Remove credentials from logs and configuration before sharing them.

## License

[Apache License 2.0](LICENSE).

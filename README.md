# indexer-go

Build EVM indexing backends in Go. Turn blocks, transactions, receipts and logs into queryable application data, then extend the indexer with your own handlers, chain profiles and APIs.

[![CI](https://github.com/0xmhha/indexer-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/0xmhha/indexer-go/actions/workflows/ci.yml)
[![Go toolchain](https://img.shields.io/badge/Go-1.26.8-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[Quickstart](docs/QUICKSTART.md) · [Documentation](docs/README.md) · [API reference](docs/API.md) · [Extending](docs/EXTENDING.md) · [Examples](#examples)

## Why indexer-go?

Use indexer-go when you need a backend for a block explorer, a contract event service, or a dApp that queries indexed chain data.

- **Write application logic in Go.** Register project features and GraphQL or HTTP routes with the extension packages. Declare contract events in configuration when you only need selected logs.
- **Inspect native execution calls.** Arc and StableNet replay can retain [call trees and internal-call pages](docs/EXECUTION_CALL_TRACES.md), including reverted attempts, alongside verified state journals.
- [Full execution blobs](docs/EXECUTION_BLOBS.md): optionally acquire bounded Engine V1 full blobs, retain exact input before spool admission, verify KZG, and query bounded database pages with explicit collection status.
- [Selected execution state proofs](docs/EXECUTION_STATE_PROOFS.md): retain and query account/code/storage inclusion or absence at an exact block-end root.
- **Keep block writes consistent.** Indexed data, feature writes, progress and durable outbox events commit with the block. Reorg recovery uses recorded undo.
- **Choose your storage layout.** Run with embedded Pebble, or use PostgreSQL to separate ingestion and API processes. Multi-chain mode isolates each chain's database and API routes.
- **Expose the data you collect.** Serve GraphQL, JSON-RPC, REST and GraphQL subscriptions, with cursor-based reads and resumable outbox-backed streams.
- **Customize chain behavior.** Chain profiles own decoding, verification, native accounting and extensions, while the shared indexing pipeline uses chain-neutral models.

## Quickstart

You need Git, Make and the Go toolchain pinned in [go.mod](go.mod). This local example also uses [Anvil](https://getfoundry.sh/anvil/overview), an EVM development node.

```bash
git clone https://github.com/0xmhha/indexer-go.git
cd indexer-go
go mod download
make build
```

`make build` writes `build/indexer`, the indexer for generic EVM chains. StableNet and Arc have their own binaries (`make build CHAIN=stablenet`, `make build CHAIN=arc`); see the [quickstart](docs/QUICKSTART.md).

Start a local chain in a separate terminal:

```bash
anvil --block-time 2
```

From the repository root, start the indexer with the local development configuration:

```bash
./build/indexer --config configs/config-anvil.yaml
```

Query the indexed height from another terminal:

```bash
curl -sS http://127.0.0.1:8080/graphql \
  -H 'Content-Type: application/json' \
  -d '{"query":"{ latestHeight }"}'
```

Open the [GraphQL Playground](http://127.0.0.1:8080/playground) to explore the API. The example stores its index in `./data-anvil`.

For your own RPC endpoint, a retained starting height, or PostgreSQL, follow the [full quickstart](docs/QUICKSTART.md). State-dependent features need the state for the blocks they process; a recent start does not create an index of earlier history.

## Build your own indexer

| Your goal | Start here |
| --- | --- |
| Collect selected contract events | [Declared records](docs/CONFIG.md#선언형-수집-featuresrecords-indexermode) and the [receipts example](examples/receipts/README.md) |
| Add derived data, a block handler or a custom API | [Extending the indexer](docs/EXTENDING.md) |
| Support custom headers, transactions or native accounting | [Chain customization](docs/CHAIN_CUSTOMIZATION.md) and the [profile example](examples/chainprofile/README.md) |
| Integrate an API client | [API reference](docs/API.md) and [schema snapshots](pkg/app/testdata/api/) |
| Index verified StableNet execution journals into PostgreSQL | [Execution ingest](docs/EXECUTION_INGEST.md), [offline bootstrap](docs/EXECUTION_BOOTSTRAP.md), [offline retention](docs/EXECUTION_RETENTION.md) — opt-in, explicit trusted anchor |
| Continuously collect verified Arc execution journals | [Arc execution adapter](docs/EXECUTION_ARC.md) — official Arc EVM, read-only native adapter, bounded durable queue and restart/reorg/ACK supervision; live operation remains unverified |
| Decouple Arc intake from downstream processing | [Opt-in feed application](docs/FEED_APP.md) and [Arc feed module](x/arc/feed/README.md) |

Projects build their own binary and link the packages that register their features or profiles. YAML configures linked code; it does not load Go plugins. No package carries a compatibility promise: pin your module version and run your extension's tests before upgrading ([migration from the former SDK](docs/EXTENDING.md#8-이전-sdk에서-옮기기)).

## Examples

These are separate Go modules that use the framework without modifying its core:

- **[receipts](examples/receipts/README.md)** — collect a settlement contract's events, maintain merchant totals and expose a project-specific HTTP API. Includes local end-to-end and determinism checks.
- **[chainprofile](examples/chainprofile/README.md)** — register a project Arc testnet profile while retaining the base profile's verification and native accounting.

Run both example modules with `make test-examples`.

## Supported paths and scope

| Area | Available today |
| --- | --- |
| Chain profiles | Generic EVM (`evm`), StableNet (`stablenet` / `stableone`) and Arc (`arc`) |
| Sources | JSON-RPC, recorded RPC replay and profile-supported era1 archives |
| Storage | Pebble and PostgreSQL |
| APIs | GraphQL, selected JSON-RPC methods, REST, GraphQL subscriptions and authenticated notification streams |
| StableNet execution ingest | Two executions, durable result spool, seven writer queues on pool4, public state/API fence and bounded reorg; full/partial offline bootstrap and full-baseline-only offline retention with durable maintenance fences |
| Arc execution adapter | Official Arc second-pass replay, pre-block/transaction/Finalize state journals and root checks, read-only continuous collection with durable quotas/cursor and exact-ACK/reorg supervision; [full native checkpoints with bounded disk catalogs and supervised history retention](docs/EXECUTION_ARC_RETENTION.md). Further native allocation/index reclamation and traces/blob availability remain separate |
| Arc feed | Opt-in ETH/68 header/body/receipt intake, a durable disk window, permanent source facts and independent projection workers |

The Arc feed has local protocol and storage coverage; live Arc peer onboarding, external-volume operation and sustained throughput still need operational validation. Optional state proofs and provider-reported call traces have explicit scopes. Full blob verification is a separate library, and Arc blob admission is disabled.

Choose features according to the data your source can supply. State-dependent RPC features stop on missing required historical state by default. Reorg recovery has a bounded undo window, and APIs return the indexed range and enabled features. These boundaries matter when choosing a starting height, a node provider or an extension; see [architecture](docs/ARCHITECTURE.md) and [remaining work](docs/analysis/open-items.md).

## Documentation

The [documentation index](docs/README.md) groups guides by task. Entry guides are in English; most detailed references are currently in Korean.

- [Quickstart](docs/QUICKSTART.md) — build, run and query a local chain; connect your own RPC.
- [Configuration](docs/CONFIG.md) — features, databases, roles, multi-chain mode and notifications.
- [API reference](docs/API.md) — endpoints, query examples, pagination and subscriptions.
- [Extending](docs/EXTENDING.md) and [chain customization](docs/CHAIN_CUSTOMIZATION.md) — extend application behavior and chain rules.
- [Architecture](docs/ARCHITECTURE.md) — ingestion, storage, recovery and extension boundaries.

## Contributing and support

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, required checks and the pull request workflow. Pull requests merge with squash after the required CI checks pass.

The project is maintained by its [contributors](https://github.com/0xmhha/indexer-go/graphs/contributors). Use [GitHub Issues](https://github.com/0xmhha/indexer-go/issues) for questions, bug reports and feature proposals. Include the commit or version, enabled features, storage driver and a minimal reproduction. Remove credentials from configurations and logs before sharing them.

## License

[Apache License 2.0](LICENSE).

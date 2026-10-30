# Contributing to indexer-go

Contributions to the framework, examples and documentation are welcome. Start with the [project README](README.md), [architecture](docs/ARCHITECTURE.md) and the relevant extension contract.

## Discuss the change

Use [GitHub Issues](https://github.com/0xmhha/indexer-go/issues) for questions, bugs and feature proposals. For a bug, include the commit or version, chain/profile, enabled features, storage driver, expected behavior and a minimal reproduction. Redact credentials from RPC URLs, DSNs and logs.

For changes to storage formats, chain accounting, extension interfaces (`pkg/feature`, `pkg/api`, `pkg/chains`) or source completeness, describe the compatibility and recovery behavior before implementation. Existing design notes are context; they do not make every proposal part of the supported runtime.

## Development setup

Clone the repository and install the Go toolchain pinned in [go.mod](go.mod). Builds, tests, golangci-lint and the separate `tools/astgraph` module use Go 1.26.8.

```bash
go mod download
make build
```

Use a feature branch based on `main`. Keep local node credentials, `config.yaml`, database files, build outputs and test artifacts out of commits.

Docker is needed for `make test-postgres` unless you supply `INDEXER_TEST_POSTGRES`. The script creates an identifiable temporary PostgreSQL database with `fsync=on` and removes its own container and named volume. Use a disposable database when supplying a DSN yourself; the tests create schemas and write data.

The optional Rust native-feed adapter has its own prerequisites in [its README](x/arc/nodefeed/README.md). The [Arc execution adapter](x/arc/executor/README.md) pins Rust 1.98.1 and adds `make test-executor-arc` for build/fmt/clippy/offline replay. Its CI `arc` job also regenerates and compares the frozen Go inputs; it starts no node. Rust and an Arc execution node are not required for normal Go development.

## Find the right extension point

| Change | Location or contract |
| --- | --- |
| Project handlers, derived records and HTTP/GraphQL routes | [Extending](docs/EXTENDING.md), `pkg/feature`, `pkg/api`, [receipts example](examples/receipts/README.md) |
| Chain detection, decoding, verification or native accounting | [Chain customization](docs/CHAIN_CUSTOMIZATION.md), `x/<chain>` |
| Storage interfaces and driver implementations | `pkg/core/port`, `pkg/storage`, `pkg/storage/postgres` |
| Application lifecycle and multi-chain wiring | `pkg/app`, `internal/multichain` |
| Source/feed contracts and workers | `pkg/source`, `pkg/chainfeed`, `pkg/window`, `pkg/capture`, `x/arc/feed` |
| API schema and registered methods | `pkg/api`, [API snapshots](pkg/app/testdata/api/) |

Keep chain-specific rules in profiles or chain modules rather than adding chain-id branches to shared code. Project integrations should use the extension packages in [Extending](docs/EXTENDING.md) rather than storage or writer internals.

## Preserve the data contracts

- Bind feature writes to the block transaction. Preserve rollback, cursor and outbox semantics on success, error, restart and reorg.
- Keep deterministic handlers independent of wall clocks, randomness and unbound external state. Use the [determinism checks](docs/EXTENDING.md#6-결정성-규약) (`pkg/feature/featuretest`).
- Keep canonical checks immediately before commit, with the existing generation/epoch fences. Physical RPC attempts, including retries and batch fallback, share the configured budget.
- Keep PostgreSQL values parameter-bound and dynamic identifiers validated and quoted. Capture/projection ACKs require durable database commits.
- Preserve raw retention, finality, lease and ACK gates. Missing data or failed validation must not become successful completion.
- Document any change to stored formats, profile behavior, coverage, defaults or public API compatibility. Update examples and snapshots only when the intended contract changes.

## Run the checks

Run checks for the changed code while developing. The default PR checks are short:

```bash
go build ./...
go vet ./...
make test-ci-short
make test-ci-postgres-short
make test-examples
```

Documentation-only changes need link/anchor and CI policy checks, with no Go, Rust or PostgreSQL startup. PRs run one short functional pass and a durable core DB smoke; they do not run stress200 or the full native qualification. See [CI tiers](docs/CI.md). Format Go files before submitting. Run the linter with the pinned toolchain:

```bash
GOTOOLCHAIN=go1.26.8 make lint
```

The `execwire`, `x/stablenet/executor` and `tools/astgraph` directories are separate Go modules. Code PRs check their vet/lint contracts; `test-ci-short` includes root, wire and the StableNet executor. Examples are separate modules too. Executor tests use EVM/WBFT libraries and start no node services.

New concurrency logic requires targeted local race checks, including one `GOMAXPROCS=1 -race -count=200` invocation of its new cases. Partial counts are not equivalent. Run broader local regression when storage/native/lifecycle changes or failures justify it; documentation and workflow-only edits do not require a full runtime race rerun. Wait for the state a test checks instead of relying on fixed sleeps.

The explicitly requested **full** workflow retains four PostgreSQL state-proof cases in separate sequential steps,
each with `GOMAXPROCS=1`, `-race`, a single `-count=200` and its own20-minute
package timeout. Keep all four cases and those conditions; combining them under
one shared20-minute timer can exhaust the aggregate budget while tests progress.
The offline `verification/test_ci_state_proof_batches.py` checks coverage and flags.

Public-RPC and live-node checks are opt-in. Run workload/SLO checks only when they are relevant to an agreed validation scope. Keep functional success, performance measurements and live operational evidence distinct; report commands, inputs, skips, failures and the limits of the result.

For documentation changes, verify relative links and section anchors, match flags and configuration keys to the code, and check that examples point to real files. Keep dated measurements out of onboarding instructions.

## Submit a pull request

Use a concise English commit subject with a conventional prefix, such as `fix:`, `feat:` or `docs:`. Keep unrelated changes out of the patch.

A useful PR description explains the concrete problem, resulting behavior, compatibility/recovery considerations and validation. Call out incomplete checks and unresolved limitations. Include a reproduction or before/after example when it helps review.

The protected `main` branch requires **lint**, **test**, **postgres** and **examples** to pass. Pull requests merge with **squash** after the applicable short checks pass; do not bypass protection. Each named job explicitly records a documentation-only no-runtime scope. [The CI workflow](.github/workflows/ci.yml) is the source of truth for commands and toolchains.

The **arc** job compiles and runs bounded protocol smoke only when Arc Rust changes; Go/docs changes do not rebuild the native SDK. Full SDK/process-kill/COW qualification is available through manual `workflow_dispatch` with `full=true`, and is run locally when native changes require it. Do not treat native library fixtures as live peer, native data-directory or performance validation.

## Update documentation in the right place

- [README](README.md): project fit, first successful run, developer entry points and support.
- [Documentation index](docs/README.md): task-based navigation to maintained guides.
- Guides: current configuration, interfaces, examples and implementation contracts.
- [Remaining work](docs/analysis/open-items.md): open requirements, dependencies and deferred work.
- [Analysis and validation](docs/analysis/README.md): dated decisions, failures, measurements and their scope.

Preserve historical evidence when correcting a current guide. If a link or example becomes obsolete, update its callers along with it.

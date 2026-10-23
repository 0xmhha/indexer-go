# Documentation

Start with the task you want to complete. The quickstart and contribution guide are in English; most detailed references are in Korean. Examples include their own English READMEs and runnable Go modules.

## Get started

| Task | Guide |
| --- | --- |
| Understand the project and choose an entry point | [Project README](../README.md) |
| Build, run a local node and query the index | [Quickstart](QUICKSTART.md) |
| Configure features, RPC access, databases or roles | [Configuration](CONFIG.md) |
| Learn the components and data flow | [Architecture](ARCHITECTURE.md) |
| Develop and submit a change | [Contributing](../CONTRIBUTING.md) |

## Query indexed data

| Task | Reference |
| --- | --- |
| Use GraphQL, JSON-RPC, REST or subscriptions | [API reference](API.md) |
| Inspect the schema and registered method names | [GraphQL snapshot](../pkg/app/testdata/api/graphql-schema.txt), [JSON-RPC snapshot](../pkg/app/testdata/api/jsonrpc-methods.txt) |
| Query captured source blocks, transactions, addresses or logs | [Source lookup and search](SOURCE_QUERIES.md) |
| Read projection completion, gaps and independent worker output | [Projection progress](PROJECTION_PROGRESS.md) |
| Query tracked account and storage changes | [Tracked-state projections](TRACKED_STATE_PROJECTION.md) |

Snapshots describe a test configuration. Your runtime schema and data depend on the linked profiles, enabled features, mode and indexed range. Use runtime introspection when integrating a client.

## Extend the framework

| Goal | Reference or example |
| --- | --- |
| Add a feature, derived records or custom GraphQL/HTTP routes | [Extending](EXTENDING.md) |
| Collect selected contract events with a project API | [receipts example](../examples/receipts/README.md), [declared records configuration](CONFIG.md#선언형-수집-featuresrecords-indexermode) |
| Implement chain-specific decoding, verification or accounting | [Chain customization](CHAIN_CUSTOMIZATION.md) |
| Register an external profile in your own binary | [chainprofile example](../examples/chainprofile/README.md) |

No package carries a compatibility promise; pin a version and run your extension's checks when upgrading.

## Configure and operate

- [Configuration](CONFIG.md) covers RPC budgets, PostgreSQL, ingest/API roles, multi-chain routing, notifications and data management.
- [Testnet operations draft](OPERATIONS_DRAFT.md) separates the proposed APFS/PostgreSQL/Arc feed settings from unresolved storage, peer and checkpoint inputs; it is not a runnable deployment.
- [Docker operations draft](../deployments/testnets/README.md) adds isolated Compose wiring, APFS identity checks and file-based DSN injection; service startup remains on hold.
- [Capacity, backlog and recovery policy draft](OPERATIONS_CAPACITY_DRAFT.md) proposes DB growth monitoring and stop/resume criteria; it does not install automatic monitoring or controls.
- [Backup, migration and recovery objectives draft](OPERATIONS_BACKUP_DRAFT.md) classifies what cannot be rebuilt, proposes RPO/RTO per class, continuous WAL archiving plus stopped-state copies, and migration/restore checks; nothing is configured or run.
- [Checkpoint, restart and GC runbook draft](OPERATIONS_RUNBOOK_DRAFT.md) covers normal shutdown, incident recovery and deletion prerequisites; execution and GC activation remain on hold.
- [Checkpoint and GC inspection](OPERATIONS_INSPECTION.md) documents the separate read-only metadata command, bounded observations and offline functional checks; it never starts the app or performs recovery.
- [Offline draft consistency review](analysis/operations-offline-review.md) records corrected cross-file mismatches and the remaining operational gaps; it is not live deployment validation.
- [Architecture](ARCHITECTURE.md) explains block transactions, reorg recovery and outbox delivery.
- [Pipeline telemetry](FEED_TELEMETRY.md) defines feed-stage latency, backlog and unavailable observations.
- [Remaining work](analysis/open-items.md) separates completed checks, unresolved operational requirements and deferred work.
- [Handover 10/23](analysis/handoff-2026-10-23.md) records the move to execution-based ingest, the approved E1 design ([decisions](decisions/20261023-e1-design/decision.md), [design](codemine/runs/20261023-e1-execution/02-design.md)) and the next step (E1 plan).

## Use the opt-in Arc feed

The implemented base plan receives and verifies headers, bodies and receipts. Its local tests do not establish live peer access, sustained Arc throughput or external-disk reliability. Start with the application guide before selecting storage or optional workers.

| Layer | Guide |
| --- | --- |
| Configuration, application lifecycle and mixed RPC/feed mode | [Feed application](FEED_APP.md) |
| Arc protocol module and supported input plan | [Arc feed module](../x/arc/feed/README.md) |
| Raw disk window, external paths, leases and quotas | [Window storage](WINDOW_STORAGE.md) |
| Verified source transactions, generations and deletion fences | [Capture storage](CAPTURE_STORAGE.md) |
| Permanent objects and normalized source records | [Permanent source storage](PERMANENT_SOURCE_STORAGE.md) |
| Finality policy, reorg recovery, ACK and garbage collection | [Feed runtime](FEED_RUNTIME.md) |
| DB-only inputs for the existing ordered writer | [Source facts](SOURCE_FACTS.md) |
| Independent worker completion and output APIs | [Projection progress](PROJECTION_PROGRESS.md) |

### Optional data and workers

- [State proofs](STATE_PROOFS.md): explicitly selected accounts, code and storage; parent checkpoints and completion gates.
- [Tracked-state projections](TRACKED_STATE_PROJECTION.md): current values, per-block changes and independent undo/events.
- [Call traces](CALL_TRACES.md): provider-reported call trees, validation boundaries and DB-only pages.
- [Blob inputs](BLOB_INPUTS.md): full-blob/KZG verification and the current Arc admission policy. This library does not provide a configured blob transport.
- [Source lookup and search](SOURCE_QUERIES.md): independent indexes and epoch-pinned pages.
- [Pipeline telemetry](FEED_TELEMETRY.md): stage timing and backlog interpretation.

These options have separate supply and completion contracts. Enabling one does not imply complete EVM state, independently proven call execution or additional raw-deletion authority.

## Design, validation and migration

Use the [analysis index](analysis/README.md) for design documents and dated evidence. It distinguishes implementation contracts, proposed work, historical measurements and the earlier ExEx comparison.

[Frontend migration](FRONTEND_MIGRATION.md) is a guide for consumers moving their GraphQL documents or REST calls. Changes to a separate frontend repository are outside this repository's implementation.

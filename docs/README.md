# Documentation

현재 배포 판정과 운영자 입력은 [운영 준비 현황](OPERATIONS_READINESS.md)을 따른다. 배포는 HOLD이며 legacy 초안과 native 실행 경로를 구분한다.

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
| Evaluate saved diagnostic/capacity/backlog observations | [Offline Native monitoring](NATIVE_OFFLINE_MONITORING.md) |
| Authenticate external PostgreSQL backup evidence | [Offline backup adapter](EXECUTION_BACKUP_ADAPTER.md) |
| Review diagnostic-only Docker image/profile wiring | [Native Docker draft](../deployments/native/README.md) |
| Build a new Arc native DB offline from an authenticated full checkpoint | [Arc native bootstrap](EXECUTION_ARC_BOOTSTRAP.md) |
| Review the owned Arc first-execution native writer and crash fences | [Arc native writer](EXECUTION_ARC_NATIVE_WRITER.md) |
| Configure opt-in App-owned raw/native intake and its safety boundaries | [Arc ingress](EXECUTION_ARC_INGRESS.md) |
| Run focused PR checks or request full qualification | [CI tiers](CI.md) |
| Understand managed read views, native epochs and E1 reorg recovery | [Managed Arc replay](EXECUTION_ARC_MANAGED.md) |
| Review Arc first-import design, N0–N3 and remaining N4–N6 | [Arc native import design](ARC_NATIVE_IMPORT_DESIGN.md) |
| Review Native App/collector/executor topology and held lifecycle | [Native runtime composition draft](../deployments/native/runtime/README.md) |
| Diagnose native/source metadata and exported checkpoint/retention observations | [Native read-only diagnostics](NATIVE_EXECUTION_DIAGNOSTICS.md) |
| Check native App/collector/checkpoint/retention files offline | [Native execution preflight](NATIVE_EXECUTION_PREFLIGHT.md) |
| Read native replay call trees and internal calls | [Native execution traces](EXECUTION_CALL_TRACES.md) |
| Full blobs in execution ingestion | [Bounded Engine V1 supply, private retained input, KZG and DB-only pages](EXECUTION_BLOBS.md) |
| Retain selected account/storage proofs in execution inputs | [Execution state proofs](EXECUTION_STATE_PROOFS.md) |

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
- [Capacity, backlog and recovery policy draft](OPERATIONS_CAPACITY_DRAFT.md) documents implemented filesystem/PGDATA/raw monitoring and halt/resume controls, plus the remaining live operational checks.
- [Backup, migration and recovery objectives draft](OPERATIONS_BACKUP_DRAFT.md) classifies what cannot be rebuilt, proposes RPO/RTO per class, continuous WAL archiving plus stopped-state copies, and migration/restore checks; nothing is configured or run.
- [Checkpoint, restart and GC runbook draft](OPERATIONS_RUNBOOK_DRAFT.md) covers normal shutdown, incident recovery and deletion prerequisites; execution and GC activation remain on hold.
- [Checkpoint and GC inspection](OPERATIONS_INSPECTION.md) documents the separate read-only metadata command, bounded observations and offline functional checks; it never starts the app or performs recovery.
- [Offline draft consistency review](analysis/operations-offline-review.md) records corrected cross-file mismatches and the remaining operational gaps; it is not live deployment validation.
- [Architecture](ARCHITECTURE.md) explains block transactions, reorg recovery and outbox delivery.
- [Pipeline telemetry](FEED_TELEMETRY.md) defines feed-stage latency, backlog and unavailable observations.
- [Remaining work](analysis/open-items.md) separates completed checks, unresolved operational requirements and deferred work.
- [Handover 10/23](analysis/handoff-2026-10-23.md) points to the current [E1 plan](codemine/runs/20261023-e1-execution/03-plan.md), [checklist](codemine/runs/20261023-e1-execution/checklist.md) and [continuity](codemine/runs/20261023-e1-execution/continuity.json).

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

[StableNet execution ingest](EXECUTION_INGEST.md) documents the opt-in application and isolated official-client executor, prepared baseline requirements, seven queues on pool4, public/fresh/ACK boundaries and recovery. The [wire module](../execwire/README.md) and [executor](../x/stablenet/executor/README.md) have independent Go modules. [Arc execution journals](EXECUTION_ARC.md) adds the pinned official Arc EVM, read-only native replay and continuous source-spool/ACK/restart/reorg supervision, with a retained one-file alternative; Arc native checkpoint export and retention remain separate. [Offline bootstrap](EXECUTION_BOOTSTRAP.md) covers full genesis/checkpoint state, selected proofs, atomic new-schema import and read-only recovery diagnosis. [Offline retention](EXECUTION_RETENTION.md) covers supervised ACK/window-based node and quarantine collection, crash recovery and capacity gates. [Execution feature writers](EXECUTION_FEATURE_WRITERS.md) covers per-unit publication and native journal balance/history/cursor compatibility. The [E1 checklist](codemine/runs/20261023-e1-execution/checklist.md), [E2 checklist](codemine/runs/20261024-e2-bootstrap/checklist.md), [E3 checklist](codemine/runs/20261024-e3-retention/checklist.md), [E4 checklist](codemine/runs/20261024-e4-feature-writers/checklist.md) [E5 replay checklist](codemine/runs/20261024-e5-arc-execution/checklist.md) and [Arc collector checklist](codemine/runs/20261025-e5-arc-collector/checklist.md) record functional verification and CI; live node, deployment and performance validation remain on hold.

Use the [analysis index](analysis/README.md) for design documents and dated evidence. It distinguishes implementation contracts, proposed work, historical measurements and the earlier ExEx comparison.

[Frontend migration](FRONTEND_MIGRATION.md) is a guide for consumers moving their GraphQL documents or REST calls. Changes to a separate frontend repository are outside this repository's implementation.

[Arc native checkpoints and retention](EXECUTION_ARC_RETENTION.md) explains full native export/key coverage, private snapshots, managed ownership and E3 copy/swap/recovery without node startup.

- [Independent WBFT consensus](EXECUTION_CONSENSUS.md): explicit trust policy, opt-in execution publication, finality/recovery and offline fixtures.

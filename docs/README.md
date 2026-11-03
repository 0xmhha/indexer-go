# Documentation

Choose the task you want to complete. Start with the [project README](../README.md) for project fit and a first run; use [remaining work](analysis/open-items.md) for priorities and [operating readiness](OPERATIONS_READINESS.md) for deployment inputs. Detailed references are primarily in Korean.

## Document roles

| Location | Owns |
| --- | --- |
| Root README | Current product purpose, supported paths, quickstart and developer entry points |
| This index | Navigation by task; one primary category per root guide |
| `analysis/open-items.md` | Outstanding tasks, dependencies and release/input/run gates |
| `SESSION_STATE.md` | Replaceable current continuation state and recent functional contexts |
| Maintained guides | Current code/config/API behavior and usage boundaries |
| Accepted designs and validation | Explicit contracts and bounded evidence; not deployment approval |

Keep guides current by replacing superseded statements. Do not append PR timelines, session diaries or old performance results to README/usage guides. Use Git history for superseded narrative; retain required accepted plans and scoped validation evidence through the design index.

## Get started

| Task | Guide |
| --- | --- |
| Build, run a local EVM chain and query the index | [Quickstart](QUICKSTART.md) |
| Configure features, RPC, storage, roles and multi-chain routes | [Configuration Guide](CONFIG.md) |
| Understand components, publication and recovery boundaries | [Architecture](ARCHITECTURE.md) |
| Add Go handlers, derived records and APIs | [기능과 API 확장](EXTENDING.md) |
| Implement chain profiles and custom accounting | [EVM 체인 커스터마이징 계약](CHAIN_CUSTOMIZATION.md) |

## Query indexed data

| Task | Guide |
| --- | --- |
| GraphQL, JSON-RPC, REST, subscriptions and pagination | [API 안내](API.md) |
| Current UserOperation, bundler and paymaster queries | [Account abstraction queries](ACCOUNT_ABSTRACTION.md) |
| Permanent source lookup and search | [영구 원천 데이터 검색](SOURCE_QUERIES.md) |
| Worker completion, gaps and output APIs | [기능별 완료·독립 worker·coverage API](PROJECTION_PROGRESS.md) |
| Tracked values, changes and undo | [지정 상태 후처리 worker](TRACKED_STATE_PROJECTION.md) |
| Provider-reported capture traces and DB pages | [호출 trace 수집과 DB 조회](CALL_TRACES.md) |
| Selected capture proofs and their completion gates | [지정 계정·code·storage 증명 공급](STATE_PROOFS.md) |
| Capture full-blob verification and profile admission | [전체 blob 공급·검증 모듈](BLOB_INPUTS.md) |

## Ingest and store source data

| Task | Guide |
| --- | --- |
| Opt-in feed application and mixed RPC/feed mode | [앱의 window feed 실행](FEED_APP.md) |
| Feed finality, reorg, ACK and collection | [Feed finality·reorg·GC 런타임](FEED_RUNTIME.md) |
| Raw disk window, external paths, leases and quotas | [원본 디스크 윈도우 저장소](WINDOW_STORAGE.md) |
| Verified source transactions, generations and deletion fences | [원천 데이터 저장과 삭제 승인](CAPTURE_STORAGE.md) |
| Permanent objects and normalized records | [영구 객체와 정규화 원천 저장](PERMANENT_SOURCE_STORAGE.md) |
| DB-backed input for the existing ordered writer | [원천 DB를 이용한 기존 writer 후처리](SOURCE_FACTS.md) |
| Stage timings and backlog interpretation | [Feed 단계별 지연과 운영 판단](FEED_TELEMETRY.md) |
| Optional node-feed/ExEx IPC contract | [Node-feed 연결](NODE_FEED.md) |

## Native execution contracts

| Task | Guide |
| --- | --- |
| Prepared PostgreSQL execution input and publication | [StableNet execution ingest](EXECUTION_INGEST.md) |
| Full/partial checkpoint preparation and import | [Execution checkpoint preparation](EXECUTION_BOOTSTRAP.md) |
| Logical writers, public visibility and native balance authority | [Execution feature writers](EXECUTION_FEATURE_WRITERS.md) |
| Supervised retention and recovery fences | [Offline execution retention](EXECUTION_RETENTION.md) |
| Replay call trees and internal-call pages | [Native replay call traces and internal calls](EXECUTION_CALL_TRACES.md) |
| Bounded execution full blobs, KZG and Engine supply | [Full blobs in execution ingestion](EXECUTION_BLOBS.md) |
| Selected account/code/storage evidence and DB queries | [Selected state inclusion proofs in execution ingestion](EXECUTION_STATE_PROOFS.md) |
| Authenticated bounded StableNet WBFT policy | [Independent execution consensus](EXECUTION_CONSENSUS.md) |

## Arc native lifecycle

| Task | Guide |
| --- | --- |
| Pinned Arc replay and continuous collector | [Arc execution journals](EXECUTION_ARC.md) |
| Authenticated full-state native bootstrap | [Arc native full-state bootstrap](EXECUTION_ARC_BOOTSTRAP.md) |
| First execution, native publication and crash fences | [Arc first-execution native writer](EXECUTION_ARC_NATIVE_WRITER.md) |
| App-owned ETH68/raw/import/replay composition | [App-owned Arc native ingress](EXECUTION_ARC_INGRESS.md) |
| Managed read views, native epochs and reorg recovery | [Managed Arc replay and reorg recovery](EXECUTION_ARC_MANAGED.md) |
| Source ownership and original input archive | [Execution ingress ownership and input archive](EXECUTION_INGRESS_ARCHIVE.md) |
| Park/resume workers before explicit maintenance | [Execution worker drain](EXECUTION_DRAIN.md) |
| Managed copy-on-write maintenance | [Managed Arc native maintenance bridge](EXECUTION_MANAGED_MAINTENANCE.md) |
| Maintenance coordination and phase recovery | [Explicit Arc execution maintenance](EXECUTION_MAINTENANCE_COORDINATOR.md) |
| Raw deletion authority and recovery | [Managed execution raw retention](EXECUTION_RAW_RETENTION.md) |
| Native full export, lookup reclamation and matched backup admission | [Arc native checkpoints and retention](EXECUTION_ARC_RETENTION.md) |
| Read-only diagnosis of managed metadata | [Managed Arc execution inspection](EXECUTION_MANAGED_INSPECTION.md) |
| Owned integrated acceptance scope and reproduction | [Arc native integrated acceptance](EXECUTION_ARC_ACCEPTANCE.md) |
| Accepted native import and ownership design | [Arc 첫 import와 native DB 갱신 설계](ARC_NATIVE_IMPORT_DESIGN.md) |

## Operate and observe

| Task | Guide |
| --- | --- |
| Current operating inputs, recommendations and HOLD | [운영 설정 준비 현황](OPERATIONS_READINESS.md) |
| Testnet storage and deployment configuration draft | [네 테스트넷 운영 설정 초안](OPERATIONS_DRAFT.md) |
| Capacity, backlog and halt/resume policy | [용량·backlog·복구 정책 초안](OPERATIONS_CAPACITY_DRAFT.md) |
| Backup custody, WAL and recovery objectives | [백업·이전과 복구 목표 초안](OPERATIONS_BACKUP_DRAFT.md) |
| Shutdown, checkpoint, restart and GC prerequisites | [Checkpoint·재시작·GC 운영 절차 초안](OPERATIONS_RUNBOOK_DRAFT.md) |
| Read-only checkpoint/GC inspection | [Checkpoint and GC inspection](OPERATIONS_INSPECTION.md) |
| Native/source and exported checkpoint diagnostics | [Native read-only diagnostics](NATIVE_EXECUTION_DIAGNOSTICS.md) |
| Offline settings admission: PASS/HOLD/FAIL | [Native execution file preflight](NATIVE_EXECUTION_PREFLIGHT.md) |
| Evaluate supplied diagnostics, metrics and growth snapshots | [Offline Native monitoring](NATIVE_OFFLINE_MONITORING.md) |
| Opt-in DB/WAL observation, alert state and source boundaries | [Native 관측·경보 연동 설계](NATIVE_MONITORING_INTEGRATION.md) |
| Injected VM probe lifecycle and separate observer health | [Docker VM 관측 라이브러리와 observer health](NATIVE_VM_OBSERVATION.md) |
| Authenticate external PostgreSQL backup evidence offline | [Offline PostgreSQL backup attestation adapter](EXECUTION_BACKUP_ADAPTER.md) |

## Contribute and continue

| Task | Guide |
| --- | --- |
| Scoped short checks and explicit full qualification | [CI checks for developers](CI.md) |
| Integrate a client with the current backend contract | [Frontend integration](FRONTEND_MIGRATION.md) |
| Current task and previous functional contexts | [현재 작업과 다음 세션 시작점](SESSION_STATE.md) |
| Resume/update protocol across sessions | [세션 재개와 인계 규약](SESSION_HANDOFF.md) |

## Designs and validation

- [Design index](analysis/README.md): accepted architecture contracts and evidence entry points.
- [E1 execution plan](codemine/runs/20261023-e1-execution/03-plan.md) and [Arc first-import plan](codemine/runs/20261029-arc-first-import/plan.md): preserved accepted designs.
- [N6 acceptance](codemine/runs/20261031-arc-native-n6/validation.md): integrated offline verification scope.
- [Code graph](analysis/graph/README.md): a source-identified analysis artifact, not a current runtime inventory.

Use runtime introspection and the indexed range/collection status when integrating a client. The [schema snapshots](../pkg/app/testdata/api/) describe their fixture configuration. Native replay, proofs, traces, blobs and diagnostics have distinct supply/verification contracts.

Docker compositions remain documented drafts: [testnets](../deployments/testnets/README.md), [native diagnostics](../deployments/native/README.md) and [native runtime](../deployments/native/runtime/README.md). Mock/library checks do not establish live peer access, physical storage reliability or operating readiness.

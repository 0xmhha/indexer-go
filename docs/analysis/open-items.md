# 남은 작업과 우선순위

E1 #214~E4 #217 및 기존 E5/native calls #223·blobs #224·selected proofs #225·Arc proofs #226을 보존한다. 네 순차 기능은 [Engine blob #227](https://github.com/0xmhha/indexer-go/pull/227), [call ranges/orphan #228](https://github.com/0xmhha/indexer-go/pull/228), [E6 WBFT #229](https://github.com/0xmhha/indexer-go/pull/229), [native retention/backup #230](https://github.com/0xmhha/indexer-go/pull/230)으로 모두 exact-head CI5·squash·깨끗한 main 동기화를 마쳤다. [운영 준비 현황](../OPERATIONS_READINESS.md)에 검증된 인계와 미확정 운영 입력을 반영했다. KZG는 beacon/PeerDAS/독립 합의가 아니며 E6 지원은 명시적 trust/fork 범위에 한정된다.

## 현재 정본과 연속성

- n: N4 [계획](../codemine/runs/20261030-arc-native-n4/plan.md), [체크리스트](../codemine/runs/20261030-arc-native-n4/checklist.md). managed replay·E1 reorg 통합 #245 릴리스 완료. CI short #246 릴리스 완료; main 중복 검사·Python 캐시 정리를 마친 뒤 N5 진행. N5~N6 미완료
- n−1: N3 #244 [계획](../codemine/runs/20261030-arc-native-n3/plan.md), [App ingress](../EXECUTION_ARC_INGRESS.md)
- n−2: N2 #243 [계획](../codemine/runs/20261029-arc-native-n2/plan.md), [writer](../EXECUTION_ARC_NATIVE_WRITER.md)
- 수용한 Arc first-import 설계 #239 [정본](../ARC_NATIVE_IMPORT_DESIGN.md), [계획](../codemine/runs/20261029-arc-first-import/plan.md), [체크리스트](../codemine/runs/20261029-arc-first-import/checklist.md), [연속성](../codemine/runs/20261029-arc-first-import/continuity.json)
- 이전 완료: Native 운영 구성 초안 #238 [계획](../codemine/runs/20261029-native-runtime-draft/plan.md), [사용법](../../deployments/native/runtime/README.md)
- 이전 완료: 운영 입력 권장안 #237 [계획](../codemine/runs/20261029-operations-inputs/plan.md), [체크리스트](../codemine/runs/20261029-operations-inputs/checklist.md), [연속성](../codemine/runs/20261029-operations-inputs/continuity.json), [결정표](../OPERATIONS_READINESS.md)
- 이전 완료: 오프라인 Native 모니터링 #236 [계획](../codemine/runs/20261028-offline-monitoring/plan.md), [체크리스트](../codemine/runs/20261028-offline-monitoring/checklist.md), [연속성](../codemine/runs/20261028-offline-monitoring/continuity.json), [사용법](../NATIVE_OFFLINE_MONITORING.md)
- 이전 완료: 오프라인 백업 어댑터 #235 [계획](../codemine/runs/20261028-backup-adapter/plan.md), [체크리스트](../codemine/runs/20261028-backup-adapter/checklist.md), [연속성](../codemine/runs/20261028-backup-adapter/continuity.json), [사용법](../EXECUTION_BACKUP_ADAPTER.md)
- 이전 완료: Native 진단 Docker 연결 #234 [계획](../codemine/runs/20261028-native-docker/plan.md), [체크리스트](../codemine/runs/20261028-native-docker/checklist.md), [연속성](../codemine/runs/20261028-native-docker/continuity.json), [사용법](../../deployments/native/README.md)
- 이전 완료: Native 읽기 전용 진단 #233 [계획](../codemine/runs/20261028-native-diagnostics/plan.md), [체크리스트](../codemine/runs/20261028-native-diagnostics/checklist.md), [연속성](../codemine/runs/20261028-native-diagnostics/continuity.json), [사용법](../NATIVE_EXECUTION_DIAGNOSTICS.md)
- 이전 완료: Native preflight #232 [계획](../codemine/runs/20261028-native-preflight/plan.md), [체크리스트](../codemine/runs/20261028-native-preflight/checklist.md), [사용법](../NATIVE_EXECUTION_PREFLIGHT.md)
- 이전 완료: native retention #230 [계획](../codemine/runs/20261027-native-retention-backup/plan.md), [체크리스트](../codemine/runs/20261027-native-retention-backup/checklist.md), [사용법](../EXECUTION_ARC_RETENTION.md)
- E6 #229 [계획](../codemine/runs/20261027-e6-independent-consensus/plan.md), [체크리스트](../codemine/runs/20261027-e6-independent-consensus/checklist.md), [사용법](../EXECUTION_CONSENSUS.md)
- 이전 완료: call ranges/orphan traces #228 [계획](../codemine/runs/20261026-e5-call-ranges-orphans/plan.md), [체크리스트](../codemine/runs/20261026-e5-call-ranges-orphans/checklist.md), [검증](../codemine/runs/20261026-e5-call-ranges-orphans/validation.md)
- Arc native proofs [계획](../codemine/runs/20261026-e5-arc-native-proofs/plan.md), [체크리스트](../codemine/runs/20261026-e5-arc-native-proofs/checklist.md), [연속성](../codemine/runs/20261026-e5-arc-native-proofs/continuity.json), [검증](../codemine/runs/20261026-e5-arc-native-proofs/validation.md), [사용법](../EXECUTION_STATE_PROOFS.md#arc-native-acquisition-and-continuous-collection)
- Engine blob supply #227. 수용한 E1~E4 설계는 아래 링크로 보존한다.

- Execution state proofs [계획](../codemine/runs/20261026-e5-execution-state-proofs/plan.md), [체크리스트](../codemine/runs/20261026-e5-execution-state-proofs/checklist.md), [연속성](../codemine/runs/20261026-e5-execution-state-proofs/continuity.json), [검증](../codemine/runs/20261026-e5-execution-state-proofs/validation.md), [사용법](../EXECUTION_STATE_PROOFS.md)

- Execution blobs [계획](../codemine/runs/20261026-e5-execution-blobs/plan.md), [체크리스트](../codemine/runs/20261026-e5-execution-blobs/checklist.md), [연속성](../codemine/runs/20261026-e5-execution-blobs/continuity.json), [검증](../codemine/runs/20261026-e5-execution-blobs/validation.md), [사용법](../EXECUTION_BLOBS.md)

- Native calls [계획](../codemine/runs/20261025-e5-native-call-traces/plan.md), [체크리스트](../codemine/runs/20261025-e5-native-call-traces/checklist.md), [연속성](../codemine/runs/20261025-e5-native-call-traces/continuity.json), [검증](../codemine/runs/20261025-e5-native-call-traces/validation.md), [사용법](../EXECUTION_CALL_TRACES.md)

- Arc history [계획](../codemine/runs/20261025-e5-arc-history/plan.md), [체크리스트](../codemine/runs/20261025-e5-arc-history/checklist.md), [연속성](../codemine/runs/20261025-e5-arc-history/continuity.json), [검증](../codemine/runs/20261025-e5-arc-history/validation.md)

- Arc catalog [계획](../codemine/runs/20261025-e5-arc-checkpoint-catalog/plan.md), [체크리스트](../codemine/runs/20261025-e5-arc-checkpoint-catalog/checklist.md), [연속성](../codemine/runs/20261025-e5-arc-checkpoint-catalog/continuity.json), [검증](../codemine/runs/20261025-e5-arc-checkpoint-catalog/validation.md)

- Arc checkpoint/retention [계획](../codemine/runs/20261025-e5-arc-checkpoint-retention/plan.md), [체크리스트](../codemine/runs/20261025-e5-arc-checkpoint-retention/checklist.md), [연속성](../codemine/runs/20261025-e5-arc-checkpoint-retention/continuity.json), [검증](../codemine/runs/20261025-e5-arc-checkpoint-retention/validation.md), [사용법](../EXECUTION_ARC_RETENTION.md)

- Arc collector [계획](../codemine/runs/20261025-e5-arc-collector/plan.md), [체크리스트](../codemine/runs/20261025-e5-arc-collector/checklist.md), [연속성](../codemine/runs/20261025-e5-arc-collector/continuity.json), [검증](../codemine/runs/20261025-e5-arc-collector/validation.md)
- E5 replay [계획](../codemine/runs/20261024-e5-arc-execution/plan.md), [체크리스트](../codemine/runs/20261024-e5-arc-execution/checklist.md), [연속성](../codemine/runs/20261024-e5-arc-execution/continuity.json), [검증](../codemine/runs/20261024-e5-arc-execution/validation.md), [Arc 실행 수집](../EXECUTION_ARC.md)
- E4 [계획](../codemine/runs/20261024-e4-feature-writers/plan.md), [체크리스트](../codemine/runs/20261024-e4-feature-writers/checklist.md), [연속성](../codemine/runs/20261024-e4-feature-writers/continuity.json), [검증](../codemine/runs/20261024-e4-feature-writers/validation.md), [기능 writer](../EXECUTION_FEATURE_WRITERS.md)

- E3 [계획](../codemine/runs/20261024-e3-retention/plan.md), [체크리스트](../codemine/runs/20261024-e3-retention/checklist.md), [연속성](../codemine/runs/20261024-e3-retention/continuity.json), [기능 검증](../codemine/runs/20261024-e3-retention/validation.md), [사용법](../EXECUTION_RETENTION.md)
- E2 [계획](../codemine/runs/20261024-e2-bootstrap/plan.md), [체크리스트](../codemine/runs/20261024-e2-bootstrap/checklist.md), [연속성](../codemine/runs/20261024-e2-bootstrap/continuity.json), [bootstrap](../EXECUTION_BOOTSTRAP.md)
- E1 [설계](../codemine/runs/20261023-e1-execution/02-design.md), [결정](../decisions/20261023-e1-design/decision.md), [구현 계획](../codemine/runs/20261023-e1-execution/03-plan.md), [실행 수집](../EXECUTION_INGEST.md)

2회 실행과 upstream 미수정, 체인별 기존 pool4/part2, RPC5 physical HTTP/s와 공개 직전 uncached fresh를 유지한다. writer7개는 연결7개를 뜻하지 않는다. E3는 검증된 full baseline, 현재 DB pending permit/live 감독 lease, 원본 ACK/window, node/source intent·완료·release를 함께 검증한다. 일부 checkpoint, 미처리/보호 구간, unknown/corrupt 자료는 삭제 권한으로 전환하지 않는다.

현재 문서는 최신 상태를 유지한다. 과거 판본·수치·종료된 실험은 Git history로 확인하며, 수용한 E1/E2 설계와 현재 n/n-1/n-2는 유지한다. 이미 끝난 API 진단/파싱/compact JSON/keepalive/DB 검증을 새 이유 없이 반복하거나 host-load/SLO 확인 루프를 다시 제안하지 않는다.

## 구현 후속 작업

| 우선순위 | 작업 | 완료 조건·의존성 |
| --- | --- | --- |
| 완료 #234 | Native 진단 Docker 연결 | [별도 진단 image/profile](../../deployments/native/README.md): read-only/network-none/no-DSN 바인딩·오프라인 checker·실제 host CLI fixture. 운영 image/container 실행은 보류. CI5 run40651236922 통과 후 squash/main8fd812c 동기화 완료 |
| 완료 #233 | Native 읽기 전용 진단 | [진단](../NATIVE_EXECUTION_DIAGNOSTICS.md): source/native 유지보수 metadata·checkpoint/ACK/generation/floor/fence·명시적 export 관찰, 운영 권한 없음. exact-head CI5 run40619486115 뒤 squash/main63fafea 동기화 완료 |
| 완료 #232 | Native 실행 설정 통합 검사 | [오프라인 preflight](../NATIVE_EXECUTION_PREFLIGHT.md): production schema·strict 입력·PASS/HOLD/FAIL·운영 권한 없음. 릴리스 증거는 PR metadata와 부모 인계 영수증 참조 |
| 완료 #235 | 오프라인 backup adapter | [백업 영수증 서명·파일 인증](../EXECUTION_BACKUP_ADAPTER.md), pinned key/chain/DB·exact permit·private atomic receipt. 실제 PG 내용·WAL 연속성·복원·운영 권한은 검증하지 않음. CI5 run40667988110 통과 후 squash/main e20635f 동기화 완료 |
| 완료 #236 | 오프라인 monitoring | [일회성 경보 판정](../NATIVE_OFFLINE_MONITORING.md): Arc/StableNet 진단 JSON·선언된 capacity/pipeline·명시적 시각, strict 입력·unknown/stale·redaction·독립 위험 판정. 체인/DB/RPC/watcher/알림·actuation 없음. 최종 CI5 run40704614928 통과 후 squash/main a49065d 동기화 완료 |
| 권장안 정리 완료·실제 값 HOLD | 저장소·배포 입력 | [결정표](../OPERATIONS_READINESS.md): 체인별 분리 경로, PostgreSQL pool4의 전체 예산, Arc 24h/172800블록의 가정과 native/spool 추가 용량, 인증 checkpoint 시작 권장안. 읽기 전용 metadata만 확인. 미확정 값을 template에 채우거나 provisioning하지 않음 |
| 초안 구현·운영 HOLD | Native App/collector/executor 구성 | [별도 runtime 초안](../../deployments/native/runtime/README.md): 두 App과 producer, 외부 PostgreSQL16·DSN_FILE·private IPC·분리 scratch·native lease/RO DB mount·수동 lifecycle. bounded 오프라인 검사와 모의 기능 검증. Arc executor는 collector의 stdio 자식. 실제 image/host/서비스는 검증·실행하지 않음 |
| 설계·N0~N4 릴리스, N5~N6 미완료 | Arc first-import 공급 경로 | [App 내부 Go 감독+Rust 첫 실행 설계](../ARC_NATIVE_IMPORT_DESIGN.md)와 N0~N6 계획 확정. 기존 ETH/68과 공유 RPC5/Store4, 두 번째 collector를 유지. native bootstrap·첫 writer·App ingress는 릴리스했고 N4 managed collector·E1 reorg는 #245로 릴리스했고 CI short #246은 릴리스했고 중복 main 검사·캐시 정리를 우선 마무리한다. N5 영구 raw/GC·SQL owner binding과 N6 통합 acceptance는 미완료다. 전체 노드/새 독립 RPC·DB pool 없음 |
| N0 구현 | bounded import/fence와 pinned SDK qualification | [N0](../codemine/runs/20261029-arc-native-n0/plan.md): AIN1 frame·private durable fence RED→GREEN, 실제 native-v2 Full 저장·별도 RO 갱신·두 번째 replay·H1 full checkpoint·unwind/reimport·controlled 중단/재시작. 운영 writer/실제 실행은 없음 |
| N1 구현 | 인증된 native bootstrap | [전체 상태 검증·새 DB 초기화](../EXECUTION_ARC_BOOTSTRAP.md), original anchor/floor/H+1·authentic header/body/receipts와 atomic no-overwrite publication. 운영 입력 HOLD |
| N2 구현 | 첫 실행 writer | [owned stdio·첫 Arc 실행·native 완료](../EXECUTION_ARC_NATIVE_WRITER.md), 단일 writer/run/publication/read gate, checksum intent/receipt/head·실제 SIGKILL·정확한 duplicate. 관리 legacy 유지보수 거부 |
| N3 #244 릴리스 완료 | N3 App-owned ingress | generic opt-in factory와 Arc bundle 등록, 기존 ETH/68·verifier·공유 RPC5/Store4, bounded durable raw/channel과 child env/cancel/join. 이후 N4 managed collector/reorg→N5 영구 raw/GC→N6 통합 |
| 다음 운영 준비·HOLD | 실제 APFS·배포 값 → Arc 공급자·접속·인증 checkpoint → 실제 backup/restore 검증 | 아래 미확정 조건을 충족한 뒤 별도 운영 범위로 진행. 새 측정·운영 확인 루프를 만들지 않음 |
| 완료 #226 | Arc native selected proof와 collector/window | RED→GREEN, 실제 임시 native-v2/history와 2회 ArcEVM 증거, strict selection·고정 source 정책·capacity/lease/restart/reorg/ACK, PG/API, 전체 로컬 검사·새 CPU1/race/single200, staged/history scan, 정확한 head CI5, squash·깨끗한 main 동기화. source-branch PR을 완료 정본으로 사용 |
| 완료 #227 | 지원 체인의 bounded Engine blob transport | 명시적 peer/beacon/relay·fork/proof 형식과 원격 보존 범위를 확보하고 head 수신·local window·capacity·재시작에 연결. Arc pinned positive admission 미지원 정책을 강제 활성화하지 않음. full-blob 파일 보존/KZG는 beacon SSZ inclusion·PeerDAS/cell 복원을 대신하지 않음 |
| 완료 #228 | Call 조회 확장 | 주소별 범위·coverage 검색, orphan trace 보관·조회, richer GraphQL/JSON-RPC의 별도 계약과 검증. 기존 block/tx 페이지·RPC provider 경로·legacy 미수집을 혼합하지 않음 |
| 완료 #229 | **E6 독립 canonical/finality** | 고정 go-stablenet4d17eea018c8 WBFT BLS quorum·명시적 trust/fork·인증 전환·원자적 finality/복구 구현과 로컬 전체 게이트가 통과했다. 정확한 head `f89109a492fc9991fd8ec0f636d78a937a22ba97`의 CI5 run40377905374, squash/main `f7c0fa26f6e2b62fa2dc50820369ea9a3b9a5818`, 한 부모·reviewed tree 일치와 깨끗한 primary main 동기화·전용 임시 자료 정리가 확인됐다. RPC5/final fresh를 유지하며 기본 경로를 독립 검증으로 이름 붙이지 않음 |
| 완료 #230 | Native 보존 추가 확장 | 실제 v2 lookup/MDBX compact-copy, matched native/source/indexer backup 및 restore/floor admission을 구현하고 real native·PG·RED/GREEN·전체 로컬·CI5 run40418831898 뒤 squash/main 동기화를 검증했다. 외부 PG snapshot/WAL 인증·설치와 운영 활성화는 보류하며 native 전체 고정 크기·측정 절감량을 약속하지 않음 |

N1의 [오프라인 인증 native bootstrap](../EXECUTION_ARC_BOOTSTRAP.md)은 구현됐으며 실제 운영 입력은 HOLD다. N2 writer는 구현했으며 N3 App ingress는 구현했고 N4 managed collector/reorg를 구현/검증하며 N5 영구 raw/GC·SQL owner binding과 N6 통합 acceptance가 남는다.

## 운영·실제 자료 검증 — 현재 보류

| 항목 | 남은 전체 조건 |
| --- | --- |
| 외장 디스크·배포 값 확정 | APFS mount/device identity, 실제 경로·용량·안전 여유, 이미지/체인별 private DSN·자격 증명, 원본/PGDATA/native 경로, 중단·재개 기준 확정. 포맷·provisioning·운영 실행은 하지 않음 |
| DB 연결 예산 | 기존 PostgreSQL 방식과 **체인별·프로세스별 pool4/part2/API 예약** 유지. 여러 ingest/API 프로세스를 함께 운영할 때 서버 전체 연결 합산과 메모리·용량 계획 확정. writer7개를 연결7개로 바꾸지 않음 |
| Arc 공급자·접속·fork·시작점 | 첫 import/P2P·native 갱신 공급 경로, 공식 EL enode/접속 조건, 실제 fork·genesis/network identity·보존 범위, 검증된 checkpoint/anchor와 시작 모드 확정. 공개 자료/오프라인 조사 결과를 실제 peer 연결 성공으로 처리하지 않음 |
| E3 운영 활성화 | 실제 외장 디스크에서 COW 여유, 전원/분리 장애, native layout·backup·실제 노드 보존 범위를 검증한 뒤 명시적 활성화. 현재 DB pending permit/live lease, checkpoint·source exact ACK·삭제 intent/완료/release가 모두 필요. 원본 checkpoint 이전 미수집 payload·필요 headers/indices·현재 state/code/preimages를 보존 |
| 용량·backlog 운영 | 실제 node/raw/PG budget·임계값·중단/재개 기준, DB 증가 감시, worker 지연·watcher 장애 알림, Docker VM 여유와 외장 mount 분리 대응 검증. 이미 구현된 filesystem/capacity guard를 운영 증거로 혼동하지 않음 |
| 일관된 백업·복원 | WAL 보관소, 체인별 상태 보존, matched DB/node/spool/checkpoint backup·restore와 RPO/RTO 검증. 서로 다른 시점의 임의 자료로 ACK/GC 완료를 추정하지 않음 |
| 실제 source conformance | 실제 era1 export/RPC 대조, StableNet/Arc peer·mainnet·실제 retained DB와 fork 경계, 외장 디스크 분리·장기 운전 검증. public RPC의 proof/hash-selector와 실제 blob source 지원·원격 보존은 아직 미확인 |
| 성능 | 합의된 범위에서만 E1/RPC 대비, Arc 수신·writer·ACK/API tail·backlog, E3 copy/정리 비용, macOS/Docker/물리 호스트 비교. **추가 성능 측정은 보류**하며 host-load/SLO 재측정 루프를 재개하지 않음 |
| 별도 frontend 저장소 | GraphQL/REST 및 필요한 새 실행 API 연동 확인. 다른 프로젝트를 이 작업에서 수정·재검증하지 않음. [이전 안내](../FRONTEND_MIGRATION.md) |

## 선택적 확장·별도 결정

- 다른 EVM 체인의 native producer와 custom account/header/fork encoding은 실제 지원 계약·fixture를 확보한 뒤 확대한다. 표준 MPT나 일반 `evm` profile 이름만으로 체인별 실행 conformance를 선언하지 않는다.
- 비EVM 모델은 현재 EVM 중심 범위 밖의 별도 결정이다.
- 선택 proof는 bounded block-end evidence다. 전체 상태/모든 변경 coverage, transaction witness, 임의 `eth_call`, 과거 미수집 backfill이나 새 state projection을 자동 제공하지 않는다. 필요하면 별도 설계한다.

완료된 API 시간 진단·single parse·compact JSON·keepalive·DB/race 검증·문서 정리와 E1~E4를 새 변경/실패 없이 다시 작업 목록에 올리지 않는다. 오래된 선택지·수치는 Git history를 참고하며 수용한 E1 설계와 n/n-1/n-2를 보존한다. 실제 노드/peer, 포맷·운영 GC·추가 성능 측정, 태그·배포·원격 브랜치 삭제는 보류한다. 다른 프로젝트의 프로세스·container·volume·공유 cache는 유지하고 이 작업 전용 cache만 정리한다. 네 기능 순차 릴리스는 완료됐다. 누락된 운영 입력은 HOLD로 유지한다. 현재 Native 진단은 오프라인 metadata/artifact 관찰만 구현한다. 오프라인 일회성 monitoring은 구현됐다. 운영 watcher/수집·DB 증가 추세·hysteresis 통합·알림 연동과 실제 장애 검증은 보류한다. Native runtime 구성은 초안과 오프라인 검사만 추가됐으며 실제 이미지·설정·mount·연결 검증이 아니다. 다음 우선순위는 N0~N4를 구현한 Arc first-import의 N5~N6 순차 통합이다. 미확정 저장소·배포 값, Arc peer·인증 checkpoint, 실제 PG backup/restore 검증은 외부 입력과 별도 실행 권한까지 HOLD다. 백업 어댑터는 외부 발급자 인증과 byte integrity를 제공하며 실제 snapshot/WAL 생성·내용 검증·복원은 보류한다. Native App/collector/executor의 운영 배포는 외부 입력과 실제 host 검증 후 별도 승인 범위다.

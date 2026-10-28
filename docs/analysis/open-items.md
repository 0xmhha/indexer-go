# 남은 작업과 우선순위

E1 #214~E4 #217 및 기존 E5/native calls #223·blobs #224·selected proofs #225·Arc proofs #226을 보존한다. 네 순차 기능은 [Engine blob #227](https://github.com/0xmhha/indexer-go/pull/227), [call ranges/orphan #228](https://github.com/0xmhha/indexer-go/pull/228), [E6 WBFT #229](https://github.com/0xmhha/indexer-go/pull/229), [native retention/backup #230](https://github.com/0xmhha/indexer-go/pull/230)으로 모두 exact-head CI5·squash·깨끗한 main 동기화를 마쳤다. [운영 준비 현황](../OPERATIONS_READINESS.md)에 검증된 인계와 미확정 운영 입력을 반영했다. KZG는 beacon/PeerDAS/독립 합의가 아니며 E6 지원은 명시적 trust/fork 범위에 한정된다.

## 현재 정본과 연속성

- n: Native 읽기 전용 진단 [계획](../codemine/runs/20261028-native-diagnostics/plan.md), [체크리스트](../codemine/runs/20261028-native-diagnostics/checklist.md), [연속성](../codemine/runs/20261028-native-diagnostics/continuity.json), [사용법](../NATIVE_EXECUTION_DIAGNOSTICS.md)
- n−1: Native preflight #232 [계획](../codemine/runs/20261028-native-preflight/plan.md), [체크리스트](../codemine/runs/20261028-native-preflight/checklist.md), [사용법](../NATIVE_EXECUTION_PREFLIGHT.md)
- n−2: native retention #230 [계획](../codemine/runs/20261027-native-retention-backup/plan.md), [체크리스트](../codemine/runs/20261027-native-retention-backup/checklist.md), [사용법](../EXECUTION_ARC_RETENTION.md)
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
| 구현 | Native 읽기 전용 진단 | [진단](../NATIVE_EXECUTION_DIAGNOSTICS.md): source/native 유지보수 metadata·checkpoint/ACK/generation/floor/fence·명시적 export 관찰, 운영 권한 없음. 부모가 로컬 commit 검토 후 CI5/릴리스 진행 |
| 완료 #232 | Native 실행 설정 통합 검사 | [오프라인 preflight](../NATIVE_EXECUTION_PREFLIGHT.md): production schema·strict 입력·PASS/HOLD/FAIL·운영 권한 없음. 릴리스 증거는 PR metadata와 부모 인계 영수증 참조 |
| 다음 offline | Docker wiring → backup adapter → monitoring | 실제 노드/운영 DB/peer/APFS 없이 재현 가능한 기능부터 진행. 미확정 운영 입력은 HOLD 유지 |
| 완료 #226 | Arc native selected proof와 collector/window | RED→GREEN, 실제 임시 native-v2/history와 2회 ArcEVM 증거, strict selection·고정 source 정책·capacity/lease/restart/reorg/ACK, PG/API, 전체 로컬 검사·새 CPU1/race/single200, staged/history scan, 정확한 head CI5, squash·깨끗한 main 동기화. source-branch PR을 완료 정본으로 사용 |
| 완료 #227 | 지원 체인의 bounded Engine blob transport | 명시적 peer/beacon/relay·fork/proof 형식과 원격 보존 범위를 확보하고 head 수신·local window·capacity·재시작에 연결. Arc pinned positive admission 미지원 정책을 강제 활성화하지 않음. full-blob 파일 보존/KZG는 beacon SSZ inclusion·PeerDAS/cell 복원을 대신하지 않음 |
| 완료 #228 | Call 조회 확장 | 주소별 범위·coverage 검색, orphan trace 보관·조회, richer GraphQL/JSON-RPC의 별도 계약과 검증. 기존 block/tx 페이지·RPC provider 경로·legacy 미수집을 혼합하지 않음 |
| 완료 #229 | **E6 독립 canonical/finality** | 고정 go-stablenet4d17eea018c8 WBFT BLS quorum·명시적 trust/fork·인증 전환·원자적 finality/복구 구현과 로컬 전체 게이트가 통과했다. 정확한 head `f89109a492fc9991fd8ec0f636d78a937a22ba97`의 CI5 run40377905374, squash/main `f7c0fa26f6e2b62fa2dc50820369ea9a3b9a5818`, 한 부모·reviewed tree 일치와 깨끗한 primary main 동기화·전용 임시 자료 정리가 확인됐다. RPC5/final fresh를 유지하며 기본 경로를 독립 검증으로 이름 붙이지 않음 |
| 완료 #230 | Native 보존 추가 확장 | 실제 v2 lookup/MDBX compact-copy, matched native/source/indexer backup 및 restore/floor admission을 구현하고 real native·PG·RED/GREEN·전체 로컬·CI5 run40418831898 뒤 squash/main 동기화를 검증했다. 외부 PG snapshot/WAL 인증·설치와 운영 활성화는 보류하며 native 전체 고정 크기·측정 절감량을 약속하지 않음 |

## 운영·실제 자료 검증 — 현재 보류

| 항목 | 남은 전체 조건 |
| --- | --- |
| 외장 디스크·배포 값 확정 | APFS mount/device identity, 실제 경로·용량·안전 여유, 이미지/체인별 private DSN·자격 증명, 원본/PGDATA/native 경로, 중단·재개 기준 확정. 포맷·provisioning·운영 실행은 하지 않음 |
| DB 연결 예산 | 기존 PostgreSQL 방식과 **체인별·프로세스별 pool4/part2/API 예약** 유지. 여러 ingest/API 프로세스를 함께 운영할 때 서버 전체 연결 합산과 메모리·용량 계획 확정. writer7개를 연결7개로 바꾸지 않음 |
| Arc 접속·fork·시작점 | 공식 EL enode/접속 조건, 실제 실행 노드의 fork·genesis/network identity·보존 범위, 검증된 checkpoint/anchor와 시작 모드 확정. 공개 자료/오프라인 조사 결과를 실제 peer 연결 성공으로 처리하지 않음 |
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

완료된 API 시간 진단·single parse·compact JSON·keepalive·DB/race 검증·문서 정리와 E1~E4를 새 변경/실패 없이 다시 작업 목록에 올리지 않는다. 오래된 선택지·수치는 Git history를 참고하며 수용한 E1 설계와 n/n-1/n-2를 보존한다. 실제 노드/peer, 포맷·운영 GC·추가 성능 측정, 태그·배포·원격 브랜치 삭제는 보류한다. 다른 프로젝트의 프로세스·container·volume·공유 cache는 유지하고 이 작업 전용 cache만 정리한다. 네 기능 순차 릴리스는 완료됐다. 누락된 운영 입력은 HOLD로 유지한다. 현재 Native 진단은 오프라인 metadata/artifact 관찰만 구현한다. 다음 offline 우선순위는 Docker wiring, backup adapter와 monitoring이다.

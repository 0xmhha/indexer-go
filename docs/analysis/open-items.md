# 남은 작업과 우선순위

현재 작업의 시작점은 [SESSION_STATE](../SESSION_STATE.md), 인계 방식은
[세션 규약](../SESSION_HANDOFF.md)이다. 이 문서는 **활성 작업 ID·의존성·완료 조건**을
관리한다. 마지막 확인된 완료 릴리스는 MON-02-D #266이며, E1~E6의 수용 범위와
Arc N0~N6는 완료했다. 실제 운영과 선택 확장은 별개다.

## 상태와 선택 규칙

| 상태 | 의미 |
| --- | --- |
| READY | 표시된 범위에서 바로 진행 가능. 운영 실행을 뜻하지 않음 |
| PLAN | 선행 설계·범위를 확정하고 RED/완료 조건을 기록할 작업 |
| HOLD_INPUT | 필요한 외부 입력이 아직 제공·검증되지 않음 |
| HOLD_RUN | 포맷/실행/운영 검증/측정에 대한 기존 보류가 유지됨 |
| DEFERRED | 현재 수용 범위 밖의 선택적 확장. 자동 착수하지 않음 |
| DONE | 완료 요약·정본으로 이동. ID 재사용과 불필요한 재검증 없음 |

[MON-01 설계](../NATIVE_MONITORING_INTEGRATION.md)와 [MON-02](#mon-02)는 완료했다.
[MON-03](#mon-03)의 비운영 구현/로컬 검증 후 actual 릴리스 gates를 확인한다.
완료됐다면 남은 우선순위는 **OPS-01→OPS-02→OPS-03→OPS-04**의 실제 입력 준비다.
현재 입력·실행 HOLD는 해제되지 않았다. 사용자 새 지시가 우선하며,
`다음 진행`만으로 실제 probe/선택 확장이나 완료된 검사 반복을 시작하지 않는다.

## 지금 진행할 수 있는 모니터링 후속 작업

| ID | 상태 | 작업 | 완료 조건·의존성 |
| --- | --- | --- | --- |
| <a id="mon-03"></a>MON-03 | READY / REVIEW | injected VM probe·parser·observer health 릴리스 | [현재 계약](../NATIVE_VM_OBSERVATION.md)·[검증](../codemine/runs/20261103-mon03-vm-observer/validation.md). fake Runner로 bounded parsing/output·single-flight·cancel/actual wait·exact cleanup/pending·health를 구현/검증. actual PR/CI/squash/clean main을 확인하고 완료됐다면 반복하지 않음. 실제 Docker 실행기·App/config activation·durable crash admission은 운영 입력/실행 scope를 확정할 OPS-03/05/06까지 보류. 현재 문서 스냅샷은 미래 머지 완료를 보증하지 않음 |

이미 구현된 [두 스냅샷 DB 증가 추세](../NATIVE_OFFLINE_MONITORING.md#optional-indexer-growth-assessment)는
제출된 값의 조건부 예산 소진 판정이다. 실제 수집·지속 추세·hysteresis·latch·알림
구현이나 운영 권한을 대신하지 않는다. 기존 live opsguard를 다시 구현하지 않는다.

## 운영·실제 자료 검증 — 현재 보류

실제 관측 시점과 필요한 값의 정본은 [운영 준비 현황](../OPERATIONS_READINESS.md)이다.
선택 경로의 부재를 머신 전체 부재로 확대하지 않고, 과거 head·fixture·placeholder를
운영값으로 채우지 않는다. 아래 항목은 문서나 오프라인 PASS만으로 완료되지 않는다.

| ID | 상태 | 작업 | 완료 조건·의존성 |
| --- | --- | --- | --- |
| <a id="ops-01"></a>OPS-01 | HOLD_INPUT | 전용 APFS·경로·용량 확정 | 실제 mount/VolumeUUID/physical-store/ownership, native/raw/source/scratch/checkpoint/PGDATA/WAL 별 예산·안전 여유·COW headroom. 포맷·데이터 이동은 별도 HOLD_RUN |
| <a id="ops-02"></a>OPS-02 | HOLD_INPUT | 공급자·접속·인증 시작점 | Arc 공식 EL enode/접속 조건·fork/genesis/network·history 보존, 인증 full checkpoint H/hash/root/coverage와 H+1. StableNet E6는 지원 trust/fork/validator anchor 인증, 나머지 RPC 체인은 실제 확정 시작점·상태 보존 계약 확인 |
| <a id="ops-03"></a>OPS-03 | HOLD_INPUT | 비공개 배포·DB 준비 | [OPS-01](#ops-01)·[OPS-02](#ops-02)에 맞춘 reviewed image digest·지원 schema의 선언·private DSN/key 경로·UID/IPC/mount·준비된 PG schema/역할. 체인·프로세스별 pool4와 서버 전체 연결/메모리 예산. 비밀 값은 Git/대화에 넣지 않음 |
| <a id="ops-04"></a>OPS-04 | HOLD_INPUT | 일관된 백업·WAL·복구 증거 | 준비된 저장소/DB와 일치하는 native/source/indexer backup·WAL 경계·독립 인증·별도 보관 장치·RPO/RTO. [백업 admission](../EXECUTION_ARC_RETENTION.md)·[서명 어댑터](../EXECUTION_BACKUP_ADAPTER.md)를 실제 snapshot 내용·WAL 연속성·복원 성공으로 간주하지 않음. 실제 복원은 HOLD_RUN |
| <a id="ops-05"></a>OPS-05 | HOLD_RUN | 실제 배포·source conformance·장기 운전 | OPS-01~04 입력과 별도로 허용된 실행 범위 필요. 실제 peer/fork/retained DB·era1/RPC 대조·proof/hash-selector/blob 원격 보존을 검증. 같은 schema에 legacy/native writer를 겹치지 않음. E3 활성화는 현재 permit/live lease/source exact ACK/full baseline·raw/native/source intent/completion/release 모두 유지 |
| <a id="ops-06"></a>OPS-06 | HOLD_RUN | 운영 용량/backlog·장애/복구 검증 | 실제 예산/임계값·DB 증가/worker/ACK 지연·관측 실패·디스크 분리/전원 장애·재시작과 COW 여유 검증. [MON-02](#mon-02)·[MON-03](#mon-03)의 모의 통과와 실제 운영 증거를 구분. pending/corrupt를 TTL/latest/zero로 복구하지 않음 |
| <a id="perf-01"></a>PERF-01 | HOLD_RUN | 추가 성능·호스트 비교 | 새로 합의한 범위에서만 수집/writer/ACK/API tail/backlog·E3 copy 비용·macOS/Docker/호스트 비교. 현재 추가 성능 측정 보류를 유지하고 host-load/SLO 재측정 루프를 만들지 않음 |

DB 방식은 PostgreSQL 운영 초안이며 pool4/part2/API 예약1은 체인·프로세스 Store 전체
예산이다. 일곱 writer는 일곱 연결이 아니다. 실행·배포 전 서버 전체 프로세스/관리
연결 합계를 계산하며, 오프라인 확장을 이유로 새 pool이나 RPC 한도를 추가하지 않는다.

## 선택적 확장·별도 결정

| ID | 상태 | 작업·경계 |
| --- | --- | --- |
| <a id="ext-01"></a>EXT-01 | DEFERRED | 다른 EVM 체인의 native producer·custom account/header/fork encoding. 실제 지원 계약/fixture가 필요하며 일반 evm/MPT만으로 conformance를 선언하지 않음 |
| <a id="ext-02"></a>EXT-02 | DEFERRED | 전체 상태/변경 coverage·transaction witness·임의 eth_call·과거 미수집 backfill/새 projection. 현재 selected block-end proof와 별도 설계 |
| <a id="ext-03"></a>EXT-03 | DEFERRED | 별도 frontend 저장소의 신규 실행 API 연동. [기존 안내](../FRONTEND_MIGRATION.md)를 참고하며 이번 저장소 작업으로 다른 프로젝트를 수정하지 않음 |
| <a id="ext-04"></a>EXT-04 | DEFERRED | 비EVM 모델. 현재 EVM 중심 수용 범위 밖 |

## 완료된 구현과 보존할 정본

완료된 기능을 다음 작업으로 다시 나열하지 않는다. 최근 세 맥락의 실제 PR/CI와
source/main 식별자는 [SESSION_STATE](../SESSION_STATE.md)에서 관리한다.

<a id="mon-02"></a>**MON-02 · DONE — A~D 수용 범위 완료.** #263/#264/#265/#266의
[관측 모델·owner tap·경보/checkpoint·기본 비활성 DB/WAL App 통합](../NATIVE_MONITORING_INTEGRATION.md),
[D 검증](../codemine/runs/20261102-mon02d-app-assembly/validation.md).
미확정 native/derived epoch·backlog/lag·growth forecast producer나 실제 sender를
추가한 것은 아니다. 운영 수집/경보·stop/resume 확대는 별도 OPS 범위다.

<a id="mon-01"></a>**MON-01 · DONE — 설계 완료.** [정본](../NATIVE_MONITORING_INTEGRATION.md),
[계획/체크리스트](../codemine/runs/20261102-mon01-observation-design/plan.md),
[후속 구현/RED 계획](../codemine/runs/20261102-mon01-observation-design/implementation.md).
리소스·시각·epoch·예산·reset/unknown·cancel/join·경보/sink 경계를 확정했다.
현재 문서 릴리스는 실제 PR/CI로 확인하며 runtime 구현/실행 완료를 뜻하지 않는다.

| 완료 범위 | 정본·한계 |
| --- | --- |
| E1~E4 | [E1 설계/계획](../codemine/runs/20261023-e1-execution/03-plan.md), [E2 bootstrap](../EXECUTION_BOOTSTRAP.md), [E3 retention](../EXECUTION_RETENTION.md), [E4 writer](../EXECUTION_FEATURE_WRITERS.md). 두 EVM 실행과 upstream 미수정 유지 |
| E5·E6 수용 범위 | [Arc 실행](../EXECUTION_ARC.md), [call/internal ranges·orphans](../EXECUTION_CALL_TRACES.md), [blob 공급/KZG](../EXECUTION_BLOBS.md), [selected proof](../EXECUTION_STATE_PROOFS.md), [지원 WBFT](../EXECUTION_CONSENSUS.md). KZG는 beacon/PeerDAS 합의가 아니며 E6는 인증된 trust/fork 범위만 지원 |
| Arc N0~N6 | [first-import 설계](../ARC_NATIVE_IMPORT_DESIGN.md)·[계획](../codemine/runs/20261029-arc-first-import/plan.md), [N5](../codemine/runs/20261030-arc-native-n5/plan.md), [N6 acceptance](../EXECUTION_ARC_ACCEPTANCE.md). bootstrap/writer/App/managed replay·reorg/SQL archive/raw GC/drain/COW/coordinator/recovery·통합 기능 검증 완료. 운영 입력/실행은 별개 |
| Native 진단·운영 초안 | [preflight](../NATIVE_EXECUTION_PREFLIGHT.md), [관리형 진단](../EXECUTION_MANAGED_INSPECTION.md), [진단 Docker](../../deployments/native/README.md), [runtime 초안](../../deployments/native/runtime/README.md). 실제 배포 완료가 아님 |
| 백업·오프라인 경보 | [Native 보존/admission](../EXECUTION_ARC_RETENTION.md), [서명 어댑터](../EXECUTION_BACKUP_ADAPTER.md), [오프라인 monitoring/두 스냅샷 추세](../NATIVE_OFFLINE_MONITORING.md). 실제 backup/WAL restore·관측/알림 경로는 남음 |
| 기존 API·CI 개선 | 시간/오류 진단·중복 파싱·compact JSON·keepalive race·실제 DB 기능 검증과 적용 가능한 [scoped short/full CI](../CI.md) 완료. 새 원인 없이 반복하지 않음 |

현재 상태는 덮어써 최신으로 유지하고 과거 수치·종료된 실험은 Git history로 확인한다.
수용한 E1/first-import 계획과 n/n−1/n−2는 보존한다. 테스트·fixture·metadata 호환성은
canonical/COMMIT/ACK/GC 권한이 아니다. 다른 프로젝트·프로세스/container/volume·공유
cache는 그대로 두며 이 작업 전용 임시 자료만 필요할 때 정리한다.

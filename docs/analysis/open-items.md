# 남은 작업과 우선순위

이 문서는 **미완료 작업의 정본**이다. 현재 코드·설정·릴리스 상태는
[세션 상태](../SESSION_STATE.md), 실제 운영 입력은
[운영 준비](../OPERATIONS_READINESS.md)를 따른다. 완료된 작업/PR/측정 이력을
이 목록에 누적하지 않는다.

현재 합의된 오프라인 구현은 완료됐다. 바로 착수할 READY 코드 작업은 없으며,
권장 순서는 **OPS-01 → OPS-02 → OPS-03 → OPS-04 → 허용된 OPS-05/06**이다.
새 입력이나 범위 변경 없이 완료 검사를 반복하거나 선택 확장에 착수하지 않는다.

## 상태

| 상태 | 의미 |
| --- | --- |
| READY | 현재 허용 범위에서 착수 가능 |
| PLAN | 수용 범위와 검증 조건을 정할 작업 |
| HOLD_INPUT | 필요한 외부 입력이 아직 제공·검증되지 않음 |
| HOLD_RUN | 실행/포맷/운영 검증/측정 보류가 유지됨 |
| DEFERRED | 별도 선택·설계가 필요한 확장 |

## 운영 준비와 검증

| 순서·ID | 상태 | 남은 작업 | 필요한 입력·완료 조건 |
| --- | --- | --- | --- |
| <a id="ops-01"></a>1 · OPS-01 | HOLD_INPUT | 전용 APFS·경로·용량 확정 | 실제 mount 경로를 기준으로 UUID/physical-store/ownership 확인. native/raw/source/scratch/checkpoint/PGDATA/WAL 별 예산·증가/중단 여유·COW headroom 확정. 포맷/이동은 별도 보류 |
| <a id="ops-02"></a>2 · OPS-02 | HOLD_INPUT | 공급자·접속·인증 시작점 확보 | Arc 공식 EL enode/접속·fork/genesis/network/history 보존 조건, 인증 full checkpoint H/hash/root/coverage와 H+1. StableNet은 지원 WBFT trust/fork/validator anchor, 다른 RPC 체인은 확정 시작점·필요 상태 보존 계약 |
| <a id="ops-03"></a>3 · OPS-03 | HOLD_INPUT | 비공개 배포·DB·실행기 준비 | OPS-01/02에 맞춘 image digest·지원 manifest/App schema·private DSN/key 경로·UID/IPC/mount·준비된 PG 역할/schema. 체인/프로세스 Store pool4와 서버 전체 연결/메모리 예산. 실제 VM 실행기/label·cidfile·durable crash admission은 운영 범위와 함께 설계/구현하며 현재 미연결 |
| <a id="ops-04"></a>4 · OPS-04 | HOLD_INPUT | 일관된 백업·WAL·복구 증거 준비 | 실제 native/source/indexer backup·연속 WAL 경계·독립 인증·별도 보관 장치/custody·RPO/RTO. [백업 admission](../EXECUTION_ARC_RETENTION.md)은 실제 PG snapshot 생성·복원 성공을 대체하지 않음 |
| <a id="ops-05"></a>5 · OPS-05 | HOLD_RUN | 실제 배포·source 적합성·장기 운전 | OPS-01~04와 별도 허용된 실행 범위. 실제 peer/fork/history·원본/proof/hash-selector/blob 공급 검증. qualified native/derived backlog·lag·growth/health adapter와 operator transport의 실제 연결·권한/수신자 준비. legacy/native writer 중복 금지, 기존 E3 authority 유지 |
| <a id="ops-06"></a>6 · OPS-06 | HOLD_RUN | 운영 용량/backlog·장애·복구 검증 | 실제 DB 증가·worker/ACK 지연·예산/임계값·관측 실패·디스크 분리/전원 장애·재시작·COW 여유·복원 증거. 모의 통과를 실제 운영 보장으로 바꾸지 않음 |
| <a id="perf-01"></a>7 · PERF-01 | HOLD_RUN | 추가 성능·호스트 비교 | 별도 합의한 수집/writer/ACK/API tail/backlog·copy 비용·macOS/Docker 비교 범위. 기존 host-load/SLO 재측정 루프를 재개하지 않음 |

현재 먼저 필요한 사용자 입력은 **준비된 외장 APFS의 실제 마운트 경로**다.
UUID·소유권 등 읽기 전용으로 확인할 수 있는 값은 경로 확보 뒤 확인한다.
경로가 없으면 후보를 운영값으로 만들거나 디스크를 포맷하지 않는다.
비밀번호·DSN·키 값은 Git/대화에 넣지 않는다.

## 별도 결정이 필요한 선택 확장

| ID | 상태 | 작업·경계 |
| --- | --- | --- |
| <a id="ext-01"></a>EXT-01 | DEFERRED | 다른 EVM 체인의 native producer·custom account/header/fork 지원. 실제 계약과 fixture 필요 |
| <a id="ext-02"></a>EXT-02 | DEFERRED | 전체 상태/변경 coverage·witness·임의 eth_call·과거 backfill·새 projection. selected block-end proof와 별도 설계 |
| <a id="ext-03"></a>EXT-03 | DEFERRED | 별도 frontend 저장소의 API 연동. [현재 연동 가이드](../FRONTEND_MIGRATION.md)를 따르며 다른 프로젝트를 자동 수정하지 않음 |
| <a id="ext-04"></a>EXT-04 | DEFERRED | 비EVM 모델. 현재 EVM 지원 범위 밖 |

## 현재 구현 범위와 유지할 계약

E1~E6 수용 범위와 Arc N0~N6는 구현됐으며 실제 운영 검증과 구분한다.
<a id="mon-01"></a><a id="mon-02"></a><a id="mon-03"></a>
MON-01~03의 [관측 모델·DB/WAL App·경보/checkpoint](../NATIVE_MONITORING_INTEGRATION.md)와
[injected VM probe/health](../NATIVE_VM_OBSERVATION.md)도 구현됐다. 실제 Docker
실행기·미확정 source/derived adapter·네트워크 sender는 위 OPS 범위에 남는다.
완료 근거와 수용 설계는 [세션 상태](../SESSION_STATE.md)·[설계 색인](README.md)으로 확인한다.

체인/프로세스 Store pool4·part2·API 예약1·일곱 logical writer, physical RPC5
(retry/failover/batch fallback 포함), 두 정상 EVM 실행과 final NEW uncached
canonical → 응답 후 generation fence → COMMIT → exact ACK를 유지한다.
SQL parameter binding/validated identifiers와 original anchor·epoch·lease·full
baseline·permit·GC/복구 권한도 그대로다. 포맷/provisioning·운영 실행·GC/restore·
추가 측정 보류와 다른 프로젝트/프로세스/container/volume/cache 보존을 유지한다.

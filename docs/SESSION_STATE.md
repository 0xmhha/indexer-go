# 현재 작업과 다음 세션 시작점

기준:2026-11-03 KST. 다음 세션은 [인계 규약](SESSION_HANDOFF.md)에 따라 실제
Git/PR/CI와 이 스냅샷을 대조한다. 이 파일은 runtime 상태나 운영 권한의 증거가 아니다.

## 현재 상태

| 항목 | 확인된 상태 |
| --- | --- |
| 마지막 완료 릴리스 | MON-02-C, [PR #265](https://github.com/0xmhha/indexer-go/pull/265), squash/main `7c4e9f62c5e0babe823b093c552a0e0e48985030` |
| 구현 범위 | E1~E6의 수용 범위, Arc N0~N6와 기존 진단/백업/모니터링·두 스냅샷 증가 추세 구현 완료. 실제 운영 검증과 선택 확장은 남음 |
| 활성 작업 | MON-02-D / READY / REVIEW. DB/WAL opt-in App/config·공유 sampler/worker·별도 operator 경계 구현/로컬 검증 완료. 실제 운영/발송은 보류 |
| 작업 기준·계획 | base main `7c4e9f62c5e0babe823b093c552a0e0e48985030`, branch `refactor/observation-app-assembly`, [계획](codemine/runs/20261102-mon02d-app-assembly/plan.md)·[체크리스트](codemine/runs/20261102-mon02d-app-assembly/checklist.md)·[검증](codemine/runs/20261102-mon02d-app-assembly/validation.md) |
| 현재 인계 문서의 릴리스 | 실제 branch/HEAD/PR/CI를 확인. MON-02-C #265의 exact-head CI341 성공·squash/main 동기화 확인. 이 문서를 포함한 후속 SHA는 Git 정본 |
| 다음 행동 | D의 정상 PR·actual exact-head short CI·squash·clean main을 확인. 완료됐다면 [MON-03](codemine/runs/20261102-mon01-observation-design/implementation.md#mon-03--docker-vm-source와-observer-health)의 fake process/parser·unknown·cancel/join부터 진행. 실제 probe는 보류 |
| 외부 입력 | 마지막 관측은 [운영 준비 현황](OPERATIONS_READINESS.md). 이후 APFS/enode/checkpoint/private 배포·백업 값의 새 제공/검증 없음 |

프로세스가 없거나 어떤 CI가 현재 실행 중인지 이 파일만으로 추정하지 않는다.
재개할 소유 작업이 실제로 있다면 branch/diff/PR 결과를 확인해 활성 작업 칸과
다음 행동을 갱신한다. 다른 머신의 임시 디렉터리나 PID는 필요하지 않다.

## 직전 세 맥락과 검증 근거

| 맥락 | 정본과 릴리스 | 실제 검증 범위 |
| --- | --- | --- |
| n: MON-02-D App/config | [계획](codemine/runs/20261102-mon02d-app-assembly/plan.md)·[체크리스트](codemine/runs/20261102-mon02d-app-assembly/checklist.md)·[검증](codemine/runs/20261102-mon02d-app-assembly/validation.md) | RED→GREEN·focused race·새13사례 CPU1/race/count200·owned PG runtime/API1·다중 체인 종료·build/vet/lint. 실제 운영/발송/성능 없음. release gates는 현재 Git/PR/CI 정본으로 확인 |
| n−1: MON-02-C alert/checkpoint | [계획](codemine/runs/20261102-mon02c-alert-state/plan.md)·[검증](codemine/runs/20261102-mon02c-alert-state/validation.md), [PR265](https://github.com/0xmhha/indexer-go/pull/265), [CI341](https://github.com/0xmhha/indexer-go/actions/runs/41475394794), main `7c4e9f62c5e0babe823b093c552a0e0e48985030` | 6 job 성공. source `b9c8fbda30c3504b5e730fa2700be09809c6cf05`, tree `61a3bed04a5caf15519c58450c4f4f8a4349844b`. 로컬 reducer/file RED→GREEN·focused race·private writer1사례 CPU1/race/count200. 실제 DB/native/발송/성능 없음 |
| n−2: MON-02-B owner tap/sampler | [계획](codemine/runs/20261102-mon02b-owner-taps/plan.md)·[검증](codemine/runs/20261102-mon02b-owner-taps/validation.md), [PR264](https://github.com/0xmhha/indexer-go/pull/264), [CI340](https://github.com/0xmhha/indexer-go/actions/runs/41425246922), main `802d97751342bbc100387c09e2da818f01f1166f` | 6 job 성공. source `3834dc41e66ba5ea59f45add7882e687f87ce335`, tree `1fe9c9b74396f40d5d2843a1ccb592991139fb8b`. 로컬 새 concurrency7종 single200·owned PG pool4/API1·취소. 실제 App/config/발송 활성화 없음 |

[MON-01 설계](NATIVE_MONITORING_INTEGRATION.md)와 [A~D 계획](codemine/runs/20261102-mon01-observation-design/implementation.md)을 계속 따른다.

앞선 [오프라인 증가 추세 검증](codemine/runs/20261102-indexer-growth/validation.md)은
v1 CLI 호환/이력의 근거다. [PR260](https://github.com/0xmhha/indexer-go/pull/260)의
실제 검증을 이번 순수 모델 구현이나 운영 검증으로 바꿔 쓰지 않는다.

수용한 [E1 설계](codemine/runs/20261023-e1-execution/02-design.md)·[구현 계획](codemine/runs/20261023-e1-execution/03-plan.md),
[E2 bootstrap](codemine/runs/20261024-e2-bootstrap/plan.md),
[E3 retention](codemine/runs/20261024-e3-retention/plan.md),
[E4 writer](codemine/runs/20261024-e4-feature-writers/plan.md),
[Arc first-import](codemine/runs/20261029-arc-first-import/plan.md),
[N5](codemine/runs/20261030-arc-native-n5/plan.md)와
[관리형 진단](EXECUTION_MANAGED_INSPECTION.md),
[N6 acceptance 계획](codemine/runs/20261031-arc-native-n6/plan.md)·[검증](codemine/runs/20261031-arc-native-n6/validation.md)을 보존한다.

## 유지할 경계

- 체인/프로세스의 기존 pool4, part2, API 예약1, 일곱 logical writer. 새 pool 없음.
- 공유 RPC5 physical attempts/s: retry/failover/batch fallback 포함.
- 두 번의 정상 EVM 실행, final NEW uncached canonical → 응답 후 generation
  fence → COMMIT → exact ACK. 기존 SQL 바인딩/undo/outbox/원본·epoch·lease·GC 유지.
- 포맷/provisioning·실제 App/node/peer/배포·운영 GC/복원·추가 성능 측정은 HOLD.
  다른 프로젝트·프로세스·container/volume·공유 cache는 유지한다.

운영 입력 부재를 확인하기 위해 같은 probe를 반복하거나 완료된 SDK/DB/200회/
host-load/SLO 검사를 다시 제안하지 않는다. 새 변경·실패·명시된 재검증 범위가
있을 때 해당 검사만 선택한다. 상세 작업 ID·의존성·완료 조건은
[남은 작업 목록](analysis/open-items.md)을 따른다.

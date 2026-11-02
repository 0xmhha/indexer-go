# 현재 작업과 다음 세션 시작점

기준:2026-11-02 KST. 다음 세션은 [인계 규약](SESSION_HANDOFF.md)에 따라 실제
Git/PR/CI와 이 스냅샷을 대조한다. 이 파일은 runtime 상태나 운영 권한의 증거가 아니다.

## 현재 상태

| 항목 | 확인된 상태 |
| --- | --- |
| 마지막 완료 릴리스 | MON-02-A, [PR #263](https://github.com/0xmhha/indexer-go/pull/263), squash/main `acb2f93e8958f15abac35c864246c6bbd16ff27f` |
| 구현 범위 | E1~E6의 수용 범위, Arc N0~N6와 기존 진단/백업/모니터링·두 스냅샷 증가 추세 구현 완료. 실제 운영 검증과 선택 확장은 남음 |
| 활성 작업 | MON-02-B / READY / REVIEW. 단일 stats sampler·취소/join·비차단 owner tap과 partial progress. 로컬 RED→GREEN/race200/owned PG 검증 완료. 실제 릴리스는 Git/PR/CI 확인 |
| 작업 기준·계획 | base main `acb2f93e8958f15abac35c864246c6bbd16ff27f`, branch `refactor/observation-owner-taps`, [계획](codemine/runs/20261102-mon02b-owner-taps/plan.md)·[체크리스트](codemine/runs/20261102-mon02b-owner-taps/checklist.md)·[검증](codemine/runs/20261102-mon02b-owner-taps/validation.md) |
| 현재 인계 문서의 릴리스 | 실제 branch/HEAD/PR/CI를 확인. MON-02-A #263의 exact-head CI339 성공·squash/main 동기화 확인. 이 문서를 포함한 후속 SHA는 Git 정본 |
| 다음 행동 | B의 applicable short CI·squash·clean main을 확인한 뒤 [MON-02-C](codemine/runs/20261102-mon01-observation-design/implementation.md#mon-02-c--경보-reducer와-private-checkpointqueue): 순수 alert reducer·private checkpoint/queue·fake/disabled sink |
| 외부 입력 | 마지막 관측은 [운영 준비 현황](OPERATIONS_READINESS.md). 이후 APFS/enode/checkpoint/private 배포·백업 값의 새 제공/검증 없음 |

프로세스가 없거나 어떤 CI가 현재 실행 중인지 이 파일만으로 추정하지 않는다.
재개할 소유 작업이 실제로 있다면 branch/diff/PR 결과를 확인해 활성 작업 칸과
다음 행동을 갱신한다. 다른 머신의 임시 디렉터리나 PID는 필요하지 않다.

## 직전 세 맥락과 검증 근거

| 맥락 | 정본과 릴리스 | 실제 검증 범위 |
| --- | --- | --- |
| n: MON-02-B owner tap/sampler | [계획](codemine/runs/20261102-mon02b-owner-taps/plan.md)·[체크리스트](codemine/runs/20261102-mon02b-owner-taps/checklist.md)·[검증](codemine/runs/20261102-mon02b-owner-taps/validation.md) | RED→GREEN·focused race·새 concurrency7종 CPU1/race/single200·실제 owned PG pool4/API1·취소. 실제 App/config 활성화/발송·운영 검증은 없음. 릴리스는 실제 Git/PR/CI 확인 |
| n−1: MON-02-A 순수 관측 모델 | [계획](codemine/runs/20261102-mon02a-observation-model/plan.md)·[검증](codemine/runs/20261102-mon02a-observation-model/validation.md), [PR263](https://github.com/0xmhha/indexer-go/pull/263), [CI339](https://github.com/0xmhha/indexer-go/actions/runs/41414832596), main `acb2f93e8958f15abac35c864246c6bbd16ff27f` | 6개 job 성공, root/execution short·build/vet/lint·examples·PG core11. source `f1af07360629db7fcd2adf0bbc302542b2cff290`, reviewed tree `098df8201528c0342dc2fbad150ec80fc5899e32`. Runtime 수집/알림 없음 |
| n−2: MON-01 설계 | [정본](NATIVE_MONITORING_INTEGRATION.md)·[계획](codemine/runs/20261102-mon01-observation-design/plan.md)·[검증](codemine/runs/20261102-mon01-observation-design/validation.md), [PR262](https://github.com/0xmhha/indexer-go/pull/262), [CI338](https://github.com/0xmhha/indexer-go/actions/runs/41408868611), main `f81f28873ba271ae9028d19f02ba20634c8a8ff0` | 6개 docs-only job 성공. 링크292/정책24, runtime 없음. source `dbaf4516e7eab7e2635e1840ef8b25561528e23c`, reviewed tree `c7c20aaf207c9ac613c91ceada993cc34d14ee8d` |

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

# 현재 작업과 다음 세션 시작점

기준:2026-11-02 KST. 다음 세션은 [인계 규약](SESSION_HANDOFF.md)에 따라 실제
Git/PR/CI와 이 스냅샷을 대조한다. 이 파일은 runtime 상태나 운영 권한의 증거가 아니다.

## 현재 상태

| 항목 | 확인된 상태 |
| --- | --- |
| 마지막 완료 기능 | 오프라인 DB 증가 추세, [PR #260](https://github.com/0xmhha/indexer-go/pull/260), squash/main `030c1646b02f41dbad57022681d180c59e135458` |
| 구현 범위 | E1~E6의 수용 범위, Arc N0~N6와 기존 진단/백업/모니터링·두 스냅샷 증가 추세 구현 완료. 실제 운영 검증과 선택 확장은 남음 |
| 활성 작업 | MON-01 / READY / REVIEW. 재사용 설계·후속 RED 조건은 문서화했으며 현재 문서 PR의 CI/머지/main 상태를 실제 Git/PR에서 확인. runtime 구현·실행은 없음 |
| 작업 기준·계획 | base main `5250f4d3753e5321aae9736991922345b55b75c1`, branch `refactor/monitoring-integration-design`, [계획](codemine/runs/20261102-mon01-observation-design/plan.md)·[체크리스트](codemine/runs/20261102-mon01-observation-design/checklist.md) |
| 현재 인계 문서의 릴리스 | 실제 branch/HEAD/PR/CI를 확인. 위 SHA는 마지막 검증된 기능 기준점이며 후속 문서 커밋의 HEAD를 뜻하지 않음 |
| 다음 행동 | 현재 문서 릴리스를 정리한 뒤 [MON-02-A](codemine/runs/20261102-mon01-observation-design/implementation.md#mon-02-a--다음-권장-순수-관측-모델과-qualification): 순수 관측 DTO/qualification과 공통 predicate 추출을 RED→GREEN으로 구현 |
| 외부 입력 | 마지막 관측은 [운영 준비 현황](OPERATIONS_READINESS.md). 이후 APFS/enode/checkpoint/private 배포·백업 값의 새 제공/검증 없음 |

프로세스가 없거나 어떤 CI가 현재 실행 중인지 이 파일만으로 추정하지 않는다.
재개할 소유 작업이 실제로 있다면 branch/diff/PR 결과를 확인해 활성 작업 칸과
다음 행동을 갱신한다. 다른 머신의 임시 디렉터리나 PID는 필요하지 않다.

## 직전 세 맥락과 검증 근거

| 맥락 | 정본과 릴리스 | 실제 검증 범위 |
| --- | --- | --- |
| n: MON-01 설계 | [정본](NATIVE_MONITORING_INTEGRATION.md)·[계획](codemine/runs/20261102-mon01-observation-design/plan.md)·[체크리스트](codemine/runs/20261102-mon01-observation-design/checklist.md)·[검증](codemine/runs/20261102-mon01-observation-design/validation.md) | 코드 source 분석과 문서 검증. 실제 수집/경보 구현·실행이 아니며 현재 PR 릴리스는 실제 Git/CI에서 확인 |
| n−1: 세션 인계 구조 | [규약](SESSION_HANDOFF.md), [PR261](https://github.com/0xmhha/indexer-go/pull/261), [CI337](https://github.com/0xmhha/indexer-go/actions/runs/41395065513), main `5250f4d3753e5321aae9736991922345b55b75c1` | 6개 docs-only job 성공. 링크255/정책24, compiler/DB/서비스 실행 없음. 현재 상태·작업 ID·실제 Git/PR 대조 절차 확립 |
| n−2: DB 증가 추세 | [계획](codemine/runs/20261102-indexer-growth/plan.md)·[체크리스트](codemine/runs/20261102-indexer-growth/checklist.md)·[검증](codemine/runs/20261102-indexer-growth/validation.md), [PR260](https://github.com/0xmhha/indexer-go/pull/260), [CI336](https://github.com/0xmhha/indexer-go/actions/runs/41386648007) | 6개 job 성공. Go build/short/정적/예제·disposable PG 핵심11개, 로컬 RED→GREEN·monitor race·strict/private 파일/경계 검사. native SDK/full stress는 적용하지 않음 |

직전 기능 #260의 source head는 `680d57c0b4b1ae8047424180d660ea130886a620`,
reviewed tree는 `157ddb05974220efae05be51eaddcfa1ec3910bc`이다. Squash/main
`030c1646b02f41dbad57022681d180c59e135458`의 tree가 같고 부모는
`cc3e35c27d255e5efb0907b332b7ded0c711bb03`이었다. 실제 CI와 Git을 다시 확인할
식별자이며 로컬 `/private/tmp`의 영수증이 없어도 재개할 수 있다.

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

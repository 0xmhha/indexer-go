# 블록 실행 기반 수집

현재 첫 구현 단계는 StableNet·PostgreSQL의 E1이다. 정본은 [최종 결정](../decisions/20261023-e1-design/decision.md), [E1 설계](../codemine/runs/20261023-e1-execution/02-design.md), [구현 계획](../codemine/runs/20261023-e1-execution/03-plan.md)이다. 초기 설계와 바뀐 선택지는 Git history로 확인한다.

## 1. 목표

peer로 블록을 받고 체인 규칙으로 실행한 결과를 indexer가 PostgreSQL에 이력과 현재 값으로 저장한다. 실행부는 노드 프로젝트를 수정하지 않고 별도 모듈로 import한다. 정상 import 뒤 두 번째 실행에서 거래별·Finalize 변화를 기록한다. 실행부는 PostgreSQL에 접근하지 않는다.

indexer의 표별 queue·writer7개가 기존 pool4개를 공유한다. 미공개 자료 저장과 최종 공개 transaction을 구분하며, fresh·generation 검사 뒤에만 API·outbox·ACK를 전진시킨다. 공개 시점의 최신 값은 이력으로 보장하고 rewind 중 조회는 fence한다. 이 계약은 E1 계획 2장을 따른다.

## 2. 단계

| 단계 | 범위 | 현재 상태 |
| --- | --- | --- |
| E0 | 실행 엔진 import와 state/receipt root 타당성 조사 | 이전 조사 완료. 실제 운영/속도 보장은 아님 |
| E1 | 실행부·IPC·spool·상태 이력·writer7개·공개 높이·복구·앱 연결 | [체크리스트](../codemine/runs/20261023-e1-execution/checklist.md) |
| E2 | 시작 모드·전체 상태 bootstrap·검증 도구 | 후속 |
| E3 | 노드 DB와 원본 보존·정리 | 후속 |
| E4 | 기존 기능 표의 writer 분리·중복 데이터 정리 | 후속. 핵심 writer7개는 E1 |
| E5 | 체인별 실행부 확대 | 후속. 비EVM은 모델 결정 필요 |
| E6 | 합의 검증에 근거한 canonical/finality와 RPC 의존 축소 | 후속 |

## 3. 유지할 조건

- 기존 RPC 경로는 유지한다. E1은 별도 opt-in 모드다.
- RPC5 HTTP/s와 기존 fresh·epoch·durability 검사를 유지한다.
- 노드 DB 자동 삭제는 E3다. 미처리 결과를 버려 capacity를 해소하지 않는다.
- 실제 노드 서비스·peer·추가 성능 측정·포맷·GC 활성화는 보류다.
- 현재 작업, 직전 두 기능과 남은 운영 조건은 [작업 목록](open-items.md)과 E1 continuity를 따른다.

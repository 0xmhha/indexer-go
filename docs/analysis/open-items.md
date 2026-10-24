# 남은 작업

현재 작업은 StableNet·PostgreSQL의 E1 실행 수집이다. 2026-10-24 사용자가 계획·추천 결정·RED/GREEN 구현·문서·PR·CI-gated squash merge·main 동기화까지 위임했다.

## 1. E1의 정본과 재개 지점

- [설계](../codemine/runs/20261023-e1-execution/02-design.md), [결정](../decisions/20261023-e1-design/decision.md), [구현 계획](../codemine/runs/20261023-e1-execution/03-plan.md)
- [기능별 체크리스트](../codemine/runs/20261023-e1-execution/checklist.md), [n/n-1/n-2 연속성](../codemine/runs/20261023-e1-execution/continuity.json)
- U00~U08은 #210~[#213](https://github.com/0xmhha/indexer-go/pull/213)에서 필수 CI·squash/main 동기화를 완료했다. U09~U11 실행 수집 구현과 최종 전체 기능·race·정적·예제·PostgreSQL 회귀, 새 동시성 단일200회 검증은 완료했다. 최종 통합 [PR #214](https://github.com/0xmhha/indexer-go/pull/214)에서 최신-head CI4·squash merge와 해당 main commit을 추적한다. 최신 사용법은 [실행 수집](../EXECUTION_INGEST.md), 현재 n/n-1/n-2는 U11/U10/U09다.

2회 실행과 노드 프로젝트 미수정, 기존 pool4개, RPC5 HTTP/s와 공개 직전 fresh를 유지한다. writer7개는 DB 연결7개를 뜻하지 않는다. 공개 경계·reorg·기록 무누락 검증의 구체 계약은 구현 계획을 따른다. 현재 문서는 최신 상태만 유지하며 과거 판본·수치·종료된 실험은 Git history로 확인한다.

## 2. 이미 구현한 기반

공통 원본 window, capture DB와 삭제 fence, Arc ETH/68 기본 adapter, DB-only facts writer, opt-in 앱 lifecycle, 독립 projection·지정 상태 proof·call trace·blob 검증 library, telemetry, checkpoint/GC 읽기 전용 진단, capacity halt/resume guard가 있다. 이 기반을 E1 전체 archive 실행이나 실제 Arc peer·외장 디스크 운영 완료로 확대하지 않는다.

API 시간 구분·오류 판정, GraphQL 단일 파싱·compact JSON, keepalive 검사 보완, 기존 DB 기능 검사와 회귀·동시성200회는 완료 기록을 유지한다. 관련 새 변경이나 실패가 없으면 반복할 작업으로 제안하지 않는다. 호스트 부하 확인 루프, capture SQL batching, K2 binary key도 다음 작업이 아니다.

## 3. E1 이후

| 단계 | 남은 작업 |
| --- | --- |
| E2 · 다음 우선순위 | 시작 모드와 검증된 checkpoint/bootstrap 도구. 전체/부분 coverage 구분, anchor·baseline 검증, parent state/header 부족과 DB/source generation 차이의 명시적 복구 |
| E3 | node DB·원본·quarantine 보존/정리. ACK·reorg·lease·복구를 통과한 삭제와 용량 중단/재개; 노드 history 자동 삭제 전 검증 |
| E4 | 기존 기능 표 writer 분리와 중복 잔액/projection 정리. 기존 feature/undo/outbox/API 공개 경계 유지; 핵심 writer7개는 E1이다 |
| E5 | Arc 등 체인별 producer·실행부 확대. trace/blob 등 자료 공급과 검증 범위를 구분하며 비EVM 모델은 별도 결정 |
| E6 | 체인 합의에 근거한 canonical/finality 검증과 RPC 의존 축소. 현재 final fresh RPC를 독립 합의 검증 완료로 확대하지 않음 |

## 4. 운영과 외부 조건

| 항목 | 현재 남은 조건 |
| --- | --- |
| 운영 설정 | APFS mount·identity·예산, private 경로·자격 증명, 이미지, checkpoint, Arc enode·peer fork·보존 범위 확정 |
| 용량 감시 | 실행 중 Docker VM 여유와 watcher 장애 알림. app filesystem/PGDATA/raw 감시와 halt/resume는 구현돼 있다 |
| 백업 | WAL 보관소, Sepolia/BSC의 상태 보존 방침, 실제 백업·복원과 RPO/RTO 검증 |
| 실제 자료 검증 | 실제 era1 export/RPC 비교, Arc 실제 peer와 mainnet, 외장 mount·장치 분리·장기 운전 |
| 성능 | E1 RPC 대비 p50/p99, macOS SLO·물리 호스트 비교. 추가 측정은 보류다 |
| Frontend | 별도 저장소의 GraphQL/REST 연동 상태 확인. E1에서 그 저장소를 변경하거나 재검증하지 않음. [이전 안내](../FRONTEND_MIGRATION.md) |

실제 노드 서비스, peer 접속, disk provisioning, GC 활성화, 추가 성능 측정은 보류한다. 태그·외부 배포·원격 정리는 자동 후속 단계가 아니다. 다른 프로젝트의 프로세스·container·volume·자료를 유지한다.

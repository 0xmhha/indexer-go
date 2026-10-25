# 남은 작업

E1 #214, E2 #215, E3 #216, E4 #217, E5 replay #218·collector #219·native checkpoint/보존 #220·대규모 catalog [#221](https://github.com/0xmhha/indexer-go/pull/221)은 main에 반영됐다. 현재 작업은 Arc native 상태 이력·index 압축이다. 정본은 아래 계획·체크리스트와 [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-history-compaction)이다. 해당 PR이 CI4+Arc를 통과해 squash MERGED됐으면 이 범위는 완료다. 실제 운영 활성화와 고정 native 디스크 크기 보장은 별도다. 종료된 검사·성능 측정을 새 이유 없이 반복하지 않는다.

## 현재 정본과 연속성

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

## 다음 우선순위

| 단계 | 범위와 완료 조건 |
| --- | --- |
| Arc history/index 릴리스 gate | cutoff full seed 선검증·저장, COW history index/changeset 정리·native floor·물리 compaction, 보호 상태·재시작·재시도·실패 보존과 문서. 필수 로컬 검사와 최신-head CI4+Arc 후 squash/main. [체크리스트](../codemine/runs/20261025-e5-arc-history/checklist.md)가 정본 |
| Native 보존의 운영/추가 확장 | actual layout·backup·용량 검증 후 명시적 활성화. 최신 상태·headers·보수적 jar·기타 indices가 남으므로 전체 native 디스크 고정 크기를 약속하지 않음. MDBX 여유 페이지/추가 index 정리는 별도 검증 필요 |
| E5 후속 3 | 완전한 call trace/internal transaction, blob sidecar/KZG·가용성 및 상태 증명 공급. 현재 account/storage journal·receipt가 이 자료를 대신하지 않음. 기타 체인 producer 확대; 비EVM 모델은 별도 결정 |
| E6 | 체인 합의에 근거한 canonical/finality 검증과 RPC 의존 축소. 현재 final fresh나 native-library 검사를 독립 합의/실제 peer 검증으로 확대하지 않음 |

## 운영과 별도 검증

| 항목 | 남은 조건 |
| --- | --- |
| 운영 설정 | APFS mount/device identity·실제 용량·private 경로/자격 증명·이미지·checkpoint·Arc enode/peer fork/보존 범위 확정 |
| E3 활성화 | 실제 외장 디스크에서 copy 여유와 전원/분리 장애, native layout·백업·기존 노드 보존 범위 검증 후 명시적 활성화. 원본 checkpoint 이전 미수집 payload, 모든 headers/indices, 현재 상태/code/preimages는 유지하므로 전체 디스크가 고정 크기가 되지는 않음 |
| 용량 감시 | 실제 node budget/중단·재개 기준 확정, Docker VM 여유와 watcher 장애 알림. app filesystem/PGDATA/raw와 선택 producer capacity guard는 구현 |
| 백업 | WAL 보관소, 체인별 상태 보존, 실제 matched DB/node/spool 백업·복원과 RPO/RTO 검증 |
| 실제 자료 | 실제 era1 export/RPC 비교, StableNet/Arc 실제 peer와 mainnet, 외장 mount·장치 분리·장기 운전 |
| 성능 | E1 RPC 대비 및 E3 copy/정리 비용, macOS SLO·물리 호스트 비교. 추가 측정은 보류 |
| Frontend | 별도 저장소의 GraphQL/REST 연동 확인. 이 작업에서 변경/재검증하지 않음. [이전 안내](../FRONTEND_MIGRATION.md) |

실제 노드 서비스·peer·disk provisioning·운영 GC·추가 성능 측정은 보류한다. 현재 릴리스 완료 후 다음 단계를 자동 시작하거나 태그·배포·원격 브랜치 삭제를 수행하지 않는다. 다른 프로젝트의 프로세스·container·volume·자료를 유지한다.

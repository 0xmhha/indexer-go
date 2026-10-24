# 남은 작업

E1 [#214](https://github.com/0xmhha/indexer-go/pull/214), E2 [#215](https://github.com/0xmhha/indexer-go/pull/215), E3 [#216](https://github.com/0xmhha/indexer-go/pull/216), E4 [#217](https://github.com/0xmhha/indexer-go/pull/217)은 main에 반영됐다. 현재 작업은 E5의 공식 Arc EVM 재실행·읽기 전용 단일 블록 export·보존 파일 전송이다. 정본은 E5 계획·체크리스트와 [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-execution)이다. 해당 PR이 머지됐으면 이 릴리스 범위는 완료다. 지속 수집·checkpoint export·Arc 원본 보존 자동화까지 끝난 것은 아니다. 종료된 E1~E4 검사나 성능 측정을 이유 없이 반복하지 않는다.

## 현재 정본과 연속성

- E5 [계획](../codemine/runs/20261024-e5-arc-execution/plan.md), [체크리스트](../codemine/runs/20261024-e5-arc-execution/checklist.md), [연속성](../codemine/runs/20261024-e5-arc-execution/continuity.json), [검증](../codemine/runs/20261024-e5-arc-execution/validation.md), [Arc 실행 수집](../EXECUTION_ARC.md)
- E4 [계획](../codemine/runs/20261024-e4-feature-writers/plan.md), [체크리스트](../codemine/runs/20261024-e4-feature-writers/checklist.md), [연속성](../codemine/runs/20261024-e4-feature-writers/continuity.json), [검증](../codemine/runs/20261024-e4-feature-writers/validation.md), [기능 writer](../EXECUTION_FEATURE_WRITERS.md)

- E3 [계획](../codemine/runs/20261024-e3-retention/plan.md), [체크리스트](../codemine/runs/20261024-e3-retention/checklist.md), [연속성](../codemine/runs/20261024-e3-retention/continuity.json), [기능 검증](../codemine/runs/20261024-e3-retention/validation.md), [사용법](../EXECUTION_RETENTION.md)
- E2 [계획](../codemine/runs/20261024-e2-bootstrap/plan.md), [체크리스트](../codemine/runs/20261024-e2-bootstrap/checklist.md), [연속성](../codemine/runs/20261024-e2-bootstrap/continuity.json), [bootstrap](../EXECUTION_BOOTSTRAP.md)
- E1 [설계](../codemine/runs/20261023-e1-execution/02-design.md), [결정](../decisions/20261023-e1-design/decision.md), [구현 계획](../codemine/runs/20261023-e1-execution/03-plan.md), [실행 수집](../EXECUTION_INGEST.md)

2회 실행과 upstream 미수정, 체인별 기존 pool4/part2, RPC5 physical HTTP/s와 공개 직전 uncached fresh를 유지한다. writer7개는 연결7개를 뜻하지 않는다. E3는 검증된 full baseline, 현재 DB pending permit/live 감독 lease, 원본 ACK/window, node/source intent·완료·release를 함께 검증한다. 일부 checkpoint, 미처리/보호 구간, unknown/corrupt 자료는 삭제 권한으로 전환하지 않는다.

현재 문서는 최신 상태를 유지한다. 과거 판본·수치·종료된 실험은 Git history로 확인하며, 수용한 E1/E2 설계와 현재 n/n-1/n-2는 유지한다. 이미 끝난 API 진단/파싱/compact JSON/keepalive/DB 검증을 새 이유 없이 반복하거나 host-load/SLO 확인 루프를 다시 제안하지 않는다.

## 다음 우선순위

| 단계 | 범위와 완료 조건 |
| --- | --- |
| E5 현재 릴리스 gate | 공식 Arc 재실행·단일 파일 export/ACK 전송, PreBlock 원인과 schema0017. 기능·race·정적·예제·전용 PG, 새 동시성 단일200, native Rust/fixture 재생성, secret scan, draft PR 최신-head CI4+Arc 후 squash/main. [체크리스트](../codemine/runs/20261024-e5-arc-execution/checklist.md)가 정본 |
| E5 후속 1 | 지속 Arc collector의 durable cursor/spool quota, 재시작·reorg 세대/부모 선택과 ACK 감독. 현재 단일 파일 sender는 자동 재실행/rewind/삭제를 수행하지 않음 |
| E5 후속 2 | Arc native full checkpoint export, 실제 보존 DB layout와 E3 lease/ACK/window/recovery 기반 native retention adapter. 기존 StableNet 삭제 도구를 Arc에 적용하지 않음 |
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

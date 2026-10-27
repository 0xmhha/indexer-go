# 운영 준비 문서 정합 검토 (2026-10-27)

비교 기준은 이번 문서 branch의 base commit `724405594990128471d04fe825b09b0797bd21df`다. 행 번호는 이 base의 원문 기준이며 Git history로 재현할 수 있다. 현재 판정은 [운영 준비 현황](../OPERATIONS_READINESS.md)을 따른다.

| 원문 위치 | 불일치 | 이번 수정 |
| --- | --- | --- |
| CLAUDE.md:8,199 | #230이 구현 중·release pending으로 남음 | 검증한 인계 및 PR/CI5/squash 영수증을 현재 상태로 반영. n/n−1/n−2 유지 |
| docs/analysis/open-items.md:3,45 | feature4가 미완료이고 main이 E6로 남음 | 네 릴리스 완료와 실제 운영 HOLD 분리 |
| native-retention-backup/checklist.md:9~11, continuity.json:16, validation.md:142,168 | local/CI/squash/observer 대기 | 해시 검증한 인계와 portable priority4 sync를 반영. 기존 실패·수정 근거 보존 |
| docs/OPERATIONS_DRAFT.md:10,39,41,65 | capacity/interlock 전체 미구현 주장 | 한정된 guard/latch/watcher 구현과 운영 검증 미완료 구분 |
| docs/OPERATIONS_DRAFT.md:24,55,92~114 | arc.eth68 baseline을 현재 native 설정처럼 읽을 여지, check-host의 컨테이너 실행 누락 | legacy와 native 경로 구분, 실제 Docker wiring·설정은 변경 없음, check-host 미실행 명시 |
| docs/OPERATIONS_CAPACITY_DRAFT.md:18~19 | PGDATA 중단·분류 오류 재시작 억제 부재 주장 | internal/opsguard·multichain의 현재 범위 반영. VM 지속 감시·watcher 상주 관리 등 보류 유지 |
| docs/OPERATIONS_BACKUP_DRAFT.md:45,65,82 | dated archive·RPO·처리량을 현재 보장처럼 읽을 여지 | 10/22 관측·legacy 계산 provenance 유지, 현재 보장 배제 |
| docs/OPERATIONS_BACKUP_DRAFT.md:104,116 | stopped-copy 유일 경로와 native admission 누락 | matched source/native/DB backup admission과 실제 인증 PG snapshot+WAL/restore activation 분리 |

코드 대조 대상: `internal/opsguard`, `internal/multichain/manager.go`, `pkg/execution`, `pkg/storage/postgres`, `internal/config`, `deployments/testnets/preflight.py`, `docs/examples/testnets/*.draft.yaml`, `configs/execution/arc-retention.json.example`. pool4/part2/API 예약·writer7, 공유5 physical HTTP attempts/s와 final NEW fresh 후 generation fence/COMMIT은 바꾸지 않는다. 원래 full-baseline/E3 permit·lease·ACK·intent·release, 두 EVM 실행·upstream, E6 trust/fork 계약도 그대로다.

문서 검증은 상대 링크/anchor와 코드의 실제 flag/key, 기존 testnets offline mock/unittest, 필수 make test-race 및 새 PR exact-head CI5다. 로컬 full PostgreSQL/200/Rust/native build나 실제 운영 probe·성능 실험을 반복하지 않는다. 과거 strict-release observer는 검증한 보존 로그로 provenance를 확인하며 이번 테스트 실행으로 세지 않는다.

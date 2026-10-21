# 운영 초안의 오프라인 일관성 점검

2026-10-19. [Docker](../../deployments/testnets/README.md)·[운영 설정](../OPERATIONS_DRAFT.md)·[용량 정책](../OPERATIONS_CAPACITY_DRAFT.md)·[checkpoint/재시작/GC](../OPERATIONS_RUNBOOK_DRAFT.md)와 설정/코드를 대조했다. **오프라인 점검 완료이며 실제 운영 준비/실행 승인이나 성능 검증 완료가 아니다.**

## 발견한 문제와 수정

| 항목 | 확인한 차이/누락 | 수정한 내용 |
| --- | --- | --- |
| 검사 범위 | 기존 host policy는 root의 strict-state만 확인하고 RPC chain의 override·schema·window 연결·API listener 일부 변경을 놓칠 수 있었음 | 실제 private config에도 적용되는 `check_policy`를 보완. schema base·유효한 per-chain strict-state·단일 volume/window·연결/floor·listener 불일치를 거부 |
| 파일 간 연결 | Compose/app/PostgreSQL을 각각 읽는 것만으로 DB/RPC 예산·secret/entrypoint·image/mount·network·API·내구성의 연결을 확인하지 못함 | `check_artifacts`를 offline/host/start 진입 전 공통 적용. 두 파일을 함께 잘못 바꿔도 고정 DB4/RPC5 정책을 먼저 검사. PostgreSQL 조각의 중복/예상 밖 문법도 거부 |
| 5분 설정 | `max_unhealthy_duration`은 구조체/기본값/manager 전달에는 있으나 실행부의 unhealthy 중단 timer로 사용되지 않음 | 운영 YAML에서 제외하고 CONFIG/운영/용량/재시작/Docker 문서에 표시. worker5분/30분 감시는 별도 미구현 정책 |
| 로그 저장 | 외장 APFS 용량 정책만으로 Docker Desktop 내부의 stdout/stderr 로그 소비를 설명하지 못하고 서비스별 회전 상한도 없었음 | 두 서비스에만 `local` driver·`max-size: "10m"`·`max-file: "3"` 추가. VM/내부 저장 영역 감시가 별도로 필요함을 명시. 전역 Docker 설정/기존 container 변경 없음 |
| 경고·재개 경계 | filesystem150GiB, PGDATA128GiB의 등호에서 warning과 resume이 동시에 참일 수 있었음 | warning은 유지하고 재개를 free>150GiB·PGDATA<128GiB로 통일. 자동 controller를 구현한 것은 아님 |
| 용량 합산 | 기존420GiB는 raw160+DB160+runtime floor100만 포함해 재개 여유와 구분이 부족했음 | 정책의 free>150GiB를 함께 계획하면470GiB 초과+관리/파일시스템 여유가 필요함을 두 문서에 반영. APFS 확보·물리 예약/장기 보장은 미확인 |
| 큐 설명 | `queue_blocks: 256`을 raw 참조 큐로 오해할 수 있는 주석 | writer의 RAM 큐/256MiB, raw 알림 채널,24h 디스크 backlog를 구분 |
| 접속 전제 | host manifest 파일의0600 조건과 `Path.is_relative_to`에 필요한 Python 버전 안내가 빠졌음 | private manifest 권한·보관 위치와 Python3.9+를 Docker 안내에 명시 |
| 연결/RPC 적용 범위 | 정상 합계16개를 장애/중복 App까지 포함한 전역 hard cap으로 읽을 수 있었음 | 체인당 live App1개일 때의 pool 예산으로 한정. `Stop` timeout 뒤 지연 Close와 종료 확인 전 재기동 금지를 설명 |

연결 범위의 코드 근거는 [Stop](../../pkg/multichain/instance.go)과 [manager 재기동](../../pkg/multichain/manager.go)이다. timeout 때 Close가 지연될 수 있다는 경로는 확인했지만 실제 중복 실행·연결/RPC 초과는 관측하거나 재현하지 않았다. Go 실행 코드를 고친 항목이 아니며 원인별 재시작 억제와 전역 interlock은 남아 있다.

## 공통 기준의 현재 상태

| 기준 | 설정/문서 사이의 결론 |
| --- | --- |
| DB | PostgreSQL16, 체인별 Store pool4개, 단일 App/체인인 정상 실행 예산16개, 서버32개. 전용 DB/schema base는 `indexer_testnets_v1`. 별도 앱 pool은 추가하지 않음 |
| RPC·정확성 | 체인별 기존 공유5 HTTP/s·retry/failover 포함. source/writer 커밋 직전 fresh·post-RPC epoch/transaction 경계는 Go 코드 그대로 |
| raw | volume160GiB·Arc128GiB·최소24h·floor100GiB. 논리 예산이고 DB160GiB는 강제 quota가 아님 |
| 재개 | floor100 통과는 운영 정책의 free>150·DB/raw·identity/관측 조건 통과와 다름. 현재 host guard는 추가 정책 전체를 자동 강제하지 않음 |
| 생성·삭제 | `initialize_volume/create_window/collect=false` 유지. host guard는true를 거부. GC=false여도 과거 승인된 pending 삭제는 coordinated Open에서 복구 완료될 수 있음 |
| checkpoint | 최초 anchor H0/hash 고정과 C/G/F·P·필수 unit을 분리. 재시작 때 최신 anchor로 점프하거나 receipt/최대 높이를 연속 완료로 대신하지 않음 |
| 종료·장애 | indexer 취소/join/Close 후 필요 시 전용 DB 정상 종료.120/60초 Compose 유예는 적용(실제 종료 시간 미측정), capacity controller·원인별 restart 억제는 10/20 구현 |
| 비밀 | 소유자 전용 directory0700/일반 single-link 파일0600, DSN은 secret 파일에서 프로세스 환경으로만 전달. 실제 private 파일/키/자격 증명 생성 없음 |
| 실제 환경 | host 입력4개와 app 입력9개가 미확정. enode/실제 fork·checkpoint·APFS UUID·이미지 등의 미확정을 임시 값으로 확정하지 않음 |

## 이번 확인과 한계

- 최종 오프라인 기능 검사 **21개 통과, 실패/skip0**. 기존 mount/UUID·비밀 권한·기동 차단·DSN wrapper fixture와 새 per-chain override·파일 간 불일치·내구성/중복 설정 거부를 포함한다. diskutil/context/기동 dispatch는 fixture/mock이며 indexer/DB 대신 임시 가짜 프로그램만 실행한다.
- 기본 offline lint는 `draftArtifacts: consistent`, 미확정 host4개/app9개·`servicesStarted: 0`을 반환했다. `--check-host`/`--start`를 실제 호스트/서비스에 실행하지 않았다.
- 존재하지 않는 Docker 소켓과 합성 경로/파일로 `docker compose ... config --quiet`를 통과했다. 이미지 build/pull·daemon 자원 조작·container 실행은 없다. Compose 성공은 APFS sharing/UID·DB bootstrap/role grant·실제 log rotation 동작의 증거가 아니다.
- entrypoint 셸 구문·문서 링크/anchor·예산 산식/정책 경계·보호 파일 해시를 정적으로 확인했다. 실제 `config.yaml`·Go runtime/의존성·커밋 fresh 코드는 유지했다.
- 원래 app 초안의 숫자 placeholder는 typed decoding을 거부하도록 남겨 두었다. 새 Python 검사는 선택한 운영 계약의 검사이며 전체 Go schema/module 검증을 대체하지 않는다. 이번 Go 전체 기능/race/200회·기존 순수 Go loader 검사는 반복하지 않았다.

Docker 로그 설정은 [공식 local driver 설명](https://docs.docker.com/engine/logging/drivers/local/)의 per-container rotation 옵션을 사용한다. 최신 actual daemon 설정·디스크 여유·RPC/peer/DB 상태는 조회하지 않았다. 실제 포맷·초기화·종료/재개·삭제·장애/복원·성능 측정·다른 프로젝트 조작·commit/push는 없다.

남은 것은 실제 입력 확정, 읽기 전용 checkpoint/GC 진단 도구, DB/filesystem/Docker 영역 감시와 중단/재개 interlock(10/20 구현)·종료 유예(Compose 적용), 백업/이전/RPO/RTO와 실제 기능 검증이다. 기존 초안 작성/오프라인 점검을 이 구현·실환경 검증의 완료로 표시하지 않는다. 실행·포맷·추가 성능 측정 보류는 계속 유지한다.

후속 구현(2026-10-19): [읽기 전용 checkpoint/GC metadata 명령](../OPERATIONS_INSPECTION.md)을 추가하고 모의 DB/임시 파일로 검증했다. 이로써 위 목록 중 metadata 조회 도구의 첫 구현은 완료했다. live lease·payload/proof integrity·완전한 GC 승인 진단, 실제 DB/APFS 검증과 나머지 운영 제어는 완료한 것으로 확대하지 않는다.

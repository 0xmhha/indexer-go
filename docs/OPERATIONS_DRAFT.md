# 네 테스트넷 운영 설정 초안

**상태: 초안 작성 완료, 운영 적용·디스크 포맷·초기화·서비스 실행·추가 성능 측정은 보류.**

2026-10-19 기준 사용자 요청에 따른 운영안이다. 현재 무시 파일인 `config.yaml`은 Pebble 기반이며 변경하지 않았다. 이 초안은 PostgreSQL로의 전환과 Arc execution-only feed 활성화 목표를 기록한다. APFS 저장소·peer·checkpoint가 확정되기 전에는 실행 설정이 아니다.

- [체인별 애플리케이션 YAML 초안](examples/testnets): 체인마다 프로세스 하나(StableNet은 `indexer-stablenet`, Arc는 `indexer-arc`, BSC와 Sepolia는 `indexer`)
- [PostgreSQL 설정 조각](examples/postgresql-testnets.draft.conf)
- [Docker 운영 구성 초안](../deployments/testnets/README.md): 전용 Compose·호스트 APFS/UUID 검사·secret 파일 DSN 주입과 오프라인 검증.
- [용량·backlog·복구 정책 초안](OPERATIONS_CAPACITY_DRAFT.md): DB/WAL 증가 감시와 worker 지연·디스크 부족의 경고/중단/재개 제안; 자동 제어는 미구현.
- [백업·이전과 복구 목표 초안](OPERATIONS_BACKUP_DRAFT.md): 다시 만들 수 없는 자료, RPO/RTO 제안, 연속 WAL 보관과 정지 상태 복사, 이전·복원 검증 절차.
- [Checkpoint·재시작·GC 운영 절차 초안](OPERATIONS_RUNBOOK_DRAFT.md): 진행 기록·정상 종료·장애 복구·원본 삭제 전 조건과 중단된 삭제의 복구를 구분.
- [문서·설정 오프라인 일관성 점검](analysis/operations-offline-review.md): 발견한 누락·수정·검사 범위와 남은 운영 구현을 정리.
- [Arc identity 확인 결과](analysis/arc-testnet-identity.md): genesis/network ID 확정, fork ID는 공개 소스 계산 후보와 실제 peer 미확인을 구분.
- 현재 구현 계약: [feed 앱 실행](FEED_APP.md), [윈도우 저장](WINDOW_STORAGE.md), [DB 설정](CONFIG.md)

YAML의 `UNCONFIRMED_*`는 수동으로 검증된 값으로 교체할 표시이며 환경변수 문법이 아니다. 숫자 필드에도 이 문자열을 넣어 원본 초안의 typed decoding이 실패하도록 했다. placeholder를 채웠다는 사실만으로 운영 준비가 끝난 것은 아니다. 이 문서에는 실행·포맷 명령을 제공하지 않는다.

## 반영한 운영 기준

| 항목 | 초안 값과 적용 범위 |
| --- | --- |
| 대상 | StableNet 8283, BSC 97, Sepolia 11155111, Arc 5042002 테스트넷 |
| 구성 | 체인마다 `node.role: all` 단일 체인 프로세스 하나(그 체인용 바이너리); Arc만 `arc.eth68`, 나머지는 기존 RPC |
| 공개 RPC | 기존 네 endpoint 유지; 현재 가용성/제한/상태 보존은 이번에 조회하지 않음 |
| RPC 예산 | **체인별 공유 5 HTTP/s**; 수집·fresh·보충 조회·failover/재시도를 포함하며 worker별 추가 예산 없음 |
| DB | 새 전용 PostgreSQL 16 인스턴스, 새 DB 이름 제안 `indexer_testnets_v1`, 체인별 schema |
| DB 연결 | `database.postgres.max_conns: 4`는 **체인 프로세스별 Store pool 전체 상한**; 네 프로세스의 합계 예산16개 |
| 서버 연결 | 전용 PostgreSQL의 `max_connections: 32`; indexer16개 외 여유는 관리/점검용이며 추가 worker 예산이 아님 |
| 내구성 | 원본 fsync, PostgreSQL `fsync/synchronous_commit/full_page_writes: on` |
| 정확성 | source와 writer 각각 커밋 직전 fresh canonical 검사, RPC 후 epoch·transaction fence 유지 |
| 원본 보관 | 최소24시간; 필수 원천 DB 저장·raw 소비자 ACK·finality·lease·삭제 fence를 만족한 자료만 삭제 가능 |
| 최초 삭제 정책 | 고정 authority label 제안 `arc-testnet-public-rpc-v1`, 초기 `collect: false` |
| API | 체인마다 컨테이너 listener `0.0.0.0:<port>`, 호스트 공개는 `127.0.0.1:<port>`만을 제안(StableNet 8088, BSC 8089, Sepolia 8090, Arc 8091); 한 주소로 묶는 reverse proxy는 없음; GraphQL 깊이/복잡도·keepalive 유지 |
| Docker 로그 | 이 프로젝트 다섯 서비스에만 `local` driver·파일당10m/최대3개 회전 설정; Docker Desktop 저장 영역 사용량은 외장 APFS 여유와 별도 |

연결4개는 수집·writer·API가 함께 나누는 pool이다. 다른 indexer/API 프로세스를 추가하면 각 프로세스가 새 pool을 만들므로 합계가 증가한다. 이번 초안에는 별도 API 프로세스를 추가하지 않는다. 체인 프로세스마다 단일 체인 root API이므로 예약1개/유휴 연결 차용 동작이 각 프로세스에 적용된다. 이 조합을 실제로 측정하지는 않았다. SQL 값의 파라미터 바인딩은 기존 구현을 유지한다.

16개는 단일 실행 예산이며 장애 시까지 포함한 전역 hard cap이 아니다. 같은 체인의 프로세스를 겹쳐 실행하지 않고, 종료·반납 완료를 확인한 후 재기동해야 한다. RPC5도 기존 live pool의 공유 예산이다. 전체 중단/재개 interlock은 미구현이며 실제 중복 실행/초과를 이번에 관측한 것은 아니다.

체인마다 프로세스를 나누면서 멀티체인 manager의 자동 재시작(`auto_restart`)이 없어졌다. 실패한 체인은 운영자가 다시 시작할 때까지 멈춰 있고, 다른 체인은 영향을 받지 않는다. worker5분/30분 경보·capacity 중단/재개는 별도 미구현 감시 정책이다. Arc `queue_blocks: 256`/256MiB는 writer의 RAM 큐이고 raw 알림 채널·24h 디스크 backlog와 별개다.

PostgreSQL schema 기준 이름은 `indexer_testnets_v1`이며 체인별 설정이 각각 `indexer_testnets_v1_stablenet_testnet`, `indexer_testnets_v1_bsc_testnet`, `indexer_testnets_v1_sepolia`, `indexer_testnets_v1_arc_testnet`을 지정한다(이전 멀티체인 구성이 만들던 이름과 같다). Docker 초안의 DSN은 전용 내부 network의 `postgres:5432/indexer_testnets_v1`, 비관리자 `indexer` role과 `sslmode=disable`을 제안한다. 외부 DB용 TLS 구성으로 재사용하지 않는다. 실제 자격 증명과 비공개 파일 경로는 미확정이다. DSN 값은 secret 파일에서 `INDEXER_DB_POSTGRES_DSN`으로 주입하고 Git에 기록하지 않는다. PostgreSQL 설정 조각만으로 인증·접속 경로·컨테이너를 구성하거나 시작하지 않는다.

## 외장 저장소와 용량

실물 디스크 후보는 **X31**이다. 직전 읽기 전용 확인의 여유 공간은 약506GiB, 파일시스템은 **exFAT**이었다. 이것을 이미 확보된 APFS 공간으로 취급하지 않는다. 기존 자료의 보존 방법과 전용 APFS 저장소 확보 방법을 정해야 하며, 포맷·파티션 변경·파일 이동은 보류한다. 내부 여유 공간은 약48GiB였으므로 원본과 영구 DB를 모두 외장에 두는 안이다. 이 수치는 당시 여유 공간으로, 물리 예약이나 내구성 검증 결과가 아니다.

| 용도 | 제안 호스트 경로 | 제안 컨테이너 경로 | 상태 |
| --- | --- | --- | --- |
| APFS 실제 mount root | `/Volumes/IndexerData` | indexer의 `/data/feed` | **미확정·미생성** |
| Arc raw window | `/Volumes/IndexerData/indexer/window/arc` | `/data/feed/indexer/window/arc` | 경로 제안만 |
| PostgreSQL PGDATA | `/Volumes/IndexerData/indexer/postgres/16` | DB 컨테이너의 `/var/lib/postgresql/data` | 경로 제안만 |
| Arc peer identity | `/Volumes/IndexerData/indexer/identity/arc-peer.key` | `/data/feed/indexer/identity/arc-peer.key` | 키 미생성; 일반 파일0600 필요 |

두 컨테이너의 mount는 각 용도에 맞춰 별도로 구성한다. Docker Desktop 전체 데이터 위치나 다른 프로젝트의 volume을 옮기지 않는다. [Docker 초안](../deployments/testnets/README.md)은 APFS VolumeUUID·physical-store partition UUID·외장 physical disk·ownership·여유100GiB와 경로/secret/연결 예산을 검사하는 호스트 진입점을 포함한다. 모든 bind는 `create_host_path: false`이며 누락 경로를 자동 생성하지 않는다. **구성·검사 코드는 작성했으나 실제 APFS/컨테이너/DB 기동으로 검증하지 않았다.** 기본 모드는 오프라인 lint, `--check-host`는 읽기 전용이며 별도 `--start`는 실행 보류 해제 이후의 진입점이다. 직접 Compose 기동은 호스트 검사를 우회하므로 사용 경로로 삼지 않는다. PostgreSQL이 보는 Linux 컨테이너 경로와 macOS 호스트 경로를 혼용하지 않는다.

호스트 전용 private directory0700의 config·DSN·앱/관리자 password 파일0600을 각각 마운트한다. DSN 값은 Compose 환경 정의/인자/로그에 넣지 않고 indexer entrypoint에서 secret 파일을 읽어 기존 `INDEXER_DB_POSTGRES_DSN`으로만 전달한다. PostgreSQL은 별도 비관리자 앱 role을 사용한다. 현재 파일들은 생성하지 않았다. 호스트 UID/GID 매핑, 초기 SQL role 생성, Docker Desktop의 APFS sharing/fsync는 실제 실행 보류 해제 후 확인해야 한다. 기동 전 UUID 검사는 장치 분리 이후 PostgreSQL을 정지시키는 실시간 watchdog이 아니며 원본 저장소의 기존 runtime guard는 유지한다.

| 항목 | 값 | 강제 여부 |
| --- | --- | --- |
| raw volume 전체 예산 | 160GiB = 171798691840 bytes | window manager의 논리 한도; 물리 preallocation 아님 |
| Arc raw window | 128GiB = 137438953472 bytes | 체인별 논리 한도; 전체 예산의 나머지는 관리/미할당 여유 |
| 원본 쓰기 최소 여유 | 100GiB = 107374182400 bytes | raw append/seal의 filesystem 여유 검사 |
| PostgreSQL 데이터·색인·WAL | 160GiB 계획 예산 | **자동 quota/삭제/DB 쓰기 제한 아님**; [감시·중단 기준 초안](OPERATIONS_CAPACITY_DRAFT.md)은 작성했으나 자동 제어는 미구현 |
| raw retention | 24h | 최소 보관 시간; 삭제 조건 충족까지 더 오래 보존 |

전체 raw160 + DB계획160 + runtime 최소 여유100 = **420GiB**는 코드의 floor만 반영한 산술이다. [용량 정책](OPERATIONS_CAPACITY_DRAFT.md)의 재개 여유 **150GiB 초과**까지 두 예산을 보수적으로 합산하면 **470GiB 초과에 filesystem/관리 여유를 더한 계획**이 필요하다. 현재 X31의 과거 exFAT 여유로 APFS 공간이 확보됐다고 보지 않으며 APFS 확보 후 실제 가용 공간을 다시 확인해야 한다. 어느 합계도 물리 예약·지속 용량 보장이 아니다. 같은 물리 디스크에서 raw와 DB가 I/O를 공유하므로 이 배치의 처리량 우위는 검증하지 않았다. raw의100GiB floor가 PostgreSQL의 공간 소비까지 차단하는 것은 아니다. 영구 DB는 원본 윈도우와 달리 계속 증가한다.

초당2블록·평균 원본256KiB라면 하루 약42.2GiB, 48시간 약84.4GiB다. 평균1MiB라면 하루 약168.8GiB여서 Arc128GiB를 넘는다. 색인/metadata와 fork 보관도 필요하므로 **128GiB가24시간·허용 중단 시간을 보장하지 않는다.** 실제 평균 크기와 증가량의 검증은 보류 상태이며, 용량 부족 시 미처리 자료를 버리거나 retention/fresh를 완화하지 않는다.

별도 영구 객체 디렉터리는 현재 앱 YAML 옵션이 아니다. 이번 초안에서 영구 source facts·projection은 PostgreSQL에 저장하며 제안 단계의 `objects_path`/`feed_storage`를 추가하지 않는다.

## 미확정 값과 확인 주체

| 구분 | 미확정 항목 | 확정 방법/담당 |
| --- | --- | --- |
| 운영자 선택 | X31 사용 가능 여부, 기존 자료 보존, 전용 APFS 확보 방식·실제 mount root | 사용자가 저장소 사용 범위를 결정; 현재 exFAT을 포맷하지 않음 |
| 운영자 선택 | 최대 허용 중단·복구 시간, 장기 DB 증설/백업 방침 | [정책 초안](OPERATIONS_CAPACITY_DRAFT.md)은 worker 적체5분/30분·운영자 대응15분·2시간 내 복구 방침 결정을 제안; [백업 초안](OPERATIONS_BACKUP_DRAFT.md)이 등급별 RPO/RTO·WAL 보관소·주기를 제안. RPC 노드 상태 보관 범위와 실제 복원 가능 시간은 미확정 |
| 접속 입력 | Arc 실행 계층의 실제 static enode | 현재 없음; 네트워크 운영자가 제공한 연결 가능한 peer 필요 |
| 접속 입력 | PostgreSQL 자격 증명과 비공개 DSN 파일/host manifest 경로·운영 이미지 | Docker 내부 DB/role은 초안에 제안; 실제 값은 운영 적용 전 확정하고 키/비밀번호를 문서에 기록하지 않음 |
| 확인 완료 | Arc genesis hash·network ID | 2026-10-19 공개 RPC·공식 소스로 확인해 YAML에 반영; [근거](analysis/arc-testnet-identity.md) |
| 기술 검증 | 실제 peer fork hash/next | 공개 감사 소스 계산 후보는 `910609e1/0`; RPC 배포 commit 소스404·peer 미연결이므로 YAML은 미확정 유지 |
| 기술 검증 | Arc finalized 높이 `H`·hash, 시작 높이 `H+1` | 실제 활성화 직전 같은 RPC 신뢰 정책으로 검증해 기록 |
| 기술 검증 | 나머지3체인의 시작 높이 | 새 DB 시작 시 최근 확정 기준과 부모 상태 조회 가능성을 확인해 정함 |
| 생성 작업 | peer key0600, volume marker/ledger와 DB binding | 경로/정책 확정 후 최초 초기화 단계; 지금 생성하지 않음 |
| 정책 전환 | `collect: true`로의 변경 | 아래 기능 확인 이후 별도 적용; 현재false 유지 |

volume ID `indexer-apfs-testnets-v1`과 authority label `arc-testnet-public-rpc-v1`은 초안의 고정 label이다. 물리 디스크 UUID나 독립적인 finality 증명으로 해석하지 않는다. 최초 binding 후 marker/ledger/DB 기록을 지우거나 label을 바꾸어 시작 오류를 우회하지 않는다.

## checkpoint와 Arc 활성화 순서

다음은 **후속 적용 순서이며 이번에 실행한 작업이 아니다.**

정상 종료·재시작·장애/GC의 상세 순서는 [운영 절차 초안](OPERATIONS_RUNBOOK_DRAFT.md)을 따른다. `collect: false`는 새 수거를 막지만 이미 승인돼 중단된 삭제는 앱의 window Open에서 완료할 수 있으므로 startup을 읽기 전용 점검으로 사용하지 않는다. 실제 GC 활성화는 현재 host guard도 거부하므로 별도 변경·검증이 필요하다.

1. 저장소·DB 접속·peer를 확정한다. 신규 DB/schema를 사용하고, 다른 위치에서 기존 Pebble/PostgreSQL DB가 발견되면 보존한다. 현재 설정의 과거 시작 높이를 새 checkpoint로 복사하지 않는다.
2. 실제 시작 직전에 Arc chain ID·genesis·RPC finalized 블록 `H`와 hash·peer fork 정보를 확인한다. YAML에서 `anchor_height=H`, `anchor_hash=해시`, Arc `start_height=H+1`을 함께 고정한다. anchor 자체의 source facts는 수집된 것으로 간주하지 않는다.
3. 나머지3체인은 기존 RPC ingest를 유지하고 새 시작 높이를 정한다. strict 상태 기능에 필요한 부모 상태를 읽지 못하면 시작을 중단한다. latest 대체나 0 잔액으로 우회하지 않는다.
4. Arc만 execution-only feed로 시작한다. `balance.native`, `token.metadata`, `dex.orderbook`는 Arc override에서 끈다. 다른 state-reading feature를 추가하면 기본 plan은 시작을 거부한다. 선택 proof/trace/추가 projection worker는 이번 초안에 포함하지 않는다.
5. 승인된 최초 초기화 때만 새 빈 저장소에 `initialize_volume/create_window: true`를 적용한다. 현재 초안은 둘 다false다. 최초 binding 후 false로 유지하고 동일 DB/window/identity로 재시작한다. 디스크 소실 시 원본을 재생성하거나 내부 저장소로 전환하지 않는다.
6. 최초 저장·정상 종료·durable cursor 재시작·원본 누락 시 거부·ACK/finality 삭제 조건을 기능 검증한다. **성능/장기 측정의 자동 재개는 포함하지 않는다.** 이후 고정 authority의 RPC-finalized 신뢰 정책을 유지한 채 `collect: true` 적용 여부를 정한다. false를 영구 설정으로 두면 원본은 한도까지 계속 쌓인다.

재시작은 DB의 durable cursor를 사용한다. 매번 최신 finalized로 anchor를 옮기거나 미처리 높이를 건너뛰지 않는다. source와 projection의 commit fresh 검사를 각각 유지하며 `indexer.finality: head`는 DB capture prefix를 처리한다는 뜻이다. RPC-finalized 신뢰 정책은 독립 CL certificate 검증이 아니다. bootstrap 아래·undo 범위 밖의 reorg는 자동 최신 점프 대신 중단/명시적 재구성 대상이다. raw24h 보유가24h reorg undo를 의미하지 않는다.

Arc 기본 plan은 header/body/transaction/receipt/log 범위다. 모든 account/code/storage 상태와 internal call의 완전한 수집을 뜻하지 않으며, raw24h만으로 사라진 과거 EVM 상태가 복구되지 않는다. 실제 enode가 없으면 Arc feed는 활성화할 수 없고, enabled Arc entry를 남긴 혼합 구성은 전체 preflight를 통과할 수 없으므로 이 초안을 나머지3체인용 실행 파일로 사용하지 않는다.

## 초안 검토 범위

최초 초안에서 YAML 중복 key·11개 미확정 placeholder·보호값·경로/용량 관계·PostgreSQL 조각과 문서 링크 대상을 확인했다. Go1.24.9의 실제 `config.Load`는 원본 초안의 숫자 placeholder를 거부했고, 별도 임시 가상 값 대입본은 strict known-fields·설정 validation·Arc 모듈의 순수 validation·window 옵션 검증을 통과했다. DB/RPC/peer/window를 여는 앱 함수는 호출하지 않았다.

후속 identity 확인에서는 공개 RPC를 읽기 전용2배치로 조회하고 공식 고정 소스를 대조했다. genesis/network ID2개를 확정 반영해 **현재 placeholder는9개**다. fork는 계산 후보를 기록하되 실제 peer 값의 확정을 보류했다. 최초 초안 검증과 후속 공개 RPC 조회의 범위를 구분하며 성능/장기 측정은 재개하지 않았다.

가상 값은 실제 checkpoint나 실행 설정이 아니며 결과를 peer/mount/DB 동작 또는24시간·450ms 성능 증거로 사용하지 않는다. 후속 Docker 초안은 오프라인15개 검사·Compose config 구문·셸 구문을 통과했으며 실제 APFS/DB 초기화는 미검증이다. 실제 `config.yaml`과 코드·설정·의존성 등1094개 보호 파일의 해시 일치를 확인했다. 다른 프로젝트와 기존 Docker 자원은 변경하지 않았다. 검토 기록은 [최초 설정 초안](analysis/work-log-2026-10.md#391-네-테스트넷-운영-설정-초안-1019)과 [Docker 초안 작업 로그](analysis/work-log-2026-10.md#393-docker-운영-구성apfs-검사비공개-dsn-초안-1019)를 따른다.

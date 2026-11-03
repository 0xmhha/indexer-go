# 운영 설정 준비 현황

**배포 판정: HOLD.** 운영 입력은 제공/검증되지 않았으며 아래 값은 권장안이다.
현재 코드의 구현 범위는 [문서 색인](README.md), 미완료 작업·우선순위는
[남은 작업](analysis/open-items.md), 개발 인계 상태는 [SESSION_STATE](SESSION_STATE.md)가
관리한다. 이 문서에 완료 PR·세션 로그·과거 측정치를 누적하지 않는다.

## 입력 상태

| 입력 | 현재 판정 | 다음에 필요한 것 |
| --- | --- | --- |
| 전용 외장 APFS | HOLD · 준비된 실제 경로 미제공/미검증 | 실제 mount·VolumeUUID/physical-store·ownership와 경로별 예산 |
| Arc peer·시작점 | HOLD · 공식 EL enode와 인증 checkpoint 미제공/미검증 | 접속/fork/history 조건, full state H/hash/root/coverage와 인증 주체 |
| 배포·DB | HOLD · private host/App 선언·image digest·역할/schema 미확정 | 지원 schema·비밀 파일 경로·UID/IPC/mount·준비된 전용 PG |
| WAL·백업 | HOLD · 별도 장치/custody·snapshot/WAL/복구 증거 미확정 | 인증된 matched backup와 실제 복구 검증 범위/RPO/RTO |

새 운영 입력이나 실제 관측은 추가되지 않았다. 마지막 지정 경로/identity 확인은
2026-11-01이며 그 당시 경로 부재나 외장 후보 exFAT를 현재 APFS 상태로 추정하지 않는다.
현재 디스크 여유·호스트 부하·Docker/RPC/DB/peer를 재측정하지 않았다. 개발용
config.yaml을 PostgreSQL 운영 설정으로 교체하지 않았고 비밀 값은 기록하지 않는다.

## 경로와 용량 결정표

아래 경로는 **제안**, 할당·생성·mount된 사실이 아니다. 실제 외장 APFS root와 배치 예산을 운영자가 정해야 한다.

| 용도 | 제안·기존 초안 | 남은 결정 / HOLD 이유 |
| --- | --- | --- |
| legacy raw window | root 아래 `indexer/window/arc`, volume160GiB / Arc128GiB, 최소24h | [legacy YAML](examples/testnets/arc-testnet.draft.yaml)용 논리 한도. native spool 예산과 혼합하지 않음. 24h 수용 보장 없음 |
| native DB | `<root>/indexer/native/<chain>/node` | `<chain>`별 전용 parent를 두고 진단에는 parent를 mount. 실제 root·크기·증가 예산 미확정. mount point 자체를 native directory로 사용하지 않음 |
| 실행 source spool | 전용 부모 `<root>/indexer/source/<chain>` 아래 `spool` | [runtime 초안](../deployments/native/runtime/README.md)은 부모를 `/source` RW mount, spool을 `/source/spool`로 선언. 관리형 Arc scratch는 현재 코드가 spool 부모 `/source`에 만들며 spool quota 밖의 별도 용량이다. 별도 `/scratch` mount·설정 지원은 없다. native와 분리하며 실제 quota·scratch 예산 HOLD. 관리형 source 기본256MiB/4096 records는 24h 보장 아님. 기존 경로 이동 없음 |
| checkpoint·seed·export / 별도 도구 scratch | `<root>/indexer/checkpoints/<chain>`, `<root>/indexer/scratch/<chain>`, `<root>/indexer/exports/<chain>` | native parent/source와 분리. 인증 full baseline·retained seed·출력 예산/보관/소유권 HOLD. partial proof를 full baseline으로 사용 불가 |
| PGDATA | root 아래 `indexer/postgres/16`, 계획160GiB | 데이터·색인·WAL 포함 계획이며 quota 아님. Docker 컨테이너의 `/var/lib/postgresql/data`와 호스트 경로 구분 |
| identity·private 설정 | `<root>/indexer/identity/<chain>-peer.key`; repo·데이터 볼륨 밖의 전용 private 설정/상태 디렉터리 | 디렉터리0700/파일0600, non-root 소유권 권장. 진단/Indexer UID 후보501:20이며 매핑 확인 전 HOLD. PostgreSQL 서비스 UID/PGDATA 소유권은 별도 HOLD. key/설정 생성·이동 없음 |
| 진단 입력·IPC | 비밀 없는 `<root>/indexer/inputs/<chain>`; socket은 데이터 볼륨 밖의 체인별 private runtime 디렉터리 | inputs/source/native parent/exports는 서로 분리. DSN은 진단 bundle에 복사하지 않음. 실제 socket 길이·UID·host/container 공유 경로 HOLD |
| capacity reserve | legacy raw floor100GiB, stop120GiB, warn150GiB, resume150GiB 초과 | 초안 권고·한정된 guard 구현. raw160+PG160+floor100=420GiB; resume까지는470GiB 초과 + 관리 여유. 물리 예약 아님 |
| native COW·checkpoint | 동일 filesystem에서 최소 `2 × logical native bytes + ReserveBytes` free, 기본 ReserveBytes1GiB | [Arc retention](EXECUTION_ARC_RETENTION.md) 기준. snapshot/catalog/secondary scratch·정상 증가·백업 공간은 추가. catalog16GiB/출력64GiB 상한도 별도 고려. legacy470GiB에 이미 포함된 것으로 계산 금지 |
| 백업·WAL | 데이터 디스크와 다른 장치 | 장치 identity·실경로·용량·암호화/권한·책임자·오프사이트/검증 보관 모두 unknown |

파일시스템/PGDATA/raw guard·latch와 호스트 watcher 중단, PostgreSQL 크기 journal은 구현돼 있다. [DB/WAL 관측](NATIVE_MONITORING_INTEGRATION.md)은 opt-in/disabled sink이며 [VM probe/health](NATIVE_VM_OBSERVATION.md)는 injected 라이브러리다. 실제 Docker 실행기/App 연결·durable crash admission·watcher 재시작/전달·부팅 자동 시작은 미구현/보류다. 실제 외장 APFS/운영 DB의 장애·종료·복구 보장은 검증하지 않았다. `--check-host`도 container를 실행하므로 순수 metadata 점검으로 취급하지 않는다.

## 운영 권장안 — 아직 미적용

| 결정 | 권장안 | 적용 상태 |
| --- | --- | --- |
| 배포 단위 | StableNet·BSC·Sepolia·Arc의 체인별 App 하나, 체인별 PostgreSQL schema. 같은 체인의 legacy/native writer를 겹쳐 실행하지 않음 | 계획 확정. 실행 보류. native Arc 전환은 아래 인증 시작점 확보 후 별도 구성 |
| DB 방식·연결 | 전용 PostgreSQL16 하나, 각 App pool4/part2/API예약1. 별도 write pool 없음. 4개 App16 + 체인 하나의 선택 API pool4 + 선택 점검 pool4 = 최대24, 서버32를 초기 계획으로 두고 나머지8은 관리·migration 여유 | 실제 서버 설정·역할·동시 연결과 메모리 검증 HOLD. API가 4개 체인 Store를 모두 열면 추가16이므로 이 예산을 초과함. 일곱 writer는 논리 작업 단위 |
| 디스크 배치 | APFS root 후보 `/Volumes/IndexerData`와 위 체인별 분리 경로. Arc의 sibling `.<NAME>.execution-retention`을 포함하는 native parent를 보존 | 실제 root·UUID·ownership·Docker 공유 경로 HOLD. X31 재포맷·데이터 이동 없음 |
| Arc 원본 보존 목표 | 사용자가 제시한 **초당2블록 가정**에서 24h = `2 × 86400 = 172800`블록. native retention `KeepBlocks` 계획값172800, 실제 spool bytes/records는 별도 용량 예산 필요 | 제안값이며 config/default 변경 없음. 현재 체인 속도를 재측정한 값이 아님 |
| 시작 방식 | Arc는 인증된 full checkpoint H/hash/root/coverage와 일치하는 빈 indexer schema에서 H+1부터. 기존 정상 운영 상태가 생긴 뒤에만 일치하는 source/native/schema로 resume | checkpoint·공식 peer·start 모드 실제 선언 HOLD. genesis 재실행이나 latest 점프를 대체값으로 사용하지 않음 |
| 비공개 값 | legacy 운영 경로의 DSN_FILE 주입 유지, 비밀 설정은 repo·데이터 볼륨 밖에 보관. 진단 Docker는 network-none/read-only/no-DSN 유지 | private 실경로·DSN·peer key·검토한 image digest HOLD. checked-in placeholder 그대로 유지 |

`execwire/retention.DefaultKeep`는 **86400블록**이며 시간 설정이 아니다. 초당2블록 가정에서는 약12시간이다. legacy raw의 `retention: 24h`, native `KeepBlocks`, source spool의 bytes/records, 메모리 scheduler의 bounded queue는 서로 다른 제한이다. 메모리 큐를 172800으로 늘리는 권장이 아니다. 미처리·미ACK·보호 구간은 window 목표 때문에 삭제할 수 없으며, full baseline·현재 permit/lease·source exact ACK 등 기존 E3 조건을 모두 유지한다. native current state/code와 보조 색인 크기까지 24h 안에 고정된다고 약속하지 않는다.

용량은 체인별 **native current+retained state**, source spool, PGDATA/index/WAL, checkpoint/catalog, scratch/export, 같은 장치에 남기는 복사본을 각각 합산한다. 그 위에 유지보수 시 `free >= 2 × logical native bytes + ReserveBytes`를 만족하는 headroom과 정상 증가/중단·재개 여유를 남긴다. 유지보수·export를 한 번에 하나씩 수행하는 운영 방침을 권장하지만 자동 직렬화가 배포된 사실은 아니다. 같은 APFS container의 free를 체인별로 중복 할당하거나 logical quota를 물리 예약으로 계산하지 않는다.

실제 native/spool 증가량이 없으므로 native 총 필요량·충분한 디스크 크기·24h 수용 여부는 **HOLD**다. 추가 측정 대신 이 값을 추측하지 않는다. legacy raw160GiB·PG160GiB·여유150GiB의 470GiB 초과 예산은 legacy 초안 비교용이며 새 native 계획의 합계가 아니다. 기존 외장 후보의 과거 exFAT 여유를 새 APFS에 확보된 공간으로 계산하지 않는다. 별도 장치의 WAL/백업 예산도 아직 없다.

## 연결과 정확성 — 유지할 값

- `database.postgres.max_conns: 4`: 체인/프로세스 Store 전체 pool. 네 프로세스면 명목16개. `max_connections: 32`는 전용 서버 초안 권고이며 배포 확인값이 아니다.
- 추가 ingest/API/replica 프로세스마다 pool이 추가된다. 네 프로세스 + API 하나라면20개, 네 프로세스 전체를 두 벌 겹치면32개에 관리·migration·점검 연결이 더해진다. 실제 프로세스 수·서버 전체 합계/예약은 HOLD다. 재시작 중복도 계산해야 한다.
- 실행 경로는 part 동시성2와 API 예약1을 기존 pool4 안에서 유지한다. 일곱 logical writer는 일곱 연결이 아니다. 별도 write pool 없음.
- 공유 RPC **5 physical HTTP attempts/s**를 유지한다. retry/failover/batch fallback도 포함하며 worker별 증액 없음. 논리 call quota나 새 프로세스 전체의 전역 제한으로 해석하지 않는다.
- 공개/COMMIT 바로 전 **NEW uncached canonical fresh → 응답 후 generation fence → COMMIT**을 유지한다. SQL 값은 bound parameter, 동적 식별자는 validated/quoted. 두 EVM 실행과 upstream clients는 그대로다.
- E1~E4, full baseline, E3 exact current DB pending permit/live lease/sourceACK/window/intent/completion/release, E6 인증 trust/fork 경계를 유지한다. 공간 부족·누락 입력은 최신 점프·ACK 위조·guard 완화의 근거가 아니다.

## checkpoint·시작점·peer 결정표

| 항목 | 확인·권고 | 미확정 외부 입력 |
| --- | --- | --- |
| Arc identity | [pinned identity 근거](analysis/arc-testnet-identity.md)의 chain/network5042002, genesis `0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f` | 고정된 소스/RPC 근거이며 현재 peer 인증이나 재조회가 아님. 실제 연결할 공식 EL peer의 identity 일치 확인 필요 |
| EL peer/fork | fork 후보 `910609e1` / next0은 과거 소스 계산값 | 공식 온보딩 enode·접속 조건·배포 소스/fork 광고·history 보존 범위 unknown. 추측·Discord 연락·peer 연결 없음 |
| legacy 시작 | RPC finalized H/hash와 start H+1을 맞추는 초안 | H/hash 미정. 과거 관측 head를 checkpoint로 복사하지 않음 |
| native 실행 시작 | [bootstrap](EXECUTION_BOOTSTRAP.md), [Arc collector](EXECUTION_ARC.md)의 인증된 expected manifest·full state root/coverage·일치하는 native/source/schema | checkpoint origin/height/hash/root·인증 주체/정책·start 모드·기존 anchor와 보존 floor 일치 unknown. retained cutoff seed는 원래 anchor를 이동시키지 않음 |
| StableNet E6 | [지원 정책](EXECUTION_CONSENSUS.md) `stablenet-wbft-4d17eea018c8`, 공식 pinned WBFT·bounded Anzeon 구간 | 전체 정책·chain/genesis·AnchorHeader·Applied/Next validator sets의 외부 인증·일치 checkpoint 필요. fixture anchor를 운영값으로 사용 불가 |
| 나머지 RPC 시작 | 최근 확정 기준과 strict 부모 상태 조회 가능성 필요 | 실제 시작 높이와 provider 상태 보존 계약 unknown. 10/22 retention/speed는 현재 보장 아님 |

[기존 운영 초안](OPERATIONS_DRAFT.md)의 `source.feed.module: arc.eth68`는 header/body/receipt 중심 legacy baseline이다. 새 native execution/collector·checkpoint·E6 경로는 opt-in이고 [legacy Compose 초안](../deployments/testnets/compose.draft.yaml)에 native App/collector/executor 운영 서비스를 추가하지 않았다. 별도 [진단 전용 Docker 초안](../deployments/native/README.md)은 설정 검사와 읽기 전용 metadata 명령만 연결하며 네트워크·DSN·DB 의존성과 실제 실행은 없다. 옛 YAML을 native 완성 배포로 이름만 바꾸거나 실제 config를 교체하지 않는다.

[Native runtime 구성 초안](../deployments/native/runtime/README.md)은 Arc App·StableNet App·StableNet executor의 세 서비스와 준비된 외부 PostgreSQL16을 선언한다. Arc App이 ETH68/raw 수신·Rust 첫 실행·in-process collector/소유 Rust 두 번째 실행을 관리하므로 외부 Arc collector는 없다. Arc App은 native 부모 RW·source 부모 RW·독립 raw volume RW·private peer key RO와 DB/RPC/peer egress를 필요로 한다. `/native/node` 개별 bind는 COW 후 예전 inode를 유지할 수 있어 제거했다. journal과 run lock은 native 부모에 보존한다. StableNet executor는 별도 import/replay 소유권을 유지한다. 두 번의 EVM, 공유 RPC5·기존 Store4와 정확한 ACK 권한은 동일하다. [N5](codemine/runs/20261030-arc-native-n5/plan.md) 구현·초안 정합성과 N6 통합 acceptance는 구분한다. runtime checker는 선언만 검사하며 실제 App YAML·host·image·prepared DB·readiness를 검사하거나 startup interlock을 제공하지 않는다. 기존 Native 파일 preflight/diagnostics의 legacy PASS는 관리형 App raw/SQL/journal 통합 검증을 대체하지 못한다.

## 백업 custody와 복구 판정

[백업 초안](OPERATIONS_BACKUP_DRAFT.md)의 운영자 수용값은 **archive_timeout5분, 기준 백업 매일·7개, 정지 복사 매주·4개 및 큰 변경 전**이다. 수용한 방침이며 PostgreSQL 설정 적용·아카이브 실행 사실은 아니다. WAL 목적 장치·실경로·용량·암호화·secret 저장소·보관 책임자와 RPO/RTO 실증은 미확정이다.

#230은 matched native/source/indexer seal과 restore **admission**을 제공한다. [backup 계약](EXECUTION_ARC_RETENTION.md#matched-backup-and-restore-admission)의 `BackupOutput`, `IndexerReceipt`, `IndexerArtifact`는 실제 외부 PG snapshot + 필요한 WAL 전체를 복원할 수 있다는 독립 인증을 요구한다. 임의 SQL 파일의 digest로 대체할 수 없다. publication/generation/permit/full baseline/root/retained floor/WAL boundary가 정확히 맞아야 한다. 실제 PG snapshot 획득·WAL 복구·설치·원래 schema 인증과 node/source 재활성화는 별도 보류이며 이 릴리스에서 실행하지 않았다. 단순 stopped-copy를 유일한 구현 경로로 단정하지 않고, 서로 다른 시점의 DB/native/spool을 임의로 조합하지 않는다.

## 실제 값이 필요할 때 해제할 HOLD

권장 경로 배치·DB 방식·프로세스 계획·24h 산식은 위와 같이 선택했다. 실제 APFS mount/UUID/ownership와 native/spool/checkpoint/PGDATA 예산·COW headroom, private 실경로·자격 증명·reviewed image digest, 공식 EL peer와 인증 checkpoint/E6 trust, 별도 WAL/백업 장치와 custody/RPO/RTO가 남는다. 파일 예제·fixture·과거 head를 채워 HOLD를 해제하지 않는다. [Native preflight](NATIVE_EXECUTION_PREFLIGHT.md)의 declaration은 실제 값을 확보한 뒤 해당 schema로 작성하며, 이 문서는 새 실행 가능한 manifest가 아니다.

관리형 진단과 N6 owned 통합 acceptance는 구현·검증됐다. 다음은 실제 APFS·peer·인증 checkpoint·private 배포 및 matched backup/restore 입력 확보와 검토다. 미확정 값을 추측해 채우거나 같은 측정을 반복하지 않는다. 기존 N0~N5 기능·동시성 검사를 새 실패 없이 반복하지 않는다. 실제 저장소·공식 EL peer·인증 checkpoint·private 설정/이미지는 계속 HOLD이며 반복 요청하거나 운영/성능 확인 루프를 만들지 않는다. [최신 통합 검증 계획](codemine/runs/20261031-arc-native-n6/plan.md)을 따르고 수용한 E1/first-import 계획을 유지한다. 운영 적용·provisioning·GC/restore activation과 추가 성능 측정은 보류한다.

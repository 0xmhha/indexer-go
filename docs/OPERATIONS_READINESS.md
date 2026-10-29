# 운영 설정 준비 현황

**배포 판정: HOLD.** 운영 입력 권장안을 정리했으며 실제 APFS·접속·인증 값은 미확정이다. [Native 설정 통합 검사](NATIVE_EXECUTION_PREFLIGHT.md)는 명시적 파일을 오프라인 검증하며 PASS도 운영을 승인하지 않는다. 관측·권장안·미확정 값을 구분한다. 추가 성능 측정은 하지 않았다.

## 근거와 완료 범위

| 구분 | 확인 내용 | 근거와 한계 |
| --- | --- | --- |
| 완료 #227 | bounded Engine full-blob 공급 | [PR #227](https://github.com/0xmhha/indexer-go/pull/227), [동기화](../verification/priority1-sync.json). KZG는 beacon/PeerDAS 합의 검증이 아님 |
| 완료 #228 | call 범위·orphan trace | [PR #228](https://github.com/0xmhha/indexer-go/pull/228), [동기화](../verification/priority2-sync.json). 미수집 과거 coverage를 만들지 않음 |
| 완료 #229 | 지원 WBFT 독립 canonical/finality | [PR #229](https://github.com/0xmhha/indexer-go/pull/229), [동기화](../verification/priority3-sync.json). 인증된 정책·anchor와 지원 fork 범위만 |
| 완료 #230 | native lookup/MDBX 회수, matched backup/restore admission | [PR #230](https://github.com/0xmhha/indexer-go/pull/230), [CI5](https://github.com/0xmhha/indexer-go/actions/runs/40418831898), [동기화](../verification/priority4-sync.json), [검증](codemine/runs/20261027-native-retention-backup/validation.md). 외부 PG snapshot/WAL 생성·인증·실제 복원은 별도 |
| 이번 관측 | 아래 저장소·설정 metadata | 2026-10-29 01:27~01:35 KST. 지정 볼륨의 `diskutil info -plist`·`statvfs`, 경로 존재·mode, 허용한 설정 필드, UID/GID와 Docker context만 읽음. 각 값은 별도 관측이며 원자적 snapshot이 아님 |
| 완료 #236 | 오프라인 Native 모니터링 | [PR #236](https://github.com/0xmhha/indexer-go/pull/236), [최종 CI5](https://github.com/0xmhha/indexer-go/actions/runs/40704614928), squash/main `a49065d292258c0399296a5e1cc45989c295cbc8`. Arc/StableNet 파일 형식과 제출된 수치만 판정하며 실제 체인을 실행하지 않음 |
| 운영 미확인 | APFS·peer·checkpoint·비밀 파일·백업 보관소·복구 증명 | node/RPC/provider/운영 DB/Docker/container probe, 실행·provisioning·GC·복원·성능 측정 없음 |

해시 검증한 이전 최종 인계의 patch SHA256은 `6db158c8e392db38293ae91cb48b64b7a9457a5950fb87747795ad2777101c83`, 완료 보고는 `3ac41ad0cc39b3ab79591f2366fb5b90a769ccbd6f8c28c4d39f27baa7c689a2`, 동기화 원본은 `1704b5d005c4c87b72064a5c866edef537fa802e0efc32c32ccc90af2e551141`이다. 보존된 strict all-four observer 로그도 SHA256 `5e1cb69539c87365f0f8958b6862c409fdae0fc38c4fe52a953efca6dbe1fbb5`와 일치했다. 이는 **이전 릴리스의 증거**이며 이번 세션에서 native/PG/200 실험을 반복했다는 뜻이 아니다. 선택한 현재 문서와 휴대 가능한 영수증을 저장소에 반영했으며 원본 작업트리는 수정하지 않았다. `verification/releases.json`과 observer는 과거 machine-local 실행 ledger 형식으로 유지하고 현재 릴리스 현황은 이 표와 각 portable sync를 따른다. 해당 observer를 이번 문서 릴리스 뒤의 main 검증기로 재사용하지 않는다.

현재 n은 [N3 App-owned ingress](EXECUTION_ARC_INGRESS.md)이며 release gates 진행 중이다. n−1은 [N2 writer #243](EXECUTION_ARC_NATIVE_WRITER.md), n−2는 [N1 bootstrap #242](EXECUTION_ARC_BOOTSTRAP.md)다. [수용한 first-import 설계 #239](ARC_NATIVE_IMPORT_DESIGN.md)도 보존한다. [Native 운영 구성 초안 #238](codemine/runs/20261029-native-runtime-draft/plan.md)도 보존한다. [운영 입력 권장안 #237](codemine/runs/20261029-operations-inputs/plan.md)도 보존한다. [오프라인 Native 모니터링 #236](NATIVE_OFFLINE_MONITORING.md)은 이전 완료 참조로 유지한다. [백업 어댑터 #235](EXECUTION_BACKUP_ADAPTER.md), [Native 진단 Docker #234](../deployments/native/README.md), #230 native retention과 #229 E6· #228 call ranges 참조도 보존한다. [수용한 E1 설계](codemine/runs/20261023-e1-execution/02-design.md)와 [계획](codemine/runs/20261023-e1-execution/03-plan.md), [현재 backlog](analysis/open-items.md)를 보존한다.

## 현재 확인한 저장소와 설정

| 값 | 현재 확인 | 판정·운영자가 정할 값 |
| --- | --- | --- |
| 외장 후보 | `/Volumes/X31`, external `disk6s1`, exFAT | APFS가 아님. 기존 자료 보존·전용 외장 APFS 확보 방법 결정 필요. device 번호는 고정 identity가 아님 |
| X31 identity | volume UUID `C1B1E211-6452-39C0-AD82-831FDE1B0D8C` | exFAT identity이며 APFS UUID로 사용 불가. device 번호는 재연결 시 변동 가능 |
| X31 용량 | diskutil/statvfs 총 1,024,190,578,688 bytes, 가용 434,701,860,864 bytes ≈404.8GiB | 현재 한 번의 관측. 예약·지속 용량·fsync 증거 아님 |
| 외장 APFS root | `/Volumes/IndexerData` 없음 | mount/device/VolumeUUID/physical-store UUID·ownership 모두 HOLD. 내부 APFS로 대체하지 않음 |
| 기존 ignored config | 현재 checkout의 `config.yaml` 존재, mode0644; driver `pebble`, path `./data/multichain-testnets`, PostgreSQL max_conns 미지정; rpc.rate_limit5, indexer.finality head/workers2/poll_interval2s | 개발 설정이며 아래 PostgreSQL 운영 권장안 적용 사실이 아님. bounded 파일을 파싱해 허용 필드만 추출. endpoint/DSN 값은 출력·사용하지 않고 참조 데이터를 열지 않음. 파일·mode 그대로 유지 |
| private manifest | 현재 checkout의 `deployments/testnets/host.json`과 `deployments/native/host.json` 없음 | 후자는 탐색한 경로일 뿐 지원 입력 schema가 아님. 다른 위치 존재 여부는 unknown. [host 초안](../deployments/testnets/host.draft.json)의 private_dir/image/UUID는 placeholder |
| 호스트 사용자·Docker | UID501 / GID20, 현재 context `desktop-linux` | 호스트 metadata만 확인. container UID 매핑·APFS 소유권·daemon·이미지·bind mount는 검증하지 않음 |
| DSN·password·peer key | 실제 private 위치가 없어 존재·0600·소유자·자격 증명 유효성 unknown | 비공개 디렉터리0700/일반 파일0600 제안. 비밀 값은 문서·PR에 넣지 않음. 파일 생성 없음 |
| 운영 이미지 | reviewed tag/digest 미확정 | 이미지 선택 필요. pull/inspect/build/컨테이너 실행 없음 |

`diskutil`의 partition 크기와 파일시스템 총 크기는 다를 수 있다. X31의 현재 여유는 legacy 계획의 470GiB 초과에도 못 미치며, 포맷 후 용량이나 native 여유를 추정하지 않는다. 저장소 내부의 사용자 파일은 순회하지 않았다. 아카이브 장치는 unknown이다.

## 경로와 용량 결정표

아래 경로는 **제안**, 할당·생성·mount된 사실이 아니다. 실제 외장 APFS root와 배치 예산을 운영자가 정해야 한다.

| 용도 | 제안·기존 초안 | 남은 결정 / HOLD 이유 |
| --- | --- | --- |
| legacy raw window | root 아래 `indexer/window/arc`, volume160GiB / Arc128GiB, 최소24h | [legacy YAML](examples/testnets/arc-testnet.draft.yaml)용 논리 한도. native spool 예산과 혼합하지 않음. 24h 수용 보장 없음 |
| native DB | `<root>/indexer/native/<chain>/node` | `<chain>`별 전용 parent를 두고 진단에는 parent를 mount. 실제 root·크기·증가 예산 미확정. mount point 자체를 native directory로 사용하지 않음 |
| 실행 source spool | 전용 부모 `<root>/indexer/source/<chain>` 아래 `spool` | [runtime 초안](../deployments/native/runtime/README.md)은 부모를 `/source` RW mount, spool을 `/source/spool`로 선언. Arc scratch는 별도 `/scratch` mount·명시적 설정으로 분리. native와 분리하며 실제 quota·scratch 예산 HOLD. 기본4GiB/262144 records는 24h 보장 아님. 기존 경로 이동 없음 |
| checkpoint·seed·scratch·export | `<root>/indexer/checkpoints/<chain>`, `<root>/indexer/scratch/<chain>`, `<root>/indexer/exports/<chain>` | native parent/source와 분리. 인증 full baseline·retained seed·출력 예산/보관/소유권 HOLD. partial proof를 full baseline으로 사용 불가 |
| PGDATA | root 아래 `indexer/postgres/16`, 계획160GiB | 데이터·색인·WAL 포함 계획이며 quota 아님. Docker 컨테이너의 `/var/lib/postgresql/data`와 호스트 경로 구분 |
| identity·private 설정 | `<root>/indexer/identity/<chain>-peer.key`; repo·데이터 볼륨 밖의 전용 private 설정/상태 디렉터리 | 디렉터리0700/파일0600, non-root 소유권 권장. 진단/Indexer UID 후보501:20이며 매핑 확인 전 HOLD. PostgreSQL 서비스 UID/PGDATA 소유권은 별도 HOLD. key/설정 생성·이동 없음 |
| 진단 입력·IPC | 비밀 없는 `<root>/indexer/inputs/<chain>`; socket은 데이터 볼륨 밖의 체인별 private runtime 디렉터리 | inputs/source/native parent/exports는 서로 분리. DSN은 진단 bundle에 복사하지 않음. 실제 socket 길이·UID·host/container 공유 경로 HOLD |
| capacity reserve | legacy raw floor100GiB, stop120GiB, warn150GiB, resume150GiB 초과 | 초안 권고·한정된 guard 구현. raw160+PG160+floor100=420GiB; resume까지는470GiB 초과 + 관리 여유. 물리 예약 아님 |
| native COW·checkpoint | 동일 filesystem에서 최소 `2 × logical native bytes + ReserveBytes` free, 기본 ReserveBytes1GiB | [Arc retention](EXECUTION_ARC_RETENTION.md) 기준. snapshot/catalog/secondary scratch·정상 증가·백업 공간은 추가. catalog16GiB/출력64GiB 상한도 별도 고려. legacy470GiB에 이미 포함된 것으로 계산 금지 |
| 백업·WAL | 데이터 디스크와 다른 장치 | 장치 identity·실경로·용량·암호화/권한·책임자·오프사이트/검증 보관 모두 unknown |

파일시스템·PGDATA·raw guard와 latch, PostgreSQL 크기 journal, 호스트 watcher의 중단은 구현돼 있다. 실제 외장 APFS/운영 DB에서 입증한 것은 아니다. Docker VM 여유는 시작·재개 전 점검뿐이며 실행 중 지속 감시는 미구현이다. watcher 자체 재시작/알림·부팅 자동 시작·실제 종료 유예 충족·장애/복원 입증은 여전히 보류다. `--check-host`는 throwaway `df` 컨테이너를 실행하므로 이번 허용 범위의 순수 metadata 관측에 포함되지 않으며 실행하지 않았다.

## 이번에 선택한 운영 권장안 — 아직 미적용

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

실제 native/spool 증가량이 없으므로 native 총 필요량·충분한 디스크 크기·24h 수용 여부는 **HOLD**다. 추가 측정 대신 이 값을 추측하지 않는다. legacy raw160GiB·PG160GiB·여유150GiB의 470GiB 초과 예산은 legacy 초안 비교용이며 새 native 계획의 합계가 아니다. X31의 exFAT 여유 공간을 새 APFS에 이미 확보된 공간으로 계산하지 않는다. 별도 장치의 WAL/백업 예산도 아직 없다.

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
| Arc identity | [2026-10-19 기록](analysis/arc-testnet-identity.md)의 chain/network5042002, genesis `0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f` | 당시 공식 소스/RPC 대조이며 이번 재조회 아님. 실제 연결할 공식 EL peer의 identity 일치 확인 필요 |
| EL peer/fork | fork 후보 `910609e1` / next0은 과거 소스 계산값 | 공식 온보딩 enode·접속 조건·배포 소스/fork 광고·history 보존 범위 unknown. 추측·Discord 연락·peer 연결 없음 |
| legacy 시작 | RPC finalized H/hash와 start H+1을 맞추는 초안 | H/hash 미정. 과거 관측 head를 checkpoint로 복사하지 않음 |
| native 실행 시작 | [bootstrap](EXECUTION_BOOTSTRAP.md), [Arc collector](EXECUTION_ARC.md)의 인증된 expected manifest·full state root/coverage·일치하는 native/source/schema | checkpoint origin/height/hash/root·인증 주체/정책·start 모드·기존 anchor와 보존 floor 일치 unknown. retained cutoff seed는 원래 anchor를 이동시키지 않음 |
| StableNet E6 | [지원 정책](EXECUTION_CONSENSUS.md) `stablenet-wbft-4d17eea018c8`, 공식 pinned WBFT·bounded Anzeon 구간 | 전체 정책·chain/genesis·AnchorHeader·Applied/Next validator sets의 외부 인증·일치 checkpoint 필요. fixture anchor를 운영값으로 사용 불가 |
| 나머지 RPC 시작 | 최근 확정 기준과 strict 부모 상태 조회 가능성 필요 | 실제 시작 높이와 provider 상태 보존 계약 unknown. 10/22 retention/speed는 현재 보장 아님 |

[기존 운영 초안](OPERATIONS_DRAFT.md)의 `source.feed.module: arc.eth68`는 header/body/receipt 중심 legacy baseline이다. 새 native execution/collector·checkpoint·E6 경로는 opt-in이고 [legacy Compose 초안](../deployments/testnets/compose.draft.yaml)에 native App/collector/executor 운영 서비스를 추가하지 않았다. 별도 [진단 전용 Docker 초안](../deployments/native/README.md)은 설정 검사와 읽기 전용 metadata 명령만 연결하며 네트워크·DSN·DB 의존성과 실제 실행은 없다. 옛 YAML을 native 완성 배포로 이름만 바꾸거나 실제 config를 교체하지 않는다.

[Native runtime 구성 초안](../deployments/native/runtime/README.md)은 두 체인의 App과 producer 네 서비스를 별도로 선언하고 PostgreSQL16을 준비된 외부 의존성으로 둔다. Arc Rust executor는 collector의 같은 컨테이너 stdio 자식이며 별도 sidecar가 아니다. StableNet executor는 기존 import/replay를 소유한다. **Arc의 계속되는 first import/P2P 동기화 공급자는 아직 이 구성에 없다.** 이미 import된 compatible native DB와 그 갱신 경로가 필요하며 enode·checkpoint만 확보해도 자동으로 해결되지 않는다. [공급자 설계](ARC_NATIVE_IMPORT_DESIGN.md)는 App 내부 Go 수신 감독과 소유한 Rust 첫 실행 worker로 확정했다. 기존 collector의 두 번째 실행을 유지하며 별도 RPC/SQL pool을 만들지 않는다. N0의 bounded frame/private pending fence와 실제 SDK 저장/RO/재시작 qualification은 구현했다. N1의 인증된 새 native DB bootstrap은 구현했다. N2 writer·native 저장 완료/read gate는 구현했다. N3의 opt-in App 준비·socket-ready 시작·owned raw/child는 구현했다. N4~N6의 managed collector/reorg·영구 raw/GC·통합 검증은 미완료다. managed collector와 E1 reorg/ACK 권한, lossless 영구 raw 및 GC를 먼저 구현해야 한다. runtime checker는 선언만 검사하며 앱 설정/host/image/readiness를 증명하거나 실제 실행을 막는 startup interlock을 제공하지 않는다.

## 백업 custody와 복구 판정

[백업 초안](OPERATIONS_BACKUP_DRAFT.md)의 운영자 수용값은 **archive_timeout5분, 기준 백업 매일·7개, 정지 복사 매주·4개 및 큰 변경 전**이다. 수용한 방침이며 PostgreSQL 설정 적용·아카이브 실행 사실은 아니다. WAL 목적 장치·실경로·용량·암호화·secret 저장소·보관 책임자와 RPO/RTO 실증은 미확정이다.

#230은 matched native/source/indexer seal과 restore **admission**을 제공한다. [backup 계약](EXECUTION_ARC_RETENTION.md#matched-backup-and-restore-admission)의 `BackupOutput`, `IndexerReceipt`, `IndexerArtifact`는 실제 외부 PG snapshot + 필요한 WAL 전체를 복원할 수 있다는 독립 인증을 요구한다. 임의 SQL 파일의 digest로 대체할 수 없다. publication/generation/permit/full baseline/root/retained floor/WAL boundary가 정확히 맞아야 한다. 실제 PG snapshot 획득·WAL 복구·설치·원래 schema 인증과 node/source 재활성화는 별도 보류이며 이 릴리스에서 실행하지 않았다. 단순 stopped-copy를 유일한 구현 경로로 단정하지 않고, 서로 다른 시점의 DB/native/spool을 임의로 조합하지 않는다.

## 실제 값이 필요할 때 해제할 HOLD

권장 경로 배치·DB 방식·프로세스 계획·24h 산식은 위와 같이 선택했다. 실제 APFS mount/UUID/ownership와 native/spool/checkpoint/PGDATA 예산·COW headroom, private 실경로·자격 증명·reviewed image digest, 공식 EL peer와 인증 checkpoint/E6 trust, 별도 WAL/백업 장치와 custody/RPO/RTO가 남는다. 파일 예제·fixture·과거 head를 채워 HOLD를 해제하지 않는다. [Native preflight](NATIVE_EXECUTION_PREFLIGHT.md)의 declaration은 실제 값을 확보한 뒤 해당 schema로 작성하며, 이 문서는 새 실행 가능한 manifest가 아니다.

지금 가능한 다음 단계는 [first-import 계획](codemine/runs/20261029-arc-first-import/plan.md)의 N4 managed collector/reorg이며 N5~N6을 순서대로 구현한다. N0은 실제 임시 native-v2/별도 RO 프로세스로 qualification했고 운영 실행 증거는 아니다. 실제 저장소가 제공되면 identity/경로/예산을 검토하고, 공식 EL peer·인증 checkpoint를 확보한 뒤 기존 오프라인 검사에 연결한다. 지금은 외부 입력을 반복 요청하거나 새 운영/성능 확인 루프를 만들지 않는다. [운영 입력 계획](codemine/runs/20261029-operations-inputs/plan.md)과 [runtime 초안 계획](codemine/runs/20261029-native-runtime-draft/plan.md)을 유지한다. 운영 적용·provisioning·GC·restore activation과 추가 성능 측정은 계속 보류한다.

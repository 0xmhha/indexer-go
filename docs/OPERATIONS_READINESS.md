# 운영 설정 준비 현황

**배포 판정: HOLD.** 네 기능 릴리스 완료와 실제 운영 준비를 구분한다. 이 문서는 운영자가 정할 값만 모으는 다음 우선순위의 문서 릴리스다. 설정·Go/Rust/SQL·Compose·테스트·런타임 동작은 변경하지 않는다. 추가 성능 측정은 하지 않았다.

## 근거와 완료 범위

| 구분 | 확인 내용 | 근거와 한계 |
| --- | --- | --- |
| 완료 #227 | bounded Engine full-blob 공급 | [PR #227](https://github.com/0xmhha/indexer-go/pull/227), [동기화](../verification/priority1-sync.json). KZG는 beacon/PeerDAS 합의 검증이 아님 |
| 완료 #228 | call 범위·orphan trace | [PR #228](https://github.com/0xmhha/indexer-go/pull/228), [동기화](../verification/priority2-sync.json). 미수집 과거 coverage를 만들지 않음 |
| 완료 #229 | 지원 WBFT 독립 canonical/finality | [PR #229](https://github.com/0xmhha/indexer-go/pull/229), [동기화](../verification/priority3-sync.json). 인증된 정책·anchor와 지원 fork 범위만 |
| 완료 #230 | native lookup/MDBX 회수, matched backup/restore admission | [PR #230](https://github.com/0xmhha/indexer-go/pull/230), [CI5](https://github.com/0xmhha/indexer-go/actions/runs/40418831898), [동기화](../verification/priority4-sync.json), [검증](codemine/runs/20261027-native-retention-backup/validation.md). 외부 PG snapshot/WAL 생성·인증·실제 복원은 별도 |
| 이번 현재 관측 | 아래 저장소·설정 metadata | 2026-10-27 02:24 UTC / 11:24 KST. bounded `diskutil list/info -plist`, `df -k`, 지정 경로 존재·mode와 허용한 설정 필드만 읽음 |
| 운영 미확인 | APFS·peer·checkpoint·비밀 파일·백업 보관소·복구 증명 | node/RPC/provider/운영 DB/Docker/container probe, 실행·provisioning·GC·복원·성능 측정 없음 |

해시 검증한 이전 최종 인계의 patch SHA256은 `6db158c8e392db38293ae91cb48b64b7a9457a5950fb87747795ad2777101c83`, 완료 보고는 `3ac41ad0cc39b3ab79591f2366fb5b90a769ccbd6f8c28c4d39f27baa7c689a2`, 동기화 원본은 `1704b5d005c4c87b72064a5c866edef537fa802e0efc32c32ccc90af2e551141`이다. 보존된 strict all-four observer 로그도 SHA256 `5e1cb69539c87365f0f8958b6862c409fdae0fc38c4fe52a953efca6dbe1fbb5`와 일치했다. 이는 **이전 릴리스의 증거**이며 이번 세션에서 native/PG/200 실험을 반복했다는 뜻이 아니다. 선택한 현재 문서와 휴대 가능한 영수증을 저장소에 반영했으며 원본 작업트리는 수정하지 않았다. `verification/releases.json`과 observer는 과거 machine-local 실행 ledger 형식으로 유지하고 현재 릴리스 현황은 이 표와 각 portable sync를 따른다. 해당 observer를 이번 문서 릴리스 뒤의 main 검증기로 재사용하지 않는다.

기존 n은 #230 native retention, n−1은 #229 E6, n−2는 #228 call ranges다. [수용한 E1 설계](codemine/runs/20261023-e1-execution/02-design.md)와 [계획](codemine/runs/20261023-e1-execution/03-plan.md), [현재 backlog](analysis/open-items.md)를 보존한다.

## 현재 확인한 저장소와 설정

| 값 | 현재 확인 | 판정·운영자가 정할 값 |
| --- | --- | --- |
| 외장 후보 | X31, `/Volumes/X31`, external `disk14` / `disk14s1`, exFAT | APFS가 아님. 기존 자료 보존·전용 외장 APFS 확보 방법 결정 필요 |
| X31 identity | volume UUID `C1B1E211-6452-39C0-AD82-831FDE1B0D8C` | exFAT identity이며 APFS UUID로 사용 불가. device 번호는 재연결 시 변동 가능 |
| X31 용량 | diskutil 총 1,024,190,578,688 bytes, df 가용 424,233,216 KiB = 434,414,813,184 bytes ≈404.6GiB | 현재 한 번의 관측. 예약·지속 용량·fsync 증거 아님 |
| 외장 APFS root | `/Volumes/IndexerData` 없음 | mount/device/VolumeUUID/physical-store UUID·ownership 모두 HOLD. 내부 APFS로 대체하지 않음 |
| 기존 ignored config | primary checkout의 `config.yaml` 존재, mode0644; driver `pebble`, path `./data/multichain-testnets`, PostgreSQL max_conns 미지정; rpc.rate_limit5, indexer.finality head/workers2/poll_interval2s | 기존 설정 유지. 이 작업트리에는 config 없음. 기존 경로의 데이터는 열거나 순회하지 않음 |
| private manifest | workspace와 primary의 `deployments/testnets/host.json` 없음 | 다른 위치 존재 여부는 unknown. [host 초안](../deployments/testnets/host.draft.json)의 private_dir/image/UUID는 placeholder |
| DSN·password·peer key | 실제 private 위치가 없어 존재·0600·소유자·자격 증명 유효성 unknown | 비공개 디렉터리0700/일반 파일0600 제안. 비밀 값은 문서·PR에 넣지 않음. 파일 생성 없음 |
| 운영 이미지 | reviewed tag/digest 미확정 | 이미지 선택 필요. pull/inspect/build/컨테이너 실행 없음 |

`diskutil`의 partition 크기와 파일시스템 총 크기는 다를 수 있다. X31의 현재 여유는 legacy 계획의 470GiB 초과에도 못 미치며, 포맷 후 용량이나 native 여유를 추정하지 않는다. 저장소 내부의 사용자 파일은 순회하지 않았다. 아카이브 장치는 unknown이다.

## 경로와 용량 결정표

아래 경로는 **제안**, 할당·생성·mount된 사실이 아니다. 실제 외장 APFS root와 배치 예산을 운영자가 정해야 한다.

| 용도 | 제안·기존 초안 | 남은 결정 / HOLD 이유 |
| --- | --- | --- |
| legacy raw window | root 아래 `indexer/window/arc`, volume160GiB / Arc128GiB, 최소24h | [legacy YAML](examples/testnets/arc-testnet.draft.yaml)용 논리 한도. native spool 예산과 혼합하지 않음. 24h 수용 보장 없음 |
| native DB | 외장 APFS 전용 parent 아래 `native` 하위 디렉터리 | 실제 root·크기·증가 예산 미확정. mount point 자체를 native directory로 사용하지 않음 |
| 실행 source spool | native와 서로 포함하지 않는 별도 절대 경로 | 실제 경로·quota 결정. [실행 수집](EXECUTION_INGEST.md)의 기본4GiB/262144 records는 24h 보장 아님 |
| checkpoint·seed·scratch | native 밖의 전용 private 디렉터리 | 인증된 full baseline과 retained seed·출력 보관/소유권 결정. partial proof를 full baseline으로 사용 불가 |
| PGDATA | root 아래 `indexer/postgres/16`, 계획160GiB | 데이터·색인·WAL 포함 계획이며 quota 아님. Docker 컨테이너의 `/var/lib/postgresql/data`와 호스트 경로 구분 |
| identity·private 설정 | root 아래 `indexer/identity/arc-peer.key`; 별도 private 설정 디렉터리 | 실경로·UID/GID·secret custody 미확정. 기존 key/설정 생성·이동 없음 |
| capacity reserve | legacy raw floor100GiB, stop120GiB, warn150GiB, resume150GiB 초과 | 초안 권고·한정된 guard 구현. raw160+PG160+floor100=420GiB; resume까지는470GiB 초과 + 관리 여유. 물리 예약 아님 |
| native COW·checkpoint | 동일 filesystem에서 최소 `2 × logical native bytes + ReserveBytes` free, 기본 ReserveBytes1GiB | [Arc retention](EXECUTION_ARC_RETENTION.md) 기준. snapshot/catalog/secondary scratch·정상 증가·백업 공간은 추가. catalog16GiB/출력64GiB 상한도 별도 고려. legacy470GiB에 이미 포함된 것으로 계산 금지 |
| 백업·WAL | 데이터 디스크와 다른 장치 | 장치 identity·실경로·용량·암호화/권한·책임자·오프사이트/검증 보관 모두 unknown |

파일시스템·PGDATA·raw guard와 latch, PostgreSQL 크기 journal, 호스트 watcher의 중단은 구현돼 있다. 실제 외장 APFS/운영 DB에서 입증한 것은 아니다. Docker VM 여유는 시작·재개 전 점검뿐이며 실행 중 지속 감시는 미구현이다. watcher 자체 재시작/알림·부팅 자동 시작·실제 종료 유예 충족·장애/복원 입증은 여전히 보류다. `--check-host`는 throwaway `df` 컨테이너를 실행하므로 이번 허용 범위의 순수 metadata 관측에 포함되지 않으며 실행하지 않았다.

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

[기존 운영 초안](OPERATIONS_DRAFT.md)의 `source.feed.module: arc.eth68`는 header/body/receipt 중심 legacy baseline이다. 새 native execution/collector·checkpoint·E6 경로는 opt-in이고 [Compose 초안](../deployments/testnets/compose.draft.yaml)에 native Docker wiring을 추가한 적 없다. 옛 YAML을 native 완성 배포로 이름만 바꾸거나 실제 config를 교체하지 않는다.

## 백업 custody와 복구 판정

[백업 초안](OPERATIONS_BACKUP_DRAFT.md)의 운영자 수용값은 **archive_timeout5분, 기준 백업 매일·7개, 정지 복사 매주·4개 및 큰 변경 전**이다. 수용한 방침이며 PostgreSQL 설정 적용·아카이브 실행 사실은 아니다. WAL 목적 장치·실경로·용량·암호화·secret 저장소·보관 책임자와 RPO/RTO 실증은 미확정이다.

#230은 matched native/source/indexer seal과 restore **admission**을 제공한다. [backup 계약](EXECUTION_ARC_RETENTION.md#matched-backup-and-restore-admission)의 `BackupOutput`, `IndexerReceipt`, `IndexerArtifact`는 실제 외부 PG snapshot + 필요한 WAL 전체를 복원할 수 있다는 독립 인증을 요구한다. 임의 SQL 파일의 digest로 대체할 수 없다. publication/generation/permit/full baseline/root/retained floor/WAL boundary가 정확히 맞아야 한다. 실제 PG snapshot 획득·WAL 복구·설치·원래 schema 인증과 node/source 재활성화는 별도 보류이며 이 릴리스에서 실행하지 않았다. 단순 stopped-copy를 유일한 구현 경로로 단정하지 않고, 서로 다른 시점의 DB/native/spool을 임의로 조합하지 않는다.

## 이번에 바로 정할 입력

운영자는 외장 APFS 확보·실제 root와 native/spool/checkpoint/PGDATA 예산·COW/backup headroom, private 위치와 reviewed image, 총 프로세스/서버 연결 예산, 공식 EL peer와 인증 checkpoint/start·E6 정책, 별도 WAL/백업 장치와 custody/RPO/RTO를 정해야 한다. 값이 없다는 사실을 HOLD로 남기며 문서 릴리스를 막는 추가 인터뷰로 바꾸지 않는다. 운영 적용·provisioning·GC·restore activation과 추가 성능 측정은 계속 보류한다.

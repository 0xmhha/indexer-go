# 백업·이전과 복구 목표 초안

**상태: 초안. 백업 실행, 보관소 준비, PostgreSQL 설정 변경, 복원 시험은 하지 않았다.**

이 문서는 테스트넷 운영 초안([운영 초안](OPERATIONS_DRAFT.md), [Docker 초안](../deployments/testnets/README.md))에서 무엇을 어떻게 보존하고, 장애 뒤 어느 시점까지 얼마 만에 되돌릴지를 제안한다. 체인마다 indexer 프로세스가 하나씩 있고(StableNet, BSC, Sepolia, Arc), 네 프로세스가 PostgreSQL 16 하나를 체인별 schema로 나눠 쓴다. Arc만 외장 APFS에 24시간 raw window를 둔다. 기존 [재시작·GC 절차 초안](OPERATIONS_RUNBOOK_DRAFT.md) 8장과 [용량 정책 초안](OPERATIONS_CAPACITY_DRAFT.md)의 복구 목표 절을 이어 받는다.

## 1. 다시 만들 수 없는 자료가 있다

먼저 자료마다 잃었을 때 다시 만들 수 있는지를 본다. 결론은 "체인 자료는 노드에서 다시 받으면 된다"가 아니라는 점이다.

| 자료 | 위치 | 잃으면 | 다시 만들 수 있나 |
|---|---|---|---|
| 알림 설정과 저장된 알림 | PostgreSQL, 체인 schema | 사용자가 API로 만든 설정과 보내지 못한 알림이 사라진다 | **없다.** 체인에 없는 자료다 |
| 계약 검증 자료(ABI, 소스, 검증 상태) | PostgreSQL, 체인 schema | 검증 결과가 사라진다 | **없다.** 사용자가 올린 자료다. 시스템 계약 검증 자료만 시작 단계가 다시 넣는다 |
| 블록·거래·로그와 기능 자료(RPC 체인) | PostgreSQL, 체인 schema | 색인이 사라진다 | **노드가 상태를 보관하는 범위에서만.** 아래 설명 |
| Arc 원천 자료(source facts)와 projection | PostgreSQL, Arc schema | Arc 색인이 사라진다 | **peer와 raw window가 보관하는 범위에서만** |
| outbox, undo, 소비자 위치 | PostgreSQL, 체인 schema | 이벤트 재전달과 reorg 되돌리기 정보가 사라진다 | 색인을 다시 하면 새로 생긴다. 이미 전달한 이벤트 번호와는 이어지지 않는다 |
| Arc raw window와 marker/ledger, 삭제 기록 | APFS `indexer/window/arc` | 24시간 원본과 DB와의 연결 정보가 사라진다 | 원본은 peer에서 다시 받을 수 있는 동안만. 연결 정보는 DB와 같은 시점이어야 쓸 수 있다 |
| Arc peer key | APFS `indexer/identity/arc-peer.key` | peer가 이 노드를 다른 노드로 본다 | 새로 만들 수 있다. 다만 peer 관계를 다시 맺어야 한다 |
| 체인별 설정, host manifest | private 디렉터리 `config-<chain>.yaml`, host manifest | 시작할 수 없다 | 초안과 확정 값으로 다시 쓸 수 있다 |
| DSN, PostgreSQL 비밀번호 | private 디렉터리의 secret 파일 | 접속할 수 없다 | 새로 발급할 수 있다. 백업은 secret 저장소 방침을 따른다 |
| halt latch, 용량 journal | private 디렉터리 `state/<chain>` | 중단 사유와 진단 기록이 사라진다 | 진단용이다. 오래된 latch를 복원하면 시작이 막히므로 복원하지 않는다 |
| 운영 이미지 | Docker | 같은 바이너리를 쓸 수 없다 | 검토한 이미지 tag로 다시 빌드하거나 받는다 |

### RPC 체인을 다시 색인할 수 없는 경우

세 RPC 체인은 `balance.native`, `token.metadata`를 엄격 모드(`allow_pruned_state: false`)로 켠다. 이 기능은 처리할 첫 블록 직전의 상태를 노드에 묻고, 노드가 그 상태를 지웠으면 시작을 거부한다([설정](CONFIG.md)). 공개 full node는 최근 일부 블록의 상태만 보관한다. 그래서 다음 두 가지가 생긴다.

- DB를 통째로 잃으면, 원래 시작 높이부터 다시 색인할 수 없다. 노드가 보관하는 최근 높이부터 새로 시작해야 하고, 그 이전 이력은 잃는다.
- 오래된 백업을 복원해도 같은 문제가 있다. 백업 시점 다음 블록부터 이어서 색인하려면 그 블록 직전 상태가 노드에 남아 있어야 한다. 백업이 노드의 상태 보관 범위보다 오래되면, 복원한 DB는 엄격 모드로 시작하지 못한다.

즉 RPC 체인의 실제 RPO 상한은 백업 주기가 아니라 **노드의 상태 보관 범위**다. 2026-10-22에 초안 설정의 공개 RPC 네 곳을 재 본 결과는 아래와 같다.

### 측정한 상태 보관 범위 (2026-10-22)

각 RPC에 `eth_getBalance`를 head에서 16, 64, 128, 256, 1,000, 10,000, 100,000, 1,000,000, 10,000,000블록 뒤의 높이로 보냈다. 처음 실패한 간격과 그 앞 사이는 이분 탐색으로 좁혔다. 실패가 없으면 블록 1의 상태도 물었다. 읽기 요청만 보냈고, RPC마다 초당 4개 이하로 모두 52개였다.

| 체인 | RPC | 상태가 남은 깊이 | 시간으로 | 실패 메시지 |
|---|---|---|---|---|
| Sepolia | `ethereum-sepolia-rpc.publicnode.com` | 128블록 | 약 26분 | `historical state ... is not available` |
| BSC testnet | `bsc-testnet-dataseed.bnbchain.org` | 120블록 | 약 54초 | `missing trie node` |
| StableNet testnet | `api.test.stablenet.network` | 블록 1까지 | 전체 | 없음 |
| Arc testnet | `rpc.testnet.arc.io` | 블록 1까지 | 전체 | 없음 (Arc는 상태를 읽지 않는다) |

StableNet은 archive처럼 동작한다. 백업이 오래돼도 상태 때문에 이어 가지 못하는 일은 없다.

Sepolia와 BSC testnet은 문제가 백업보다 크다. 엄격 모드는 처음 보는 계정이 나온 블록에서 그 직전 잔액을 노드에 묻고, 새로 만든 계약은 만든 블록의 상태로 토큰인지 묻는다(`internal/features/balance/balance.go`). 활발한 테스트넷에서는 대부분의 블록이 여기에 걸릴 것으로 본다. 그래서 색인이 head보다 Sepolia는 약 26분, BSC는 약 1분 넘게 뒤처지면 그 블록에서 멈춘다. 백업 복원뿐 아니라 평소의 재시작, 이미지 교체, 짧은 장애도 같다. BSC는 블록이 약 0.45초마다 나오므로, 5 HTTP/s 예산 안에서 색인 속도가 블록 생성을 따라가는지부터 확인해야 한다. 이 판단은 계산이며 실행해 보지 않았다.

두 체인에는 다음 중 하나를 정해야 한다(6장).

- archive RPC를 쓴다. 엄격 모드를 그대로 지킨다. 대신 공개 RPC가 아니어서 비용이나 계약이 필요하다.
- `allow_pruned_state: true`로 바꾼다. 멈추지 않는다. 대신 노드가 주지 못한 계정 잔액을 0부터 세므로 잔액이 틀릴 수 있다.
- `balance.native`, `token.metadata`를 끈다. 멈추지 않는다. 대신 잔액과 토큰 정보를 제공하지 않는다.

한 번 잰 값이다. 공개 RPC는 뒤에 여러 노드를 두고 요청을 나눠 보낼 수 있어서, 다른 시각에는 결과가 다를 수 있다.

Arc도 비슷하다. Arc는 상태를 읽지 않지만, 복원한 DB 이후의 블록을 peer가 ETH/68 history로 내줄 수 있어야 한다. raw window 24시간은 그 기간 안에 이미 받은 원본만 보관한다. peer가 얼마나 오래된 history를 내주는지는 확인하지 않았다.

---

## 2. 복구 목표 제안

자료를 세 등급으로 나눈다.

| 등급 | 자료 | RPO 제안 | RTO 제안 | 근거 |
|---|---|---|---|---|
| A | 알림 설정·알림, 계약 검증 자료 | 5분 이하 | 2시간 | 다시 만들 수 없다 |
| B | 색인 자료(RPC 체인, Arc) | 노드·peer 보관 범위 안. 5분 이하를 목표로 한다 | 2시간 + 따라잡기 시간 | 보관 범위를 넘으면 이력을 잃는다 |
| C | 설정, 이미지, peer key, secret | 바뀔 때마다 | 30분 | 수작업으로 다시 만들 수 있다 |

B등급의 5분 목표는 StableNet과 Arc에만 의미가 있다. Sepolia와 BSC testnet은 1장의 측정대로 노드 보관 범위가 약 26분과 약 1분이라, 5분 전 백업도 BSC에서는 이어 가지 못한다. 두 체인의 RPO는 6장의 결정에 달려 있다.

RTO 2시간은 용량 정책 초안의 "사고 발생 후 2시간 내 복구 방침 결정"을 그대로 쓴 것이다. 달성을 보장하는 값이 아니다.

### 따라잡기 시간

복원한 뒤에는 멈춰 있던 동안 쌓인 블록을 따라잡아야 한다. 시간은 이렇게 계산한다.

- 쌓인 블록 수 N = 중단 시간 × 블록 생성 속도
- 따라잡기 시간 = N ÷ (색인 속도 − 블록 생성 속도)

예를 들어 Arc feed writer의 로컬 비교 측정값 약 5.1블록/초(5 HTTP/s 제한 아래, 32블록 입력, [work log](analysis/work-log-2026-10.md)에 기록)와 블록 생성 2블록/초를 넣으면, 순수 따라잡기 속도는 약 3블록/초다. 1시간 중단이면 7,200블록이고 약 40분이 걸린다. RPC 체인의 실제 색인 속도는 측정하지 않았다. 상태 조회가 블록마다 더 들어가므로 이보다 느릴 수 있다.

따라잡는 동안에도 노드의 상태 보관 범위를 넘으면 안 된다. 중단 시간과 따라잡기 시간의 합이 보관 범위보다 길면, 백업이 최신이어도 엄격 모드로 이어서 색인할 수 없다.

---

## 3. 백업 방식 제안

두 방식을 함께 쓴다.

### 3.1 연속 WAL 보관 (A·B 등급, 기본)

`pg_basebackup`으로 기준 백업을 만들고, WAL을 계속 다른 저장소에 보관한다([PostgreSQL 16 연속 보관](https://www.postgresql.org/docs/16/continuous-archiving.html)). 원하는 시점으로 되돌릴 수 있고, RPO는 WAL 보관 주기(`archive_timeout`)에 가깝다.

- 보관소는 외장 APFS가 아닌 다른 장치에 둔다. 같은 디스크에 두면 디스크를 잃을 때 함께 잃는다.
- PostgreSQL 설정에 `archive_mode`, `archive_command`, `archive_timeout`(제안 5분)이 필요하다. 지금 설정 초안(`postgresql-testnets.draft.conf`)에는 없다. 넣으면 `preflight.py`의 PostgreSQL 설정 검사도 함께 고쳐야 한다.
- 기준 백업은 하루 한 번 제안한다. 복원 때 다시 적용할 WAL 양을 줄이기 위해서다.

이 방식은 PostgreSQL만 보존한다. Arc raw window와 marker/ledger는 포함하지 않는다.

### 3.2 정지 상태 전체 복사 (Arc와 이전용)

indexer 네 개를 멈추고 PostgreSQL을 정상 종료한 뒤, PGDATA, Arc raw window 디렉터리(marker/ledger 포함), peer key, private 디렉터리를 같은 시점에 복사한다([PostgreSQL 16 파일 백업](https://www.postgresql.org/docs/16/backup-file.html)). 재시작·GC 절차 초안 8장이 요구하는 "같은 시점의 DB와 raw 연결"을 지키는 유일한 방법이다.

- 순서: `preflight.py --watch`가 쓰는 것과 같은 순서로 indexer들을 멈추고(SIGTERM, 120초), 그다음 PostgreSQL을 멈춘다(SIGINT, 60초).
- 복사가 끝날 때까지 서비스가 멈춘다. 주기는 주 1회 또는 큰 변경(이미지 교체, 설정 변경, 디스크 이전) 직전을 제안한다.
- 복사본의 무결성은 파일 크기와 체크섬으로 확인한다.

### 3.3 Arc를 연속 WAL로 복원할 때의 한계

연속 WAL로 DB만 과거 시점으로 되돌리면, raw window에는 그 시점 이후의 원본과 삭제 기록이 남는다. 지금 코드는 DB와 window 기록이 어긋나면 시작을 멈춘다(fail-closed). 이 경우 raw window를 다시 만드는 절차가 필요한데, 최초 window 생성은 아직 보류된 provisioning 절차다. 그래서 Arc는 3.2의 정지 상태 복사본으로 복원하는 것을 기본으로 둔다. Arc를 연속 WAL로 되돌리는 절차는 정하지 않았다.

### 3.4 보조: schema별 논리 덤프

A등급 표(알림, 계약 검증)는 `pg_dump --schema`로 하루 한 번 따로 받는다. 물리 백업이 손상됐을 때 사용자 자료만이라도 살리기 위해서다. 복원은 빈 schema에 표 단위로 넣는다. 색인 자료와의 정합은 보장하지 않는다.

---

## 4. 이전 절차 (다른 디스크나 호스트로)

1. 3.2의 정지 상태 복사본을 만든다. 원본은 지우지 않는다.
2. 새 대상에 복사한다. 새 APFS의 VolumeUUID, physical-store UUID, mount root를 host manifest에 새로 적는다. window의 marker identity(`indexer-apfs-testnets-v1`)는 볼륨과 함께 옮겨지므로 바꾸지 않는다.
3. 원본과 새 대상을 동시에 시작하지 않는다. writer는 하나만 있어야 한다.
4. 새 대상에서 `preflight.py --check-host`를 통과시킨다.
5. 5장의 복원 검증을 새 대상에서 한 뒤에 원본을 정리한다.

---

## 5. 복원 검증

복원은 원본을 덮어쓰지 않는 격리된 대상에서 먼저 한다.

1. 읽기 전용 점검: [checkpoint/GC metadata 명령](OPERATIONS_INSPECTION.md)으로 DB와 window의 기록을 읽어 서로 맞는지 본다. 앱을 시작하지 않는다.
2. 체인별 확인: 각 schema의 마지막 색인 높이와 그 블록 hash를 노드의 같은 높이 hash와 비교한다.
3. 엄격 상태 확인: RPC 체인마다 마지막 색인 높이 다음 블록 직전의 상태를 노드가 아직 보관하는지 확인한다. 없으면 그 체인은 엄격 모드로 이어 갈 수 없다. 운영자가 새 시작 높이를 정하거나 해당 기능을 끄는 방침을 정해야 한다.
4. 시작: `collect: false` 그대로 체인 하나씩 시작해 따라잡기를 지켜본다.
5. 사용자 자료 확인: 알림 설정 수와 계약 검증 수가 백업 시점 기록과 같은지 본다.
6. 기록: 재시작·GC 절차 초안의 완료 표시(정상 종료 / 복구 확인 후 재개 / 관측·복구 미확인으로 중단 유지) 중 하나로 남긴다.

---

## 6. 정해야 할 것

2026-10-22에 운영자가 제안값 중 세 가지를 확정했다. 확정한 값도 아직 설정에 넣거나 실행하지 않았다.

| 항목 | 상태 | 값 | 정할 사람 |
|---|---|---|---|
| `archive_timeout` | 확정 | 5분 | 운영자 |
| 기준 백업·정지 복사 주기 | 확정 | 기준 백업 하루 1회, 정지 복사 주 1회와 큰 변경 직전 | 운영자 |
| 백업 보존 기간 | 확정 | 기준 백업 7개, 정지 복사 4개 | 운영자 |
| WAL 보관소 위치 | 미정 | 외장 APFS가 아닌 별도 장치. 장치가 정해질 때까지 PostgreSQL 설정과 `preflight.py`는 바꾸지 않는다 | 운영자 |
| Sepolia·BSC testnet 상태 보관 | 미정 | 1장 측정 결과에 따라 archive RPC, `allow_pruned_state: true`, 기능 끄기 중 하나 | 운영자 |
| secret 백업 | 제안 | 별도 secret 저장소. 백업 디렉터리에 평문으로 두지 않는다 | 보안 담당 |
| Arc history 보관 범위 | 미확인 | 연결할 peer가 내주는 범위를 확인 | 네트워크 운영자 |

---

## 7. 확인하지 않은 것

- Arc peer의 history 보관 범위. 공개 RPC 네 곳의 상태 보관 범위는 1장에서 한 번 쟀지만, 시각이나 뒤의 노드에 따라 달라지는지는 보지 않았다.
- Sepolia와 BSC testnet에서 색인 속도가 블록 생성을 따라가는지. 따라가지 못하면 엄격 모드로는 운영할 수 없다.
- RPC 체인의 실제 색인 속도. 따라잡기 시간의 RPC 체인 값은 계산만 했다.
- WAL 보관과 기준 백업이 이 구성(Docker Desktop, 외장 APFS)에서 실제로 동작하는지. 설정을 바꾸지도, 실행하지도 않았다.
- 정지 상태 복사본으로 Arc를 복원했을 때 window 기록이 그대로 맞는지. 코드의 fail-closed 동작은 시험으로 확인돼 있지만, 이 절차 전체를 실행한 적은 없다.
- 백업 한 번의 크기와 걸리는 시간. DB 계획 예산 160GiB를 기준으로 잡아야 하지만 측정하지 않았다.

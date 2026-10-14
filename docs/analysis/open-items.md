# 남은 작업 (2026-10-14)

현재 남은 작업의 기준 문서다. 완료 항목은 아래 요약에만 두고, 세부 결정·실험·측정 기록은 [작업 기록](work-log-2026-10.md)에 보존한다. 이 목록을 기록하는 것은 미승인 구현·외부 배포·태그 발행의 승인을 뜻하지 않는다.

## 1. 현재 범위와 운영 조건

- **EVM 전용 프레임워크**다. 현재 내장 profile은 `evm`, `stablenet`, `arc`다. Stable(988/2201, Tendermint/CometBFT)은 제외했다. StableNet과 Stable은 별개다.
- 체인별 디코딩·회계·native coin 규칙은 `pkg/chains/<chain>`에, 고유 색인·조회는 feature/SDK 확장에 둔다. 공통 코어에 chain-id 조건문을 더하지 않는다. [커스터마이징 계약](../CHAIN_CUSTOMIZATION.md)을 따른다.
- 운영 노드는 **full node이며 archive는 없다**. 로컬 `config.yaml`을 네 테스트넷 동시 실행으로 재구성하고 임시 DB에서 시작·색인·API 조회를 확인했다. 최근 시작과 상태 읽기 기능의 `allow_pruned_state: false`를 유지한다. 현재 공개 RPC의 최근 parent hash/계정 state를 다시 확인하고 후보 설정의 시작 높이를 갱신했다. 실제 운영 호스트의 상태 보존 정책·허용 중단 시간은 확인하지 않았다. archive 여부 질문을 반복하지 않는다.
- 공개 RPC의 순차 색인·API·record/replay와 같은 프로세스의 제한된 최근 구간 동시 색인·장애·재시작 검증은 StableNet·BSC·Sepolia·Arc에서 수행했다. batch16/5HTTPs로 30분 동시 관측도 완료했다(Stable/BSC/Sep 마지막 gap 1/2/0, Arc 1949). days-long 운영, 전체 회계, mainnet 지원까지 검증한 것은 아니다.
- 기존 generic EVM으로 Arc를 색인한 DB가 있다면 새 Arc 회계로 재색인이 필요하다. 해당 운영 DB의 존재나 내용은 확인하지 않았다.

## 2. 주요 작업

순서는 기존 추천 순서를 유지한다. 사용자 결정이나 외부 자료가 필요한 조건을 함께 기록한다.

| 순서 / ID | 작업 | 현재 증거와 남은 범위 | 완료 기준 / 필요한 조건 |
|---|---|---|---|
| 1 / R03 | full node 운영 설정 확정 | 로컬 4개 테스트넷 설정·명시적 profile·제한된 시작/재시작 검증 완료. BSC의 처리 중 상태 소실을 확인한 뒤 사용자가 블록 실패·재시도를 선택. `allow_pruned_state: false`를 런타임에도 적용하고 블록 전체 취소·동일 높이 재시도를 회귀 시험으로 검증 | 첫 실행 전 시작 직전 상태 재확인. 기존 DB의 다음 높이·backfill 상태와 실제 보존 범위·허용 중단 시간 확인. 영구히 지워진 상태는 재시도로 복구되지 않음. 기존 generic EVM Arc DB는 재사용하지 않음. 잔액 정확한 상시 운영 완료로 간주하지 않음 |
| 2 / R04 | full node 처리 속도·상태 확보 지연·여러 호스트 SLO | 선택적 해시 기반 상태 준비·bounded channel·체인별 순차 writer와 관측 head 공유 구현. 명시적 health/live poll은 fresh 유지. 빈 블록 동일 입력에서 HTTP89→60/head41→12/17.660→11.908초(실노드 개선률 아님). 단계별 지연/진행 중 수/payload 계측 추가. 통제된 RPC 왕복 입력에서 개선했으나 요청 제한에 걸리면 추가 canonical RPC로 더 느려짐. 5분 실노드 창에서 StableNet은 head 근처, BSC는 필수 상태 소실로 정지, Arc는 격차 증가, Sepolia는 초기 상태 요청과 transport 재시도 지연. 기존 Pebble/PostgreSQL ingest 중앙값 1.506/2.749초는 별도 입력의 과거 측정 | 실제 노드·계정의 상태 보존 범위와 RPC quota에 맞춰 선택 설정·workers/요청량을 조정. 지속 처리량이 생성률을 넘고 head 지연+상태 확보 시간이 보존 기간보다 충분히 짧을 것. 네 공개 체인의 30분 창과 동일 입력의 durable Pebble/PostgreSQL 전후 비교 완료. 50 HTTP/s 제한의 이전 prepared→현재 batch 시간은 Pebble 4.939→1.534/PostgreSQL 4.993→1.519초, 논리 RPC 수는 거의 동일. Arc는 생성 1.976/색인 0.682 blk/s와 quota 오류로 상시 조건 미충족. days-long 운영·여러 호스트 SLO·PostgreSQL 운영 목표 배수는 남음. 통제된 결과를 운영 목표로 간주하지 않음 |

R04의 구현 범위와 다음 작업:

1. 체인별 수집·디코딩·상태 조회·DB commit 지연, RPC 요청 수/제한 대기/429, queue 개수/바이트, head 지연을 측정한다. 실제 사용 계정의 과거 상태 조회로 보존 범위를 확인한다. 5분·30분 표본에서 BSC의 성공했던 hash 상태가 소실됐고 head-32는 성공/head-128은 실패했다. 30분 Sepolia 표본도 같았고, Arc는 원래 hash/head-512 조회가 성공했다. StableNet은 이 관측 창에 실제 계정의 보존 표본이 없다. 고정 보존 폭은 확정하지 않는다. 로컬 `rpc.rate_limit: 5`가 처리량을 제한할 수 있으므로 workers만 늘리는 효과를 가정하지 않는다.
2. `indexer.state_prefetch`로 초기 잔액·토큰 상태 준비를 수집 worker에서 실행하고 `state_workers`의 공유 permit으로 제한한다. 부모/생성 block hash를 사용한다. 음수 잔액 보정·backfill·준비를 지원하지 않는 확장 기능은 기존 writer 경로다. `state_batch_size`(기본1,최대64)로 같은 hash의 초기 잔액과 독립 토큰 속성을 묶을 수 있다. 배치가 거부되면 해당 client는 개별 조회로 전환하고, 상태 소실·멤버 오류·429/5xx는 숨기지 않는다. HTTP 왕복 감소와 논리 RPC 감소는 별개다. 공유 permit 확보 뒤 committed 잔액 기록을 재확인해 대기 중 완료된 초기화의 중복 조회를 생략한다. 블록 간 speculative 중복의 잔여 비용·profile 추가 헤더 비용은 후속 측정 대상이다.
3. 준비 결과는 체인별 pipeline·height·block hash에 묶인 포인터로 기존 bounded channel에 넘긴다. `state_max_bytes`는 블록별 전달 payload 계산값의 상한이며 heap 전체 상한은 아니다. producer는 전달 후 수정하지 않는다. writer에서 canonical hash를 확인하고 reorg/취소/실패 시 범위의 결과를 폐기한다. strict 상태 정책에서 latest로 대체하지 않는다.
4. 체인별 writer 하나가 높이 순서대로 잔액·트랜잭션·undo·cursor·outbox를 함께 반영한다. 계정/토큰 기록을 writer에서 재확인해 불필요해진 준비 오류를 사용하지 않는다. 아직 기록되지 않은 필수 상태 오류는 블록 전체를 취소한다.
5. 변경 전 5분 창의 head 조회는 App 논리 약21~33%/HTTP약21~44%였다. 최근 관측·진행 중 RPC를 background progress/블록 metric에 공유하고 live poll·명시적 health의 fresh 계약, 오류·취소·응답 순서를 검증했다. 초기 잔액/토큰의 hash 고정 batch를 구현하고 동일 입력 및 네 테스트넷 30분 창에서 검증했다. 논리 요청량·speculative 중복·profile 추가 비용의 개선은 남는다.
6. 같은 입력·설정으로 변경 전후 처리량과 p50/p95/p99 지연·메모리·정확성을 비교하고, 네 체인 동시 실행 및 한 RPC 지연/장애 시 다른 체인의 진행을 검증한다. 보존 기간과 안전 여유는 실제 환경을 측정해 정한다. 이 과제 기록을 전체 파이프라인 변경의 완료로 간주하지 않는다.

근거: [동일 입력 저장소 비교](work-log-2026-10.md#316-1014-동일-입력-pebblepostgresql-전후-비교), [30분 동시 관측](work-log-2026-10.md#317-1014-네-공개-테스트넷-30분-동시-관측), [운영 미확정 조건](work-log-2026-10.md#318-1014-full-node-실행-후보와-운영-미확정-조건), [중복 head 관측 감소](work-log-2026-10.md#314-1014-중복-head-관측-감소), [네 테스트넷 5분 관측](work-log-2026-10.md#313-1014-네-공개-테스트넷-5분-운영-창-관측), [로컬 설정 복구·profile 선택](work-log-2026-10.md#310-1014-로컬-full-node-설정-복구명시적-profile-선택), [Arc·공개 RPC 기록](work-log-2026-10.md#35-1013-arcstable-추가와-공개-rpc-postgresql-검증), [PostgreSQL 측정](work-log-2026-10.md#33-1012-후속-postgresql-ingest-측정), [SLO 조사](work-log-2026-10.md#31-1012-후속-slo-조사).

## 3. 별도 조건이 있는 작업

| ID | 항목 | 상태 / 시작 조건 |
|---|---|---|
| R05 | 실제 export era1과 RPC 속도 비교 | 변환한 StableNet testnet era1 색인은 수행. 실제 `gstable export-history` 파일과 같은 체인의 노드 `eth_getLogs` 비교는 미검증. 파일·노드 필요 |
| R06 | Arc mainnet 검증 | mainnet을 새로 검증하지 않음. 접근 환경과 검증 범위를 정해 수행 |
| R07 | 간헐 SLO 지연·상대 비율 변동 | 기존 170~240ms 원인은 미확인. 상태 준비 후 기본 설정 두 SLO에서 non-reader +25/+32% 초과를 관찰. main 비교는 -6%, 계측 opt-in 분리 후 기본/준비 옵션은 -37/-5%로 통과. batch 준비 후 기본 SLO 한 실행의 round2 non-reader가 절대p99337.2ms로 실패(ingest105.4/push217.8ms). 변경 전 main 기준은116.4/119.3ms(+3%) 통과. 유일한 원인은 확정하지 않음. load fixture가 이전 live loop를 종료·join하지 않는 결함을 재현·수정하고 race200/CPU1race200 검증. 수정 fixture 기본/배치16 SLO 각각 -4%로 통과. 특정 변동 원인은 확정하지 않음. CPU/GC 계측 통과 표본을 보존했으나 실패 원인 단정 안 함. 추가 MacBookAir의 SSH port 22 읽기 probe는 connection refused였다. 접속 가능한 host/port/account/path가 필요하며 여러 호스트 실측·실패 시점 관측은 남음 |
| R08 | DEX reorg flake | 두 시험 각각 race 200회와 cpu=1 200회 통과, flake 미재현. 재현 시 수정 |
| R09 | frontend 이전 | GraphQL 문서 26개와 REST 이전은 frontend 작업. [이전 안내](../FRONTEND_MIGRATION.md) 참조. 이 세션에서는 해당 저장소 수정 금지 |
| R10 | 태그·원격 branch·배포 | 새 태그 발행, 병합 branch 원격 삭제, 외부 배포는 사용자 확인 필요. 다음 개발 작업의 자동 후속 단계가 아님 |

## 4. 보류·범위 밖·미승인 제안

- **Stable 관련 작업:** 전용 회계·시스템 거래·CometBFT 증명·Stable mainnet 검증은 현재 범위 밖이다. 과거 검증 기록은 삭제하지 않는다.
- **K2 이진 키:** 재측정 후 보류 유지. 디스크 절감 6.7%이며 저장 형식을 다시 바꿀 때 함께 검토한다.
- **값 형식 통일:** 기존 데이터 변환은 보류. 새 체인 데이터는 `core/model` 인코딩, 기능 레코드는 JSON 규칙을 유지한다.
- **perp 마켓 이름:** 현재 구현하지 않는 결정을 유지한다. 소비자 요구가 생기기 전에는 다시 열지 않는다.
- **AA 목록 API:** [기존 제안](../AA_BACKEND_TASKS.md)의 `allBundlers`·`allPaymasters`는 현재 API snapshot에 없다. 승인된 남은 작업에 자동 편입하지 않는다. frontend의 현재 우회 동작은 이번에 확인하지 않았다.
- **Git history 정리:** 이전 astgraph 바이너리가 history에 남아 있다. 이를 없애기 위한 history 재작성·force push는 수행하지 않는다.

## 5. 완료 항목과 검증의 한계

- **R02 완료(10/14):** 네 공개 테스트넷을 한 App에서 실행해 17블록·372거래·654로그의 식별자, 체인별 DB·profile·기능 설정·GraphQL/JSON-RPC/REST·durable 블록 stream 분리를 확인했다. 한 RPC의 HTTP 503 동안 나머지 체인이 진행했고, 복구 후 따라잡기와 같은 DB/알림 설정을 재사용한 프로세스 재시작도 통과했다. 자동 재시작이 새 파이프라인의 컨텍스트를 즉시 취소하던 오류를 수정했다. [동시 실행 범위와 실패 기록](work-log-2026-10.md#39-1014-공개-rpc-멀티체인-동시-실행)을 참조한다. 장기 운영·운영 SLO·mainnet 검증은 포함하지 않는다.
- **R01 완료(10/14):** 실제 mint/burn·최상위 거래의 from/to 밖 이동·실패 거래·수수료 표본 4개 블록의 55개 계정/블록 델타가 독립 RPC 전후 잔액과 일치했다. 별도 100블록에서 461개 계정/980개 블록별 잔액을 대조했고, 동일 RPC 기록의 Pebble/PostgreSQL 재생에서 25,118개 port 조회가 일치했다. [표본과 비교 범위](work-log-2026-10.md#38-1014-arc-회계-표본-확대동일-rpc-저장소-대조)를 참조한다. 전체 역사·fork·SELFDESTRUCT·mainnet 검증으로 확대 해석하지 않는다.
- R0~R6 계획은 [10/9 완료 검토](plan-audit.md)에 기록했다. 실시간 구독 단계는 [설계·완료 기록](subscriptions-design.md)에 있다.
- PostgreSQL 쓰기 batch·높이 기반 undo·잔액 prefetch는 반영됐다. 추가 성능 목표는 R04에 남는다.
- full node 시작 상태 검사와 SDK 안내, Sepolia 추가 헤더 필드 보존, 공개 RPC 검증, Arc 회계 수정은 완료했다. 모든 EVM 내부 이동·fork 실행·합의 증명까지 보장하지 않는다.
- Stable 제외와 EVM 범위 정리는 완료했다.
- [체인 커스터마이징 계약](../CHAIN_CUSTOMIZATION.md)과 [별도 profile 예제](../../examples/chainprofile/README.md)는 완료했다. 예제의 로컬 시험·빌드와 PR/main CI는 통과했으며 예제의 실노드 실행은 확인하지 않았다.

작업을 완료하면 이 문서에서 상태와 근거를 갱신한다. 측정의 입력·환경·실패·미검증 범위는 작업 기록에 남기고, 과거 수치를 현재 지원 보장으로 바꾸지 않는다.

# Feed 단계별 지연과 운영 판단

후속 [누적 API 재검증](SOURCE_FACTS.md#누적-api-재검증과-호스트-부하-확인-2026-10-17)은 호스트 포화로 미완료다. 동일 바이너리 대조와 변경 전후18회 계획 중6회 시도/5회 통과 후 기준 코드의 API2초 timeout1건으로 중단했다. 저부하 조건 통과0회이며3쌍 중앙값을 만들지 않는다. placement 측정 도구의 선택적 `--max-host-load`는 startup의 Docker 접근 전·실행 전후에 부하를 확인하고 `host-load.json`/부분 `runs.json`/원본 log를 남긴다. 이를 CPU/메모리/I/O 격리로 해석하지 않는다. 기존 API SQL1회·DB4·RPC5/fresh 구현은 그대로이며 추가 실행 코드 튜닝은 하지 않았다.

Streaming capture 후속 분석은 [회전 메타데이터 준비와128블록 재측정](SOURCE_FACTS.md#streaming-capture와-회전-메타데이터-준비-2026-10-17)을 따른다. 같은 블록의 raw seal/create·capture RPC 큐·SQL span을 분리했다. 독립 임시 metadata를 먼저 쓰되 각각의 full Sync/Close/rename·index/metadata directory barrier와 다음 raw 생성 순서는 유지한다. fsync 횟수 감소가 아니다. 모드별3쌍에서 Streaming capture p99 중앙값477.593→239.330ms였지만 첫 쌍과32블록 trace 진단은 악화했다. 누적 목록 API p99도13.569→58.024ms로 세 쌍 모두 증가했다. 기존 API SQL1회·DB4·5 HTTP/s·새 fresh 구현은 그대로지만 호스트 load164.67~243.08의 공유 환경이므로 수치상 API 개선 유지나 성능 수용을 완료로 표시하지 않는다.

누적 API의 후속 분석은 [최신 높이·블록 윈도우 통합 조회](SOURCE_FACTS.md#누적-api의-최신-높이블록-윈도우-통합-조회-2026-10-17)를 따른다. handler와 resolver의 goroutine이 다를 수 있어 같은 goroutine의 SQL 수만으로 RPC0/SQL0을 판단하지 않는다. 별도 observer overlay의 backend PID/시각과 요청별 wire/서버 parse·bind·execute를 대응하고 모호한 매칭을 제외했다. 이 overlay는 본 측정/운영 바이너리에 없다. 기본 목록은 같은 statement의 tip/높이 윈도우로 SQL2→1회이며 fresh 검사를 공유하거나 줄이지 않는다. busy token을 근거로 유지보수 grace를 줄이는 후보는 Streaming HTTP를 늘려 제외했고 기존 정책을 유지했다. 누적 API 중앙값 감소와 Streaming capture/일부 API tail 증가는 함께 기록한다.

RPC 역할/배치 합류의 후속 실험은 [유지보수 RPC와 fresh](SOURCE_FACTS.md#유지보수-rpc의-fresh-배치-합류-2026-10-17)를 따른다. trace의 `rpc.readRole`은 context의 선택적인 foreground/maintenance 힌트이고 `rpc.window`/`rpc.first`는 첫 요청과 모으기 시간을 연결한다. 역할은 완료/삭제 증거가 아니며 HTTP header/params로 전송하지 않는다. ControlStep의 transaction 밖 사전 조회만 표시하고 데이터/writer/finality commit은 원래 context를 쓴다. 낮은 rate/burst1에서 유지보수만 최대500ms 모으며 foreground 도착 시 즉시 끝난다. caller deadline·5 HTTP/s·새 fresh·응답 뒤 epoch는 유지한다. 모으기 예산과 전체 RPC latency/foreground 대기를 구분한다. streaming 개선과 누적 API tail 증가도 함께 확인해야 한다.

현재 계측은 window feed의 병목을 찾는 **진단 기능**이다. 실제 Arc peer·공개 RPC의 상태 보존 기간·450ms/2블록/s·24시간 운영은 아직 검증하지 않았다. 완료/삭제의 근거는 기존 DB cursor·proof 검증·fresh canonical·raw ACK/lease이며 계측값이 이를 대체하지 않는다.

## 설정과 조회

[기존 feed 설정](FEED_APP.md)의 `source.feed` 또는 `multichain.chains[].feed` 아래에 추가한다.

```yaml
telemetry:
  receive_interval_target: 450ms
```

`telemetry: {}`의 목표 기본값도450ms다. 필드가 없으면 실행 계측은 꺼지며, 음수·1분 초과·알 수 없는 필드는 거부한다. 현재 운영 config.yaml은 바꾸지 않았다. 설정은 수집 plan/필수 상태 계약을 바꾸지 않아 기존 DB에도 명시적으로 켤 수 있다.

`GET /indexing/status`의 `pipeline`에서 조회한다. 멀티체인은 `/chains/{id}/indexing/status`다. 기존 bounded/read-only/no-store/RPC0 경로이며 읽기만으로 DB를 쓰거나 RPC quota를 쓰지 않는다. source/worker 상태는 한 capture DB snapshot이고, runtime 값은 별도 메모리 snapshot이므로 두 관측을 원자적이라고 해석하지 않는다.

| 필드 | 의미 |
| --- | --- |
| `proofBacklog` | 지정 시작 높이 이후 captured prefix 중 proof 미완료 수. proof cursor가 stale이면 null |
| `stateBacklog` | 같은 범위에서 상태 후처리 미완료 수. state cursor가 stale이면 null |
| `readyStateBacklog` | 이미 proof가 준비됐지만 상태 후처리가 끝나지 않은 수. 두 현재 cursor를 비교할 수 없으면 null |
| `proofOldestPendingAgeMs` | 첫 미완료 proof 입력을 **이 실행에서 capture commit 성공 후 관측한 때**부터 지난 시간 |
| `stateOldestReadyAgeMs` | 첫 미처리 상태 입력을 **이 실행에서 proof 완료 후 관측한 때**부터 지난 시간 |
| `networkHead` | 현재 항상 null. 추가 head RPC를 보내거나 최근 수신 높이를 실제 네트워크 latest라고 표시하지 않음 |
| `runtime.observedHeader` | 이 실행에서 마지막으로 정상 envelope를 받은 header의 높이/hash/시각/경과 시간. canonical/연속 완료나 실제 latest 보장이 아님 |
| `runtime.progress` | capture/proof/state 각 단계에서 성공한 exact input/시각/경과 시간. source generation이 다르면 제외 |

age가 null이면 측정되지 않았거나 해당 적체가 없다. 해당 backlog도 함께 확인한다. 원격 상태가 만들어진 때부터의 나이가 아니므로 원격 pruning까지 남은 시간으로 사용할 수 없다. 재시작 전에 수집한 입력·계측 전 이미 처리된 입력·1024개 범위 밖 입력은 정확한 단계 간 시각을 알 수 없다. DB 가시화 직후 다음 worker가 먼저 실행되는 경우에도 queue 시간은 측정 없음일 수 있다. 0으로 보완하지 않는다.

## 단계 정의

`runtime.metrics`는 stage별 누적 count/failures/totalMs, 최근 최대256개 표본의 nearest-rank p50/p95/p99, inFlight와 busyForMs를 제공한다. 표본이 없으면 분위수는 null이다. 실패·취소도 해당 operation 표본에 포함한다. `busyForMs`는 해당 stage의 active 수가 계속0이 아니었던 기간이며 가장 오래된 단일 요청의 시간과 같다고 보장하지 않는다.

| Stage | 포함하는 시간 |
| --- | --- |
| `receiveNext` | envelope 한 개를 반환할 때까지의 adapter 대기/조회. 블록 미생성·peer 재연결 대기도 포함 |
| `receiveBlockInterval` | 연속 높이의 성공한 header envelope 도착 간격. 블록 생성 대기·이전 데이터 append·peer fetching 포함; 같은 높이 교체/높이 불연속 간격은 표본 제외. fork 검증은 기존 capture 경로가 담당 |
| `rawAppend` | window quota/backpressure 대기와 append/fsync. 배치 adapter는 같은 블록의 최대3개 dataset을 한 작업으로 측정; 단일 입력/배치 실행의 count·percentile 분모가 다름 |
| `rawQueue` | 마지막 required frame의 원래 FirstReceived부터 assemble 시작까지. append/fsync·재시작 중단·canonical 후보 대기 포함; wall clock이 뒤로 가면 표본 제외 |
| `assemble` | raw 읽기·profile/root 등 기본 입력 검증 |
| `captureCommit` | source DB transaction·fresh 검사·내구성 대기·활성 GC fence 대기 |
| `captureInterval` | 같은 generation의 연속 capture 성공 관측 간격 |
| `proofQueue` | 관측한 capture 완료부터 성공한 proof acquisition 시작까지 |
| `proofAcquire` | DB 입력 조회·기존 proof 재검증 또는 신규 RPC batch·검증·커밋 전체 |
| `proofBatch` | 논리 proof/code batch와 응답 멤버 검사. limiter·다른 요청과의 배치 대기 포함 |
| `proofVerify` | header/MPT/code 검증. 부모 checkpoint 검증 포함 |
| `proofCommit` | proof 또는 부모 checkpoint의 DB transaction·fresh·내구성 대기 |
| `stateQueue` | 관측한 proof 완료부터 성공한 state apply 시작까지 |
| `stateApply` | DB proof 재검증·변경 계산·자체 head/undo/event transaction·fresh·내구성 대기. 정상 proof 미완료 polling은 표본 제외 |
| `traceAcquire` / `traceBatch` / `traceVerify` / `traceCommit` | 선택 호출 trace의 전체 수집·논리 RPC batch·형식/최상위 대조·fresh/내구성 커밋. [검증 범위](CALL_TRACES.md) |
| `blobAcquire` / `blobSupply` / `blobVerify` / `blobCommit` | 선택 전체 blob library의 수집·별도 공급자 읽기·KZG/거래 위치 검증·fresh/내구성 커밋. 실제 transport/앱 자동 연결은 아직 없음. [계약](BLOB_INPUTS.md) |
| `rpcLimitWait` | 기존 pool limiter의 token 대기. coalescer 예약과 failover의 추가 예약 모두 포함 |
| `rpcHTTP` | 실제 endpoint HTTP 시도와 bounded 응답 body 읽기. 실패 endpoint도 각1회로 집계 |

duration들은 중첩되므로 합산하여 end-to-end 지연으로 만들지 않는다. queue 두 값은 성공한 downstream 작업의 시작점을 사용하므로 작업 시간은 제외한다. proof parent bootstrap은 `proofBatch/Verify/Commit`에 포함되며 ordinary `proofAcquire` 블록 수와 동일하지 않다. JSON-RPC 오류 멤버는 HTTP transport 자체가 성공했을 수 있다. 물리 HTTP와 논리 batch/member 수를 섞지 않는다.

공유 pool의 기존5 HTTP/s·burst1·예약·failover 경로를 그대로 계측한다. body/endpoint URL·키·proof payload를 보관하지 않는다. fresh 요청을 생략/캐시하거나 추가 조회를 보내지 않는다. startup 후 recorder가 붙은 구간만 집계하므로 pool lifetime 전체 요청 수와 같다고 가정하지 않는다.

## 수명과 메모리 한도

stage별 표본256개, 각 완료 단계의 exact input 시각 최대1024개, 프로세스의 recorder 최대64개다. raw 작업 큐의 용량/24시간 저장 범위와 별개다. registry는 새 random runId로 소유자를 분리하고 chain/genesis/anchor/plan을 대조한다. payload·proof·DB 연결을 recorder에 넣지 않는다.

runId/목표만 기존 projection descriptor 기록에 포함한다. hot-path 진단용 DB transaction·SQL/table/fsync·영구 이벤트를 추가하지 않는다. 재시작 후 새 runId와 빈 실행 표본을 사용한다. 별도 프로세스의 read-only API나 종료된 recorder는 `runtime_unavailable`, 미설정은 `disabled`이며 DB backlog는 계속 제공한다. 같은 프로세스의 read-only DB handle은 같은 runId를 조회할 수 있다.

managed task들을 취소/join하면 `stopped`를 표시한다. 공유 RPC dispatcher에 이미 예약된 token/요청은 잠시 drain 중일 수 있으며 inFlight로 보인다. App Shutdown이 기존 pool을 닫고 recorder를 해제한다. 진단 snapshot의 나이는 조회 시점까지 증가한다. 모든 stage 표본은 실행 전체(여러 epoch 포함)의 관측이며 현재 epoch의 완료 보증이 아니다.

## 운영 기준과 다음 측정

1. `receiveBlockInterval` 최근 표본2개 이상에서 p95와450ms 목표를 비교해 `within_target`/`above_target`을 표시한다. 그 전에는 `unmeasured`다. chain이 느리거나 멈춘 구간도 간격에 포함하므로 이것은 수신 관측 경고다. worker의 CPU 처리 속도나 운영 SLO 통과 판정으로 쓰지 않는다. 실제 생성률과 관측 구간/표본 수를 함께 기록해야 한다.
2. 같은 실제 입력·선택 범위·RPC/DB/window 조건으로 기존 RPC와 feed를 비교하고 수신 간격, capture/proof/state backlog의 시간 변화, 각 p95/p99와 RPC 대기를 함께 본다. RPC 대기가 주원인이면 내부 큐 확대만으로 지속 처리량이 늘지 않는다. proof batch와 모든 fresh 요청이5 HTTP/s를 공유하며, batching은 provider의 member quota나 state retention을 늘리지 않는다.
3. proof 확보는 원격 trie가 삭제되기 전에 끝나야 한다. provider가 보장하는 **실제 보존 높이/시간과 중단 여유**를 확인한 뒤 수집/공급 age·backlog 경보선을 정한다. 현재는 그 보장을 모르므로 ‘24시간 안전’이나 임의 숫자의 pruning 안전선을 표시하지 않는다. 원본 window24시간은 원격 계정/storage proof24시간을 보장하지 않는다. proof 미공급/pruned는 기존 strict 오류이며 latest/0으로 건너뛰지 않는다.
4. proof가 영구 DB에 저장된 뒤의 상태 후처리는 그 입력으로 재생할 수 있다. readyStateBacklog가 늘면 stateApply/DB/fresh/RPC 경합을 확인한다. required proof/기존 raw ACK/finality/lease/최소24시간 삭제 gate는 계측과 독립적으로 유지한다.
5. 실제 enode·외장 mount·provider 보존 정보가 준비되면 짧은 연속 관측→장기/재연결/장애 시험 순으로 진행한다. 로컬 fixture 결과를 실제 Arc 처리량·24시간 용량·포괄 state/trace/blob 완료로 확대하지 않는다.

선택 [원천 검색](SOURCE_QUERIES.md) worker의 `lookupVerify`는 영구 source 읽기/commitment 재검증을, `lookupCommit`은 trie 구성·DB transaction·fresh 대기와 commit을 포함한다. 자체 완료점은 status workers의 `source.lookup.v1`이다. 표본 부재를0이나 검색 완료로 해석하지 않는다.

선택 source.search.v1은 `searchVerify`(영구 source/commitment 검증)와 `searchCommit`(trie 구성·transaction·fresh/commit)을 추가한다. 완료점은 worker cursor이며 HTTP 검색의 scannedCandidates/decodedBlocks는 요청별 검사량이다. 런타임 지연 표본과 실제 Arc 처리량·지속 운영을 구분한다.

## Fresh와 API의 별도 runtime trace

Go runtime trace를 명시적으로 켠 진단 실행에서는 writer fresh의 사전 검사·RPC·응답 후 epoch 검사를 구분한다. `postgres.query`/`postgres.acquire` task는 query/row 소비와 연결 획득을 나누며, 다른 goroutine에서 종료될 수 있다. `rpc.read` ID를 dispatcher의 member ID와 연결해 논리 요청의 큐 대기와 실제 `rpc.http` 시도를 구분한다. 기존 공유 limiter의 `rpc.limitWait`와 큐 대기의 **겹친 구간**은 기록할 수 있지만, 그 전체를 특정 요청의 token 대기로 단정하지 않는다. 추가 SQL/RPC·새 limiter·fresh 캐시는 없다. trace가 꺼진 일반 실행에는 새 요청 ID 할당과 trace용 clock/수집을 하지 않는다.

API handler와 GraphQL resolver는 서로 다른 goroutine일 수 있다. handler 아래의 같은 goroutine SQL 개수만 세면 실제 조회가0회로 잘못 보일 수 있으므로, 요청 context의 `ObserveQueries` 진단을 사용한다. `AcquireMS`는 연결 admission, `QueryMS`는 드라이버의 응답·행 소비까지 포함한 시간이다. `ServerMS`에서 이 두 시간을 뺀 값은 handler/GraphQL/스케줄링을 포함한 나머지 구간이며, HTTP 왕복 전체와도 구분한다. PostgreSQL의 statement duration은 별도 서버 측 관측이다. 요청별로 연결하지 않은 SQL 모양별 집계와 API percentile을 차감하거나, 서로 다른 표본의 stage percentile을 합산하지 않는다.

runtime trace·서버 SQL duration logging·IO timing은 별도 짧은 진단에만 사용하고 원래 설정으로 복원한다. 전후 성능 비교는 이를 끈 상태에서 실행한다. [Fresh DB/RPC와 API tail 분석](SOURCE_FACTS.md#fresh-dbrpc-대기와-api-tail-분리-2026-10-17)을 따른다.

capture와 bound writer의 transaction-local SQL에도 기존 API와 같은 socket 취소 감시를 사용한다. 정상 statement 종료마다 감시 goroutine과 채널로 만나는 비용을 줄이고, rows 소비/취소 callback 종료 후 소켓을 다음 statement에 돌려준다. 원래 context의 scope·fresh·취소 검사와 BEGIN/COMMIT/ROLLBACK·SendBatch·pool admission은 그대로다. profile에서 누적 scheduling delay가 줄어도 raw fsync·RPC 슬롯·WAL/HTTP tail이 남을 수 있다. [후속 비교](SOURCE_FACTS.md#capture와-writer-sql-종료-대기-감소-2026-10-17)는 API tail 감소와 streaming capture p99 증가를 함께 기록한다.

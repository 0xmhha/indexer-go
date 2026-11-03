# Feed 파이프라인 계측

window feed의 runtime 표본과 단계별 대기를 설명하는 opt-in 진단 기능이다.
관측한 완료·시각과 미검증 received/backlog를 구분하며, 누락 표본을0으로 만들지 않는다.
이 기능은 처리량·450ms 목표·24시간 보존이나 실제 Arc peer 운전을 보장하지 않는다.

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
| `runtime.rawIntake` | 이 실행의 수신 세션이 durable append한 raw 중 아직 capture하지 않은 블록. 세션 시작 전이면 null |

age가 null이면 측정되지 않았거나 해당 적체가 없다. 해당 backlog도 함께 확인한다. 원격 상태가 만들어진 때부터의 나이가 아니므로 원격 pruning까지 남은 시간으로 사용할 수 없다. 재시작 전에 수집한 입력·계측 전 이미 처리된 입력·1024개 범위 밖 입력은 정확한 단계 간 시각을 알 수 없다. DB 가시화 직후 다음 worker가 먼저 실행되는 경우에도 queue 시간은 측정 없음일 수 있다. 0으로 보완하지 않는다.

`rawIntake`의 필드는 다음과 같다. `startHeight`는 세션이 시작한 capture cursor다. `receivedThrough`는 그 위로 모든 높이가 같은 hash의 필수 dataset을 다 받았거나 capture된 마지막 높이다. 검증 전의 수신 기록이며 canonical 완료점이 아니다. `pendingBlocks`는 받았지만 capture하지 않은 블록 수이고, 일부 dataset만 받은 블록과 fork 후보도 센다. capture된 높이 이하의 블록은 fork를 포함해 제외한다. `oldestPending`은 그중 가장 이른 window `FirstReceived`와 경과 시간이다. 이전 실행이 저장하고 이번 세션이 다시 전달한 frame은 원래 수신 시각을 쓴다. 기록은 최대1024블록이며 넘으면 새 블록을 버리고 `pendingTruncated`를 true로 둔다. 이때 `receivedThrough`는 하한이다. wall clock이 저장된 수신 시각보다 뒤에 있으면 `oldestPending`은 null이다. `appendedFrames`/`appendedBytes`는 실행 동안 window에 새로 쓴 frame 수와 bytes다. 이미 있던 frame을 돌려받은 append는 세지 않으며, 수거로 줄지 않는다. 세션이 epoch 변경으로 다시 시작해도 누적값은 유지하고 대기 기록만 새로 시작한다.

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

## 관측 해석

원본 수신·검증·DB 준비·worker 완료·처리 ACK는 별도 단계다. source의 verified
prefix와 runtime 메모리 표본을 같은 atomic snapshot으로 합치지 않는다. proof나
worker cursor가 stale이면 해당 backlog는 UNKNOWN이며, 높은 received-through를
실제 network head로 바꾸지 않는다. 지표는 기존 fresh/ACK/lease/GC 권한을 대체하지 않는다.

선택 lookup/search worker의 Verify/Commit 지표는 해당 worker의 읽기/검증과
transaction/fresh/commit 경로를 뜻한다. HTTP 검색의 scannedCandidates/decodedBlocks는
요청별 검사량이며 runtime 지연이나 전체 체인 처리량과 다르다.

## Fresh와 API의 별도 runtime trace

명시적 Go runtime trace는 writer의 사전 검사·RPC·응답 후 epoch fence를 구분한다.
postgres.query/acquire는 행 소비와 연결 획득, rpc.read/http는 논리 요청과 실제
HTTP 시도를 구분한다. limiter와 큐의 겹친 시간을 특정 요청의 대기로 단정하지 않는다.
role/window 힌트는 관측 metadata이며 완료/삭제 증거가 아니다.

API handler/resolver의 goroutine과 중첩 구간을 구분하고, 신뢰된 request context의
ObserveQueries·GraphQL observer를 사용한다. 현재 [API 진단·오류 판정](SOURCE_FACTS.md#api-진단과-오류-판정)을 따른다.
표본/지표만으로 API 지연 원인이나 개선율·운영 SLO를 확정하지 않는다.
trace/SQL logging/IO timing은 별도 허용된 진단 범위에서만 사용한다.
실제 peer/mount/provider와 실행·추가 성능 측정은 [작업 목록](analysis/open-items.md)의
보류를 유지한다. 과거 수치는 Git history로 확인한다.

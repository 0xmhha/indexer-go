# 실시간 이벤트 구독과 조건식 알림 설계 (10/10)

이 문서는 프리미엄 기능인 실시간 이벤트 구독과 조건식 알림, 그 전제인 declared 모드의 멀티체인·era1 지원을 설계한다. 구현은 7장의 단계로 나누어 PR마다 검증한다.

---

## 1. 결론

사용자는 감시할 대상(EOA, 컨트랙트 전체, 컨트랙트의 특정 이벤트, 토큰 이동)을 구독으로 등록하고, 선택적으로 CEL 조건식과 보낼 내용을 고르는 식을 붙인다. indexer는 블록을 받아 decode한 직후, 저장 commit을 기다리지 않고 구독과 조건을 평가해 참인 것만 사용자의 스트림 연결이나 webhook으로 보낸다(fast path). 사용자 코드(WASM 모듈, 컨테이너)는 실행하지 않는다. 구독은 기존 알림 기능(`pkg/notifications`)을 확장해 만든다.

결정(10/10, 사용자):
- 로직: 사용자 코드를 실행하지 않는다. 구독 유형(검토안 1.3)을 기본으로, 조건식(검토안 1.2)을 선택으로 둔다. Docker sandbox(검토안 1.1)와 WASM은 쓰지 않는다(2장).
- 액션: 스트림 push와 webhook. 트랜잭션 전송은 하지 않는다.
- 실행 시점: 저장 전 fast path.
- 순서: 설계 문서 → 데이터 소스(declared 멀티체인, era1) → 구독과 조건식.

선행 조치(10/10, #123): 알림 API는 `api.keys`의 키가 있어야 쓰고, webhook·Slack은 내부 주소로 보내지 않는다(등록 때와 연결 때 검사, redirect 따르지 않음). 이 설계의 webhook은 이 방어 위에 선다.

---

## 2. 검토한 방법

### 2.1 왜 indexer 안에서 평가하는가

지금 사용자는 구독(`/graphql/ws`)으로 이벤트를 받아 자기 시스템에서 파싱·판단한다. 지연은 세 곳에서 생긴다(R5-5, 한 호스트 loopback).

| 구간 | p99 |
|---|---|
| 노드가 블록을 보인 때 → indexer가 블록을 색인 | 60~77ms. 대부분 50ms 폴링이며 `rpc.ws_endpoint`(newHeads)를 쓰면 1~2ms |
| commit → outbox relay → 구독 엔진 → WebSocket | 40~52ms |
| 사용자 시스템의 파싱·판단 | indexer 밖 |

indexer 안에서 조건을 평가하면 둘째 구간의 commit·relay 대기와, 조건이 거짓인 이벤트를 사용자가 받아 버리는 일이 사라진다.

### 2.2 세 방법

| 기준 | 1.1 Docker sandbox에서 사용자 코드 | 1.2 조건식만 | 1.3 구독 유형 확장 |
|---|---|---|---|
| 사용자 코드 실행 | 있음, 컨테이너로 격리 | 없음. CEL은 I/O·무한 루프가 없고 비용 상한이 있다 | 없음 |
| 보안 위험 | 컨테이너 탈출(커널 공유), indexer가 Docker socket을 쥐면 host root와 같은 권한 | 식의 CPU 비용 → 비용 상한 | 낮음 |
| 표현력 | 상태 있는 로직까지 | 이벤트 하나 안의 조건과 가공 | 고른 이벤트를 그대로 |
| 추가 지연(측정) | 컨테이너 왕복 p50 약 245µs, p99 약 575µs(macOS Docker VM, host loopback은 19µs·64µs), 기동 수 초 | 평가 1회 약 0.17µs | 없음 |
| 운영 부담 | 테넌트마다 컨테이너·이미지·자원 한도·장애 복구 | 라이브러리 하나 | 기존 알림 기능 재사용 |

1.1은 격리해도 사용자 코드를 실행한다는 위험과 Docker 권한 문제가 남고, 운영 부담과 지연이 크다. 상태 있는 가공이 꼭 필요해지면 그때 별도 프로세스의 WASM 실행 서비스(gVisor 등으로 격리)를 따로 설계한다.

### 2.3 엔진 비용 실측 (참고)

조건 하나(`amount > 1000 && pool == X && sender != Y`)를 이벤트 하나에 평가한 비용(Apple Silicon, 저장소 밖 임시 모듈): native Go 3.7ns, WASM(wazero) 30ns(호출마다 deadline 770ns), expr 130ns, Lua 135ns, CEL 167ns, JavaScript(goja) 560ns. 어느 쪽이든 ms 단위인 파이프라인 지연보다 훨씬 작아, 엔진은 안전성으로 골랐다(CEL: 등록 때 타입 검사, 비용 상한 `cel.CostLimit` 확인, 부작용 없음).

---

## 3. 데이터 소스 확장 (전제)

### 3.1 declared 모드의 멀티체인

지금은 `Config.Validate`가 거부한다(`internal/config/config.go`, `ModeDeclared`). 막힌 이유:

- 체인별 기능 설정이 없다. `chainAppConfig`(`pkg/app/app.go`)가 설정을 얕게 복사해 모든 체인이 `cfg.Features`를 공유한다. 컨트랙트 주소는 체인마다 다르므로 `features.records`를 체인마다 적어야 한다.
- 체인별 API(`pkg/api/chains.go`)가 declared 모드를 모른다. `ExtensionsOnly` 없이 GraphQL을 만들고 JSON-RPC·REST도 붙인다.

설계:

- `multichain.chains[].features`를 둔다. 형식은 최상위 `features`와 같고 체인 항목에 적은 기능이 최상위 값을 덮어쓴다. `chainAppConfig`는 `Features`를 깊게 복사한 뒤 덮어쓴다.
- `indexer.mode`는 전체에 하나다.
- 체인별 API는 그 체인 App이 declared 모드면 `ExtensionsOnly`로 GraphQL을 만들고 JSON-RPC·REST를 붙이지 않는다.
- 검증: 두 시험 체인에 서로 다른 records 표를 선언하고 각 체인 DB에 그 체인의 레코드만 있는지, 체인별 `/chains/<id>/graphql`이 `records`를 답하는지 확인한다.

### 3.2 declared 모드의 era1

지금은 설정 검증이 거부하고, 그 전에 `declaredSource`(`pkg/app/declared.go`)가 era와 노드를 이은 source를 버리고 노드의 `sourcerpc.Logs`만 돌려준다. era1 영수증에는 로그가 들어 있다(`evm.Profile.DeriveReceipts`).

설계:

- `pkg/source/era`에 declared용 `Logs`를 둔다. `era.Source`를 감싸 블록마다 영수증의 로그를 선언한 주소·topic0로 거르고 헤더에서 트랜잭션을 뺀다(`sourcerpc.Logs`와 같은 모양).
- `fetch.RangeSource`(`LogsInRange`, `Headers`)도 구현해 finalized 범위 경로에서 쓴다. 파일에는 범위 색인이 없으므로 범위의 블록을 모두 읽는다.
- era 범위는 era `Logs`, 그 뒤는 노드 `Logs`가 답하는 연결 source를 만들고 만나는 높이의 hash를 확인한다(`CheckJoin`).
- 단점: 로그가 드문 컨트랙트는 노드의 `eth_getLogs`보다 느릴 수 있다. 노드 부담은 없다. 실제 era1 파일로 비교는 파일이 생기면 한다.
- 검증: 시험용 era1 파일(`writeEra1` 도우미)과 시험 체인으로, 결과가 노드만으로 색인한 DB와 같은지 확인한다.

---

## 4. 구독과 조건식

### 4.1 기존 알림 기능과의 관계

`pkg/notifications`에는 이미 설정(주소·topic 필터, webhook 목적지·서명 비밀), 저장, 재시도, 전달 기록, API(GraphQL·JSON-RPC)가 있다. 구독은 이것을 확장한다. 새로 더하는 것은 구독 유형, 이벤트 decode, 조건식, fast path 전달 모드, 스트림 채널, 키별 소유다. 같은 일을 두 벌로 만들지 않기 위해서다.

| 항목 | 지금 알림 | 더하는 것 |
|---|---|---|
| 대상 | 이벤트 종류 + 주소·topic 필터 | 구독 유형(4.2), 이벤트 시그니처로 인자 decode |
| 조건 | 필터만 | CEL 조건식, 보낼 내용 식(4.3) |
| 시점 | commit 뒤 outbox, 빠짐·중복 없음 | `delivery: fast`(commit 전, at-least-once) 선택 |
| 채널 | webhook, Slack, email | 스트림(WebSocket) |
| 소유 | 없음(모든 키가 모든 설정을 봄) | 설정마다 만든 키의 label, 그 key만 보고 바꾼다 |

### 4.2 구독 유형

| 유형 | 내용 | 수집 모드 |
|---|---|---|
| `address_activity` | EOA나 컨트랙트가 보내거나 받은 트랜잭션, native transfer | full만(declared 모드는 트랜잭션을 읽지 않는다) |
| `token_transfer` | ERC-20/721 `Transfer` 중 from 또는 to가 그 주소 | full, declared |
| `contract_logs` | 그 주소의 모든 로그 | full, declared |
| `contract_event` | 주소 + 이벤트 시그니처. 인자를 decode해 보낸다 | full, declared |

declared 모드에서는 구독의 주소·topic이 수집 대상에 더해져야 한다. 구독이 바뀌면 declared source의 주소·topic 목록을 갱신한다(`sourcerpc.Logs`가 목록 교체를 받게 한다). 구독은 등록한 다음 블록부터 적용하고 과거 블록을 다시 평가하지 않는다.

### 4.3 조건식 (CEL)

```yaml
type: contract_event
chain: "8283"                 # 멀티체인에서
address: "0x..."
event: "Swap(address indexed sender, int256 amount0, int256 amount1, uint160 sqrtPriceX96)"
condition: 'event.amount0 > 1000000'                       # 선택
payload: '{"price": event.sqrtPriceX96, "block": block.number}'  # 선택
delivery: fast                # fast | durable
channels: [stream, webhook]
webhook_url: https://...
```

- 변수: `event`(인자, 타입대로), `log`(address, txHash, index), `tx`(from, to, value, address_activity에서), `block`(number, time, hash), `chain`(id).
- 등록할 때 식을 그 이벤트 인자 타입으로 타입 검사하고, 비용 추정이 한도 안인지 본다. 예제 이벤트를 주면 한 번 평가해 결과를 돌려준다(dry run). 실행 중에 드러나는 오류를 줄이려는 것이다.
- 실행할 때 `cel.CostLimit`로 비용을 막는다. 식은 부작용이 없으므로 실패해도 다른 구독에 영향이 없다.
- uint256은 CEL `uint`(64비트)를 넘으므로 비교 확장 함수(`big.cmp(a, "1000")` 등)를 둔다. 구현 단계에서 확정한다.

### 4.4 실행 위치 (fast path)

- tap: 블록이 높이 순서로 나오는 곳, `indexRange`의 reorder buffer가 블록을 writer로 넘기는 지점(`pkg/fetch/scheduler.go`, `f.indexBlock` 호출 직전). finalized 범위 경로는 `indexSparse`가 범위를 writer로 넘기기 직전(`pkg/fetch/sparse.go`)이며 결과는 범위 단위로 나간다.
- tap은 블록을 평가기의 대기열에 넣기만 한다(non-blocking). 수집은 평가 때문에 기다리지 않는다. 평가기는 체인마다 goroutine 하나가 블록 순서대로 처리한다: 블록의 로그·트랜잭션을 구독 색인(주소, topic0)으로 고르고, decode하고, 조건식을 평가하고, 참인 것을 전달기로 넘긴다.
- 대기열이 넘치면 그 블록의 평가를 버리고 소유자에게 `subscriptions_lagging`(어느 블록부터)을 알린다. 조용히 버리지 않는다.
- `delivery: durable` 구독은 지금처럼 commit 뒤 outbox로 처리한다.

### 4.5 전달

- 메시지마다 멱등 키 `(chain, block, log index 또는 tx index, 구독 id)`를 붙인다. fast는 commit 전이라 재시도·재시작 때 같은 메시지가 다시 갈 수 있다(at-least-once).
- reorg가 있는 체인(finality head)에서는 되돌린 블록의 메시지에 `retract`(같은 멱등 키)를 보낸다. StableNet(WBFT)은 블록이 들어가는 순간 확정되어 `retract`가 없다.
- 스트림: key로 `/v1/subscriptions/stream`(WebSocket)에 연결한다. 연결마다 크기가 정해진 대기열을 두고 넘치면 끊는다(`stream.Engine`의 느린 구독자 규칙). 끊긴 동안의 fast 메시지는 보관하지 않는다. 놓치면 안 되면 webhook이나 durable을 쓴다.
- webhook: 기존 전달(HMAC-SHA256 서명, 지수 재시도, 전달 기록, #123의 목적지 검사)을 쓴다. 전달은 별도 worker pool에서 하므로 느린 webhook이 평가를 막지 않는다.
- 증폭 방지: 목적지(host)마다 초당 호출 상한을 둔다. 남의 서버를 목적지로 등록해 요청을 쏟는 것을 막으려면 등록 때 목적지가 challenge에 응답하게 하는 확인을 선택으로 둔다.

### 4.6 오류를 놓치지 않기

- 조건식 컴파일 오류, 이벤트 시그니처 오류, 목적지 오류는 등록할 때 거절한다.
- 실행 오류(비용 초과, 형식 오류)는 구독마다 센다. 소유자의 스트림에 `subscription_error`(구독, 블록, 원인)를 보내고, Prometheus에 구독별 오류 수·평가 시간을 낸다. 연속 N번(기본 10) 실패하면 구독을 멈추고 알린다.

### 4.7 인증, 소유, 한도 (프리미엄)

- 인증: `api.keys`(#123). 구독 API와 스트림은 key가 반드시 필요하다.
- 소유: 설정·구독마다 만든 key의 label을 기록하고, 그 label의 key만 보고 바꾼다. 지금 알림 설정에는 소유자가 없으므로 기존 설정은 운영자 key(설정으로 지정)만 볼 수 있게 옮긴다.
- 한도(label별 설정): 구독 수, 블록당 평가 시간 합, 초당 메시지 수, 동시 스트림 연결 수. 넘으면 그 label의 구독만 멈추거나 메시지를 버리고 알린다.
- 저장: 구독은 알림 설정과 같은 곳(체인 저장소의 알림 키)에 둔다. 멀티체인에서는 알림이 체인마다 꺼져 있으므로(`chainAppConfig`) 구독 저장 위치를 최상위로 옮기는 일이 함께 필요하다.

### 4.8 지연 목표 (가설)

같은 호스트, newHeads 사용 기준으로 "노드가 블록을 보인 때 → 스트림 메시지 도착" p99 ≤ 10ms를 목표로 둔다 [Low: 측정 전 추정]. 마지막 단계에서 `TestSLOLoad`와 같은 방식으로 재고 확정한다.

---

## 5. 하지 않는 것

- 사용자 코드 실행(WASM, 컨테이너): 2.2절. 필요해지면 별도 프로세스·강한 격리로 따로 설계한다.
- 트랜잭션 전송: 서명 키 보관(KMS/HSM), 한도, 오작동 방지 설계가 먼저 필요하고 자금 손실 위험이 있다.
- 여러 이벤트를 누적하는 가공(이동평균 등): 조건식은 이벤트 하나 안에서만 계산한다. 누적 가공은 사용자 쪽에 남는다.
- 과거 블록 재평가, mempool 감시.

---

## 6. 위험과 단점

| 위험 | 대응 | 남는 단점 |
|---|---|---|
| 조건식 비용이 평가를 늦춤 | 등록 때 비용 추정, 실행 때 비용 상한, 평가기를 수집 경로와 분리 | 부하가 크면 평가를 버린다(알림은 간다) |
| fast 중복 전달 | 멱등 키, reorg 체인에서는 `retract` | 사용자가 중복을 걸러야 한다 |
| 스트림이 끊긴 동안 유실 | webhook·durable 선택 | 스트림은 지연 우선, 보관하지 않는다 |
| webhook 증폭 공격 | 목적지별 호출 상한, 선택적 challenge 확인 | challenge를 끄면 남는다 |
| 소유 구분 없음(지금) | label별 소유 | 기존 설정을 옮겨야 한다 |
| uint256 비교 | 확장 함수 | CEL 기본 정수보다 쓰기 불편하다 |

---

## 7. 단계

| 단계 | 내용 | 검증 |
|---|---|---|
| 1 (완료 10/10) | declared 멀티체인: `chains[].features`, 체인별 declared API | 체인마다 다른 표, 체인별 `records` 조회 (`TestMultiChainDeclaredMode`, `TestChainFeaturesFromYAML`) |
| 2 (완료 10/10) | declared era1: era `Logs`와 `RangeSource`, 연결 source | era 파일 + 시험 체인 결과가 노드만으로 색인한 DB와 같다 (`TestDeclaredModeReadsEra1Archives`, `TestChainedLogsSplitsAtTheArchive`) |
| 3 (완료 10/10) | 소유와 한도: 설정의 소유 label, 기존 설정은 운영자만, label별 설정 수 한도(평가 시간·메시지·연결 한도는 해당 기능과 함께 4~6단계) | 다른 key의 설정을 보거나 바꿀 수 없다 (`TestSettingsBelongToTheirKey`, `TestSettingsQuota`, `TestNotificationSettingsAreSeparatedByKey`) |
| 4a (완료 10/10) | 구독 유형: 기존 이벤트 종류·필터 위에 `event`(decode)와 `participants`를 더함. address_activity는 transaction + addresses, contract_logs는 log + addresses, contract_event는 log + event, token_transfer는 token_transfer + participants. 두 API가 topics를 버리던 결함을 고침 | `TestNotificationOfOneDecodedEvent`, `TestFilterInputParse` |
| 4b (완료 10/10) | fast path: `fetch.BlockTap`, 알림 서비스의 블록 대기열과 평가기, `delivery: fast`, 대기열 넘침은 버리고 경고(소유자에게 알리는 것은 4c 스트림과 함께) | commit을 붙잡은 동안 fast 알림이 도착하고 각 이벤트는 한 번 (`TestFastNotificationsArriveBeforeTheCommit`, `TestFastPath*`) |
| 4c | 스트림 채널 | 등록 → 시험 체인 이벤트 → 스트림 수신 |
| 5 | 조건식: CEL 타입 검사·dry run·비용 상한, payload 식, 오류 보고 | 조건이 거짓이면 오지 않는다, 비용 초과 구독만 멈춘다 |
| 6 | webhook 목적지별 상한, metrics, 지연 측정 | 지연 p99로 4.8절 확정 |

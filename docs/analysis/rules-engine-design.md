# 실시간 규칙 엔진 설계 (10/10)

이 문서는 프리미엄 기능인 실시간 규칙 엔진과, 그 전제인 declared 모드의 멀티체인·era1 지원을 설계한다. 구현은 7장의 단계로 나누어 PR마다 검증한다.

---

## 1. 결론

사용자가 감시할 컨트랙트와 이벤트, 이벤트를 가공하는 로직, 조건이 참일 때의 액션을 indexer에 등록한다. indexer는 블록을 받아 로그를 decode한 직후, 저장 commit을 기다리지 않고 로직을 실행해 결과를 사용자의 연결로 바로 보낸다(fast path). 로직은 두 층이다. 조건과 간단한 가공은 CEL 식, 상태가 필요한 가공은 WASM 모듈이다. 둘 다 등록할 때 컴파일·검사하고, 실행할 때 비용·메모리·시간 상한 안에서만 돈다. 액션은 첫 단계에서 스트림 push와 webhook이다. 트랜잭션 전송은 서명 키 보관 설계를 따로 한 뒤에 다룬다.

결정(10/10, 사용자): 로직 스택 CEL + WASM, 액션 스트림 push + webhook, 실행 시점 저장 전 fast path, 순서 설계 문서 → 데이터 소스(declared 멀티체인, era1) → 규칙 엔진.

---

## 2. 왜 indexer 안에서 실행하는가

지금 사용자는 indexer의 구독(`/graphql/ws`)으로 이벤트를 받아 자기 시스템에서 파싱·분석한 뒤 행동한다. 지연은 세 곳에서 생긴다.

| 구간 | 측정(R5-5, 한 호스트 loopback) |
|---|---|
| 노드가 블록을 보인 때 → indexer가 블록을 색인 | p99 60~77ms. 대부분 50ms 폴링이고 `rpc.ws_endpoint`(newHeads)를 쓰면 1~2ms |
| commit → outbox relay → 구독 엔진 → WebSocket | p99 40~52ms |
| 사용자 시스템의 파싱·분석·판단 | indexer 밖. 사용자마다 다르다 |

규칙을 indexer 안에서 실행하면 둘째 구간의 commit·relay 대기와 셋째 구간이 사라진다. 남는 것은 블록 수신, decode, 규칙 평가, 결과 한 건의 전송이다.

### 2.1 엔진 비용 실측

조건 하나(`amount > 1000 && pool == X && sender != Y`)를 이벤트 하나에 평가한 비용이다(Apple Silicon, `go test -bench`, 저장소 밖 임시 모듈).

| 방식 | 평가 1회 | 할당 |
|---|---|---|
| native Go (SDK로 바이너리에 컴파일) | 3.7ns | 0 |
| WASM (wazero compiler, Rust 모듈, 이벤트를 메모리에 쓰고 호출) | 30ns | 1 |
| WASM, 호출마다 deadline context | 770ns | 9 |
| expr | 130ns | 3 |
| Lua (gopher-lua) | 135ns | 3 |
| CEL (cel-go, 최적화) | 167ns | 3 |
| JavaScript (goja) | 560ns | 13 |

어느 방식이든 1µs 미만이어서 ms 단위인 파이프라인 지연에 비하면 작다. 엔진은 속도보다 안전성으로 고른다. 스크립트(Lua, JavaScript)는 등록 때 타입을 검사하지 못해 오류가 실행 중에야 드러나고, 무한 루프를 끊는 장치를 따로 만들어야 한다. CEL은 등록 때 타입을 검사하고 비용 상한을 둘 수 있으며 부작용이 없다. WASM은 상태를 가질 수 있고 메모리가 격리된다.

### 2.2 sandbox 실측

| 장치 | 결과 |
|---|---|
| WASM 무한 루프 + 2ms deadline (`WithCloseOnContextDone`) | 약 2.3ms에 중단, 모듈은 닫힌다 |
| WASM 메모리 상한 32 page(2MiB)에서 1000 page 확장 요청 | 거절(-1) |
| CEL 비용 상한 10,000에서 세제곱 비용 식 | 실행 중 중단(`cost limit exceeded`) |

호출마다 deadline을 만들면 WASM 비용이 30ns에서 770ns로 늘어난다(타이머 할당). 그래서 deadline은 블록 하나의 이벤트를 모두 평가하는 호출 단위로 한 번 건다(4.4절).

---

## 3. 데이터 소스 확장 (규칙 엔진의 전제)

### 3.1 declared 모드의 멀티체인

지금은 `Config.Validate`가 거부한다(`internal/config/config.go`, `ModeDeclared`). 막힌 이유는 두 가지다.

- 체인별 기능 설정이 없다. `chainAppConfig`(`pkg/app/app.go`)가 설정을 얕게 복사해 모든 체인이 `cfg.Features`를 공유한다. 컨트랙트 주소는 체인마다 다르므로 `features.records`를 체인마다 적어야 한다.
- 체인별 API(`pkg/api/chains.go`)가 declared 모드를 모른다. `ExtensionsOnly` 없이 GraphQL을 만들고 JSON-RPC·REST도 붙인다.

설계:

- `multichain.chains[].features`를 둔다. 형식은 최상위 `features`와 같고, 체인 항목에 적은 기능은 최상위 값을 덮어쓴다. `chainAppConfig`는 `Features`를 깊게 복사한 뒤 덮어쓴다.
- `indexer.mode`는 지금처럼 전체에 하나다. 체인마다 모드를 다르게 하는 일은 요구가 생기면 다룬다.
- 체인별 API는 그 체인 App이 declared 모드면 `ExtensionsOnly`로 GraphQL을 만들고 JSON-RPC·REST를 붙이지 않는다(단일 체인의 `DeclaredOnly`와 같은 규칙).
- 검증: 두 시험 체인에 서로 다른 records 표를 선언하고 각 체인 DB에 그 체인의 레코드만 있는지, 체인별 `/chains/<id>/graphql`이 `records`를 답하는지 확인한다.

### 3.2 declared 모드의 era1

지금은 두 겹으로 막혀 있다. 설정 검증이 거부하고, 그 전에 `declaredSource`(`pkg/app/declared.go`)가 era와 노드를 이은 source를 버리고 노드의 `sourcerpc.Logs`만 돌려준다.

era1 파일의 영수증에는 로그가 들어 있다(`evm.Profile.DeriveReceipts`). 그래서 선언한 주소·이벤트의 로그를 파일에서 걸러 낼 수 있다.

설계:

- `pkg/source/era`에 `Logs`(declared용 source)를 둔다. `era.Source`를 감싸 블록마다 영수증의 로그를 선언한 주소·topic0로 거르고, 헤더에서 트랜잭션을 뺀다(`sourcerpc.Logs`와 같은 모양).
- `fetch.RangeSource`(`LogsInRange`, `Headers`)도 구현해 finalized 범위 경로에서 쓴다. 파일에는 범위 색인이 없으므로 범위의 블록을 모두 읽는다.
- era 범위는 era `Logs`, 그 뒤는 노드 `Logs`가 답하는 연결 source(`source.Chained`의 declared판)를 만든다. 두 source가 만나는 높이의 hash 확인(`CheckJoin`)은 그대로 쓴다.
- 단점: 로그가 드문 컨트랙트는 노드의 `eth_getLogs`(노드가 bloom으로 건너뜀)보다 파일 전체를 읽는 쪽이 느릴 수 있다. 대신 노드에 부담이 없다. 실제 era1 파일로 비교 측정은 파일이 생기면 한다.
- 검증: 시험용 era1 파일(`writeEra1` 도우미)로 앞부분을, 시험 체인으로 뒷부분을 주고, declared 모드 결과가 노드만으로 색인한 DB와 같은지 확인한다.

---

## 4. 규칙 엔진

### 4.1 규칙의 모양

```yaml
id: big-swap-on-hook        # 테넌트 안에서 유일
chain: "8283"               # 단일 체인 모드에서는 생략
sources:                    # 감시할 컨트랙트와 이벤트 (declared 선언과 같은 형식)
  addresses: ["0x..."]
  events: ["Swap(address indexed sender, int256 amount0, int256 amount1, uint160 sqrtPriceX96)"]
condition: 'event.amount0 > 1000000 && log.address == "0x..."'   # CEL, bool
transform: '{"price": event.sqrtPriceX96, "block": block.number}' # CEL, 선택: 보낼 내용
module: <wasm>              # 선택: 상태가 필요한 가공 (4.3절)
actions:
  - type: stream            # 테넌트의 연결로 push
  - type: webhook
    url: https://...
    secret: ...             # HMAC-SHA256 서명
limits:                     # 테넌트 한도 안에서 규칙별로 더 줄일 수 있다
  max_eval_us_per_block: 500
```

등록은 테넌트의 API key로 `/v1/rules`(REST, JSON)에 한다. 등록할 때 다음을 모두 통과해야 저장된다. 실행 중에야 드러나는 오류를 줄이려는 것이다.

1. 이벤트 시그니처를 decode 계획으로 컴파일한다(`declared.Compile`과 같은 규칙).
2. CEL 식을 그 이벤트의 인자 타입으로 타입 검사하고, 비용 추정이 한도 안인지 본다.
3. WASM 모듈을 컴파일·instantiate하고 내보낸 함수의 시그니처를 확인한다.
4. 요청에 예제 이벤트가 있으면 한 번 실행해 결과를 돌려준다(dry run).

### 4.2 CEL 층

- 변수: `event`(이벤트 인자, 인자 타입대로 int·uint·bytes·string·bool), `log`(address, txHash, index), `block`(number, time, hash), `chain`(id).
- 큰 정수(uint256)는 CEL의 `uint`(64비트)를 넘으므로 문자열과 비교 함수(`big.cmp(a, "1000")`)를 확장 함수로 준다. 이 부분은 구현 단계에서 확정한다.
- `cel.CostLimit`으로 실행 비용을 막는다. 부작용이 없으므로 실패해도 다른 규칙에 영향이 없다.

### 4.3 WASM 층

- 런타임: wazero(순수 Go, cgo 없음). 규칙마다 instance 하나, 메모리 상한(`WithMemoryLimitPages`), 호출 deadline(`WithCloseOnContextDone`).
- ABI v1:
  - 모듈이 내보내는 것: `alloc(len i32) i32`, `on_block(ptr i32, len i32) i64`. 입력은 그 블록에서 규칙에 맞은 이벤트 배열(JSON), 출력은 `(ptr<<32)|len`이 가리키는 액션 배열(JSON).
  - host가 주는 것: `env.log(ptr, len)`(테넌트의 오류·로그 스트림으로 간다).
  - JSON을 쓰는 이유는 Rust, TinyGo, AssemblyScript 어느 언어로도 쉽게 만들 수 있어서다. 이벤트당 decode 비용이 문제가 되면 v2에서 고정 binary 배치를 더한다.
- 상태: 모듈의 메모리에 둔다. 재시작하면 사라진다. 첫 단계에서는 이를 문서로 알리고, 이후 단계에서 주기적 snapshot(`export state`)을 더한다.
- deadline에 걸리면 instance가 닫혀 상태가 사라지므로 그 규칙은 `failed`가 되고 테넌트에게 알린다(4.6절).

### 4.4 실행 위치와 순서 (fast path)

- tap: 블록이 높이 순서로 나오는 곳, `indexRange`의 reorder buffer가 블록을 writer로 넘기는 지점(`pkg/fetch/scheduler.go`, `f.indexBlock` 호출 직전)이다. writer의 commit을 기다리지 않는다. finalized 범위 경로는 `indexSparse`가 범위의 블록을 writer로 넘기기 직전(`pkg/fetch/sparse.go`)이며, 이 경로의 결과는 범위 단위로 나간다.
- tap은 블록을 규칙 실행기의 대기열에 넣기만 한다(non-blocking). 수집 경로는 규칙 때문에 기다리지 않는다. 실행기는 체인마다 goroutine 하나가 블록 순서대로 처리하고, 블록마다:
  1. 블록의 로그를 규칙들의 (address, topic0) 색인으로 고른다(`declared.Plan.Match`와 같은 방식).
  2. 규칙마다 CEL 조건, 필요하면 WASM `on_block`을 한 번 호출한다. deadline은 이 호출 단위로 한 번 건다.
  3. 나온 액션을 액션 dispatcher로 넘긴다.
- 대기열이 넘치면 그 블록의 평가를 버리고 테넌트에게 `rules_lagging`(어느 블록부터)을 알린다. 조용히 버리지 않는다.
- declared 모드에서 규칙의 주소·이벤트는 수집 대상에 더해져야 한다. 규칙이 바뀌면 declared source의 주소·topic 목록을 갱신한다(`sourcerpc.Logs`가 목록 교체를 받게 한다). 규칙은 등록한 다음 블록부터 적용하며 과거 블록을 다시 평가하지 않는다.

### 4.5 전달 의미

- 액션마다 멱등 키 `(chain, block, log index, rule id)`를 붙인다. fast path는 commit 전에 실행하므로 블록 처리가 실패해 재시도되거나 재시작하면 같은 액션이 다시 나갈 수 있다(at-least-once). 사용자는 멱등 키로 거른다.
- reorg가 있는 체인(finality head)에서는 되돌린 블록의 액션에 대해 `retract`(같은 멱등 키)를 보낸다. live loop의 rollback 경로에서 나온다. StableNet(WBFT)은 블록이 들어가는 순간 확정되므로 `retract`가 나가지 않는다.
- 스트림 push: 테넌트 API key로 `/v1/rules/stream`(WebSocket)에 연결한다. 메시지는 액션 결과, 오류, 지연 알림이다. 연결마다 크기가 정해진 대기열을 두고 넘치면 끊는다(`stream.Engine`의 느린 구독자 규칙). 끊긴 동안의 결과는 보관하지 않는다(fast path). 놓치면 안 되는 액션은 webhook을 쓴다.
- webhook: `pkg/notifications`의 webhook 전달(HMAC-SHA256 서명 `X-Signature-256`, 지수 재시도, 전달 기록)을 재사용한다. 전달은 별도 worker pool에서 하므로 느린 webhook이 규칙 실행을 막지 않는다.

### 4.6 오류를 놓치지 않기

- 규칙의 컴파일 오류는 등록할 때 거절한다.
- 실행 오류(비용 초과, deadline, WASM trap, 출력 형식 오류)는 규칙마다 센다. 테넌트 스트림에 `rule_error`(규칙, 블록, 원인)를 보내고, Prometheus에 규칙별 오류 수·평가 시간을 낸다. 연속 N번(기본 10) 실패하면 규칙을 `failed`로 멈추고 알린다. 다시 켜려면 테넌트가 고쳐서 다시 등록한다.
- 한 규칙의 실패는 다른 규칙과 수집에 영향을 주지 않는다(CEL은 부작용이 없고, WASM은 instance가 격리되며, 실행기는 panic을 규칙 오류로 바꾼다).

### 4.7 테넌트, 인증, 한도 (프리미엄)

- 인증: 이미 있는 `middleware.APIKeyAuth`(key → label)를 설정(`api.auth.keys`)에서 켤 수 있게 한다. 규칙 API와 규칙 스트림은 key가 반드시 필요하다. 다른 API에 인증을 걸지는 따로 정한다.
- 테넌트: API key의 label이 테넌트다. 규칙, 사용량, 오류는 테넌트 단위다. 과금은 이 설계의 범위 밖이다.
- 한도(테넌트별 설정): 규칙 수, 블록당 평가 시간 합, WASM 메모리, 초당 액션 수, 동시 스트림 연결 수. 한도를 넘으면 그 테넌트의 규칙만 멈추거나 액션을 버리고 알린다.
- 저장: 규칙과 테넌트 상태는 체인 데이터와 분리된 control 저장소에 둔다(재색인으로 지워지지 않게 keyspace `Control`, 멀티체인에서는 최상위 DB). 시작할 때 읽어 실행기에 올리고, 바뀌면 즉시 반영한다.

### 4.8 지연 목표 (가설)

같은 호스트, newHeads 사용 기준으로 "노드가 블록을 보인 때 → 스트림 메시지 도착" p99 ≤ 10ms를 목표로 둔다 [Low: 측정 전 추정]. 5단계에서 SLO 시험(`TestSLOLoad`)과 같은 방식으로 재고 확정한다.

---

## 5. 하지 않는 것

- 트랜잭션 전송 액션: 서명 키 보관(KMS/HSM), 한도, 오작동 방지 설계가 먼저 필요하다. 자금 손실 위험이 있어 별도 설계 문서로 다룬다.
- 과거 블록 재평가(backtest): 규칙은 등록한 다음 블록부터 적용한다.
- mempool(pending 트랜잭션) 감시: 블록 이전 단계라 이 설계 밖이다.

---

## 6. 위험과 단점

| 위험 | 대응 | 남는 단점 |
|---|---|---|
| 사용자 코드가 프로세스를 멈춤 | CEL 비용 상한, WASM 메모리·시간 상한, panic 격리, 실행기를 수집 경로와 분리 | deadline에 걸린 WASM은 상태를 잃는다 |
| 규칙 부하가 수집을 늦춤 | tap은 대기열에 넣기만 하고, 넘치면 버리고 알린다 | 부하가 크면 규칙 결과가 빠진다(알림은 간다) |
| fast path 중복 전달 | 멱등 키, reorg 체인에서는 `retract` | 사용자가 중복을 걸러야 한다 |
| 스트림이 끊긴 동안의 결과 유실 | 놓치면 안 되는 액션은 webhook(재시도·기록) | 스트림은 지연 우선, 보관하지 않는다 |
| 인증 없는 API에 프리미엄 기능 추가 | 규칙 API와 스트림은 key 필수 | 나머지 API의 인증은 따로 정해야 한다 |
| uint256 값 비교 | 확장 함수 | CEL 기본 정수보다 쓰기 불편하다 |

---

## 7. 단계

| 단계 | 내용 | 검증 |
|---|---|---|
| 1 | declared 멀티체인: `chains[].features`, 체인별 declared API | 체인마다 다른 표, 체인별 `records` 조회 |
| 2 | declared era1: era `Logs`와 `RangeSource`, 연결 source | era 파일 + 시험 체인 결과가 노드만으로 색인한 DB와 같다 |
| 3 | 규칙 엔진 핵심: control 저장소, API key 인증 설정, `/v1/rules` 등록(CEL 컴파일·타입 검사·dry run), tap과 실행기, 스트림 push, 오류 보고 | 규칙 등록 → 시험 체인 이벤트 → 스트림 수신, 실패 규칙 격리, 대기열 넘침 알림 |
| 4 | WASM 층: ABI v1, 한도, 예제 모듈(Rust) | 상태 있는 규칙, 무한 루프·메모리 초과 모듈이 그 규칙만 멈춘다 |
| 5 | webhook 액션, 테넌트 한도, metrics, 지연 측정 | 지연 p99 측정으로 4.8절 확정, 한도 초과 시 그 테넌트만 영향 |
| 이후 | WASM 상태 snapshot, binary ABI, 트랜잭션 전송(별도 설계) | — |

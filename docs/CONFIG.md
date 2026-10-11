# Configuration Guide

## 설정 우선순위

설정은 다음 순서로 적용됩니다 (높은 순위가 덮어씀):

1. **기본값** (Built-in)
2. **config.yaml** (권장)
3. **환경변수**
4. **CLI 플래그** (최우선)

CLI 플래그는 명령줄에 실제로 준 것만 적용된다. 플래그의 기본값(예: `--workers`의 100)은 설정 파일 값을 덮지 않는다. bool 플래그는 끄는 쪽으로도 쓸 수 있다(`--api=false`). 설정 검증은 플래그를 적용한 뒤에 하므로, 설정 파일에 없는 필수값(`rpc.endpoint`, `database.path`)을 플래그로 줄 수 있다. `--config`를 주지 않았고 `config.yaml`도 없으면 기본값, 환경 변수, 플래그만으로 시작한다. `--config`로 지정한 파일이 없으면 오류다.

다음 설정은 시작할 때 거부된다.
- `multichain.chains[].id`가 디렉터리 이름으로 쓸 수 없는 값(`/`, 앞의 `.` 등)이거나 중복일 때.
- `database.readonly: true`: 수집기는 써야 한다. API만 하는 프로세스는 `node.role: api`로 DB를 읽기 전용으로 연다.
- `node.role: api`인데 `database.driver`가 postgres가 아닐 때(Pebble은 한 프로세스만 연다), multi-chain 모드에서 `node.role`이 all이 아닐 때.

다음 설정은 읽지만 아직 동작에 반영되지 않는다. 설정되어 있으면 시작 로그에 경고가 남는다: `eventbus.type`(local 외), `node.priority`, `account_abstraction.entry_point_addresses`(대신 `features.aa.erc4337.entry_points`). `watchlist.enabled`와 `resilience.enabled`는 v0.1.0 이후 해당 기능을 지웠으므로 효과가 없고, 켜져 있으면 경고가 남는다.

---

## config.yaml (권장)

### 기본 설정

```yaml
# config.yaml
rpc:
  endpoint: "http://127.0.0.1:8545"    # RPC 엔드포인트 (IPv4 권장)
  timeout: 30s                          # 요청 타임아웃

database:
  driver: "pebble"                      # pebble(기본) | postgres
  path: "./data"                        # PebbleDB 데이터 디렉토리 (driver pebble)
  readonly: false                       # 읽기 전용 모드
  cache_mb: 0                           # Pebble block cache(MB), 0은 128. 범위 조회의 블록이 다 들어가지 않으면 파일에서 다시 읽는다. 멀티체인은 체인마다 이 크기
  postgres:                             # driver postgres일 때
    dsn: ""                             # postgres://user:pass@host:port/db (로그에 남기지 않음)
    schema: ""                          # 테이블을 둘 schema, 비우면 public
    max_conns: 0                        # 연결 풀 상한, 0은 pgxpool 기본값

log:
  level: "info"                         # debug | info | warn | error
  format: "json"                        # json | console

indexer:
  workers: 100                          # 병렬 워커 수 (RPC 부하에 따라 조정)
  chunk_size: 1                         # 배치당 블록 수 (1 = 실시간 모드)
  start_height: 0                       # 인덱싱 시작 블록
  orphan_retention: 1000                # 보관할 reorg 기록 수 (0 = 전부 보관)

api:
  enabled: true
  host: "localhost"                     # 바인딩 호스트 (0.0.0.0 = 외부 접근 허용)
  port: 8080
  enable_graphql: true
  enable_jsonrpc: true
  enable_websocket: true
  enable_websocket_keepalive: false     # WebSocket keepalive 활성화
  enable_rest: true                     # 자주 폴링하는 경로의 REST API (/v1, multi-chain은 /chains/<id>/v1)
  enable_cors: true
  allowed_origins:
    - "*"                               # CORS와 WebSocket Origin 검사의 허용 오리진 (* = 전체 허용)
  trusted_proxies: []                   # X-Forwarded-For/X-Real-IP를 믿을 리버스 프록시 (IP 또는 CIDR)
  keys: {}                              # 키가 필요한 조작(알림 API)의 API key, 라벨: 키 (24자 이상). 없으면 그 조작은 모두 거절
  rate_limit:
    enabled: true                       # 클라이언트 주소마다 요청 수 제한 (기본 켜짐)
    per_second: 100
    burst: 200
  graphql:
    max_depth: 15                       # 필드 중첩 깊이 상한 (0 = 제한 없음)
    max_complexity: 5000                # 복잡도 상한 (0 = 제한 없음)
  subscription_engine: true             # GraphQL 구독을 구독 엔진으로 전달 (false = 이전 방식)

  # REST API(refactoring plan R4-4): 클라이언트가 자주 폴링하는 조회를 GET으로 제공한다.
  #   GET /v1/blocks?limit&offset&numberFrom&numberTo&miner
  #   GET /v1/transactions?limit&offset&blockNumberFrom&blockNumberTo&from&to&type
  #   GET /v1/addresses/<address>/balance?blockNumber
  #   GET /v1/addresses/<address>/overview
  #   GET /v1/addresses/<address>/tokens?tokenType
  #   GET /v1/addresses/<address>/transactions?limit&offset&after
  #   GET /v1/stats/miners?limit&fromBlock&toBlock
  #   GET /v1/stats/network?fromTime&toTime        (둘 다 필수, unix 초)
  # 경로마다 고정된 GraphQL 문서를 같은 resolver로 실행하므로, 응답은 그 GraphQL 응답과
  # 같다({"data": ...}, resolver 오류가 있으면 "errors"). 필드는 indexer-frontend가 같은
  # 조회에서 묻는 것이다. limit은 1~100. 숫자는 10진수, 주소는 hex다. 잘못된 인자는 400,
  # 요청한 값 자체를 못 얻으면 500이다. 성공 응답에는 ETag와 Cache-Control:
  # public, max-age=1이 붙고, If-None-Match가 맞으면 본문 없이 304로 답한다. after 커서는
  # 주소별 거래에만 있다(blocks·transactions는 블록 범위로 읽어 offset으로 넘긴다).
  # 서버는 성공한 응답을 Cache-Control과 같은 1초 동안 보관해, 같은 경로와 인자를 묻는
  # 요청에 다시 실행하지 않고 돌려준다. 보관된 응답이 없을 때 함께 도착한 같은 요청들은
  # 한 번의 실행을 나눠 받는다(singleflight, R4-5). 그래서 응답은 최대 1초 늦을 수 있다.

  # 공개 API 보호(refactoring plan R4-3):
  # - 클라이언트 주소: 연결 상대(peer)의 주소다. X-Forwarded-For와 X-Real-IP는 연결이
  #   trusted_proxies에서 왔을 때만 믿는다. 누구나 이 헤더를 쓸 수 있기 때문이다.
  #   X-Forwarded-For는 오른쪽부터 읽어 trusted_proxies가 아닌 첫 주소를 클라이언트로
  #   본다. 그 왼쪽은 클라이언트가 쓴 값이라 믿지 않는다. 리버스 프록시 뒤에서
  #   trusted_proxies를 비워 두면 모든 요청이 프록시 주소 하나로 보여 rate limit을 함께
  #   쓰게 된다. 그런 요청이 처음 오면 경고 로그를 한 번 남긴다.
  # - rate_limit: 클라이언트 주소마다 초당 per_second개, 한 번에 burst개까지 받고,
  #   넘으면 429와 Retry-After: 1로 답한다. /health와 /metrics도 포함된다.
  # - CORS: 허용 오리진에서 온 요청에만 CORS 헤더를 붙인다. "*"이면
  #   Access-Control-Allow-Origin: *로 답하고, 목록이면 그 오리진을 그대로 돌려준다.
  #   Access-Control-Allow-Credentials는 보내지 않는다(API는 쿠키를 쓰지 않는다).
  # - WebSocket(/graphql/ws, /ws, /chains/<id>/graphql/ws): 브라우저는 WebSocket에
  #   CORS를 적용하지 않으므로 서버가 Origin을 검사한다. Origin이 없는 클라이언트(브라우저가
  #   아님), 허용 오리진, 서버 자신의 호스트만 받고 나머지는 403으로 거절한다.
  # - graphql: 요청을 실행하기 전에 깊이와 복잡도를 계산한다. 필드는 1이고, 페이지를 묻는
  #   필드(pagination: {limit}, limit, first 인자)의 하위 필드는 행 수만큼 곱한다. 예를 들어
  #   blocks(pagination: {limit: 100}) { number hash }는 1 + 100 × 2 = 201이다. 변수로 준
  #   페이지 크기도 읽는다. 넘으면 실행하지 않고 오류(extensions.code: QUERY_TOO_DEEP 또는
  #   QUERY_TOO_COMPLEX, extensions.limit)로 답한다. 기본값은 GraphQL 도구의 introspection
  #   쿼리(깊이 13)와, indexer-frontend 쿼리 중 가장 비싼 것을 100행 페이지로 물었을 때의
  #   복잡도(약 2,200)를 받아들인다. 페이지 안에 다시 페이지를 묻는 쿼리
  #   (blocks 100개마다 트랜잭션 100개 등)는 거절된다.

  # 구독 엔진(refactoring plan R3-3): 이벤트 버스에는 엔진 하나만 구독하고, 엔진이
  # 구독 종류(newBlock, logs 등)마다 이벤트를 한 번 직렬화해 조건이 맞는 연결에 같은
  # 바이트를 넣는다. 연결마다 대기열은 eventbus.subscriber_buffer_size개까지이고, 넘치면
  # 그 연결만 끊는다. 끊기 전에 구독마다 오류(extensions.code: SLOW_SUBSCRIBER,
  # extensions.resumeFrom: 받지 못한 첫 sequence)를 보내고 close 1008로 닫는다. 소켓까지
  # 막힌 클라이언트는 오류를 받지 못하므로, 마지막으로 받은 sequence 다음부터 다시
  # 구독한다. "next" 메시지에는 extensions.sequence(체인 안의 이벤트 번호)가 붙는다.
  # false는 구독마다 이벤트 버스를 직접 구독하던 이전 방식(가득 차면 버림)으로 되돌린다.
  #
  # 재동기화(R3-4, eventbus.outbox가 켜져 있을 때): 구독 변수 fromSequence: N을 주면
  # outbox에 남은 N 이후 이벤트를 먼저 보내고 실시간 이벤트로 이어 간다. 각 sequence는
  # 한 번씩, 순서대로 온다. 끊긴 클라이언트는 마지막으로 받은 sequence + 1(또는 오류의
  # resumeFrom)로 다시 구독한다. 처음 붙는 클라이언트는 snapshot부터 시작한다:
  # { streamSequence }로 위치 S를 읽고, 필요한 상태를 조회하고, fromSequence: S+1로
  # 구독한다(두 조회 사이에 commit된 변경은 이벤트로 다시 올 수 있으니 블록 번호와 hash로
  # 한 번만 반영한다). N 이후가 이미 지워졌으면(outbox_retention) SEQUENCE_TOO_OLD
  # 오류(extensions.oldest)가 오고, snapshot부터 다시 시작한다. outbox가 꺼져 있으면
  # RESUME_UNSUPPORTED 오류가 오고 streamSequence는 null이다.

### Account Abstraction (EIP-4337)

`enabled`의 기본값은 true다(키를 생략하면 켜진다). UserOp(ERC-4337)과 모듈(ERC-7579) 색인을 함께 켜고 끈다.

```yaml
account_abstraction:
  enabled: true
features:
  aa.erc4337:
    entry_points:                       # 알려진 EntryPoint(v0.6 0x5FF137D4…, v0.7 0x0000000071727De2…) 외에 색인할 것
      - address: "0xEf6817fe73741A8F10088f9511c64b666a338A14"   # poc-contract EntryPoint (chain 8283 배포 기록)
        version: "v0.9"                 # v0.6, v0.7, v0.8, v0.9 중 하나
```

- UserOp는 색인할 EntryPoint가 낸 `UserOperationEvent`만 읽는다. 알려진 두 주소와 `features.aa.erc4337.entry_points`에 적은 주소가 그 대상이다. 같은 이벤트는 누구나 낼 수 있으므로 서명만으로 찾지 않는다.
- 버전은 저장하는 UserOp의 `entryPointVersion` 이름표다. v0.6~v0.9는 indexer가 읽는 세 이벤트(`UserOperationEvent`, `AccountDeployed`, `UserOperationRevertReason`)가 같아 해석이 바뀌지 않는다. UserOp의 calldata·gas 필드는 이벤트에 없어 어느 버전이든 비워 둔다.
- 주소 형식이 틀리거나, 버전이 목록에 없거나, 같은 주소를 두 번 적거나, 알려진 주소에 다른 버전을 적으면 시작할 때 오류가 난다.
- 멀티체인 모드에서는 체인 항목의 `features`(`multichain.chains[].features.aa.erc4337`)에 체인마다 적는다.
- 이미 색인한 DB에 주소를 더하면 그 뒤 블록부터 색인된다. 앞선 블록의 UserOp까지 필요하면 재색인한다. 다시 채우는 경로를 두지 않은 이유는 bundler·paymaster 통계가 누적 값이라 같은 블록을 두 번 처리하면 두 번 세기 때문이다.
- `account_abstraction.entry_point_addresses`는 지원하지 않는다(시작 로그에 경고). 버전을 적을 수 없고, 프로세스 전체 설정이라 체인마다 다르게 줄 수 없기 때문이다.

### System Contracts (Stable-One)

```yaml
system_contracts:
  enabled: true
  source_path: ""                       # 시스템 컨트랙트 소스 경로
  include_abstracts: false
```

### DEX (dex.pools, dex.trades)

두 기능은 기본으로 꺼져 있다. `dex.pools`가 시장(풀, 페어, 선물 시장)을 등록하고 상태·유동성·주문을 기록하며, `dex.trades`(dex.pools 필요)가 등록된 시장의 체결을 기록하고 `dexTrade` 이벤트를 낸다.

```yaml
features:
  dex.pools:
    enabled: true
    venues:
      - type: uniswap_v3                # PoolCreated를 내는 factory
        factory: "0x077Bb7aA6d918D87692c7E07C97ED197240bb8C4"   # StableNet testnet(8283)
      - type: uniswap_v2                # PairCreated를 내는 factory
        factory: "0x..."
      - type: perp_orderbook            # 무기한 선물 주문장
        engine: "0x..."                 # MarketCreated를 내는 engine
        order_manager: "0x..."          # 주문·체결 이벤트를 내는 order manager
  dex.trades:
    enabled: true
```

- 설정에 적은 factory와 engine이 낸 등록 이벤트만 시장으로 인정한다. 같은 이벤트는 누구나 낼 수 있기 때문이다. 등록되지 않은 컨트랙트의 Swap 등은 무시한다.
- 체결 가격은 quote/base를 1e18배 한 정수다. 토큰 소수 자릿수는 적용하지 않은 원시 단위다. AMM은 token0이 base, token1이 quote이고, 풀에서 base가 나가면 taker의 매수다.
- 선물 `matchOrders`는 `OrderPartiallyFilled` 두 개와 `OrdersMatched`를 낸다. 이를 체결 하나로 기록한다. 운영자를 상대로 한 `fillOrder`와 `MarketOrderExecuted`도 각각 체결이다.
- 색인 시작 높이보다 먼저 만들어진 시장과 주문은 알 수 없으므로, 그 체결은 기록되지 않는다. 시장이 만들어진 높이부터 색인하거나 backfill한다.
- 기능 설정 절에서 `enabled` 외의 키는 그 기능이 읽는다. 기능이 모르는 키를 쓰면 시작할 때 오류가 난다.
- GraphQL: `dexMarkets`, `dexMarket(address, marketId)`, `dexTrades(market, marketId)`, `dexTradesByTrader(trader)`, `dexLiquidityChanges(market)`, `dexOrders(market, marketId)`, `dexOrder(manager, id)`. 목록은 최신순이고 `pagination.after`와 `pageInfo.endCursor`로 넘긴다(전체 개수는 없다). 구독 `dexTrade(markets: [...])`는 체결의 블록이 색인될 때 그 체결을 보낸다. `markets`를 주면 그 시장 주소의 체결만 받는다. reorg로 블록이 되돌려지면 그 블록의 체결을 `removed: true`로 다시 보낸다(`reorg` 이벤트 뒤, 새 체결보다 먼저, 최신 체결부터). 받은 쪽은 같은 블록·log index의 체결을 지운다.
- reorg: 시장, 체결, 주문, tick, 남은 주문 목록, 캔들, 시계열은 모두 블록 트랜잭션 안에 저장되므로 rollback이 함께 되돌린다. 호가창은 `reorg` 이벤트에 모든 시장을 다시 읽는다.

### DEX 호가창 (dex.orderbook)

`dex.orderbook`(dex.pools 필요, 기본 꺼짐)은 등록된 시장마다 호가창을 메모리에 둔다. 데이터를 따로 저장하지 않으므로 켜도 backfill은 없다. 질의를 받는 프로세스(`node.role`이 `all` 또는 `api`)가 호가창을 만든다. 멀티체인 모드에서는 아직 만들지 않는다(`dexOrderBook`이 오류를 돌려준다).

```yaml
features:
  dex.orderbook:
    enabled: true
    step_bps: 10             # 풀·페어 호가의 가격 간격(bp), 1~5000
    levels: 20               # 한쪽 호가 단계 수, 1~200
    reconcile_interval: 1m   # 체인 상태와 대조하는 주기, 0이면 대조하지 않는다
```

- 시장마다 actor 하나가 호가창을 가진다. 블록이 시장을 바꾸면(`dexMarket` 이벤트) 또는 reorg가 나면 저장소에서 상태를 다시 읽어 새 호가창으로 통째로 바꾼다. 읽는 쪽은 잠금 없이 그 시점의 호가창을 본다.
- 선물 주문장: 남아 있는(open, partially_filled) 지정가 주문의 남은 수량을 가격별로 합친다. 스탑·익절 주문은 `OrderTriggered` 전까지 `pending`이라 호가창에 없다.
- Uniswap V3: 현재 가격에서 위(asks)·아래(bids)로 `step_bps`씩 움직일 때 풀이 내주거나 받는 수량이다. 초기화된 tick을 지날 때 그 tick의 net 유동성만큼 유동성이 바뀐다(swap과 같다).
- Uniswap V2: reserve 비율이 가격이고, 가격 p까지 움직일 때 base 수량은 √(k/p)의 변화다(k = reserve0·reserve1).
- 수량은 수수료를 빼기 전, 풀이 실제로 내주거나 받는 양이다. 가격·수량은 체결과 같은 원시 단위(가격은 quote/base×1e18)다.
- 대조: `reconcile_interval`마다 호가창을 다시 읽고, 그 호가창의 블록에서 컨트랙트를 `eth_call`로 읽어 비교한다. V2는 `getReserves`, V3는 `slot0`·`liquidity`·각 tick의 `ticks`와, 색인한 tick과 현재 tick이 든 `tickBitmap` 단어, 선물은 남아 있는 주문마다 `getOrder`(상태, 수량, 체결량, 가격)다. 결과는 GraphQL `reconciliation`과 metric `indexer_dex_orderbook_reconciliations_total{venue,result}`(in_sync, mismatch, error)로 보이고, 다르면 경고 로그를 남긴다. 노드가 그 블록의 상태를 가지고 있어야 한다(보통 최근 128블록).
- 대조가 못 잡는 것: V3에서 색인한 tick과 현재 tick이 든 단어 밖의 tick, 선물에서 색인이 모르는 주문(order manager에는 시장별 주문 목록 조회가 없다). 시장은 등록 이벤트부터 색인하므로 보통 둘 다 생기지 않는다.
- GraphQL: `dexOrderBook(market, marketId, levels, stepBps)`는 `bids`(높은 가격부터), `asks`(낮은 가격부터), `midPrice`, `blockNumber`, `reconciliation`을 돌려준다.
- 0009 이전에 `dex.pools`로 색인한 DB는 V3 tick과 pending 상태가 없다. tick이 필요하면 재색인한다.

### 캔들과 시계열 (agg.candles, agg.timeseries)

두 기능은 기본으로 꺼져 있다. 블록의 저장 트랜잭션 안에서 집계를 갱신하므로, rollback하면 그 블록의 몫이 함께 되돌아간다. 집계는 순서와 무관하게 합쳐지므로(시가·종가는 체결 위치로 정한다) 색인된 DB에서 켜면 저장된 블록으로 background backfill한다.

```yaml
features:
  agg.candles:                 # dex.trades 필요
    enabled: true
    intervals: [1m, 5m, 15m, 1h, 4h, 1d]   # <숫자><s|m|h|d>, 30일 이하
  agg.timeseries:
    enabled: true
    series: [chain, dex, token]            # 기본 [chain]; dex는 dex.trades 필요
```

- 캔들: 시장·주기마다 [start, start+interval) 안의 체결로 open, high, low, close(가격은 quote/base×1e18), base·quote 거래량(원시 단위), 체결 수를 둔다. start는 Unix 0부터 주기의 배수다. 체결이 없는 구간에는 캔들이 없다(채우지 않는다).
- 시계열은 UTC 일, ISO 주(월요일 시작), 월마다 점 하나를 둔다.
  - `chain`: 블록 수, 트랜잭션 수, gasUsed, 수수료(gasUsed × 실제 낸 가격, wei), 첫·마지막 블록.
  - `dex`: 시장별 체결 수, base·quote 거래량. `dex.trades`가 같은 블록에서 먼저 실행된다.
  - `token`: 토큰별 전송 수와 ERC-20 전송량. `token.transfers`와 같은 규칙으로 전송을 판별한다(체인의 native coin 컨트랙트는 제외).
- GraphQL: `dexCandles(market, marketId, interval, from, to)`, `chainActivity(period, from, to)`, `dexVolume(market, marketId, period, from, to)`, `tokenTransferVolume(token, period, from, to)`. `from`·`to`는 구간 시작의 Unix 초이고 둘 다 포함한다. 오래된 것부터 `pagination.after`로 넘긴다.
- 0010 이전에 Pebble로 색인한 DEX 체결은 블록별 색인(`/dex/block/`)이 없어 캔들·DEX 시계열에 잡히지 않는다. 재색인한다.

### 선언형 수집 (features.records, indexer.mode)

프로젝트가 필요한 컨트랙트와 이벤트, 시작 블록, 그 로그를 담을 표를 설정으로 적는다(refactoring plan R6-1). `records` 기능이 선언된 표의 로그를 레코드로 저장하고 선언된 키로 찾게 한다.

```yaml
indexer:
  mode: declared            # full(기본): 체인 전체 저장 / declared: 헤더와 선언한 로그만
  finality: finalized       # 확정된 블록만 읽으려면
features:
  records:
    enabled: true
    sources:
      - name: settlement
        address: "0x..."    # 또는 addresses: [...]
        start_block: 1200   # indexer.start_height가 없으면 declared 모드는 여기서 시작
        events:             # 사람이 읽는 시그니처(인자 이름 필수) ...
          - "PaymentSettled(address indexed merchant, bytes32 indexed orderId, address device, uint256 amount)"
      - name: vault
        address: "0x..."
        abi: abis/vault.json   # ... 또는 ABI 파일과 이벤트 이름
        events: [Deposited]
    tables:
      - name: receipts      # 소문자 식별자
        source: settlement
        event: PaymentSettled
        keys:               # 조회 키: 이벤트 인자 목록
          - [merchant, orderId]
    rebuild: []             # 정의가 호환되지 않게 바뀌면 지우고 처음부터 다시 채울 표 이름
```

- 레코드는 로그 하나이고 (블록, log index)로 식별한다. 같은 범위를 두 번 색인해도 한 번만 저장된다. 필드는 이벤트 인자이고 주소·bytes는 소문자 0x hex, 정수는 10진수, indexed 동적 타입(string, bytes)은 topic hash다.
- 선언된 컨트랙트·이벤트인데 decode되지 않는 로그(인자 indexed 구성이 다른 같은 이름 이벤트)는 경고를 남기고 건너뛴다.
- GraphQL: `records(table, where: [{field, value}], pagination)`. `where`가 없으면 표 전체, 있으면 그 필드들이 선언된 키 하나와 같아야 한다. 값은 저장할 때와 같은 형식으로 맞춘다(주소 대소문자, 0x 정수). 오래된 것부터 돌려주므로 같은 키의 로그가 둘 이상이면 첫 번째가 가장 이른 로그다.
- `indexer.mode: declared`(`INDEXER_MODE`): 블록마다 헤더(`eth_getBlockByNumber`, 트랜잭션 없이)와 선언한 컨트랙트·이벤트의 로그(`eth_getLogs`)를 한 batch로 읽는다. `indexer.finality: finalized`와 함께 쓰면 확정된 블록은 reorg가 없으므로 1,000블록 범위를 `eth_getLogs` 한 번으로 읽고, 선언한 로그가 있는 블록의 헤더만 읽어 저장하며, 범위마다 한 트랜잭션으로 cursor를 범위 끝으로 옮긴다(노드가 범위를 거부하면 반으로 나눈다). 로그가 없는 블록은 저장하지 않는다. 기능을 나중에 켜면 declared 모드의 backfill은 저장소가 아니라 노드에서 로그를 범위로 다시 읽는다. `--gap-recovery`는 declared 모드에서 쓸 수 없다. 헤더(reorg 판정용)와 레코드만 저장하고 트랜잭션·영수증·로그는 저장하지 않는다. `LogsOnly` 기능(지금은 `records`)만 실행되고 다른 기능을 켜면 시작하지 않는다. API는 GraphQL 확장과 구독만 제공한다(탐색기 조회, REST, JSON-RPC, Etherscan API 없음). 멀티체인에서도 쓸 수 있다: 체인마다 `multichain.chains[].features.records`로 자기 소스와 표를 선언하고, 체인별 API(`/chains/<id>/graphql`)도 GraphQL 확장만 제공한다. `source.era_dir`를 주면 archive 범위의 블록은 era1 파일에서 선언한 로그만 걸러 읽고(노드를 부르지 않는다) 그 뒤는 노드에서 읽는다. 파일에는 주소·topic 색인이 없어 범위의 블록을 모두 decode하므로, 로그가 드문 컨트랙트는 노드의 `eth_getLogs`보다 느릴 수 있다. 멀티체인에서는 `source.era_dir`를 쓰지 않는다(체인 항목에 era 설정이 없다).
- `full` 모드에서도 `records`를 켤 수 있다. 이때는 체인 전체와 함께 레코드를 저장한다.
- 표마다 진행 상태를 따로 기록한다(`/meta/features/records/<표>`, 표 정의의 hash 포함). 색인된 DB에 표를 추가하면 그 표만 background로 backfill한다(다른 표는 다시 처리하지 않는다. declared 모드면 노드에서 1,000블록씩 로그를 다시 읽고, 시작 블록 아래는 읽지 않는다). 표를 지우면 그 표의 레코드는 지운 높이까지로 남고, 같은 정의로 다시 추가하면 이어서 채운다. 이미 색인한 표의 source에 컨트랙트 주소를 더하면(이벤트와 키는 그대로) 그 표를 처음부터 다시 채운다(online backfill, 이미 있는 레코드는 그대로 다시 쓰이고 더한 컨트랙트의 과거 로그가 들어온다). 그 밖의 정의 변경(이벤트, 인자 이름·타입·indexed, 키, 주소 삭제)은 기존 레코드가 옛 정의의 것이므로 시작하지 않는다. 표 이름을 바꿔 새 표로 색인하거나, 그 표를 `rebuild`에 적거나, `--reindex`한다. `rebuild`에 적은 표는 정의가 그렇게 바뀌었을 때만 레코드와 키 항목을 지우고(새 정의 기록과 같은 트랜잭션) 처음부터 다시 채운다(online backfill, declared 모드면 노드에서 다시 읽는다). 다시 채우는 동안 그 표의 조회는 일부만 돌려준다. 정의가 그대로면 적혀 있어도 아무 일도 하지 않으므로 다시 채운 뒤 지우지 않아도 된다. 같은 source를 쓰는 표는 주소 변경이 함께 적용되므로 함께 적어야 한다. 표 단위 기록 이전에 색인한 DB는 첫 시작 때 `records`의 상태를 각 표가 이어받는다.

### Contract Verification

```yaml
verifier:
  enabled: true
  solc_bin_dir: ""                      # solc 바이너리 디렉토리
  solc_cache_dir: ""                    # solc 캐시 디렉토리
  max_compilation_time: 120             # 최대 컴파일 시간 (초)
  auto_download: true                   # solc 자동 다운로드
  allow_metadata_variance: false        # 메타데이터 차이 허용
```

### EventBus

```yaml
eventbus:
  type: "local"                         # local | redis | kafka | hybrid
  publish_buffer_size: 1000
  history_size: 100                     # 이벤트 히스토리 버퍼 크기
  outbox: true                          # 블록 이벤트를 outbox에 기록하고 relay가 sequence 순서로 전달
  outbox_retention: 100000              # 가장 느린 소비 그룹 뒤로 DB에 남기는 이벤트 수, 0이면 모두 남김

  # outbox(refactoring plan R3-1): 블록의 이벤트를 그 블록을 저장하는 트랜잭션에 함께
  # 기록한다(`/outbox/<seq>`). relay가 commit 뒤 sequence 순서로 이벤트 버스에 넘기고,
  # 버스가 차 있으면 버리지 않고 기다린다. 이벤트마다 체인 안에서 1부터 빈칸 없이
  # 증가하는 sequence가 붙고(`events.SequenceOf`), 버스는 이미 받은 sequence를 버린다.
  # reorg 이벤트와 제거된 log 이벤트는 되돌리는 트랜잭션에 함께 기록된다.
  # 재색인은 outbox 항목을 지우지만 sequence는 이어간다. outbox: false는 commit 뒤
  # 직접 발행하던 이전 방식(sequence 없음)으로 되돌린다.
  #
  # 소비 그룹(R3-2): relay는 outbox를 `node.id`라는 소비 그룹으로 읽고, 그룹마다
  # 어디까지 받았는지(`/meta/outbox/cursor/<group>`)를 따로 기록한다. 그래서 노드마다
  # 모든 이벤트를 받고, 재시작하면 마지막으로 받은 batch 뒤부터 이어 받는다. 처음 보는
  # 그룹은 시작 시점 뒤에 commit된 이벤트부터 받는다. node.id가 바뀌면 새 그룹이 되므로
  # 이전 그룹이 받지 못한 이벤트는 넘어간다. 전달이 끝난 항목은 지금 소비 중인 그룹 중
  # 가장 느린 그룹보다 outbox_retention개 넘게 뒤처진 것만 지운다.

  # Redis 백엔드 (type: redis 또는 hybrid)
  redis:
    enabled: false
    addresses:
      - "localhost:6379"
    password: ""
    db: 0
    pool_size: 10
    channel_prefix: "indexer:"
    cluster_mode: false
    tls:
      enabled: false

  # Kafka 백엔드 (type: kafka 또는 hybrid)
  kafka:
    enabled: false
    brokers:
      - "localhost:9092"
    topic: "indexer-events"
    group_id: "indexer"
    compression: "snappy"               # none | gzip | snappy | lz4 | zstd
    required_acks: -1                   # 0 | 1 | -1 (all)
```

### Multi-Chain

> 체인마다 DB를 따로 둔다: `<database.path>/chains/<id>`(PostgreSQL이면 schema `<schema>_<id>`, schema를 비우면 `chain_<id>`. 소문자로 바꾸고 schema 이름에 쓸 수 없는 문자는 `_`로 바꾸며, 두 체인이 같은 schema가 되면 시작하지 않는다). 각 체인은 자기 수집 루프, 이벤트 버스, 기능으로 돌고 `features.*` 같은 공통 설정을 함께 쓴다. 체인 항목에 `features`를 두면 그 체인에서는 같은 이름의 공통 기능 설정을 통째로 바꾼다(`enabled`를 적지 않으면 공통 값). 체인마다 컨트랙트 주소가 다르므로 `records`(선언형 수집)는 보통 체인 항목에 적는다. 루트의 `rpc.endpoint`는 필요 없고(노드 주소와 대체 노드는 체인 항목의 `rpc_endpoint`, `ws_endpoint`, `fallback_endpoints`에 둔다), `rpc.fallback_endpoints`·`rpc.ws_endpoint`·`rpc.record_dir`·`source.era_dir`·`notifications`·`verifier`는 이 모드에서 쓰이지 않는다(시작 시 경고). `chain_id`는 노드가 알려 주는 값과 같아야 그 체인이 시작된다. `--reindex`는 체인 DB마다 적용된다.
>
> API는 체인별 경로로만 열린다: `/chains/<id>/graphql`, `/chains/<id>/graphql/ws`, `/chains/<id>/playground`, `/chains/<id>/rpc`, 체인 목록 `GET /chains`. 루트의 `/graphql`, `/rpc`, `/api`는 없다.

```yaml
multichain:
  enabled: false
  health_check_interval: 30s
  max_unhealthy_duration: 5m
  auto_restart: true
  auto_restart_delay: 10s
  chains:
    - id: "stableone-mainnet"
      name: "Stable-One Mainnet"
      rpc_endpoint: "http://127.0.0.1:8545"
      ws_endpoint: "ws://127.0.0.1:8546"   # 선택: newHeads로 새 블록을 바로 읽는다
      fallback_endpoints: []                # 선택: 같은 체인의 다른 HTTP(S) 노드. 단일 체인의 rpc.fallback_endpoints처럼 차례로 failover (API로는 노출하지 않음)
      chain_id: 1000
      adapter_type: "auto"              # auto | evm | stableone | anvil
      start_height: 0
      enabled: true
      workers: 100
      batch_size: 10
```

### Notifications

```yaml
notifications:
  enabled: false
  allow_private_destinations: false     # true면 webhook·Slack이 내부 주소로도 보낸다 (개발용)
  operator_labels: []                   # 모든 알림 설정을 보고 관리하는 api.keys 라벨 (소유자 없는 예전 설정 포함)
  max_settings_per_owner: 100           # 운영자가 아닌 키 하나가 가질 수 있는 설정 수 (0 = 제한 없음)
  max_streams_per_owner: 4              # 키 하나가 /v1/subscriptions/stream에 동시에 열 수 있는 연결 수
  destination_rate_limit: 10            # webhook은 host마다, Slack은 incoming webhook URL마다 초당 전달 수 (0 = 제한 없음)
  destination_burst: 20                 # 한 번에 보낼 수 있는 수

  webhook:
    enabled: true
    timeout: 10s
    max_retries: 3
    max_concurrent: 10

  email:
    enabled: false
    smtp_host: "smtp.example.com"
    smtp_port: 587
    smtp_username: ""
    smtp_password: ""
    from_address: "indexer@example.com"
    use_tls: true

  slack:
    enabled: false
    timeout: 10s
    max_retries: 3

  retry:
    initial_delay: 1s
    max_delay: 5m
    multiplier: 2.0
    max_attempts: 5

  queue:
    buffer_size: 1000
    workers: 5
    batch_size: 10
    flush_interval: 5s

  storage:
    history_retention: 720h             # 30일
    max_settings_per_user: 100
    max_pending_notifications: 10000

  # 이벤트 유입(R3-5, eventbus.outbox가 켜져 있을 때): 알림 서비스는 이벤트 버스를
  # 구독하지 않고 변경 스트림을 자기 소비 그룹(notifications)으로 읽는다. 이벤트마다 알림을
  # 설정 id와 sequence로 만든 고정 id로 먼저 저장한 뒤에 다음 이벤트로 넘어가므로, 대기열이
  # 가득 차거나 재시작해도 알림이 사라지지 않고, 같은 이벤트가 다시 와도 알림이 두 번
  # 만들어지지 않는다. 대기열에 못 들어간 알림은 저장된 채 대기하다 flush_interval마다
  # 재시도 처리기가 보낸다. 처음 켠 서비스는 켠 뒤에 commit된 이벤트부터 알린다.
  # outbox가 꺼져 있으면 이전처럼 이벤트 버스를 구독한다.
```

- 알림 API(GraphQL `notificationSettings`·`createNotificationSetting` 등, JSON-RPC `notification_*`)는 `api.keys`의 키를 `X-API-Key` 헤더(또는 `Authorization: Bearer`)로 보낸 요청만 처리한다. 키가 없으면 GraphQL은 `UNAUTHENTICATED`, JSON-RPC는 `-32001`로 거절한다. 모르는 키를 보낸 요청은 어느 경로든 401이다. 나머지 API는 키 없이 열려 있다. 설정이 webhook 주소와 서명 비밀을 담고, 설정을 만들면 서버가 그 주소로 요청을 보내기 때문이다. 설정은 만든 키의 것이다(설정의 `owner`에 그 키의 라벨). 키는 자기 설정과 그 알림만 보고 바꾸며, 다른 키의 설정은 없는 것처럼 보인다(조회는 null, 변경은 not found). 모든 설정의 통계는 운영자만 본다. 설정의 필터(`filter`)는 `addresses`(트랜잭션은 from/to, 로그는 컨트랙트), `topics`(eth_getLogs와 같은 위치별 목록), `minValue`, `event`(인자 이름이 있는 이벤트 시그니처: 그 이벤트의 로그만 맞고 알림의 `data.decoded`에 인자가 decode되어 온다), `participants`(indexed 인자 중 하나가 이 주소인 로그: 토큰 이동의 from 또는 to)를 받는다. 잘못된 주소·topic·시그니처는 등록을 거절한다. 설정의 `delivery`는 알림을 만드는 시점이다. `durable`(기본)은 블록을 저장한 뒤 변경 스트림에서 만들어 빠지거나 두 번 만들어지지 않는다. `fast`는 블록을 받아 저장하기 전에 만든다(fast path). 저장 commit과 relay를 기다리지 않아 빠르지만 at-least-once다: 블록 처리를 다시 하면 같은 이벤트를 다시 평가하며(알림 id가 같아 두 번 만들지는 않는다), 재시작 직후 이미 보낸 알림이 다시 갈 수 있으니 수신 쪽은 `id`로 거른다. fast path의 대기열(블록 1,024개)이 차면 그 블록은 fast 평가에서 빠지고 경고 로그를 남긴다(수집은 기다리지 않는다). reorg 알림은 두 방식 모두 받는다. 블록 수집이 일어나는 프로세스(`all`, `ingest`)에서만 fast 평가를 한다.
- 스트림 채널: 설정의 `type`이 `stream`(GraphQL `STREAM`, `destination`은 빈 객체)이면 알림을 그 설정을 만든 키의 스트림 연결로 보낸다. 같은 키로 `/v1/subscriptions/stream`에 WebSocket으로 연결한다(키는 `X-API-Key` 헤더, `Authorization: Bearer`, 또는 `api_key` 질의 인자. 질의 인자는 접속 로그에 남을 수 있다). 키가 없으면 401, 키의 연결 수가 `max_streams_per_owner`를 넘으면 429다. 메시지는 JSON 텍스트 하나씩이다: `{"type":"notification","notification":{...}}`(webhook과 같은 알림 객체, `id`로 중복을 거른다)와 `{"type":"lagging","from_block":N}`. `lagging`은 fast 설정을 가진 키에게, fast path 대기열이 차서 블록 N부터 fast 평가를 건너뛰었다는 것을 알린다(대기열에 다시 블록이 들어갈 때까지 한 번). 스트림 알림은 저장하지도 재시도하지도 않는다. 연결이 없을 때 만든 알림은 사라지고, 연결의 대기열(메시지 1,024개)이 차면 그 연결을 close 1008 `SLOW_SUBSCRIBER`로 끊는다. 놓치면 안 되는 알림은 webhook을 쓴다. 스트림은 블록을 평가하는 프로세스(`all`)에서만 열린다. `node.role: api` 프로세스와 멀티체인 모드에서는 열리지 않는다. `operator_labels`의 키는 모든 설정을 보고 관리하며, 소유자 기록 전에 만든 설정(owner 없음)은 운영자만 볼 수 있다. 운영자가 설정을 고쳐도 owner는 그대로다.
- 조건식과 보낼 내용 식(CEL, https://cel.dev): 설정의 `condition`이 참인 이벤트만 알리고, `payload` 식의 값을 알림의 `payload.result`로 보낸다. 둘 다 선택이다. 변수는 `block.number`, `block.time`(uint), `block.hash`, `chain.id`(uint), 트랜잭션 이벤트의 `tx.hash`, `tx.from`, `tx.to`, `tx.value`(문자열, 10진수), 로그 이벤트의 `log.address`, `log.txHash`, `log.index`(uint), 그리고 `filter.event`의 인자마다 `event.<인자 이름>`이다. 인자 타입은 uint8~uint64가 uint, int8~int64가 int, bool이 bool이고, 나머지(64비트보다 큰 정수, 주소, bytes, 문자열)는 decode된 문자열이다. 다른 종류의 이벤트에서 읽은 변수는 빈 값(""이나 0)이다. 주소와 hash는 소문자 0x hex다. 64비트를 넘는 정수는 `bigCmp(a, b)`로 비교한다(10진수나 0x 문자열, 또는 int·uint를 받아 -1, 0, 1을 돌려준다). 예: `condition: bigCmp(event.value, "1000000000000000000") >= 0 && event.to == "0x..."`, `payload: {"to": event.to, "value": event.value, "block": block.number}`.
  - 등록할 때 검사한다: 문법, 없는 변수·인자(오타), 타입(`condition`은 bool이어야 한다), 길이(2,048자), 예상 비용(문자열을 4,096자로 보고 최대 비용이 100,000을 넘으면 거절). 실행 중에도 같은 비용 한도에서 멈춘다.
  - 실행 오류(0으로 나누기, 정수가 아닌 값을 `bigCmp`에 넘김 등)가 나면 그 이벤트는 알리지 않는다. 다른 설정과 수집은 영향을 받지 않는다. 오류는 경고 로그와 소유자의 스트림 메시지 `{"type":"error","setting_id","block","error"}`로 알린다. 같은 설정에서 10번 잇달아 오류가 나면 설정을 끄고(`enabled: false`) `disabled: true`를 함께 보낸다. 고친 뒤 다시 켠다.
  - 한 이벤트의 평가는 약 3.5µs다(조건 3개와 필드 3개 payload, `BenchmarkExpressionEvaluate`). 지표: `indexer_notification_expression_evaluations_total{result}`(notify, skip, error), `indexer_notification_expression_seconds`, `indexer_notification_settings_disabled_total`.
  - reorg 알림에는 식을 적용하지 않는다.
  - dry run: 등록하기 전에 식을 시험한다. GraphQL `checkNotificationExpressions(input: {event, condition, payload, log | transaction, block})`, JSON-RPC `notification_checkExpressions`(같은 필드)를 쓴다. 키가 필요하다. 식이 등록될 수 있는지(`valid`, 아니면 `error`)를 돌려준다. 예제 로그(eth_getLogs 형식의 `address`, `topics`, `data`, `index`, `transactionHash`)나 트랜잭션(`hash`, `from`, `to`, `value`)과 그 블록(`number`, `time`, `hash`)을 주면, 그 이벤트가 알림될지(`notify`), payload 값(`result`), decode한 인자(`decoded`)도 돌려준다. 실행 오류와 "예제 로그가 필터 이벤트가 아님"은 `error`로 알린다. 아무것도 저장하거나 보내지 않는다. 형식이 틀린 예제(주소, hex)는 요청 오류다.
- 목적지별 상한: webhook은 같은 host로 가는 전달이, Slack은 같은 incoming webhook URL로 가는 전달이 `destination_rate_limit`(초당, 기본 10)과 `destination_burst`(기본 20)를 나눠 쓴다. 설정 하나로 남의 서버에 요청을 쏟지 못하게 하려는 것이다. 상한을 넘은 전달은 시도하지 않고, 상한이 허락하는 때로 미룬다(시도 횟수에 들지 않는다). 그래서 붐비는 목적지는 자기 알림만 늦어진다. 스트림은 상한이 없다(키별 연결 수와 연결별 대기열이 막는다).
- 알림 저장은 디스크 sync를 기다리지 않는다(`storage.PebbleStorage.PutUnsynced`). 프로세스가 죽어도 남고, 시스템이 죽으면 다음 블록 commit의 sync 전 기록이 사라질 수 있다. 이때는 그 이벤트가 outbox에서 다시 오거나(cursor도 sync 전이므로) 블록을 다시 처리하며 같은 id로 다시 만들어지고, 전달 상태는 다시 보낸다(at-least-once). 키마다 fsync를 기다리면 fast webhook 알림이 블록마다 밀려 수 초 늦어졌다(macOS에서 p50 3초).
- fast path는 블록마다 스트림 설정을 먼저 평가한다. 저장해야 하는 설정이 스트림을 늦추지 않게 하려는 것이다.
- 지연(`make test-slo`의 `TestNotificationLatency`, 10/11, macOS 한 대, 시험 체인, 1ms polling으로 newHeads를 대신함, 100ms마다 swap 10개 블록, CEL 조건): fast 스트림은 노드가 블록을 보인 때부터 p50 3.5ms, p99 9.6~12.1ms(4회)이고 그중 fast path에 들어온 뒤로는 p99 1.3~5.1ms다(나머지는 블록 fetch). fast webhook은 p50 12ms, p99 22ms, durable 스트림은 p50 13ms, p99 25ms다. 시험은 fast 스트림 p99를 처음부터 20ms, fast path부터 10ms 이하로 검사한다. 설계의 가설(p99 10ms)은 알림 경로만으로는 맞고, 처음부터 재면 이 기계에서 경계에 있다. 실제 노드와 newHeads로는 재지 않았다.
- 지표: `indexer_notification_deliveries_total{type,result}`(sent, retry, failed), `indexer_notification_delivery_seconds{type}`, `indexer_notification_deferred_total{type}`(상한으로 미룸), `indexer_notification_stream_messages_total{type,result}`(sent, unsent), `indexer_notification_stream_overflows_total`, `indexer_notification_fast_dropped_blocks_total`.
- `filter.event`는 첫 topic과 함께 indexed 인자 수도 맞아야 한다. 시그니처가 같아도 indexed 인자가 다른 이벤트(ERC-20 Transfer 필터에 대한 ERC-721 Transfer)는 맞지 않는다. 전에는 첫 topic만 보아 이런 로그가 `decoded` 없이 알림됐다.
- 알림의 `payload.chain_id`는 연결한 노드의 체인 id다. 전에는 항상 1이었다.
- webhook과 Slack의 주소가 loopback, 사설·link-local 대역(cloud metadata `169.254.169.254` 포함), CGNAT, 문서·예약 대역이거나 `localhost`·`.internal`·`.local`·점 없는 이름이면 등록을 거절하고, 보낼 때도 이름을 푼 실제 주소를 다시 검사한다(등록 뒤 DNS를 바꾸는 우회 방지). redirect는 따라가지 않는다(3xx 응답이 결과가 된다). 같은 망의 수신기로 보내야 하는 개발 환경만 `allow_private_destinations: true`를 쓴다.

### Node Identity

```yaml
node:
  id: "node-1"                         # 노드 식별자, 기본값은 hostname. 이벤트 스트림의 소비 그룹 이름으로 쓴다
  role: "all"                           # all | ingest | api (writer·reader는 ingest·api의 예전 이름)
  priority: 0
```

실행 역할(리팩터링 계획 R4-1). DB 하나를 색인하는 프로세스 하나(`ingest` 또는 `all`)와 그 DB를 서비스하는 API 프로세스 여러 개(`api`)로 나눠 띄울 수 있다.

| 역할 | 색인 | API | 비고 |
|---|---|---|---|
| `all`(기본) | 한다 | 한다 | 지금까지의 동작 |
| `ingest` | 한다 | `/health`·`/version`·`/metrics`·`/subscribers`만 | 알림, 계약 검증처럼 쓰는 일도 여기서 한다 |
| `api` | 안 한다 | 한다 | `database.driver: postgres` 필요, DB를 읽기 전용으로 연다 |

- `api` 프로세스는 schema를 만들거나 올리지 않는다. 색인 프로세스가 먼저 migration을 적용해야 시작한다.
- `api` 프로세스의 GraphQL 구독은 색인 프로세스가 쓴 outbox를 `indexer.poll_interval`마다 읽어 받는다(소비 그룹 `node.id`, 위치는 메모리에만 둔다). 시작한 뒤의 이벤트부터 받으므로, 그 전 이벤트가 필요한 구독자는 `fromSequence`로 이어 받는다.
- `api` 프로세스에서 계약 검증(`verifier.enabled`)은 무시하고 경고를 남긴다. 알림(`notifications.enabled`)은 API만 서빙한다: 설정·조회·재시도 요청을 처리하고 알림 키(`/data/notification/`, `/index/notification/`)만 DB에 쓸 수 있다(나머지는 읽기 전용). 전송은 ingest 프로세스가 하며, 그쪽 알림 서비스가 설정을 5초마다 다시 읽어 반영한다. 그래서 ingest와 api 양쪽에 `notifications.enabled`와 같은 handler 설정(webhook, email, slack)을 둔다. 재시도 요청은 pending으로 저장되고 ingest가 보낸다. 시험 전송(`testNotificationSetting`)은 api 프로세스가 직접 보낸다. genesis 잔액과 노드에서 가져온 토큰 메타데이터는 응답에는 쓰지만 저장하지 않는다.

---

## CLI Flags

```bash
./indexer-go [flags]

# 필수
  --rpc string              RPC 엔드포인트 URL
  --db string               데이터베이스 경로

# 인덱서
  --workers int             병렬 워커 수 (명시했을 때만 indexer.workers를 덮음)
  --batch-size int          배치당 블록 수 (default: 100)
  --start-height uint       시작 블록 높이 (default: 0)
  --gap-recovery            갭 감지 및 복구 활성화

# API 서버
  --api                     API 서버 활성화
  --api-host string         API 호스트 (default: "localhost")
  --api-port int            API 포트 (default: 8080)
  --graphql                 GraphQL 활성화
  --jsonrpc                 JSON-RPC 활성화
  --websocket               WebSocket 활성화

# 로깅
  --log-level string        로그 레벨 (default: "info")
  --log-format string       로그 포맷 (default: "json")

# 체인 어댑터
  --adapter string          어댑터 강제 지정 (auto-detect if empty)

# 데이터 관리
  --clear-data              전체 데이터 삭제 후 시작
  --reindex                 블록체인 데이터만 삭제 (검증 데이터 보존)

# 기타
  --config string           설정 파일 경로 (default: "config.yaml")
  --version                 버전 정보 출력
```

---

## PostgreSQL

`database.driver: postgres`로 색인을 PostgreSQL에 둔다(리팩터링 계획 R4-2). 여러 프로세스가 같은 DB를 볼 수 있어, 수집 프로세스 하나와 API 프로세스 여러 개로 나누는 실행 역할(R4-1)의 바탕이 된다.

- 시작할 때 `migrations/`의 스키마를 적용한다(advisory lock으로 한 번만). 이 빌드보다 새 스키마는 거부한다.
- reorg rollback은 block 트랜잭션마다 바뀐 행을 `undo_log`에 남겨 최근 128블록을 되돌린다. 그래서 행을 쓸 때마다 기록이 하나씩 더 생긴다.
- `--reindex`는 체인 데이터 테이블을 비우고 계약 검증(ABI, 소스), outbox 번호와 소비자 위치는 남긴다. `--clear-data`는 schema 버전만 남기고 모두 지운다. schema 자체는 지우지 않는다.
- DSN에 비밀번호가 들어갈 수 있으므로 로그에는 schema만 남긴다.

## Environment Variables

Docker/Kubernetes 배포 시 환경변수를 사용할 수 있습니다:

```bash
INDEXER_RPC_ENDPOINT=http://localhost:8545
INDEXER_RPC_TIMEOUT=30s
INDEXER_DB_PATH=./data
INDEXER_DB_READONLY=false
INDEXER_DB_DRIVER=pebble                 # pebble | postgres
INDEXER_DB_CACHE_MB=0
INDEXER_API_KEYS=ops:<24자 이상 키>,partner:<키>
INDEXER_DB_POSTGRES_DSN=postgres://indexer:secret@db:5432/indexer
INDEXER_DB_POSTGRES_SCHEMA=
INDEXER_DB_POSTGRES_MAX_CONNS=0
INDEXER_WORKERS=100
INDEXER_CHUNK_SIZE=1
INDEXER_START_HEIGHT=0
INDEXER_ORPHAN_RETENTION=1000
INDEXER_EVENTBUS_OUTBOX=true
INDEXER_EVENTBUS_OUTBOX_RETENTION=100000
INDEXER_API_ENABLED=true
INDEXER_API_HOST=localhost
INDEXER_API_PORT=8080
INDEXER_API_GRAPHQL=true
INDEXER_API_JSONRPC=true
INDEXER_API_WEBSOCKET=true
INDEXER_API_REST=true
INDEXER_API_CORS_ALLOWED_ORIGINS=https://explorer.example.com
INDEXER_API_TRUSTED_PROXIES=10.0.0.0/8,127.0.0.1
INDEXER_API_RATE_LIMIT_ENABLED=true
INDEXER_API_RATE_LIMIT_PER_SECOND=100
INDEXER_API_RATE_LIMIT_BURST=200
INDEXER_API_GRAPHQL_MAX_DEPTH=15
INDEXER_API_GRAPHQL_MAX_COMPLEXITY=5000
INDEXER_LOG_LEVEL=info
INDEXER_LOG_FORMAT=json
```

---

## 환경별 설정 예시

### 로컬 개발 (Anvil)

```yaml
# configs/config-anvil.yaml
rpc:
  endpoint: "http://127.0.0.1:8545"
  timeout: 10s
database:
  path: "./data-anvil"
log:
  level: "debug"
  format: "console"
indexer:
  workers: 10
  chunk_size: 1
api:
  enabled: true
  host: "localhost"
  port: 8080
  enable_graphql: true
  enable_jsonrpc: true
  enable_websocket: true
  enable_cors: true
  allowed_origins: ["*"]
```

### 프로덕션

```yaml
rpc:
  endpoint: "http://10.0.1.100:8545"
  timeout: 30s
database:
  path: "/opt/indexer-go/data"
log:
  level: "info"
  format: "json"
indexer:
  workers: 200
  chunk_size: 10
api:
  enabled: true
  host: "0.0.0.0"
  port: 8080
  enable_graphql: true
  enable_jsonrpc: true
  enable_websocket: true
  enable_cors: true
  allowed_origins:
    - "https://explorer.example.com"
  trusted_proxies:
    - "10.0.0.0/8"                      # 로드밸런서·리버스 프록시 대역
account_abstraction:
  enabled: true
verifier:
  enabled: true
  auto_download: true
eventbus:
  type: "local"
  publish_buffer_size: 5000
  history_size: 500
```

---

## Data Management

### 재인덱싱 (reindex)

블록체인 데이터만 삭제하고 검증 데이터(ABI, 소스코드)는 보존합니다.

```bash
./indexer-go --config config.yaml --reindex
```

**보존되는 데이터:**
- `/data/abi/` — 컨트랙트 ABI
- `/data/verification/` — 컨트랙트 소스코드, 검증 메타데이터
- `/index/verification/` — 검증된 컨트랙트 인덱스

**삭제되는 데이터:**
- 블록, 트랜잭션, 영수증, 로그
- 주소 인덱스, 토큰 전송
- SetCode delegation 데이터
- Account Abstraction 데이터 (UserOps, bundler/paymaster 통계)
- 컨센서스 데이터

### 전체 초기화

```bash
./indexer-go --config config.yaml --clear-data
```

---

## Performance Tuning

| 파라미터 | 기본값 | 권장 (동기화) | 권장 (실시간) | 설명 |
|---------|--------|-------------|-------------|------|
| `workers` | 100 | 200-500 | 50-100 | RPC 노드 용량에 따라 조정 |
| `chunk_size` | 1 | 10-50 | 1 | 실시간 모드에서는 1 권장 |
| `eventbus.publish_buffer_size` | 1000 | 5000 | 1000 | EventBus 버퍼 크기 |
| `eventbus.history_size` | 100 | 100 | 500 | 이벤트 히스토리 (Replay용) |

> **SSD 사용 권장**: PebbleDB 성능을 위해 SSD 스토리지를 사용하세요.
> **IPv4 권장**: `127.0.0.1` 사용 (`localhost`는 IPv6로 해석될 수 있음).

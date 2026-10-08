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

다음 설정은 읽지만 아직 동작에 반영되지 않는다. 설정되어 있으면 시작 로그에 경고가 남는다: `eventbus.type`(local 외), `node.priority`, `account_abstraction.entry_point_addresses`. `watchlist.enabled`와 `resilience.enabled`는 v0.1.0 이후 해당 기능을 지웠으므로 효과가 없고, 켜져 있으면 경고가 남는다.

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
  enable_cors: true
  allowed_origins:
    - "*"                               # CORS 허용 오리진 (* = 전체 허용)
  subscription_engine: true             # GraphQL 구독을 구독 엔진으로 전달 (false = 이전 방식)

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

`enabled`의 기본값은 true다(키를 생략하면 켜진다). UserOp(ERC-4337)과 모듈(ERC-7579) 색인을 함께 켜고 끈다. `entry_point_addresses`는 아직 처리기가 지원하지 않아 무시되고, 알려진 EntryPoint 주소(v0.6, v0.7)를 쓴다.

```yaml
account_abstraction:
  enabled: true
  entry_point_addresses:                # EntryPoint 컨트랙트 주소 (빈 배열 = 이벤트 시그니처로 자동 감지)
    - "0x0000000071727De22E5E9d8BAf0edAc6f37da032"  # EntryPoint v0.7
    - "0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"  # EntryPoint v0.6
```

### System Contracts (Stable-One)

```yaml
system_contracts:
  enabled: true
  source_path: ""                       # 시스템 컨트랙트 소스 경로
  include_abstracts: false
```

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

> 체인마다 DB를 따로 둔다: `<database.path>/chains/<id>`(PostgreSQL이면 schema `<schema>_<id>`, schema를 비우면 `chain_<id>`. 소문자로 바꾸고 schema 이름에 쓸 수 없는 문자는 `_`로 바꾸며, 두 체인이 같은 schema가 되면 시작하지 않는다). 각 체인은 자기 수집 루프, 이벤트 버스, 기능으로 돌고 `features.*` 같은 공통 설정을 함께 쓴다. 루트의 `rpc.endpoint`는 필요 없고, `rpc.fallback_endpoints`·`rpc.ws_endpoint`·`rpc.record_dir`·`source.era_dir`·`notifications`·`verifier`는 이 모드에서 쓰이지 않는다(시작 시 경고). `chain_id`는 노드가 알려 주는 값과 같아야 그 체인이 시작된다. `--reindex`는 체인 DB마다 적용된다.
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
- `api` 프로세스에서는 쓰는 기능이 동작하지 않는다: 알림(`notifications.enabled`)과 계약 검증(`verifier.enabled`)은 무시하고 경고를 남긴다. 알림 설정처럼 저장하는 API 요청은 읽기 전용 오류로 끝난다. genesis 잔액과 노드에서 가져온 토큰 메타데이터는 응답에는 쓰지만 저장하지 않는다.

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
account_abstraction:
  enabled: true
  entry_point_addresses:
    - "0x0000000071727De22E5E9d8BAf0edAc6f37da032"
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

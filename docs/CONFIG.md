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
- `database.readonly: true`: 수집기는 써야 한다. API 전용 실행은 별도 작업으로 계획되어 있다.

다음 설정은 읽지만 아직 동작에 반영되지 않는다. 설정되어 있으면 시작 로그에 경고가 남는다: `eventbus.type`(local 외), `node.*`, `account_abstraction.entry_point_addresses`. `watchlist.enabled`와 `resilience.enabled`는 v0.1.0 이후 해당 기능을 지웠으므로 효과가 없고, 켜져 있으면 경고가 남는다.

---

## config.yaml (권장)

### 기본 설정

```yaml
# config.yaml
rpc:
  endpoint: "http://127.0.0.1:8545"    # RPC 엔드포인트 (IPv4 권장)
  timeout: 30s                          # 요청 타임아웃

database:
  path: "./data"                        # PebbleDB 데이터 디렉토리
  readonly: false                       # 읽기 전용 모드

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
```

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

> 체인마다 DB를 따로 둔다: `<database.path>/chains/<id>`. 각 체인은 자기 수집 루프, 이벤트 버스, 기능으로 돌고 `features.*` 같은 공통 설정을 함께 쓴다. 루트의 `rpc.endpoint`는 필요 없고, `rpc.fallback_endpoints`·`rpc.ws_endpoint`·`rpc.record_dir`·`source.era_dir`·`notifications`·`verifier`는 이 모드에서 쓰이지 않는다(시작 시 경고). `chain_id`는 노드가 알려 주는 값과 같아야 그 체인이 시작된다. `--reindex`는 체인 DB마다 적용된다.
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
```

### Node Identity

```yaml
node:
  id: "node-1"                         # 노드 식별자
  role: "all"                           # writer | reader | all
  priority: 0
```

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

## Environment Variables

Docker/Kubernetes 배포 시 환경변수를 사용할 수 있습니다:

```bash
INDEXER_RPC_ENDPOINT=http://localhost:8545
INDEXER_RPC_TIMEOUT=30s
INDEXER_DB_PATH=./data
INDEXER_DB_READONLY=false
INDEXER_WORKERS=100
INDEXER_CHUNK_SIZE=1
INDEXER_START_HEIGHT=0
INDEXER_ORPHAN_RETENTION=1000
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

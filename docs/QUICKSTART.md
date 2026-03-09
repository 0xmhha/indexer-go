# Quick Start

## Prerequisites

- **Go 1.24+** (toolchain 1.24.9)
- RPC 엔드포인트 (Stable-One, Ethereum, 또는 Anvil)

---

## 1. 빌드

```bash
git clone https://github.com/0xmhha/indexer-go.git
cd indexer-go

# 의존성 설치
go mod download

# 빌드
go build -o build/indexer-go ./cmd/indexer

# 버전 정보 포함 빌드
VERSION=$(git describe --tags --always --dirty)
COMMIT=$(git rev-parse --short HEAD)
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')

go build -ldflags "-s -w \
  -X main.version=$VERSION \
  -X main.commit=$COMMIT \
  -X main.buildTime=$BUILD_TIME" \
  -o build/indexer-go ./cmd/indexer
```

---

## 2. 설정

`config.yaml` 파일 생성:

```yaml
rpc:
  endpoint: "http://127.0.0.1:8545"
  timeout: 30s

database:
  path: "./data"

log:
  level: "info"
  format: "json"

indexer:
  workers: 100
  chunk_size: 1
  start_height: 0

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

> 전체 설정 옵션은 [CONFIG.md](CONFIG.md) 참조

---

## 3. 실행

### 인덱서 + API 서버

```bash
./build/indexer-go --config config.yaml
```

### CLI 플래그로 실행

```bash
./build/indexer-go \
  --rpc http://127.0.0.1:8545 \
  --db ./data \
  --api --graphql --jsonrpc --websocket \
  --api-port 8080 \
  --log-level info
```

---

## 4. 동작 확인

### 헬스체크

```bash
curl http://localhost:8080/health
```

### GraphQL Playground

브라우저에서 열기: http://localhost:8080/playground

### 최신 인덱싱 높이

```bash
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ latestHeight }"}'
```

### 블록 조회

```bash
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ block(height: \"1\") { hash height time numTxs } }"}'
```

### 트랜잭션 조회

```bash
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ transaction(hash: \"0xabc...\") { hash from to value } }"}'
```

### JSON-RPC 조회

```bash
curl -s http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1}'
```

---

## 5. 로컬 개발 (Anvil)

[Anvil](https://book.getfoundry.sh/anvil/)을 사용한 로컬 개발:

```bash
# Terminal 1: Anvil 실행
anvil --block-time 2

# Terminal 2: Anvil 설정으로 인덱서 실행
./build/indexer-go --config configs/config-anvil.yaml

# Terminal 3: 테스트 트랜잭션 전송
cast send \
  --from 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 \
  --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
  0x70997970C51812dc3A010C7d01b50e0d17dc79C8 \
  --value 1ether \
  --rpc-url http://127.0.0.1:8545
```

---

## 6. Ethereum Sepolia 테스트넷

```bash
./build/indexer-go --config configs/config-sepolia.yaml
```

---

## 7. Docker

```bash
# 빌드
docker build -t indexer-go:latest .

# 실행
docker run -d \
  --name indexer-go \
  -p 8080:8080 \
  -v $(pwd)/data:/data \
  -v $(pwd)/config.yaml:/app/config.yaml \
  indexer-go:latest
```

### Docker Compose

```yaml
version: '3.8'
services:
  indexer:
    image: indexer-go:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
      - ./config.yaml:/app/config.yaml
    extra_hosts:
      - "host.docker.internal:host-gateway"
    restart: unless-stopped
```

---

## 8. 프로덕션 배포

```bash
# systemd 서비스 설치
sudo cp build/indexer-go /opt/indexer-go/bin/
sudo cp deployments/systemd/indexer-go.service /etc/systemd/system/
sudo mkdir -p /etc/indexer-go
sudo cp config.yaml /etc/indexer-go/

# 서비스 시작
sudo systemctl daemon-reload
sudo systemctl enable indexer-go
sudo systemctl start indexer-go

# 상태 확인
sudo systemctl status indexer-go
curl http://localhost:8080/health
```

---

## 9. 데이터 관리

```bash
# 재인덱싱 (ABI, 검증 데이터 보존)
./build/indexer-go --config config.yaml --reindex

# 전체 초기화
./build/indexer-go --config config.yaml --clear-data
```

---

## 10. 지원 네트워크

| Network | Adapter | Chain ID | Config |
|---------|---------|----------|--------|
| Anvil (로컬) | anvil | 31337 | `configs/config-anvil.yaml` |
| Ethereum Sepolia | evm | 11155111 | `configs/config-sepolia.yaml` |
| Stable-One | stableone | custom | `config.yaml` |
| 기타 EVM 체인 | auto-detect | any | 커스텀 config |

---

## Next Steps

- [ARCHITECTURE.md](ARCHITECTURE.md) — 시스템 아키텍처 및 코드 구조
- [API.md](API.md) — GraphQL, JSON-RPC, WebSocket API 레퍼런스
- [CONFIG.md](CONFIG.md) — 전체 설정 옵션

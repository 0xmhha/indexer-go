# indexer-go

> High-performance blockchain indexer for Stable-One (Ethereum-based) chain

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**indexer-go**는 Stable-One 블록체인의 블록 및 트랜잭션 데이터를 실시간으로 인덱싱하고, GraphQL 및 JSON-RPC API를 통해 효율적으로 쿼리할 수 있게 해주는 고성능 인덱서입니다.

---

## 🚀 Features

- ✅ **Ethereum JSON-RPC 기반** - go-ethereum (ethclient) 사용
- ✅ **병렬 인덱싱** - Worker pool을 통한 고속 인덱싱 (80-150 블록/초)
- ✅ **완전한 데이터** - Block + Transaction + Receipt 인덱싱
- ✅ **GraphQL API** - 유연한 쿼리 및 필터링
- ✅ **JSON-RPC 2.0 API** - 표준 호환 API
- ✅ **WebSocket 구독** - 실시간 블록/트랜잭션 알림
- ✅ **임베디드 DB** - PebbleDB (LevelDB 호환)
- ✅ **EIP 지원** - EIP-1559, EIP-4844 등 최신 EIP
- ✅ **Fee Delegation** - WEMIX 특화 수수료 대납 기능

---

## 📊 Architecture

```
┌─────────────────┐
│  Stable-One     │
│  Node (RPC)     │
└────────┬────────┘
         │ ethclient
         ↓
┌─────────────────┐
│  Client Layer   │  ← Ethereum JSON-RPC
└────────┬────────┘
         │
         ↓
┌─────────────────┐
│  Fetcher        │  ← Worker Pool (100 workers)
│  (Worker Pool)  │
└────────┬────────┘
         │
         ↓
┌─────────────────┐
│  Storage        │  ← PebbleDB (RLP encoding)
│  (PebbleDB)     │
└────────┬────────┘
         │
         ↓
┌─────────────────────────────────────┐
│  API Server                         │
│  ┌──────────┐  ┌──────────┐        │
│  │ GraphQL  │  │ JSON-RPC │        │
│  │   API    │  │   API    │        │
│  └──────────┘  └──────────┘        │
│  ┌──────────────────────┐          │
│  │  WebSocket Subscribe │          │
│  └──────────────────────┘          │
└─────────────────────────────────────┘
```

---

## 🛠️ Tech Stack

- **Language**: Go 1.21+
- **Ethereum**: [go-ethereum](https://github.com/ethereum/go-ethereum) (ethclient, types, RLP)
- **Database**: [PebbleDB](https://github.com/cockroachdb/pebble)
- **GraphQL**: [gqlgen](https://github.com/99designs/gqlgen)
- **HTTP**: [chi](https://github.com/go-chi/chi)
- **WebSocket**: [gorilla/websocket](https://github.com/gorilla/websocket)
- **Logging**: [zap](https://github.com/uber-go/zap)

---

## 📦 Installation

### Prerequisites

- Go 1.21 or higher
- Access to Stable-One RPC endpoint

### Build from source

```bash
# Clone repository
git clone https://github.com/your-org/indexer-go.git
cd indexer-go

# Install dependencies
go mod download

# Build production binary
go build -o build/indexer-go ./cmd/indexer

# Build with version information
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

## 🚀 Quick Start

### 1. Start indexing (indexer only)

```bash
./build/indexer-go \
  --rpc http://localhost:8545 \
  --db ./data \
  --log-level info
```

### 2. Start with API server (GraphQL + JSON-RPC + WebSocket)

```bash
./build/indexer-go \
  --rpc http://localhost:8545 \
  --db ./data \
  --api \
  --graphql \
  --jsonrpc \
  --websocket \
  --api-port 8080
```

### 3. Query via GraphQL

```bash
# GraphQL Playground (browser)
open http://localhost:8080/playground

# GraphQL API (curl)
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ block(height: 1000) { hash time num_txs } }"
  }'
```

### 4. Query via JSON-RPC

```bash
curl -X POST http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "getBlock",
    "params": [1000],
    "id": 1
  }'
```

### 5. Subscribe via WebSocket

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.send(JSON.stringify({
  jsonrpc: '2.0',
  method: 'subscribe',
  params: ['newBlock'],
  id: 1
}));

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('New block:', data);
};
```

### 6. Check health

```bash
curl http://localhost:8080/health
# {"status":"ok","timestamp":"2025-10-20T15:00:00Z"}

curl http://localhost:8080/version
# {"version":"1.0.0","name":"indexer-go"}
```

---

## ⚙️ Configuration

Configuration can be provided through (in order of priority):
1. Command-line flags (highest priority)
2. Environment variables
3. Configuration file (YAML)
4. Default values (lowest priority)

### Command-line flags

```bash
./indexer-go [flags]

Required Flags:
  --rpc string              Ethereum RPC endpoint URL
  --db string               Database path

Indexer Flags:
  --workers int             Number of concurrent workers (default: 100)
  --batch-size int          Number of blocks per batch (default: 100)
  --start-height uint       Block height to start indexing from (default: 0)
  --gap-recovery            Enable gap detection and recovery at startup

API Server Flags:
  --api                     Enable API server
  --api-host string         API server host (default: "localhost")
  --api-port int            API server port (default: 8080)
  --graphql                 Enable GraphQL API
  --jsonrpc                 Enable JSON-RPC API
  --websocket               Enable WebSocket API

Logging Flags:
  --log-level string        Log level: debug, info, warn, error (default: "info")
  --log-format string       Log format: json, console (default: "json")

Other Flags:
  --config string           Path to configuration file (YAML)
  --version                 Show version information and exit
```

### Environment variables

```bash
# RPC Configuration
INDEXER_RPC_ENDPOINT=http://localhost:8545
INDEXER_RPC_TIMEOUT=30s

# Database Configuration
INDEXER_DB_PATH=./data
INDEXER_DB_READONLY=false

# Indexer Configuration
INDEXER_WORKERS=100
INDEXER_CHUNK_SIZE=100
INDEXER_START_HEIGHT=0

# API Server Configuration
INDEXER_API_ENABLED=true
INDEXER_API_HOST=localhost
INDEXER_API_PORT=8080
INDEXER_API_GRAPHQL=true
INDEXER_API_JSONRPC=true
INDEXER_API_WEBSOCKET=true

# Logging Configuration
INDEXER_LOG_LEVEL=info
INDEXER_LOG_FORMAT=json
```

See [`.env.example`](.env.example) for a complete example.

### Config file (YAML)

```yaml
# config.yaml
rpc:
  endpoint: "http://localhost:8545"
  timeout: 30s

database:
  path: "./data"
  readonly: false

log:
  level: "info"
  format: "json"

indexer:
  workers: 100
  chunk_size: 100
  start_height: 0

api:
  enabled: true
  host: "localhost"
  port: 8080
  enable_graphql: true
  enable_jsonrpc: true
  enable_websocket: true
  enable_cors: true
  allowed_origins:
    - "*"
```

See [`config.example.yaml`](config.example.yaml) for a complete example.

### Example usage

```bash
# Using config file
./indexer-go --config config.yaml

# Using environment variables
export INDEXER_RPC_ENDPOINT=http://localhost:8545
export INDEXER_DB_PATH=./data
./indexer-go

# Using CLI flags (override config file and env vars)
./indexer-go \
  --config config.yaml \
  --rpc http://custom-rpc:8545 \
  --workers 200
```

---

## 📖 API Documentation

### GraphQL API

#### Queries

```graphql
# Get block by height
query {
  block(height: 1000) {
    hash
    height
    time
    miner
    gas_used
    gas_limit
    num_txs
    txs {
      hash
      from
      to
      value
      gas_used
    }
  }
}

# Get transactions with filter
query {
  transactions(filter: {
    block_height_min: 1000
    block_height_max: 2000
    from: "0x1234..."
  }) {
    hash
    block_height
    from
    to
    value
    status
  }
}

# Get transactions by address
query {
  transactionsByAddress(address: "0x1234...") {
    hash
    block_height
    from
    to
    value
  }
}
```

#### Subscriptions

```graphql
# Subscribe to new blocks
subscription {
  newBlock {
    hash
    height
    time
    num_txs
  }
}

# Subscribe to new transactions
subscription {
  newTransaction {
    hash
    block_height
    from
    to
    value
  }
}
```

### JSON-RPC API

#### Methods

```javascript
// Get block by height
{
  "jsonrpc": "2.0",
  "method": "getBlock",
  "params": [1000],
  "id": 1
}

// Get transaction by hash
{
  "jsonrpc": "2.0",
  "method": "getTxResult",
  "params": ["0xabc..."],
  "id": 1
}

// Get transaction receipt
{
  "jsonrpc": "2.0",
  "method": "getTxReceipt",
  "params": ["0xabc..."],
  "id": 1
}

// Get latest height
{
  "jsonrpc": "2.0",
  "method": "getLatestHeight",
  "params": [],
  "id": 1
}
```

---

## 🔧 Development

### Setup development environment

```bash
# Install dependencies
go mod download

# Install tools
make tools

# Generate GraphQL code
make generate

# Run tests
make test

# Run linter
make lint
```

### Project structure

```
indexer-go/
├── cmd/                # Entry points
├── client/             # Ethereum RPC client
├── fetch/              # Blockchain data fetcher
├── storage/            # Database layer (PebbleDB)
├── events/             # Event subscription system
├── serve/              # API server (GraphQL, JSON-RPC)
├── types/              # Common types
├── internal/           # Internal packages
├── docs/               # Documentation
└── scripts/            # Build & utility scripts
```

### Run locally

```bash
# Terminal 1: Start Stable-One node (or use testnet)
# ...

# Terminal 2: Start indexer
go run ./cmd start \
  --remote http://localhost:8545 \
  --db-path ./dev-data \
  --log-level debug
```

---

## 📈 Performance

### Benchmarks

| Metric | Target | Achieved |
|--------|--------|----------|
| Indexing speed | 80-150 blocks/s | TBD |
| GraphQL query | <100ms | TBD |
| JSON-RPC query | <50ms | TBD |
| WebSocket latency | <20ms | TBD |
| Memory usage | <2GB (100 workers) | TBD |

### Optimization tips

- **Worker pool size**: Adjust `--max-slots` based on RPC node capacity
- **Chunk size**: Increase `--max-chunk-size` for faster sync (if RPC allows)
- **Database**: Use SSD for better PebbleDB performance
- **Network**: Low-latency connection to RPC node recommended

---

## 🧪 Testing

```bash
# Run all tests
make test

# Run unit tests only
go test ./... -short

# Run integration tests
go test ./... -tags=integration

# Run with coverage
make coverage

# Run benchmarks
make bench
```

---

## 📚 Documentation

- 📄 [IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) - Detailed implementation plan
- 📄 [STABLE_ONE_TECHNICAL_ANALYSIS.md](docs/STABLE_ONE_TECHNICAL_ANALYSIS.md) - Stable-One chain analysis
- 📄 [API_REFERENCE.md](docs/API_REFERENCE.md) - Complete API reference (TBD)

---

## 🐳 Docker

### Build image

```bash
docker build -t indexer-go:latest .
```

### Run container

```bash
docker run -d \
  --name indexer-go \
  -p 8080:8080 \
  -v $(pwd)/data:/data \
  -e INDEXER_REMOTE=http://stable-one-node:8545 \
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
    environment:
      INDEXER_REMOTE: http://stable-one-node:8545
      INDEXER_LOG_LEVEL: info
    restart: unless-stopped
```

---

## 🤝 Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

---

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## 🙏 Acknowledgments

- Inspired by [tx-indexer](https://github.com/gnolang/tx-indexer) (Gno chain indexer)
- Built with [go-ethereum](https://github.com/ethereum/go-ethereum)
- Database powered by [PebbleDB](https://github.com/cockroachdb/pebble)

---

## 📞 Support

- 📧 Email: support@example.com
- 💬 Discord: [Join our server](https://discord.gg/example)
- 🐛 Issues: [GitHub Issues](https://github.com/your-org/indexer-go/issues)

---

**Status**: 🚧 Under Development (Phase 1)

**Version**: 0.1.0

**Last Updated**: 2025-10-16

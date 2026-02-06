# indexer-go

> High-performance blockchain indexer for Stable-One (Ethereum-based) chain

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**indexer-go** is a high-performance indexer that indexes Stable-One blockchain blocks and transaction data in real-time, enabling efficient querying through GraphQL and JSON-RPC APIs.

---

## 📊 Architecture

```
Stable-One Node (RPC)
         ↓
    Client Layer (ethclient)
         ↓
    Fetcher (Worker Pool) ──→ EventBus (Pub/Sub)
         ↓                          ↓
    Storage (PebbleDB)              ↓
         ↓                          ↓
    ┌─────────────────────────────────────┐
    │  API Server                         │
    │  GraphQL │ JSON-RPC │ WebSocket     │
    └─────────────────────────────────────┘
```

> 📖 See detailed architecture: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

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

- Go 1.24 or higher
- Access to Stable-One RPC endpoint

### Build from source

```bash
# Clone repository
git clone https://github.com/0xmhha/indexer-go.git
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

### 6. Testing with Different Networks

The indexer supports multiple EVM-compatible networks through auto-detection. Pre-configured configs are available in the `configs/` directory.

#### Anvil (Local Development)

[Anvil](https://book.getfoundry.sh/anvil/) is a local Ethereum node for development and testing.

```bash
# Terminal 1: Start Anvil with 2-second block time
anvil --block-time 2

# Terminal 2: Start indexer with Anvil config
go run ./cmd/indexer --config configs/config-anvil.yaml

# Optional: Send a test transaction
cast send --from 0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266 \
  --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
  0x70997970C51812dc3A010C7d01b50e0d17dc79C8 \
  --value 1ether \
  --rpc-url http://127.0.0.1:8545
```

#### Ethereum Sepolia Testnet

Connect to Ethereum Sepolia testnet using public RPC endpoints.

```bash
# Start indexer with Sepolia config
go run ./cmd/indexer --config configs/config-sepolia.yaml

# Check current Sepolia block height
curl -s -X POST -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' \
  https://ethereum-sepolia-rpc.publicnode.com | jq -r '.result'
```

#### Supported Networks

| Network | Adapter | Chain ID | Config |
|---------|---------|----------|--------|
| Anvil (local) | anvil | 31337 | `configs/config-anvil.yaml` |
| Ethereum Sepolia | evm (geth) | 11155111 | `configs/config-sepolia.yaml` |
| Stable-One | stableone | custom | `config.yaml` |
| Any EVM chain | auto-detect | any | custom config |

### 7. Subscribe to Real-Time Events

```go
package main

import (
    "fmt"
    "github.com/0xmhha/indexer-go/events"
    "github.com/ethereum/go-ethereum/common"
)

func main() {
    // Create EventBus
    bus := events.NewEventBus(1000, 100)
    go bus.Run()
    defer bus.Stop()

    // Subscribe to block events
    blockSub := bus.Subscribe(
        "block-monitor",
        []events.EventType{events.EventTypeBlock},
        nil, // no filter
        100,
    )

    // Subscribe to high-value transactions
    filter := &events.Filter{
        MinValue: big.NewInt(1000000000000000000), // 1 ETH
    }
    txSub := bus.Subscribe(
        "high-value-tx",
        []events.EventType{events.EventTypeTransaction},
        filter,
        100,
    )

    // Process block events
    go func() {
        for event := range blockSub.Channel {
            blockEvent := event.(*events.BlockEvent)
            fmt.Printf("New block %d: %d txs\n",
                blockEvent.Number, blockEvent.TxCount)
        }
    }()

    // Process transaction events
    go func() {
        for event := range txSub.Channel {
            txEvent := event.(*events.TransactionEvent)
            fmt.Printf("High-value TX: %s (%s)\n",
                txEvent.Hash, txEvent.Value)
        }
    }()

    // Keep running
    select {}
}
```

### 8. Monitor with Prometheus

```bash
# Check system health with EventBus statistics
curl http://localhost:8080/health

# View subscriber statistics
curl http://localhost:8080/subscribers

# Scrape Prometheus metrics
curl http://localhost:8080/metrics
```

### 9. Data Management

```bash
# Clear all data and start fresh
./build/indexer-go --config config.yaml --clear-data

# Re-index blockchain while preserving contract verification data
# This keeps ABIs, source code, and verification status intact
./build/indexer-go --config config.yaml --reindex
```

The `--reindex` option is useful when:
- You need to re-sync blockchain data due to data corruption
- You want to rebuild indexes without losing verified contract information
- Upgrading the indexer requires a fresh re-index

**Data preserved with `--reindex`:**
- `/data/abi/` - Contract ABIs
- `/data/verification/` - Contract source code and verification metadata
- `/index/verification/` - Verified contracts index

**Data cleared with `--reindex`:**
- Blocks, transactions, receipts, logs
- Address indexes, token transfers
- All other blockchain-derived data

---

## ⚙️ Configuration

Configuration can be provided through (in order of priority):
1. Command-line flags (highest priority)
2. Configuration file (YAML - `config.yaml`)
3. Environment variables (still supported for deployment flexibility)
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

Chain Adapter Flags:
  --adapter string          Force specific adapter type (anvil, stableone, evm). Auto-detected if empty

Data Management Flags:
  --clear-data              Clear (delete) the entire data folder before starting
  --reindex                 Clear blockchain data only, preserving verification data
                            (ABIs, source code, verification status)

Other Flags:
  --config string           Path to configuration file (YAML) (default: "config.yaml")
  --version                 Show version information and exit
```

### Environment variables (Optional)

Environment variables are still supported for deployment flexibility (e.g., Docker, Kubernetes), but **config.yaml is now the recommended primary configuration method**.

```bash
# RPC Configuration
INDEXER_RPC_ENDPOINT=http://localhost:8545
INDEXER_RPC_TIMEOUT=30s

# Database Configuration
INDEXER_DB_PATH=./data
INDEXER_DB_READONLY=false

# Indexer Configuration
INDEXER_WORKERS=100
INDEXER_CHUNK_SIZE=1          # Use 1 for real-time mode
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

**Note**: `.env` files are no longer automatically loaded. Use environment variables directly or configure via `config.yaml`.

### Config file (YAML) - Recommended

The recommended way to configure the indexer is using `config.yaml`:

```yaml
# config.yaml
rpc:
  # Use 127.0.0.1 instead of localhost to force IPv4
  endpoint: "http://127.0.0.1:8501"
  timeout: 30s

database:
  path: "./data"
  readonly: false

log:
  level: "info"
  format: "json"

indexer:
  workers: 100
  chunk_size: 1        # Use 1 for real-time block delivery
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
# Using config file (recommended - auto-loaded by default)
./indexer-go

# Specify custom config file
./indexer-go --config /path/to/config.yaml

# Using environment variables (for Docker/K8s deployments)
export INDEXER_RPC_ENDPOINT=http://127.0.0.1:8501
export INDEXER_DB_PATH=./data
export INDEXER_CHUNK_SIZE=1
./indexer-go

# Using CLI flags (override config file and env vars)
./indexer-go \
  --rpc http://127.0.0.1:8501 \
  --batch-size 1 \
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

> 📖 See project structure: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

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

### Event Subscription Performance

| Metric | Target | Achieved ✅ |
|--------|--------|-------------|
| Event throughput | 1,000 events/s | **100M+ events/s** |
| Delivery latency | <10ms | **Sub-microsecond** |
| Max subscribers | 1,000 | **10,000+** |
| Memory allocations | Minimal | **Zero** |
| Subscriber delivery | <100µs | **8.5 ns/op** |

### Optimization tips

- **Worker pool size**: Adjust `--max-slots` based on RPC node capacity
- **Chunk size**: Increase `--max-chunk-size` for faster sync (if RPC allows)
- **Database**: Use SSD for better PebbleDB performance
- **Network**: Low-latency connection to RPC node recommended
- **Event buffers**: Tune subscriber channel sizes based on processing speed
- **Monitoring**: Enable Prometheus metrics for production deployments

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

### Core Documentation
- 📄 [ARCHITECTURE.md](docs/ARCHITECTURE.md) - System architecture and internals
- 📄 [STABLE_ONE_TECHNICAL_ANALYSIS.md](docs/STABLE_ONE_TECHNICAL_ANALYSIS.md) - Stable-One chain analysis

### Event Subscription System
- 📄 [EVENT_SUBSCRIPTION_API.md](docs/EVENT_SUBSCRIPTION_API.md) - Complete Event Subscription API reference
- 📄 [METRICS_MONITORING.md](docs/METRICS_MONITORING.md) - Prometheus metrics and monitoring guide

### Production Deployment
- 📄 [OPERATIONS_GUIDE.md](docs/OPERATIONS_GUIDE.md) - Production deployment and operations guide

---

## 🚀 Production Deployment

### Quick Deploy

```bash
# Automated deployment with systemd
cd deployments/scripts
sudo ./deploy.sh latest

# Configure
sudo nano /etc/indexer-go/config.yaml
sudo nano /etc/indexer-go/indexer-go.env

# Start service
sudo systemctl enable indexer-go
sudo systemctl start indexer-go

# Verify
curl http://localhost:8080/health
```

### Manual Setup

```bash
# 1. Install binary
sudo cp build/indexer-go /opt/indexer-go/bin/

# 2. Install systemd service
sudo cp deployments/systemd/indexer-go.service /etc/systemd/system/
sudo systemctl daemon-reload

# 3. Install logrotate
sudo cp deployments/logrotate/indexer-go /etc/logrotate.d/

# 4. Configure and start
sudo systemctl enable indexer-go
sudo systemctl start indexer-go
```

### Health Check

```bash
# Run automated health check
./deployments/scripts/health-check.sh localhost:8080
```

See [OPERATIONS_GUIDE.md](docs/OPERATIONS_GUIDE.md) for complete deployment documentation.

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
  -e INDEXER_REMOTE=http://host.docker.internal:8545 \
  indexer-go:latest

# For Linux, add: --add-host=host.docker.internal:host-gateway
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
      INDEXER_REMOTE: http://host.docker.internal:8545
      INDEXER_LOG_LEVEL: info
    extra_hosts:
      - "host.docker.internal:host-gateway"  # For Linux
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

- 🐛 Issues: [GitHub Issues](https://github.com/0xmhha/indexer-go/issues)

---

**Version**: 0.7.1

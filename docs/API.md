# API Reference

indexer-go는 3가지 프로토콜로 데이터를 제공합니다: **GraphQL**, **JSON-RPC**, **WebSocket**.

---

## Endpoints

| Path | Protocol | Description |
|------|----------|-------------|
| `/graphql` | POST | GraphQL API |
| `/playground` | GET | GraphQL Playground (브라우저) |
| `/graphql/ws` | WebSocket | GraphQL 서브스크립션 |
| `/rpc` | POST | JSON-RPC API |
| `/ws` | WebSocket | 실시간 이벤트 구독 |
| `/api` | GET/POST | Etherscan 호환 API |
| `/health` | GET | 헬스체크 |
| `/metrics` | GET | Prometheus 메트릭 |

---

## GraphQL API

### Custom Scalars

| Scalar | Format | Example |
|--------|--------|---------|
| `BigInt` | 숫자 문자열 | `"1000000"` |
| `Hash` | 0x 접두사 32바이트 hex | `"0xabc..."` |
| `Address` | 0x 접두사 20바이트 hex | `"0x1234..."` |
| `Bytes` | 0x 접두사 hex | `"0x..."` |

> **Note**: GraphQL 변수에서 custom scalar는 모두 `String` 타입으로 전달합니다.

### Pagination

페이지네이션이 필요한 쿼리는 `pagination` 인자를 받습니다:

```graphql
pagination: { limit: Int, offset: Int }
```

응답은 Connection 타입으로 반환됩니다:
```graphql
{
  nodes: [T!]!
  totalCount: Int!
  pageInfo: {
    hasNextPage: Boolean!
    hasPreviousPage: Boolean!
  }
}
```

---

### Core Queries — 블록/트랜잭션/영수증

```graphql
# 최신 인덱싱 높이
query { latestHeight }

# 블록 조회 (높이)
query {
  block(height: "1000") {
    hash
    height
    parentHash
    time
    miner
    gasUsed
    gasLimit
    numTxs
    baseFeePerGas
  }
}

# 블록 조회 (해시)
query {
  blockByHash(hash: "0xabc...") {
    height
    time
    numTxs
  }
}

# 블록 범위 조회
query {
  blocksRange(from: "100", to: "110") {
    height
    time
    numTxs
  }
}

# 트랜잭션 조회
query {
  transaction(hash: "0xabc...") {
    hash
    blockNumber
    from
    to
    value
    gas
    gasPrice
    type
    input
    nonce
    receipt {
      status
      gasUsed
      contractAddress
      logs {
        address
        topics
        data
        decoded {
          eventName
          params { name type value indexed }
        }
      }
    }
  }
}

# 주소별 트랜잭션 (페이지네이션)
query {
  transactionsByAddress(
    address: "0x1234..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes {
      hash
      blockNumber
      from
      to
      value
      type
    }
    totalCount
    pageInfo { hasNextPage hasPreviousPage }
  }
}

# 영수증 조회
query {
  receipt(transactionHash: "0xabc...") {
    status
    gasUsed
    cumulativeGasUsed
    effectiveGasPrice
    contractAddress
    logs { address topics data logIndex }
  }
}

# 로그 필터
query {
  logs(filter: {
    fromBlock: "100"
    toBlock: "200"
    address: "0x1234..."
    topics: ["0xddf252..."]
  }) {
    nodes { address topics data blockNumber transactionHash logIndex }
    totalCount
  }
}

# 카운트
query { blockCount }
query { transactionCount }
```

#### curl 예시

```bash
# 최신 높이
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ latestHeight }"}'

# 블록 조회
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ block(height: \"1000\") { hash height time numTxs } }"}'

# 트랜잭션 조회
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ transaction(hash: \"0xabc...\") { hash from to value } }"}'
```

---

### Address Indexing Queries — 주소/컨트랙트/토큰

```graphql
# 주소 개요
query {
  addressOverview(address: "0x1234...") {
    address
    balance
    txCount
    isContract
    contractCreator
  }
}

# 컨트랙트 생성 정보
query {
  contractCreation(address: "0x1234...") {
    creator
    txHash
    blockNumber
  }
}

# ERC-20 전송 (주소별)
query {
  erc20TransfersByAddress(
    address: "0x1234..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes {
      txHash
      from
      to
      value
      tokenAddress
      blockNumber
    }
    totalCount
  }
}

# ERC-721 전송 (토큰별)
query {
  erc721TransfersByToken(
    token: "0x5678..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes { txHash from to tokenId blockNumber }
    totalCount
  }
}

# Internal 트랜잭션
query {
  internalTransactionsByAddress(
    address: "0x1234..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes { txHash from to value callType blockNumber }
    totalCount
  }
}

# 컨트랙트 검증 상태
query {
  contractVerification(address: "0x1234...") {
    verified
    contractName
    compilerVersion
    sourceCode
    abi
  }
}
```

---

### EIP-4337 Account Abstraction Queries

```graphql
# UserOperation 단건 조회
query {
  userOp(userOpHash: "0xabc...") {
    userOpHash
    txHash
    blockNumber
    blockHash
    txIndex
    logIndex
    sender
    paymaster
    nonce
    success
    actualGasCost
    actualUserOpFeePerGas
    bundler
    entryPoint
    timestamp
  }
}

# sender별 UserOp (페이지네이션)
query {
  userOpsBySender(
    sender: "0x1234..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes {
      userOpHash
      txHash
      blockNumber
      sender
      paymaster
      success
      actualGasCost
      bundler
      entryPoint
      timestamp
    }
    totalCount
    pageInfo { hasNextPage hasPreviousPage }
  }
}

# bundler별 UserOp (페이지네이션)
query {
  userOpsByBundler(
    bundler: "0x5678..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes { userOpHash sender success actualGasCost paymaster }
    totalCount
  }
}

# paymaster별 UserOp (페이지네이션)
query {
  userOpsByPaymaster(
    paymaster: "0x9abc..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes { userOpHash sender success actualGasCost bundler }
    totalCount
  }
}

# 트랜잭션 내 UserOp (번들 조회)
query {
  userOpsByTx(txHash: "0xabc...") {
    userOpHash
    sender
    paymaster
    success
    actualGasCost
    bundler
    entryPoint
  }
}

# 블록 내 UserOp
query {
  userOpsByBlock(blockNumber: "1000") {
    userOpHash
    txHash
    sender
    success
    bundler
  }
}

# 최근 UserOp (최대 100개)
query {
  recentUserOps(limit: 20) {
    userOpHash
    txHash
    blockNumber
    sender
    paymaster
    success
    actualGasCost
    bundler
    entryPoint
    timestamp
  }
}

# UserOp 총 개수
query { userOpCount }

# Bundler 통계
query {
  bundlerStats(bundler: "0x5678...") {
    address
    totalOps
    successfulOps
    failedOps
    totalGasSponsored
    lastActivityBlock
    lastActivityTime
  }
}

# Paymaster 통계
query {
  paymasterStats(paymaster: "0x9abc...") {
    address
    totalOps
    successfulOps
    failedOps
    totalGasSponsored
    lastActivityBlock
    lastActivityTime
  }
}

# Account Deployment 조회
query {
  accountDeployment(userOpHash: "0xabc...") {
    userOpHash
    sender
    factory
    paymaster
    txHash
    blockNumber
    logIndex
    timestamp
  }
}

# Factory별 배포 목록
query {
  accountDeploymentsByFactory(
    factory: "0xdef..."
    pagination: { limit: 20, offset: 0 }
  ) {
    userOpHash
    sender
    factory
    paymaster
    blockNumber
  }
}

# UserOp Revert 이유
query {
  userOpRevert(userOpHash: "0xabc...") {
    userOpHash
    sender
    nonce
    revertReason
    txHash
    blockNumber
    logIndex
    revertType    # "execution" or "postop"
    timestamp
  }
}
```

#### curl 예시

```bash
# UserOp 조회
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ userOp(userOpHash: \"0xabc...\") { userOpHash sender success bundler actualGasCost } }"}'

# 최근 UserOp
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ recentUserOps(limit: 10) { userOpHash sender success bundler blockNumber } }"}'

# Bundler 통계
curl -s http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ bundlerStats(bundler: \"0x5678...\") { totalOps successfulOps failedOps } }"}'
```

---

### EIP-7702 SetCode Queries

```graphql
# SetCode authorization 조회 (트랜잭션별)
query {
  setCodeAuthorizationsByTx(txHash: "0xabc...") {
    txHash
    address
    authority
    chainId
    nonce
    applied
    blockNumber
    authIndex
  }
}

# target 주소별 authorization
query {
  setCodeAuthorizationsByTarget(
    target: "0x1234..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes { txHash address authority applied blockNumber }
    totalCount
  }
}

# authority별 authorization
query {
  setCodeAuthorizationsByAuthority(
    authority: "0x5678..."
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes { txHash address authority applied blockNumber }
    totalCount
  }
}

# 주소 SetCode 정보 (delegation 상태)
query {
  addressSetCodeInfo(address: "0x1234...") {
    address
    hasDelegation
    delegationTarget
    asTargetCount
    asAuthorityCount
    lastActivityBlock
    lastActivityTimestamp
  }
}

# SetCode 트랜잭션 카운트
query { setCodeTransactionCount }

# 블록 내 SetCode 트랜잭션
query {
  setCodeTransactionsInBlock(blockNumber: "1000") {
    txHash
    address
    authority
    applied
  }
}

# 최근 SetCode authorization
query {
  recentSetCodeTransactions(limit: 20) {
    txHash
    address
    authority
    applied
    blockNumber
  }
}
```

---

### WBFT Consensus Queries

```graphql
# WBFT 블록 Extra Data
query {
  wbftBlockExtra(blockNumber: "1000") {
    blockNumber
    randaoReveal
    aggregatedSig
    epochNumber
  }
}

# 에폭 정보
query {
  epochInfo(epochNumber: "10") {
    epochNumber
    startBlock
    endBlock
    validators
  }
}

# 검증자 서명 통계
query {
  validatorSigningStats(
    validatorAddress: "0x1234..."
    epochNumber: "10"
  ) {
    prepareSignCount
    commitSignCount
    missCount
    participationRate
  }
}

# 블록 서명자
query {
  blockSigners(blockNumber: "1000") {
    blockNumber
    proposer
    signers
  }
}

# 에폭 목록 (페이지네이션)
query {
  epochs(pagination: { limit: 10, offset: 0 }) {
    nodes { epochNumber startBlock endBlock }
    totalCount
  }
}
```

---

### Subscriptions (GraphQL WebSocket)

```graphql
# 새 블록 구독
subscription {
  newBlock {
    hash
    height
    time
    numTxs
  }
}

# 새 트랜잭션 구독
subscription {
  newTransaction {
    hash
    blockNumber
    from
    to
    value
  }
}

# 로그 구독 (필터)
subscription {
  logs(filter: {
    address: "0x1234..."
    topics: ["0xddf252..."]
  }) {
    address
    topics
    data
    blockNumber
    transactionHash
  }
}

# 컨센서스 블록 구독
subscription {
  consensusBlock {
    blockNumber
    epochNumber
  }
}
```

#### JavaScript 연결 예시

```javascript
import { createClient } from 'graphql-ws'

const client = createClient({
  url: 'ws://localhost:8080/graphql/ws',
})

// 새 블록 구독
const unsubscribe = client.subscribe(
  {
    query: `subscription { newBlock { hash height time numTxs } }`,
  },
  {
    next: (data) => console.log('New block:', data),
    error: (err) => console.error('Error:', err),
    complete: () => console.log('Subscription completed'),
  }
)
```

---

## JSON-RPC API

JSON-RPC 2.0 프로토콜을 따릅니다. 엔드포인트: `POST /rpc`

### Request Format

```json
{
  "jsonrpc": "2.0",
  "method": "methodName",
  "params": { ... },
  "id": 1
}
```

### Core Methods

```bash
# 최신 높이
curl -s http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"getLatestHeight","params":{},"id":1}'

# 블록 조회
curl -s http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"getBlock","params":{"height":1000},"id":1}'

# 블록 조회 (해시)
curl -s http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"getBlockByHash","params":{"hash":"0xabc..."},"id":1}'

# 트랜잭션 조회
curl -s http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"getTxResult","params":{"hash":"0xabc..."},"id":1}'

# 영수증 조회
curl -s http://localhost:8080/rpc \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"getTxReceipt","params":{"hash":"0xabc..."},"id":1}'
```

### Method Reference

#### Blockchain Data
| Method | Parameters | Description |
|--------|-----------|-------------|
| `getLatestHeight` | — | 최신 인덱싱 높이 |
| `getBlock` | `height` | 블록 조회 (높이) |
| `getBlockByHash` | `hash` | 블록 조회 (해시) |
| `getTxResult` | `hash` | 트랜잭션 조회 |
| `getTxReceipt` | `hash` | 영수증 조회 |
| `getBlockCount` | — | 총 블록 수 |
| `getTransactionCount` | — | 총 트랜잭션 수 |
| `getBlocksByTimeRange` | `start, end` | 시간 범위 블록 |
| `getBlockByTimestamp` | `timestamp` | 타임스탬프로 블록 |

#### Address & Token
| Method | Parameters | Description |
|--------|-----------|-------------|
| `getAddressBalance` | `address` | 잔액 조회 |
| `getContractCreation` | `address` | 컨트랙트 생성 정보 |
| `getContractsByCreator` | `creator` | 크리에이터별 컨트랙트 |
| `getERC20Transfer` | `txHash, logIndex` | ERC-20 전송 |
| `getERC20TransfersByToken` | `token, limit, offset` | 토큰별 ERC-20 전송 |
| `getERC20TransfersByAddress` | `address, limit, offset` | 주소별 ERC-20 전송 |
| `getERC721Transfer` | `txHash, logIndex` | ERC-721 전송 |
| `getERC721TransfersByToken` | `token, limit, offset` | 토큰별 ERC-721 전송 |
| `getERC721TransfersByAddress` | `address, limit, offset` | 주소별 ERC-721 전송 |
| `getERC721Owner` | `token, tokenId` | NFT 소유자 |
| `getInternalTransactions` | `txHash` | Internal 트랜잭션 |
| `getInternalTransactionsByAddress` | `address, limit, offset` | 주소별 Internal 트랜잭션 |

#### Consensus (WBFT)
| Method | Parameters | Description |
|--------|-----------|-------------|
| `getWBFTBlockExtra` | `blockNumber` | WBFT Extra Data |
| `getWBFTBlockExtraByHash` | `blockHash` | WBFT Extra Data (해시) |
| `getEpochInfo` | `epochNumber` | 에폭 정보 |
| `getLatestEpochInfo` | — | 최신 에폭 |
| `getValidatorSigningStats` | `validator, epoch` | 검증자 서명 통계 |
| `getAllValidatorsSigningStats` | `epoch` | 전체 검증자 통계 |
| `getValidatorSigningActivity` | `validator, limit` | 서명 활동 |
| `getBlockSigners` | `blockNumber` | 블록 서명자 |

#### System Contracts
| Method | Parameters | Description |
|--------|-----------|-------------|
| `getTotalSupply` | — | 총 공급량 |
| `getActiveMinters` | — | 활성 Minter 목록 |
| `getMinterAllowance` | `address` | Minter 허용량 |
| `getActiveValidators` | — | 활성 Validator 목록 |
| `getBlacklistedAddresses` | — | 블랙리스트 주소 |
| `getProposals` | — | 거버넌스 제안 목록 |
| `getProposal` | `proposalId` | 제안 상세 |
| `getMintEvents` | `limit, offset` | Mint 이벤트 |
| `getBurnEvents` | `limit, offset` | Burn 이벤트 |

#### SetCode (EIP-7702)
| Method | Parameters | Description |
|--------|-----------|-------------|
| `getSetCodeAuthorization` | `txHash, authIndex` | SetCode authorization |
| `getSetCodeAuthorizationsByTx` | `txHash` | 트랜잭션별 authorization |
| `getSetCodeAuthorizationsByTarget` | `target, limit, offset` | target별 |
| `getSetCodeAuthorizationsByAuthority` | `authority, limit, offset` | authority별 |
| `getAddressSetCodeInfo` | `address` | 주소 SetCode 정보 |
| `getSetCodeTransactionCount` | — | SetCode 트랜잭션 수 |
| `getSetCodeTransactionsInBlock` | `blockNumber` | 블록 내 SetCode |
| `getRecentSetCodeTransactions` | `limit` | 최근 SetCode |

#### Ethereum Filter API
| Method | Parameters | Description |
|--------|-----------|-------------|
| `eth_newFilter` | `fromBlock, toBlock, address, topics` | 로그 필터 생성 |
| `eth_newBlockFilter` | — | 블록 필터 생성 |
| `eth_newPendingTransactionFilter` | — | 보류 트랜잭션 필터 |
| `eth_uninstallFilter` | `filterId` | 필터 제거 |
| `eth_getFilterChanges` | `filterId` | 필터 변경사항 |
| `eth_getFilterLogs` | `filterId` | 필터 로그 |
| `eth_getLogs` | `fromBlock, toBlock, address, topics` | 로그 조회 |

#### ABI Management
| Method | Parameters | Description |
|--------|-----------|-------------|
| `setContractABI` | `address, abi` | ABI 등록 |
| `getContractABI` | `address` | ABI 조회 |
| `deleteContractABI` | `address` | ABI 삭제 |
| `listContractABIs` | — | ABI 목록 |
| `decodeLog` | `address, topics, data` | 로그 디코딩 |

---

## WebSocket API

WebSocket 엔드포인트: `ws://localhost:8080/ws`

### 구독

```javascript
const ws = new WebSocket('ws://localhost:8080/ws')

// 새 블록 구독
ws.send(JSON.stringify({
  jsonrpc: '2.0',
  method: 'subscribe',
  params: ['newBlock'],
  id: 1
}))

// 메시지 수신
ws.onmessage = (event) => {
  const data = JSON.parse(event.data)
  console.log('Event:', data)
}
```

### 구독 타입

| Type | Description |
|------|-------------|
| `newBlock` | 새 블록 인덱싱 시 알림 |
| `newTransaction` | 새 트랜잭션 인덱싱 시 알림 |
| `logs` | 로그 이벤트 (필터 가능) |
| `consensusBlock` | WBFT 컨센서스 블록 |

---

## Go Client 연동 예시

```go
package main

import (
    "fmt"
    "math/big"

    "github.com/0xmhha/indexer-go/pkg/eventbus"
    "github.com/0xmhha/indexer-go/pkg/events"
)

func main() {
    // EventBus 생성
    bus := eventbus.NewLocalEventBus(1000, 100)
    go bus.Run()
    defer bus.Stop()

    // 블록 이벤트 구독
    blockSub := bus.Subscribe(
        "block-monitor",
        []events.EventType{events.EventTypeBlock},
        nil,
        100,
    )

    // 고가 트랜잭션 구독 (필터)
    filter := &events.Filter{
        MinValue: big.NewInt(1e18), // 1 ETH 이상
    }
    txSub := bus.Subscribe(
        "high-value-tx",
        []events.EventType{events.EventTypeTransaction},
        filter,
        100,
    )

    // 블록 이벤트 처리
    go func() {
        for event := range blockSub.Channel {
            blockEvent := event.(*events.BlockEvent)
            fmt.Printf("Block %d: %d txs\n",
                blockEvent.Number, blockEvent.TxCount)
        }
    }()

    // 트랜잭션 이벤트 처리
    go func() {
        for event := range txSub.Channel {
            txEvent := event.(*events.TransactionEvent)
            fmt.Printf("High-value TX: %s (%s)\n",
                txEvent.Hash, txEvent.Value)
        }
    }()

    select {}
}
```

---

## Health & Monitoring

```bash
# 헬스체크
curl http://localhost:8080/health

# 구독자 통계
curl http://localhost:8080/subscribers

# Prometheus 메트릭
curl http://localhost:8080/metrics

# 버전 정보
curl http://localhost:8080/version
```

# Address Indexing Design

> 주소 인덱싱 확장 설계 문서

**Created**: 2025-11-21
**Status**: Implementation

---

## 개요

주소 인덱싱을 확장하여 다음 기능을 제공:
1. 컨트랙트 생성 트랜잭션 추적
2. 내부 트랜잭션 (Internal Transactions) 추적
3. ERC20 토큰 전송 인덱싱
4. ERC721 NFT 전송 인덱싱

---

## 1. 컨트랙트 생성 트랜잭션

### 1.1 감지 방법

```go
// 컨트랙트 생성 트랜잭션 감지
if tx.To() == nil {
    // 컨트랙트 생성 트랜잭션
    receipt := getReceipt(tx.Hash())
    contractAddress := receipt.ContractAddress
    creator := tx.From()
}
```

### 1.2 데이터 구조

```go
// ContractCreation represents a contract creation event
type ContractCreation struct {
    ContractAddress  common.Address  // 생성된 컨트랙트 주소
    Creator          common.Address  // 생성자 주소
    TransactionHash  common.Hash     // 생성 트랜잭션 해시
    BlockNumber      uint64          // 블록 번호
    Timestamp        uint64          // 생성 시각
    BytecodeSize     int             // 배포된 바이트코드 크기
}
```

### 1.3 스토리지 키 스키마

```
/data/contract/creation/{contractAddress}           -> ContractCreation (JSON)
/index/contract/creator/{creatorAddress}/{blockNumber}/{txHash}  -> contractAddress
/index/contract/block/{blockNumber}/{contractAddress}            -> contractAddress
```

---

## 2. 내부 트랜잭션 (Internal Transactions)

### 2.1 개요

`debug_traceTransaction` RPC를 사용하여 트랜잭션 실행 중 발생한 내부 호출을 추적합니다.

### 2.2 추적 대상

- **CALL**: 일반 함수 호출 (value 전송 가능)
- **DELEGATECALL**: 위임 호출 (호출자 컨텍스트 유지)
- **STATICCALL**: 읽기 전용 호출
- **CREATE**: 컨트랙트 생성
- **CREATE2**: 결정적 주소 컨트랙트 생성
- **SELFDESTRUCT**: 컨트랙트 소멸

### 2.3 데이터 구조

```go
// InternalTransaction represents an internal call during transaction execution
type InternalTransaction struct {
    TransactionHash  common.Hash     // 원본 트랜잭션 해시
    BlockNumber      uint64          // 블록 번호
    Type             string          // CALL, DELEGATECALL, STATICCALL, CREATE, etc.
    From             common.Address  // 호출자 주소
    To               common.Address  // 피호출자 주소 (CREATE의 경우 생성된 주소)
    Value            *big.Int        // 전송된 ETH 양 (wei)
    Gas              uint64          // 할당된 가스
    GasUsed          uint64          // 사용된 가스
    Input            []byte          // 호출 데이터
    Output           []byte          // 반환 데이터
    Error            string          // 에러 메시지 (실패 시)
    Depth            int             // 호출 깊이 (0 = 루트)
}

// TraceResult represents the result of debug_traceTransaction
type TraceResult struct {
    Type    string          `json:"type"`
    From    string          `json:"from"`
    To      string          `json:"to"`
    Value   string          `json:"value"`
    Gas     string          `json:"gas"`
    GasUsed string          `json:"gasUsed"`
    Input   string          `json:"input"`
    Output  string          `json:"output"`
    Error   string          `json:"error,omitempty"`
    Calls   []*TraceResult  `json:"calls,omitempty"`
}
```

### 2.4 스토리지 키 스키마

```
/data/internal/{txHash}/{index}                               -> InternalTransaction (JSON)
/index/internal/from/{fromAddress}/{blockNumber}/{txHash}     -> count
/index/internal/to/{toAddress}/{blockNumber}/{txHash}         -> count
/index/internal/block/{blockNumber}/{txHash}                  -> count
```

### 2.5 RPC 호출 예시

```go
// debug_traceTransaction 호출
result, err := client.CallContext(ctx, &trace, "debug_traceTransaction", txHash, map[string]interface{}{
    "tracer": "callTracer",
})
```

---

## 3. ERC20 토큰 전송

### 3.1 Transfer 이벤트

```solidity
event Transfer(address indexed from, address indexed to, uint256 value);
```

**Topic0**: `0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef`

### 3.2 데이터 구조

```go
// ERC20Transfer represents an ERC20 token transfer
type ERC20Transfer struct {
    ContractAddress  common.Address  // 토큰 컨트랙트 주소
    From             common.Address  // 발신자 주소
    To               common.Address  // 수신자 주소
    Value            *big.Int        // 전송량
    TransactionHash  common.Hash     // 트랜잭션 해시
    BlockNumber      uint64          // 블록 번호
    LogIndex         uint           // 로그 인덱스
    Timestamp        uint64          // 시각
}
```

### 3.3 스토리지 키 스키마

```
/data/erc20/transfer/{txHash}/{logIndex}                          -> ERC20Transfer (JSON)
/index/erc20/token/{contractAddress}/{blockNumber}/{logIndex}     -> txHash
/index/erc20/from/{fromAddress}/{blockNumber}/{logIndex}          -> txHash
/index/erc20/to/{toAddress}/{blockNumber}/{logIndex}              -> txHash
```

### 3.4 감지 로직

```go
// ERC20 Transfer 이벤트 감지
const ERC20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

for _, log := range receipt.Logs {
    if len(log.Topics) >= 3 && log.Topics[0].Hex() == ERC20TransferTopic {
        // Topics[1]: from, Topics[2]: to, Data: value
        transfer := &ERC20Transfer{
            ContractAddress: log.Address,
            From:            common.BytesToAddress(log.Topics[1].Bytes()),
            To:              common.BytesToAddress(log.Topics[2].Bytes()),
            Value:           new(big.Int).SetBytes(log.Data),
            TransactionHash: log.TxHash,
            BlockNumber:     log.BlockNumber,
            LogIndex:        log.Index,
        }
    }
}
```

---

## 4. ERC721 NFT 전송

### 4.1 Transfer 이벤트

```solidity
event Transfer(address indexed from, address indexed to, uint256 indexed tokenId);
```

**Topic0**: `0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef` (ERC20과 동일)

### 4.2 데이터 구조

```go
// ERC721Transfer represents an ERC721 NFT transfer
type ERC721Transfer struct {
    ContractAddress  common.Address  // NFT 컨트랙트 주소
    From             common.Address  // 발신자 주소
    To               common.Address  // 수신자 주소
    TokenId          *big.Int        // 토큰 ID
    TransactionHash  common.Hash     // 트랜잭션 해시
    BlockNumber      uint64          // 블록 번호
    LogIndex         uint           // 로그 인덱스
    Timestamp        uint64          // 시각
}
```

### 4.3 스토리지 키 스키마

```
/data/erc721/transfer/{txHash}/{logIndex}                         -> ERC721Transfer (JSON)
/index/erc721/token/{contractAddress}/{blockNumber}/{logIndex}    -> txHash
/index/erc721/from/{fromAddress}/{blockNumber}/{logIndex}         -> txHash
/index/erc721/to/{toAddress}/{blockNumber}/{logIndex}             -> txHash
/index/erc721/tokenid/{contractAddress}/{tokenId}                 -> current owner
```

### 4.4 ERC20 vs ERC721 구분

```go
// Topics 개수로 구분
if len(log.Topics) == 4 {
    // ERC721: Transfer(indexed from, indexed to, indexed tokenId)
    // Topics[3]에 tokenId가 있음
} else if len(log.Topics) == 3 {
    // ERC20: Transfer(indexed from, indexed to, uint256 value)
    // Data에 value가 있음
}
```

---

## 5. Storage 인터페이스

### 5.1 AddressIndexReader

```go
type AddressIndexReader interface {
    // Contract Creation
    GetContractCreation(ctx context.Context, contractAddress common.Address) (*ContractCreation, error)
    GetContractsByCreator(ctx context.Context, creator common.Address, limit, offset int) ([]common.Address, error)

    // Internal Transactions
    GetInternalTransactions(ctx context.Context, txHash common.Hash) ([]*InternalTransaction, error)
    GetInternalTransactionsByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*InternalTransaction, error)

    // ERC20 Transfers
    GetERC20Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*ERC20Transfer, error)
    GetERC20TransfersByToken(ctx context.Context, tokenAddress common.Address, limit, offset int) ([]*ERC20Transfer, error)
    GetERC20TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*ERC20Transfer, error)

    // ERC721 Transfers
    GetERC721Transfer(ctx context.Context, txHash common.Hash, logIndex uint) (*ERC721Transfer, error)
    GetERC721TransfersByToken(ctx context.Context, tokenAddress common.Address, limit, offset int) ([]*ERC721Transfer, error)
    GetERC721TransfersByAddress(ctx context.Context, address common.Address, isFrom bool, limit, offset int) ([]*ERC721Transfer, error)
    GetERC721Owner(ctx context.Context, tokenAddress common.Address, tokenId *big.Int) (common.Address, error)
}
```

### 5.2 AddressIndexWriter

```go
type AddressIndexWriter interface {
    // Contract Creation
    SaveContractCreation(ctx context.Context, creation *ContractCreation) error

    // Internal Transactions
    SaveInternalTransactions(ctx context.Context, txHash common.Hash, internals []*InternalTransaction) error

    // ERC20 Transfers
    SaveERC20Transfer(ctx context.Context, transfer *ERC20Transfer) error

    // ERC721 Transfers
    SaveERC721Transfer(ctx context.Context, transfer *ERC721Transfer) error
}
```

---

## 6. Fetcher 통합

### 6.1 블록 처리 흐름

```
1. FetchBlock/FetchRangeConcurrent
   ↓
2. processAddressIndexing(block, receipts)
   ↓
   ├─ processContractCreations(block, receipts)
   ├─ processInternalTransactions(block) [optional: --trace-enabled]
   ├─ processERC20Transfers(receipts)
   └─ processERC721Transfers(receipts)
```

### 6.2 설정 옵션

```go
type FetcherConfig struct {
    // ... 기존 필드

    // Address Indexing
    EnableInternalTxTracing  bool  // 내부 트랜잭션 추적 활성화 (비용 높음)
    EnableTokenIndexing      bool  // ERC20/ERC721 토큰 인덱싱 활성화
}
```

### 6.3 에러 처리

- 주소 인덱싱 실패는 블록 인덱싱을 중단하지 않음
- 에러는 로그로 기록하고 계속 진행
- `debug_traceTransaction` 미지원 노드는 자동으로 비활성화

---

## 7. GraphQL API

### 7.1 Contract Creation

```graphql
type ContractCreation {
  contractAddress: Address!
  creator: Address!
  transactionHash: Hash!
  blockNumber: BigInt!
  timestamp: BigInt!
  bytecodeSize: Int!
}

type Query {
  contractCreation(address: Address!): ContractCreation
  contractsByCreator(creator: Address!, pagination: PaginationInput): ContractCreationConnection!
}
```

### 7.2 Internal Transactions

```graphql
type InternalTransaction {
  transactionHash: Hash!
  blockNumber: BigInt!
  type: String!
  from: Address!
  to: Address!
  value: BigInt!
  gas: BigInt!
  gasUsed: BigInt!
  input: Bytes!
  output: Bytes!
  error: String
  depth: Int!
}

type Query {
  internalTransactions(txHash: Hash!): [InternalTransaction!]!
  internalTransactionsByAddress(
    address: Address!
    isFrom: Boolean!
    pagination: PaginationInput
  ): InternalTransactionConnection!
}
```

### 7.3 ERC20 Transfers

```graphql
type ERC20Transfer {
  contractAddress: Address!
  from: Address!
  to: Address!
  value: BigInt!
  transactionHash: Hash!
  blockNumber: BigInt!
  logIndex: Int!
  timestamp: BigInt!
}

type Query {
  erc20Transfer(txHash: Hash!, logIndex: Int!): ERC20Transfer
  erc20TransfersByToken(
    token: Address!
    pagination: PaginationInput
  ): ERC20TransferConnection!
  erc20TransfersByAddress(
    address: Address!
    isFrom: Boolean!
    pagination: PaginationInput
  ): ERC20TransferConnection!
}
```

### 7.4 ERC721 Transfers

```graphql
type ERC721Transfer {
  contractAddress: Address!
  from: Address!
  to: Address!
  tokenId: BigInt!
  transactionHash: Hash!
  blockNumber: BigInt!
  logIndex: Int!
  timestamp: BigInt!
}

type Query {
  erc721Transfer(txHash: Hash!, logIndex: Int!): ERC721Transfer
  erc721TransfersByToken(
    token: Address!
    pagination: PaginationInput
  ): ERC721TransferConnection!
  erc721TransfersByAddress(
    address: Address!
    isFrom: Boolean!
    pagination: PaginationInput
  ): ERC721TransferConnection!
  erc721Owner(token: Address!, tokenId: BigInt!): Address
}
```

---

## 8. JSON-RPC API

### 8.1 Contract Creation

```javascript
// 컨트랙트 생성 정보 조회
{
  "jsonrpc": "2.0",
  "method": "getContractCreation",
  "params": { "address": "0x..." },
  "id": 1
}

// 생성자로 컨트랙트 조회
{
  "jsonrpc": "2.0",
  "method": "getContractsByCreator",
  "params": { "creator": "0x...", "limit": 10, "offset": 0 },
  "id": 2
}
```

### 8.2 Internal Transactions

```javascript
// 트랜잭션의 내부 트랜잭션 조회
{
  "jsonrpc": "2.0",
  "method": "getInternalTransactions",
  "params": { "txHash": "0x..." },
  "id": 1
}

// 주소의 내부 트랜잭션 조회
{
  "jsonrpc": "2.0",
  "method": "getInternalTransactionsByAddress",
  "params": {
    "address": "0x...",
    "isFrom": true,
    "limit": 10,
    "offset": 0
  },
  "id": 2
}
```

### 8.3 ERC20/ERC721 Transfers

```javascript
// ERC20 전송 조회
{
  "jsonrpc": "2.0",
  "method": "getERC20TransfersByToken",
  "params": { "token": "0x...", "limit": 10, "offset": 0 },
  "id": 1
}

// ERC721 소유자 조회
{
  "jsonrpc": "2.0",
  "method": "getERC721Owner",
  "params": { "token": "0x...", "tokenId": "1" },
  "id": 2
}
```

---

## 9. 성능 고려사항

### 9.1 내부 트랜잭션 추적 비용

`debug_traceTransaction`은 **매우 비용이 높은** RPC 호출입니다:
- 트랜잭션 재실행 필요
- 노드 CPU 사용량 증가
- 응답 시간: 100ms ~ 수 초

**권장사항**:
- 기본적으로 비활성화 (`--trace-enabled=false`)
- 필요한 경우에만 선택적으로 활성화
- 레이트 리미팅 적용
- 별도 워커 풀 사용

### 9.2 토큰 전송 인덱싱

- 로그 파싱은 비교적 저렴
- 대량의 토큰 전송 시 인덱스 크기 증가
- 페이지네이션 필수

### 9.3 저장소 크기 예측

1블록당 평균:
- 컨트랙트 생성: 0-5개 (100-500 bytes)
- 내부 트랜잭션: 0-100개 (10-50 KB, 활성화 시)
- ERC20 전송: 0-500개 (10-100 KB)
- ERC721 전송: 0-50개 (1-10 KB)

---

## 10. 구현 순서

1. ✅ 설계 문서 작성
2. Storage 레이어 구현
   - 데이터 구조 정의
   - 키 스키마 함수
   - Reader/Writer 인터페이스
   - PebbleDB 구현
3. Fetcher 통합
   - 컨트랙트 생성 감지
   - 토큰 전송 파싱
   - 내부 트랜잭션 추적 (optional)
4. GraphQL API 구현
5. JSON-RPC API 구현
6. 테스트 작성
7. 문서 업데이트

---

## 11. 참고 자료

- [EIP-20: Token Standard](https://eips.ethereum.org/EIPS/eip-20)
- [EIP-721: Non-Fungible Token Standard](https://eips.ethereum.org/EIPS/eip-721)
- [Geth Debug API](https://geth.ethereum.org/docs/rpc/ns-debug)
- [Contract Creation Detection](https://ethereum.stackexchange.com/questions/760/how-is-the-address-of-an-ethereum-contract-computed)

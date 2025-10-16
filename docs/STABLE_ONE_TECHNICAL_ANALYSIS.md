# Stable-One 체인 기술 분석 (2025-10-16)

## 개요

Stable-One은 **Ethereum (go-ethereum/geth) 기반**의 블록체인으로, **WBFT (WEMIX Byzantine Fault Tolerant)** 합의 알고리즘을 사용합니다. 이는 Gno 체인과 근본적으로 다른 아키텍처를 가지고 있습니다.

---

## 1. Gno vs Stable-One 핵심 차이점 요약

| 구분 | Gno (TM2) | Stable-One (WBFT) |
|------|-----------|-------------------|
| **기반 프레임워크** | Cosmos SDK / Tendermint2 | Ethereum (go-ethereum) |
| **합의 알고리즘** | Tendermint2 BFT | WBFT (Istanbul BFT + QBFT 기반) |
| **인코딩** | Amino | RLP (Recursive Length Prefix) |
| **RPC 프로토콜** | TM2 RPC | Ethereum JSON-RPC |
| **VM** | Gno VM | EVM (Ethereum Virtual Machine) |
| **트랜잭션 타입** | VM Messages (MsgCall, MsgAddPackage 등) | Ethereum Tx Types (Legacy, EIP-1559, Blob 등) |
| **주소 포맷** | Bech32 (gno1...) | Ethereum hex (0x...) |
| **해시 함수** | SHA256 | Keccak256 |
| **블록 시간** | ~5초 | 변동 (WBFT 설정에 따름) |
| **State 관리** | Merkle Tree | Merkle Patricia Trie |

---

## 2. Stable-One 아키텍처 상세

### 2.1 WBFT 합의 알고리즘

**특징**:
- Istanbul BFT와 QBFT를 기반으로 개발
- DPoS (Delegated Proof of Stake) 사용
- Validator 선출: 스테이킹 기반
- Epoch 시스템: 주기적으로 validator set 변경
- BLS 서명 aggregation 지원
- EIP-1559 가스 정책 채택

**블록 헤더 구조** (`core/types/block.go:67-98`):
```go
type Header struct {
    ParentHash  common.Hash    // 이전 블록 해시
    UncleHash   common.Hash    // Uncle 블록 해시
    Coinbase    common.Address // 채굴자(validator) 주소
    Root        common.Hash    // State root
    TxHash      common.Hash    // Transaction Merkle root
    ReceiptHash common.Hash    // Receipt Merkle root
    Bloom       Bloom          // Log Bloom filter
    Difficulty  *big.Int       // WBFT에서는 고정값 사용
    Number      *big.Int       // 블록 높이
    GasLimit    uint64         // 가스 한도
    GasUsed     uint64         // 사용된 가스
    Time        uint64         // 타임스탬프
    Extra       []byte         // WBFT 합의 정보 (validator signatures 등)
    MixDigest   common.Hash
    Nonce       BlockNonce

    // EIP 확장 필드
    BaseFee         *big.Int     // EIP-1559 base fee
    WithdrawalsHash *common.Hash // EIP-4895 withdrawals
    BlobGasUsed     *uint64      // EIP-4844 blob gas
    ExcessBlobGas   *uint64      // EIP-4844
    ParentBeaconRoot *common.Hash // EIP-4788
}
```

**WBFT 특화 해시 계산** (`core/types/block.go:115-119`):
```go
func (h *Header) Hash() common.Hash {
    if h.Difficulty != nil && h.Difficulty.Cmp(WBFTDefaultDifficulty) == 0 {
        // WBFT 특화 해시 계산 (Extra 필드에서 seal 제외)
        if istanbulHeader := WBFTFilteredHeader(h); istanbulHeader != nil {
            return rlpHash(istanbulHeader)
        }
    }
    return rlpHash(h)
}
```

### 2.2 트랜잭션 구조

**지원하는 트랜잭션 타입** (`core/types/transaction.go:47-53`):
```go
const (
    LegacyTxType                = 0x00  // 기본 Ethereum 트랜잭션
    AccessListTxType            = 0x01  // EIP-2930
    DynamicFeeTxType            = 0x02  // EIP-1559
    BlobTxType                  = 0x03  // EIP-4844
    FeeDelegateDynamicFeeTxType = 0x16  // WEMIX 특화: 수수료 대납 (22)
)
```

**Transaction 구조** (`core/types/transaction.go:56-67`):
```go
type Transaction struct {
    inner TxData    // 실제 트랜잭션 데이터
    time  time.Time // 최초 수신 시간

    // 캐시
    hash atomic.Value
    size atomic.Value
    from atomic.Value

    // WEMIX 수수료 대납 기능
    feePayer atomic.Value
}
```

**TxData 인터페이스** (`core/types/transaction.go:79-111`):
```go
type TxData interface {
    txType() byte
    copy() TxData

    chainID() *big.Int
    accessList() AccessList
    data() []byte
    gas() uint64
    gasPrice() *big.Int
    gasTipCap() *big.Int
    gasFeeCap() *big.Int
    value() *big.Int
    nonce() uint64
    to() *common.Address

    rawSignatureValues() (v, r, s *big.Int)
    setSignatureValues(chainID, v, r, s *big.Int)

    // 수수료 대납 기능
    feePayer() *common.Address
    rawFeePayerSignatureValues() (v, r, s *big.Int)

    effectiveGasPrice(dst *big.Int, baseFee *big.Int) *big.Int

    encode(*bytes.Buffer) error
    decode([]byte) error
}
```

**RLP 인코딩** (`core/types/transaction.go:114-126`):
```go
func (tx *Transaction) EncodeRLP(w io.Writer) error {
    if tx.Type() == LegacyTxType {
        return rlp.Encode(w, tx.inner)
    }
    // EIP-2718 typed TX envelope
    buf := encodeBufferPool.Get().(*bytes.Buffer)
    defer encodeBufferPool.Put(buf)
    buf.Reset()
    if err := tx.encodeTyped(buf); err != nil {
        return err
    }
    return rlp.Encode(w, buf.Bytes())
}
```

### 2.3 RPC 인터페이스

**주요 Ethereum JSON-RPC 메서드**:

#### 블록 조회
```javascript
// 최신 블록 번호
eth_blockNumber() -> hexutil.Uint64

// 블록 조회 (by number)
eth_getBlockByNumber(blockNumber, fullTx) -> *types.Block

// 블록 조회 (by hash)
eth_getBlockByHash(blockHash, fullTx) -> *types.Block

// 블록 영수증
eth_getBlockReceipts(blockNrOrHash) -> []*types.Receipt
```

#### 트랜잭션 조회
```javascript
// 트랜잭션 조회
eth_getTransactionByHash(txHash) -> *types.Transaction

// 트랜잭션 영수증
eth_getTransactionReceipt(txHash) -> *types.Receipt

// 블록 내 트랜잭션
eth_getTransactionByBlockHashAndIndex(blockHash, index) -> *types.Transaction
eth_getTransactionByBlockNumberAndIndex(blockNumber, index) -> *types.Transaction

// 블록의 트랜잭션 개수
eth_getBlockTransactionCountByHash(blockHash) -> hexutil.Uint
eth_getBlockTransactionCountByNumber(blockNumber) -> hexutil.Uint
```

#### 상태 조회
```javascript
// 잔액
eth_getBalance(address, blockNumber) -> *big.Int

// 스토리지
eth_getStorageAt(address, key, blockNumber) -> []byte

// 컨트랙트 코드
eth_getCode(address, blockNumber) -> []byte

// Nonce
eth_getTransactionCount(address, blockNumber) -> uint64
```

#### 가스 및 수수료
```javascript
// 가스 가격 예측
eth_gasPrice() -> *big.Int
eth_maxPriorityFeePerGas() -> *big.Int

// 가스 예측
eth_estimateGas(msg) -> uint64

// 수수료 히스토리
eth_feeHistory(blockCount, lastBlock, rewardPercentiles) -> *ethereum.FeeHistory
```

#### 로그 및 필터
```javascript
// 로그 필터링
eth_getLogs(filterQuery) -> []types.Log
```

#### 실시간 구독 (WebSocket)
```javascript
// 새 블록 헤더
eth_subscribe("newHeads") -> chan *types.Header

// 로그 이벤트
eth_subscribe("logs", filterQuery) -> chan types.Log
```

#### 네트워크 정보
```javascript
// Chain ID
eth_chainId() -> *big.Int

// Network ID
net_version() -> string

// 피어 수
net_peerCount() -> hexutil.Uint64
```

**ethclient 사용 예시** (`ethclient/ethclient.go:93-102`):
```go
// 블록 조회
func (ec *Client) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
    return ec.getBlock(ctx, "eth_getBlockByNumber", toBlockNumArg(number), true)
}

// 최신 블록 번호
func (ec *Client) BlockNumber(ctx context.Context) (uint64, error) {
    var result hexutil.Uint64
    err := ec.c.CallContext(ctx, &result, "eth_blockNumber")
    return uint64(result), err
}
```

### 2.4 인코딩 방식: RLP (Recursive Length Prefix)

**RLP 특징**:
- Ethereum 표준 인코딩
- 단순하고 결정적 (deterministic)
- 바이트 배열과 리스트만 인코딩
- Keccak256 해시와 함께 사용

**RLP 인코딩 규칙**:
```
1. 단일 바이트 [0x00, 0x7f]: 그 자체
2. 짧은 문자열 (0-55 바이트): 0x80 + length, data
3. 긴 문자열 (56+ 바이트): 0xb7 + length_of_length, length, data
4. 짧은 리스트: 0xc0 + length, items
5. 긴 리스트: 0xf7 + length_of_length, length, items
```

**블록 RLP 인코딩** (`core/types/block.go:335-342`):
```go
func (b *Block) EncodeRLP(w io.Writer) error {
    return rlp.Encode(w, &extblock{
        Header:      b.header,
        Txs:         b.transactions,
        Uncles:      b.uncles,
        Withdrawals: b.withdrawals,
    })
}
```

---

## 3. tx-indexer 구현 시 핵심 변경사항

### 3.1 Client Layer 변경

**TM2 Client → ethclient 교체**:

```go
// 기존 (Gno)
import (
    rpcClient "github.com/gnolang/gno/tm2/pkg/bft/rpc/client"
    core_types "github.com/gnolang/gno/tm2/pkg/bft/rpc/core/types"
)

// 변경 (Stable-One)
import (
    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/rpc"
)
```

**Client 구현**:

```go
// client/ethereum_client.go
package client

import (
    "context"
    "math/big"

    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/common"
)

type EthereumClient struct {
    client *ethclient.Client
}

func NewEthereumClient(endpoint string) (*EthereumClient, error) {
    client, err := ethclient.Dial(endpoint)
    if err != nil {
        return nil, err
    }
    return &EthereumClient{client: client}, nil
}

func (c *EthereumClient) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
    return c.client.BlockNumber(ctx)
}

func (c *EthereumClient) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
    return c.client.BlockByNumber(ctx, big.NewInt(int64(height)))
}

func (c *EthereumClient) GetBlockReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
    blockNrOrHash := rpc.BlockNumberOrHashWithHash(blockHash, false)
    return c.client.BlockReceipts(ctx, blockNrOrHash)
}

func (c *EthereumClient) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
    return c.client.SubscribeNewHead(ctx, ch)
}
```

**배치 요청 (있는 경우)**:
```go
func (c *EthereumClient) GetBlocksBatch(ctx context.Context, from, to uint64) ([]*types.Block, error) {
    batch := make([]rpc.BatchElem, to-from+1)
    blocks := make([]*types.Block, to-from+1)

    for i := from; i <= to; i++ {
        batch[i-from] = rpc.BatchElem{
            Method: "eth_getBlockByNumber",
            Args:   []interface{}{hexutil.EncodeUint64(i), true},
            Result: &blocks[i-from],
        }
    }

    if err := c.client.Client().BatchCallContext(ctx, batch); err != nil {
        return nil, err
    }

    return blocks, nil
}
```

### 3.2 Storage Encoding 변경

**Amino → RLP**:

```go
// storage/encode_ethereum.go
package storage

import (
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/rlp"
)

func encodeBlock(block *types.Block) ([]byte, error) {
    return rlp.EncodeToBytes(block)
}

func decodeBlock(data []byte) (*types.Block, error) {
    var block types.Block
    if err := rlp.DecodeBytes(data, &block); err != nil {
        return nil, err
    }
    return &block, nil
}

func encodeTransaction(tx *types.Transaction) ([]byte, error) {
    return tx.MarshalBinary()
}

func decodeTransaction(data []byte) (*types.Transaction, error) {
    var tx types.Transaction
    if err := tx.UnmarshalBinary(data); err != nil {
        return nil, err
    }
    return &tx, nil
}

func encodeReceipt(receipt *types.Receipt) ([]byte, error) {
    return rlp.EncodeToBytes(receipt)
}

func decodeReceipt(data []byte) (*types.Receipt, error) {
    var receipt types.Receipt
    if err := rlp.DecodeBytes(data, &receipt); err != nil {
        return nil, err
    }
    return &receipt, nil
}
```

### 3.3 Storage Schema 확장

**Receipt 저장소 추가**:

```go
// storage/pebble.go
const (
    keyLatestHeight     = "/meta/lh"
    prefixKeyBlocks     = "/data/blocks/"
    prefixKeyTxs        = "/data/txs/"
    prefixKeyReceipts   = "/data/receipts/"  // 추가
    prefixKeyTxByHash   = "/index/txh/"
    prefixKeyTxByAddr   = "/index/addr/"     // 추가 (address → tx mapping)
)

func (p *PebbleStorage) WriteReceipt(height uint64, txIndex uint, receipt *types.Receipt) error {
    key := fmt.Sprintf("%s%d/%d", prefixKeyReceipts, height, txIndex)
    data, err := encodeReceipt(receipt)
    if err != nil {
        return err
    }
    return p.db.Set([]byte(key), data, pebble.Sync)
}

func (p *PebbleStorage) GetReceipt(height uint64, txIndex uint) (*types.Receipt, error) {
    key := fmt.Sprintf("%s%d/%d", prefixKeyReceipts, height, txIndex)
    data, closer, err := p.db.Get([]byte(key))
    if err != nil {
        return nil, err
    }
    defer closer.Close()

    return decodeReceipt(data)
}
```

### 3.4 Fetcher Logic 변경

**Genesis 처리 수정**:

```go
// fetch/fetch_ethereum.go
func (f *Fetcher) fetchGenesisData(ctx context.Context) error {
    // Ethereum genesis는 블록 0으로 조회
    genesisBlock, err := f.client.GetBlock(ctx, 0)
    if err != nil {
        return fmt.Errorf("unable to fetch genesis: %w", err)
    }

    // Genesis 블록 저장
    if err := f.storage.WriteBlock(genesisBlock); err != nil {
        return err
    }

    // Genesis 트랜잭션 저장 (있다면)
    for i, tx := range genesisBlock.Transactions() {
        txResult := &TxResult{
            Height: 0,
            Index:  uint32(i),
            Tx:     tx,
            Hash:   tx.Hash().Hex(),
        }

        if err := f.storage.WriteTxResult(txResult); err != nil {
            return err
        }
    }

    return nil
}
```

**워커 로직 수정 (Receipt 추가)**:

```go
func handleChunk(ctx context.Context, client *EthereumClient, info *workerInfo) {
    for height := info.from; height <= info.to; height++ {
        // 1. 블록 조회
        block, err := client.GetBlock(ctx, height)
        if err != nil {
            info.errCh <- err
            return
        }

        // 2. Receipt 조회
        receipts, err := client.GetBlockReceipts(ctx, block.Hash())
        if err != nil {
            info.errCh <- err
            return
        }

        // 3. 응답 전송
        info.resCh <- &workerResponse{
            block:    block,
            receipts: receipts,
        }
    }
}
```

### 3.5 GraphQL Schema 수정

**Block 타입**:

```graphql
# serve/graph/schema/types/block.graphql
type Block {
    # 기본 필드
    hash: String!
    height: Int!
    time: Time!

    # Ethereum 특화
    parent_hash: String!
    state_root: String!
    transactions_root: String!
    receipts_root: String!
    miner: String!              # Coinbase/Validator
    difficulty: String!
    total_difficulty: String
    size: Int!
    gas_limit: Int!
    gas_used: Int!
    base_fee_per_gas: String    # EIP-1559

    # Blob 관련 (EIP-4844)
    blob_gas_used: Int
    excess_blob_gas: Int

    # 트랜잭션
    txs: [Transaction!]!
    num_txs: Int!

    # Uncle blocks (있다면)
    uncles: [String!]!
}
```

**Transaction 타입**:

```graphql
# serve/graph/schema/types/transaction.graphql
type Transaction {
    # 기본 정보
    hash: String!
    block_hash: String!
    block_height: Int!
    index: Int!

    # Ethereum 특화
    type: Int!                  # 0=Legacy, 1=AccessList, 2=DynamicFee, 3=Blob, 22=FeeDelegation
    from: String!
    to: String
    nonce: Int!
    value: String!

    # 가스
    gas: Int!
    gas_price: String
    gas_tip_cap: String         # EIP-1559
    gas_fee_cap: String         # EIP-1559

    # 데이터
    input: String!              # Contract call data

    # 서명
    v: String!
    r: String!
    s: String!

    # Access List (EIP-2930)
    access_list: [AccessTuple!]

    # Blob (EIP-4844)
    blob_hashes: [String!]
    blob_gas_fee_cap: String

    # WEMIX 수수료 대납
    fee_payer: String
    fee_payer_signatures: Signature

    # 실행 결과 (Receipt에서)
    status: Int!                # 0=fail, 1=success
    gas_used: Int!
    cumulative_gas_used: Int!
    logs: [Log!]!
    contract_address: String    # Contract creation인 경우
}
```

**Log 타입**:

```graphql
type Log {
    address: String!
    topics: [String!]!
    data: String!
    block_height: Int!
    tx_hash: String!
    tx_index: Int!
    log_index: Int!
    removed: Boolean!
}
```

**AccessTuple 타입 (EIP-2930)**:

```graphql
type AccessTuple {
    address: String!
    storage_keys: [String!]!
}
```

### 3.6 JSON-RPC Handler 수정

**getBlock 메서드**:

```go
// serve/jsonrpc/handlers.go
func (j *JSONRPC) getBlock(params json.RawMessage) (interface{}, error) {
    var blockNum uint64
    if err := json.Unmarshal(params, &blockNum); err != nil {
        return nil, err
    }

    block, err := j.storage.GetBlock(blockNum)
    if errors.Is(err, storageErrors.ErrNotFound) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }

    // RLP 인코딩 → Hex
    encoded, err := rlp.EncodeToBytes(block)
    if err != nil {
        return nil, err
    }

    return hexutil.Encode(encoded), nil
}
```

**getTxReceipt 메서드 추가**:

```go
func (j *JSONRPC) getTxReceipt(params json.RawMessage) (interface{}, error) {
    var txHash string
    if err := json.Unmarshal(params, &txHash); err != nil {
        return nil, err
    }

    // 트랜잭션 조회 (height, index 얻기)
    tx, err := j.storage.GetTxByHash(txHash)
    if errors.Is(err, storageErrors.ErrNotFound) {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }

    // Receipt 조회
    receipt, err := j.storage.GetReceipt(tx.Height, tx.Index)
    if err != nil {
        return nil, err
    }

    // RLP 인코딩 → Hex
    encoded, err := rlp.EncodeToBytes(receipt)
    if err != nil {
        return nil, err
    }

    return hexutil.Encode(encoded), nil
}
```

---

## 4. 구현 우선순위 및 로드맵

### Phase 1: 기본 인덱싱 (2주)
- [x] Stable-One 체인 분석 완료
- [ ] ethclient 기반 Client Layer 구현
- [ ] RLP 인코딩 Storage Layer
- [ ] 기본 Fetcher (단일 블록)
- [ ] PebbleDB Schema (block, tx, receipt)

### Phase 2: 성능 최적화 (2주)
- [ ] Worker pool 인덱싱
- [ ] 배치 요청 (가능한 경우)
- [ ] 병렬 Receipt 조회
- [ ] Gap 감지 및 재시도

### Phase 3: API 서버 (2주)
- [ ] GraphQL Schema (Block, Transaction, Log)
- [ ] GraphQL Resolvers
- [ ] JSON-RPC endpoints
- [ ] WebSocket 실시간 구독

### Phase 4: 고급 기능 (2주)
- [ ] Address indexing (from/to/contract)
- [ ] Event log filtering
- [ ] Contract ABI 디코딩 (선택)
- [ ] Rate limiting 및 캐싱

---

## 5. 성능 고려사항

### 5.1 Receipt 조회 최적화

**문제**: Ethereum은 블록당 Receipt를 별도 RPC 호출로 조회해야 함

**해결 방안**:
1. **eth_getBlockReceipts** 사용 (단일 호출로 전체 receipt)
2. 배치 요청 사용
3. Receipt 캐싱

```go
// 최적화된 Receipt 조회
func (c *EthereumClient) GetBlockWithReceipts(ctx context.Context, height uint64) (*BlockWithReceipts, error) {
    // 병렬 요청
    var (
        block    *types.Block
        receipts types.Receipts
        errBlock error
        errReceipts error
    )

    var wg sync.WaitGroup
    wg.Add(2)

    // 블록 조회
    go func() {
        defer wg.Done()
        block, errBlock = c.GetBlock(ctx, height)
    }()

    // Receipt 조회 (블록 해시 미리 계산 불가능하므로 순차적으로)
    go func() {
        defer wg.Done()
        // 블록 조회 완료 대기
        wg.Wait()
        if errBlock == nil {
            receipts, errReceipts = c.GetBlockReceipts(ctx, block.Hash())
        }
    }()

    wg.Wait()

    if errBlock != nil {
        return nil, errBlock
    }
    if errReceipts != nil {
        return nil, errReceipts
    }

    return &BlockWithReceipts{
        Block:    block,
        Receipts: receipts,
    }, nil
}
```

### 5.2 인덱싱 속도

**예상 성능**:
- Gno (Amino): ~100-200 블록/초
- Stable-One (RLP): ~80-150 블록/초 (Receipt 조회 포함)

**병목 요소**:
1. 네트워크 레이턴시 (RPC 호출)
2. Receipt 별도 조회
3. RLP 디코딩 (Amino보다 빠름)

---

## 6. 참고 코드 위치

### Stable-One 체인 코드
- 블록 구조: `stable-one/core/types/block.go`
- 트랜잭션 구조: `stable-one/core/types/transaction.go`
- RPC 클라이언트: `stable-one/rpc/client.go`
- ethclient: `stable-one/ethclient/ethclient.go`

### 주요 패키지
```go
import (
    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/common/hexutil"
    "github.com/ethereum/go-ethereum/rlp"
    "github.com/ethereum/go-ethereum/rpc"
)
```

---

## 7. 추가 고려사항

### 7.1 WBFT 특화 필드

WBFT consensus는 `Extra` 필드에 추가 정보를 저장합니다:
- Validator signatures (BLS aggregated)
- Round number
- Committed seals

이 정보를 파싱하려면 stable-one의 WBFT 관련 코드 참조 필요.

### 7.2 Fee Delegation

Stable-One은 수수료 대납 기능을 지원합니다 (`FeeDelegateDynamicFeeTxType = 0x16`):
- `feePayer` 주소
- Fee payer 서명 (별도)

GraphQL 스키마에 이 필드 반영 필요.

### 7.3 EIP 호환성

Stable-One이 지원하는 EIP:
- EIP-1559: Dynamic fee (baseFee, priorityFee)
- EIP-2930: Access lists
- EIP-4844: Blob transactions
- EIP-4895: Withdrawals
- EIP-4788: Beacon root

각 EIP에 따른 필드를 GraphQL 스키마에 추가.

---

## 8. 다음 단계

1. ✅ Stable-One 체인 분석 완료
2. ⏳ ethclient 기반 Client 구현
3. ⏳ RLP Storage Layer 구현
4. ⏳ GraphQL Schema 설계
5. ⏳ 기본 Fetcher 구현

---

**문서 버전**: 1.0
**최종 업데이트**: 2025-10-16
**작성자**: Claude (SuperClaude)

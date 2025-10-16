# indexer-go 구현 플랜

> Stable-One 체인용 트랜잭션 인덱서 구현 계획서

**프로젝트**: indexer-go
**목적**: Stable-One (Ethereum 기반) 블록체인 데이터 인덱싱 및 GraphQL/JSON-RPC API 제공
**기반**: tx-indexer (Gno 체인) 아키텍처
**작성일**: 2025-10-16

---

## 📋 목차

1. [프로젝트 개요](#1-프로젝트-개요)
2. [기술 스택](#2-기술-스택)
3. [프로젝트 구조](#3-프로젝트-구조)
4. [Phase별 구현 계획](#4-phase별-구현-계획)
5. [핵심 컴포넌트 상세 설계](#5-핵심-컴포넌트-상세-설계)
6. [마일스톤 및 일정](#6-마일스톤-및-일정)
7. [성능 목표](#7-성능-목표)
8. [테스트 전략](#8-테스트-전략)

---

## 1. 프로젝트 개요

### 1.1 목표

Stable-One 체인의 블록 및 트랜잭션 데이터를 실시간으로 인덱싱하고, 효율적인 쿼리를 위한 GraphQL 및 JSON-RPC API를 제공하는 고성능 인덱서 구축.

### 1.2 핵심 기능

- ✅ **Ethereum JSON-RPC 기반 데이터 수집**
- ✅ **Receipt 포함 완전한 트랜잭션 데이터**
- ✅ **병렬 인덱싱** (Worker pool 기반)
- ✅ **GraphQL API** (필터링, 페이지네이션)
- ✅ **JSON-RPC 2.0 API** (표준 호환)
- ✅ **WebSocket 실시간 구독**
- ✅ **PebbleDB 임베디드 스토리지**
- ✅ **EIP-1559, EIP-4844 등 최신 EIP 지원**
- ✅ **Fee Delegation (WEMIX 특화 기능)**

### 1.3 tx-indexer와의 차이점

| 구분 | tx-indexer (Gno) | indexer-go (Stable-One) |
|------|------------------|-------------------------|
| 체인 | Gno (Tendermint2) | Stable-One (Ethereum) |
| RPC | TM2 RPC | Ethereum JSON-RPC |
| Client | gnolang/gno RPC client | go-ethereum/ethclient |
| 인코딩 | Amino | RLP |
| 트랜잭션 | VM Messages | Ethereum Tx Types |
| Receipt | 없음 | 필수 (별도 조회) |
| 주소 | Bech32 | Hex (0x...) |

---

## 2. 기술 스택

### 2.1 코어 라이브러리

| 카테고리 | 기술 | 버전 | 용도 |
|---------|------|------|------|
| 언어 | Go | 1.21+ | 주 언어 |
| Ethereum | go-ethereum | v1.13+ | ethclient, types, RLP |
| 데이터베이스 | PebbleDB | latest | 임베디드 LSM-tree DB |
| GraphQL | gqlgen | v0.17+ | GraphQL 서버 |
| HTTP | chi | v5 | HTTP 라우터 |
| WebSocket | gorilla/websocket | v1.5+ | 실시간 구독 |
| 로깅 | zap | v1.26+ | 구조화된 로깅 |

### 2.2 주요 패키지

```go
// Ethereum 클라이언트
"github.com/ethereum/go-ethereum/ethclient"
"github.com/ethereum/go-ethereum/core/types"
"github.com/ethereum/go-ethereum/common"
"github.com/ethereum/go-ethereum/common/hexutil"
"github.com/ethereum/go-ethereum/rlp"
"github.com/ethereum/go-ethereum/rpc"

// 데이터베이스
"github.com/cockroachdb/pebble"

// GraphQL
"github.com/99designs/gqlgen/graphql"
"github.com/99designs/gqlgen/graphql/handler"

// HTTP
"github.com/go-chi/chi/v5"
"github.com/gorilla/websocket"

// 유틸리티
"go.uber.org/zap"
"golang.org/x/sync/errgroup"
```

---

## 3. 프로젝트 구조

```
indexer-go/
├── cmd/                           # 엔트리포인트
│   ├── main.go                    # 메인 함수
│   ├── start.go                   # start 커맨드
│   └── config.go                  # 설정 관리
│
├── client/                        # Ethereum RPC 클라이언트
│   ├── ethereum_client.go         # ethclient 래퍼
│   ├── batch.go                   # 배치 요청
│   └── types.go                   # 클라이언트 타입
│
├── fetch/                         # 블록체인 데이터 페처
│   ├── fetcher.go                 # 메인 페처 로직
│   ├── worker.go                  # 워커 구현
│   ├── chunk_buffer.go            # 청크 관리
│   └── genesis.go                 # Genesis 처리
│
├── storage/                       # 데이터 저장소
│   ├── pebble.go                  # PebbleDB 구현
│   ├── encode.go                  # RLP 인코딩/디코딩
│   ├── schema.go                  # DB 스키마
│   ├── block.go                   # 블록 저장/조회
│   ├── transaction.go             # 트랜잭션 저장/조회
│   ├── receipt.go                 # Receipt 저장/조회
│   └── index.go                   # 인덱스 (주소, 해시)
│
├── events/                        # 이벤트 관리
│   ├── manager.go                 # 구독 관리자
│   ├── subscription.go            # 구독 로직
│   └── types.go                   # 이벤트 타입
│
├── serve/                         # API 서버
│   ├── server.go                  # HTTP 서버
│   ├── jsonrpc/                   # JSON-RPC API
│   │   ├── handler.go             # RPC 핸들러
│   │   ├── methods.go             # RPC 메서드
│   │   └── websocket.go           # WebSocket 지원
│   ├── graph/                     # GraphQL API
│   │   ├── schema/                # GraphQL 스키마
│   │   │   ├── schema.graphql     # 메인 스키마
│   │   │   └── types/             # 타입 정의
│   │   │       ├── block.graphql
│   │   │       ├── transaction.graphql
│   │   │       └── log.graphql
│   │   ├── resolver.go            # 리졸버
│   │   ├── model/                 # 생성된 모델
│   │   └── generated.go           # gqlgen 생성 코드
│   └── health/                    # 헬스체크
│       └── handler.go
│
├── types/                         # 공통 타입
│   ├── block.go                   # 블록 타입
│   ├── transaction.go             # 트랜잭션 타입
│   └── filter.go                  # 필터 타입
│
├── internal/                      # 내부 패키지
│   ├── utils/                     # 유틸리티
│   └── config/                    # 설정
│
├── docs/                          # 문서
│   ├── IMPLEMENTATION_PLAN.md     # 이 문서
│   ├── STABLE_ONE_TECHNICAL_ANALYSIS.md
│   └── API_REFERENCE.md           # API 문서
│
├── scripts/                       # 스크립트
│   ├── generate.sh                # gqlgen 생성
│   └── test.sh                    # 테스트 스크립트
│
├── go.mod                         # Go 모듈
├── go.sum
├── Makefile                       # 빌드 스크립트
├── README.md                      # 프로젝트 README
└── .gitignore
```

---

## 4. Phase별 구현 계획

### Phase 1: 기본 인덱싱 (2주, Sprint 1-2)

**목표**: Stable-One 체인에서 블록 및 트랜잭션 데이터를 수집하고 저장

#### Sprint 1 (Week 1)
- [ ] **프로젝트 초기화**
  - Go 모듈 초기화 (`go mod init`)
  - 디렉토리 구조 생성
  - 기본 의존성 설치
  - Makefile 작성

- [ ] **Client Layer**
  - `client/ethereum_client.go` 구현
    - `NewEthereumClient(endpoint string)` - 클라이언트 초기화
    - `GetLatestBlockNumber(ctx)` - 최신 블록 번호
    - `GetBlock(ctx, height)` - 블록 조회
    - `GetBlockReceipts(ctx, blockHash)` - Receipt 조회
  - 에러 처리 및 재시도 로직
  - 단위 테스트 작성

- [ ] **Storage Layer - 기본**
  - `storage/pebble.go` 구현
    - PebbleDB 초기화 및 닫기
    - 기본 CRUD 인터페이스
  - `storage/encode.go` 구현
    - `encodeBlock()` - RLP 블록 인코딩
    - `decodeBlock()` - RLP 블록 디코딩
    - `encodeTransaction()` - 트랜잭션 인코딩
    - `decodeTransaction()` - 트랜잭션 디코딩
  - `storage/schema.go` 구현
    - 키 스키마 정의
    - 인덱스 구조 정의

#### Sprint 2 (Week 2)
- [ ] **Storage Layer - 확장**
  - `storage/block.go` 구현
    - `WriteBlock(block)` - 블록 저장
    - `GetBlock(height)` - 블록 조회
    - `GetLatestHeight()` - 최신 높이
  - `storage/transaction.go` 구현
    - `WriteTxResult(txResult)` - 트랜잭션 저장
    - `GetTxByHash(hash)` - 해시로 트랜잭션 조회
  - `storage/receipt.go` 구현
    - `WriteReceipt(height, index, receipt)` - Receipt 저장
    - `GetReceipt(height, index)` - Receipt 조회

- [ ] **Fetcher - 기본**
  - `fetch/fetcher.go` 구현
    - `New()` - Fetcher 초기화
    - `fetchGenesisData(ctx)` - Genesis 블록 처리
    - `fetchSingleBlock(ctx, height)` - 단일 블록 fetch
  - Genesis 블록 처리 (블록 0)
  - 에러 처리 및 로깅

- [ ] **통합 테스트**
  - 로컬 Stable-One 노드 또는 테스트넷 연결
  - Genesis 블록 인덱싱 검증
  - 단일 블록 인덱싱 검증
  - 데이터 무결성 검증

**완료 기준**:
- ✅ Genesis 블록을 성공적으로 인덱싱
- ✅ 단일 블록 및 트랜잭션을 저장하고 조회 가능
- ✅ Receipt를 포함한 완전한 데이터 저장
- ✅ 단위 테스트 커버리지 >70%

---

### Phase 2: 성능 최적화 (2주, Sprint 3-4)

**목표**: Worker pool을 통한 병렬 인덱싱 및 배치 처리

#### Sprint 3 (Week 3)
- [ ] **Worker Pool 구현**
  - `fetch/worker.go` 구현
    - `workerInfo` 구조체 정의
    - `handleChunk()` - 청크 처리 워커
    - Worker 에러 처리
  - `fetch/chunk_buffer.go` 구현
    - `ChunkBuffer` - 청크 관리
    - `reserveChunkRanges()` - 청크 예약
    - `releaseChunk()` - 청크 해제
  - 동시성 제어 (최대 100 워커)

- [ ] **Fetcher - 병렬화**
  - `FetchChainData(ctx)` 구현
    - Worker pool 관리
    - 청크 단위 처리 (100 블록/청크)
    - 응답 수집 및 순서 정렬
  - Gap 감지 및 재시도 로직
  - Progress 추적 (로깅)

#### Sprint 4 (Week 4)
- [ ] **배치 최적화**
  - `client/batch.go` 구현
    - `GetBlocksBatch(ctx, from, to)` - 배치 블록 조회
    - `GetReceiptsBatch(ctx, blockHashes)` - 배치 Receipt 조회
  - 배치 크기 최적화 (실험적)
  - 에러 처리 및 부분 실패 대응

- [ ] **Receipt 조회 최적화**
  - `eth_getBlockReceipts` 사용 (단일 호출)
  - Receipt 캐싱 (선택사항)
  - 병렬 Receipt 조회

- [ ] **인덱스 추가**
  - `storage/index.go` 구현
    - 트랜잭션 해시 → (height, index) 매핑
    - Address → 트랜잭션 목록 매핑 (선택)
  - 인덱스 빌드 및 업데이트

- [ ] **성능 벤치마크**
  - 인덱싱 속도 측정
  - 메모리 사용량 프로파일링
  - 병목 지점 분석 및 최적화

**완료 기준**:
- ✅ 100개 워커로 병렬 인덱싱 동작
- ✅ 인덱싱 속도 >80 블록/초
- ✅ Gap 감지 및 자동 재시도
- ✅ Receipt 조회 최적화 완료
- ✅ 메모리 사용량 <1GB (10만 블록 기준)

---

### Phase 3: API 서버 (2주, Sprint 5-6)

**목표**: GraphQL 및 JSON-RPC API 구현

#### Sprint 5 (Week 5)
- [ ] **GraphQL Schema 정의**
  - `serve/graph/schema/schema.graphql` 작성
  - `serve/graph/schema/types/block.graphql` 작성
    - Block 타입 (Ethereum 특화 필드)
    - Header 정보, Gas 정보
  - `serve/graph/schema/types/transaction.graphql` 작성
    - Transaction 타입 (EIP별 필드)
    - Receipt 정보, Log 정보
  - `serve/graph/schema/types/log.graphql` 작성
    - Log 타입
    - Event 필터

- [ ] **GraphQL 코드 생성**
  - gqlgen 설정 (`gqlgen.yml`)
  - `go generate` 실행
  - 생성된 코드 검증

- [ ] **GraphQL Resolvers - Query**
  - `serve/graph/resolver.go` 구현
  - Block 리졸버
    - `block(height)` - 높이로 블록 조회
    - `blocks(filter)` - 필터링된 블록 목록
  - Transaction 리졸버
    - `transaction(hash)` - 해시로 트랜잭션 조회
    - `transactions(filter)` - 필터링된 트랜잭션 목록
  - 필터 및 페이지네이션

#### Sprint 6 (Week 6)
- [ ] **GraphQL Resolvers - Subscription**
  - `newBlock` - 새 블록 구독
  - `newTransaction` - 새 트랜잭션 구독
  - WebSocket 연결 관리

- [ ] **JSON-RPC API**
  - `serve/jsonrpc/handler.go` 구현
  - `serve/jsonrpc/methods.go` 구현
    - `getBlock(params)` - 블록 조회
    - `getTxResult(params)` - 트랜잭션 조회
    - `getTxReceipt(params)` - Receipt 조회
    - `getLatestHeight(params)` - 최신 높이
  - `serve/jsonrpc/websocket.go` 구현
    - `subscribe(eventType)` - 이벤트 구독
    - `unsubscribe(id)` - 구독 취소

- [ ] **HTTP 서버 구성**
  - `serve/server.go` 구현
    - chi 라우터 설정
    - GraphQL 엔드포인트 (`/graphql`)
    - JSON-RPC 엔드포인트 (`/rpc`)
    - WebSocket 엔드포인트 (`/ws`)
  - Rate limiting
  - CORS 설정
  - Health check (`/health`)

**완료 기준**:
- ✅ GraphQL API 동작 (Query, Subscription)
- ✅ JSON-RPC 2.0 호환
- ✅ WebSocket 실시간 구독 동작
- ✅ API 문서 작성
- ✅ Postman/Insomnia 테스트 컬렉션

---

### Phase 4: 고급 기능 (2주, Sprint 7-8)

**목표**: 이벤트 관리, 고급 인덱싱, 프로덕션 준비

#### Sprint 7 (Week 7)
- [ ] **Event Manager**
  - `events/manager.go` 구현
    - `Subscribe(eventTypes)` - 이벤트 구독
    - `SignalEvent(event)` - 이벤트 발생
    - `CancelSubscription(id)` - 구독 취소
  - `events/subscription.go` 구현
    - 구독 루프
    - 이벤트 큐 관리
  - Event 타입 정의
    - `BlockAdded`
    - `TransactionIndexed`

- [ ] **Address 인덱싱**
  - From/To 주소 인덱스
  - Contract 주소 인덱스
  - Address → Transaction 매핑
  - GraphQL 쿼리 지원
    - `transactionsByAddress(address)`

- [ ] **Log 필터링**
  - Event log 저장
  - Topic 인덱싱
  - GraphQL 쿼리 지원
    - `logs(filter)` - address, topics 필터

#### Sprint 8 (Week 8)
- [ ] **프로덕션 준비**
  - 설정 파일 지원 (YAML/JSON)
  - 환경 변수 지원
  - 로깅 레벨 설정
  - 메트릭 수집 (Prometheus, 선택)
  - Graceful shutdown

- [ ] **모니터링 및 관찰성**
  - 헬스체크 강화
    - DB 연결 상태
    - Fetcher 상태
    - 최근 블록 시간
  - 메트릭 엔드포인트
    - 인덱싱 속도
    - API 요청 수
    - 에러 발생률

- [ ] **문서화**
  - README.md 작성
  - API_REFERENCE.md 작성
  - 설치 가이드
  - 배포 가이드
  - 트러블슈팅 가이드

- [ ] **최종 테스트**
  - 통합 테스트 (전체 플로우)
  - 부하 테스트
  - 장애 시나리오 테스트
  - 성능 벤치마크

**완료 기준**:
- ✅ Event 구독 시스템 동작
- ✅ Address 인덱싱 및 쿼리
- ✅ Log 필터링 지원
- ✅ 프로덕션 배포 가능
- ✅ 전체 문서화 완료

---

## 5. 핵심 컴포넌트 상세 설계

### 5.1 Client Layer

**파일**: `client/ethereum_client.go`

```go
package client

import (
    "context"
    "math/big"

    "github.com/ethereum/go-ethereum/ethclient"
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/common"
    "github.com/ethereum/go-ethereum/rpc"
)

type EthereumClient struct {
    client *ethclient.Client
    rpc    *rpc.Client
}

func NewEthereumClient(endpoint string) (*EthereumClient, error) {
    client, err := ethclient.Dial(endpoint)
    if err != nil {
        return nil, err
    }

    rpcClient, _ := rpc.Dial(endpoint)

    return &EthereumClient{
        client: client,
        rpc:    rpcClient,
    }, nil
}

// 최신 블록 번호
func (c *EthereumClient) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
    return c.client.BlockNumber(ctx)
}

// 블록 조회
func (c *EthereumClient) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
    return c.client.BlockByNumber(ctx, big.NewInt(int64(height)))
}

// Receipt 조회 (단일 호출)
func (c *EthereumClient) GetBlockReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
    var receipts types.Receipts
    err := c.rpc.CallContext(ctx, &receipts, "eth_getBlockReceipts", blockHash)
    return receipts, err
}

// 실시간 구독
func (c *EthereumClient) SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error) {
    return c.client.SubscribeNewHead(ctx, ch)
}

// Close 클라이언트
func (c *EthereumClient) Close() {
    c.client.Close()
    if c.rpc != nil {
        c.rpc.Close()
    }
}
```

**배치 요청**: `client/batch.go`

```go
package client

import (
    "context"
    "math/big"

    "github.com/ethereum/go-ethereum/common/hexutil"
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/rpc"
)

// 배치 블록 조회
func (c *EthereumClient) GetBlocksBatch(ctx context.Context, from, to uint64) ([]*types.Block, error) {
    count := to - from + 1
    batch := make([]rpc.BatchElem, count)
    blocks := make([]*types.Block, count)

    for i := uint64(0); i < count; i++ {
        height := from + i
        batch[i] = rpc.BatchElem{
            Method: "eth_getBlockByNumber",
            Args:   []interface{}{hexutil.EncodeUint64(height), true},
            Result: &blocks[i],
        }
    }

    if err := c.rpc.BatchCallContext(ctx, batch); err != nil {
        return nil, err
    }

    // 에러 체크
    for i, elem := range batch {
        if elem.Error != nil {
            return nil, elem.Error
        }
    }

    return blocks, nil
}
```

---

### 5.2 Storage Layer

**스키마**: `storage/schema.go`

```go
package storage

const (
    // 메타데이터
    keyLatestHeight = "/meta/lh"

    // 블록 데이터
    prefixKeyBlocks = "/data/blocks/"     // /data/blocks/{height}

    // 트랜잭션 데이터
    prefixKeyTxs = "/data/txs/"           // /data/txs/{height}/{index}

    // Receipt 데이터
    prefixKeyReceipts = "/data/receipts/" // /data/receipts/{height}/{index}

    // 인덱스
    prefixKeyTxByHash = "/index/txh/"     // /index/txh/{hash} -> {height}/{index}
    prefixKeyTxByAddr = "/index/addr/"    // /index/addr/{address}/{height}/{index}
)

func blockKey(height uint64) string {
    return fmt.Sprintf("%s%d", prefixKeyBlocks, height)
}

func txKey(height uint64, index uint) string {
    return fmt.Sprintf("%s%d/%d", prefixKeyTxs, height, index)
}

func receiptKey(height uint64, index uint) string {
    return fmt.Sprintf("%s%d/%d", prefixKeyReceipts, height, index)
}

func txHashIndexKey(hash string) string {
    return fmt.Sprintf("%s%s", prefixKeyTxByHash, hash)
}

func addressIndexKey(address string, height uint64, index uint) string {
    return fmt.Sprintf("%s%s/%d/%d", prefixKeyTxByAddr, address, height, index)
}
```

**인코딩**: `storage/encode.go`

```go
package storage

import (
    "github.com/ethereum/go-ethereum/core/types"
    "github.com/ethereum/go-ethereum/rlp"
)

// 블록 인코딩
func encodeBlock(block *types.Block) ([]byte, error) {
    return rlp.EncodeToBytes(block)
}

// 블록 디코딩
func decodeBlock(data []byte) (*types.Block, error) {
    var block types.Block
    if err := rlp.DecodeBytes(data, &block); err != nil {
        return nil, err
    }
    return &block, nil
}

// 트랜잭션 인코딩
func encodeTransaction(tx *types.Transaction) ([]byte, error) {
    return tx.MarshalBinary()
}

// 트랜잭션 디코딩
func decodeTransaction(data []byte) (*types.Transaction, error) {
    var tx types.Transaction
    if err := tx.UnmarshalBinary(data); err != nil {
        return nil, err
    }
    return &tx, nil
}

// Receipt 인코딩
func encodeReceipt(receipt *types.Receipt) ([]byte, error) {
    return rlp.EncodeToBytes(receipt)
}

// Receipt 디코딩
func decodeReceipt(data []byte) (*types.Receipt, error) {
    var receipt types.Receipt
    if err := rlp.DecodeBytes(data, &receipt); err != nil {
        return nil, err
    }
    return &receipt, nil
}
```

---

### 5.3 Fetcher Layer

**메인 페처**: `fetch/fetcher.go`

```go
package fetch

import (
    "context"
    "fmt"
    "sync"

    "go.uber.org/zap"
)

const (
    DefaultMaxSlots     = 100  // 최대 워커 수
    DefaultMaxChunkSize = 100  // 청크 크기
)

type Fetcher struct {
    client      *client.EthereumClient
    storage     storage.Storage
    eventMgr    *events.Manager
    logger      *zap.Logger

    maxSlots     int
    maxChunkSize int

    chunkBuffer  *ChunkBuffer
}

func New(
    client *client.EthereumClient,
    storage storage.Storage,
    eventMgr *events.Manager,
    logger *zap.Logger,
    opts ...Option,
) *Fetcher {
    f := &Fetcher{
        client:       client,
        storage:      storage,
        eventMgr:     eventMgr,
        logger:       logger,
        maxSlots:     DefaultMaxSlots,
        maxChunkSize: DefaultMaxChunkSize,
    }

    for _, opt := range opts {
        opt(f)
    }

    f.chunkBuffer = NewChunkBuffer(f.maxChunkSize)

    return f
}

// Genesis 블록 처리
func (f *Fetcher) fetchGenesisData(ctx context.Context) error {
    f.logger.Info("fetching genesis block")

    genesisBlock, err := f.client.GetBlock(ctx, 0)
    if err != nil {
        return fmt.Errorf("unable to fetch genesis: %w", err)
    }

    // 블록 저장
    if err := f.storage.WriteBlock(genesisBlock); err != nil {
        return err
    }

    // Genesis 트랜잭션 저장
    for i, tx := range genesisBlock.Transactions() {
        if err := f.storage.WriteTx(0, uint(i), tx); err != nil {
            return err
        }
    }

    f.logger.Info("genesis block indexed", zap.Uint64("height", 0))
    return nil
}

// 체인 데이터 인덱싱
func (f *Fetcher) FetchChainData(ctx context.Context) error {
    // Genesis 처리
    if err := f.fetchGenesisData(ctx); err != nil {
        return err
    }

    // Worker pool
    collectorCh := make(chan *workerResponse, f.maxSlots)
    defer close(collectorCh)

    // Collector 고루틴 (순서대로 저장)
    go f.runCollector(ctx, collectorCh)

    // 메인 루프
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // 범위 fetch 시도
        if err := f.attemptRangeFetch(ctx, collectorCh); err != nil {
            return err
        }
    }
}

// 범위 fetch
func (f *Fetcher) attemptRangeFetch(ctx context.Context, collectorCh chan<- *workerResponse) error {
    latestLocal, _ := f.storage.GetLatestHeight()
    latestRemote, err := f.client.GetLatestBlockNumber(ctx)
    if err != nil {
        return err
    }

    // 동기화 완료
    if latestLocal >= latestRemote {
        time.Sleep(1 * time.Second)
        return nil
    }

    // 청크 예약
    gaps := f.chunkBuffer.reserveChunkRanges(latestLocal+1, latestRemote, f.maxSlots)

    // Worker 실행
    var wg sync.WaitGroup
    for _, gap := range gaps {
        wg.Add(1)
        go func(from, to uint64) {
            defer wg.Done()
            handleChunk(ctx, f.client, from, to, collectorCh)
        }(gap.start, gap.end)
    }

    wg.Wait()
    return nil
}
```

**워커**: `fetch/worker.go`

```go
package fetch

import (
    "context"

    "github.com/ethereum/go-ethereum/core/types"
)

type workerResponse struct {
    block    *types.Block
    receipts types.Receipts
    err      error
}

func handleChunk(
    ctx context.Context,
    client *client.EthereumClient,
    from, to uint64,
    resCh chan<- *workerResponse,
) {
    for height := from; height <= to; height++ {
        // 블록 조회
        block, err := client.GetBlock(ctx, height)
        if err != nil {
            resCh <- &workerResponse{err: err}
            return
        }

        // Receipt 조회
        receipts, err := client.GetBlockReceipts(ctx, block.Hash())
        if err != nil {
            resCh <- &workerResponse{err: err}
            return
        }

        // 응답 전송
        resCh <- &workerResponse{
            block:    block,
            receipts: receipts,
        }
    }
}
```

---

### 5.4 GraphQL Schema

**Block 타입**: `serve/graph/schema/types/block.graphql`

```graphql
# Ethereum 블록
type Block {
    # 기본 정보
    hash: String!
    height: Int!
    time: Time!

    # Header 정보
    parent_hash: String!
    state_root: String!
    transactions_root: String!
    receipts_root: String!

    # Validator
    miner: String!

    # 난이도
    difficulty: String!
    total_difficulty: String

    # 크기
    size: Int!

    # 가스
    gas_limit: Int!
    gas_used: Int!
    base_fee_per_gas: String  # EIP-1559

    # Blob (EIP-4844)
    blob_gas_used: Int
    excess_blob_gas: Int

    # 트랜잭션
    num_txs: Int!
    txs: [Transaction!]!

    # Uncle blocks
    uncles: [String!]!
}
```

**Transaction 타입**: `serve/graph/schema/types/transaction.graphql`

```graphql
# Ethereum 트랜잭션
type Transaction {
    # 기본 정보
    hash: String!
    block_hash: String!
    block_height: Int!
    index: Int!

    # 트랜잭션 타입
    type: Int!  # 0=Legacy, 1=AccessList, 2=DynamicFee, 3=Blob, 22=FeeDelegation

    # 주소
    from: String!
    to: String

    # 값
    nonce: Int!
    value: String!

    # 가스
    gas: Int!
    gas_price: String
    gas_tip_cap: String     # EIP-1559
    gas_fee_cap: String     # EIP-1559

    # 데이터
    input: String!

    # 서명
    v: String!
    r: String!
    s: String!

    # Access List (EIP-2930)
    access_list: [AccessTuple!]

    # Blob (EIP-4844)
    blob_hashes: [String!]
    blob_gas_fee_cap: String

    # Fee Delegation (WEMIX)
    fee_payer: String

    # 실행 결과
    status: Int!            # 0=fail, 1=success
    gas_used: Int!
    cumulative_gas_used: Int!
    logs: [Log!]!
    contract_address: String
}

# Access tuple for EIP-2930
type AccessTuple {
    address: String!
    storage_keys: [String!]!
}
```

**Log 타입**: `serve/graph/schema/types/log.graphql`

```graphql
# Event log
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

**Query 및 Subscription**: `serve/graph/schema/schema.graphql`

```graphql
scalar Time

type Query {
    # 블록 조회
    block(height: Int!): Block
    blocks(filter: BlockFilter): [Block!]!

    # 트랜잭션 조회
    transaction(hash: String!): Transaction
    transactions(filter: TransactionFilter): [Transaction!]!

    # 트랜잭션 by Address
    transactionsByAddress(address: String!, filter: TransactionFilter): [Transaction!]!

    # 로그 조회
    logs(filter: LogFilter): [Log!]!

    # 메타
    latestHeight: Int!
}

type Subscription {
    # 새 블록
    newBlock: Block!

    # 새 트랜잭션
    newTransaction: Transaction!
}

# 필터
input BlockFilter {
    height_min: Int
    height_max: Int
    miner: String
}

input TransactionFilter {
    block_height_min: Int
    block_height_max: Int
    from: String
    to: String
    type: Int
}

input LogFilter {
    block_height_min: Int
    block_height_max: Int
    address: String
    topics: [[String!]!]
}
```

---

## 6. 마일스톤 및 일정

### 전체 타임라인 (8주)

```
Week 1-2:  Phase 1 - 기본 인덱싱
           Sprint 1: Client + Storage 기본
           Sprint 2: Fetcher 기본 + 통합 테스트

Week 3-4:  Phase 2 - 성능 최적화
           Sprint 3: Worker Pool
           Sprint 4: 배치 + 인덱스 + 벤치마크

Week 5-6:  Phase 3 - API 서버
           Sprint 5: GraphQL Schema + Query
           Sprint 6: Subscription + JSON-RPC

Week 7-8:  Phase 4 - 고급 기능
           Sprint 7: Event Manager + Address 인덱스
           Sprint 8: 프로덕션 준비 + 문서화
```

### 주요 마일스톤

| 마일스톤 | 완료일 | 완료 기준 |
|---------|--------|----------|
| **M1: 기본 인덱싱** | Week 2 | Genesis + 단일 블록 인덱싱 동작 |
| **M2: 병렬 인덱싱** | Week 4 | 100 워커, >80 블록/초 |
| **M3: API 서버** | Week 6 | GraphQL + JSON-RPC 동작 |
| **M4: 프로덕션 준비** | Week 8 | 전체 기능 + 문서 완료 |

---

## 7. 성능 목표

### 7.1 인덱싱 성능

| 메트릭 | 목표 | 최소 요구사항 |
|-------|------|--------------|
| 초기 동기화 속도 | 80-150 블록/초 | 50 블록/초 |
| 실시간 추적 지연 | <2초 | <5초 |
| Worker 수 | 100 | 50 |
| Chunk 크기 | 100 블록 | 50 블록 |

### 7.2 API 성능

| 메트릭 | 목표 | 최소 요구사항 |
|-------|------|--------------|
| GraphQL 쿼리 응답 | <100ms | <300ms |
| JSON-RPC 응답 | <50ms | <150ms |
| WebSocket 이벤트 전파 | <20ms | <50ms |
| 동시 연결 수 | 1000+ | 500 |

### 7.3 리소스 사용

| 메트릭 | 목표 | 최대 허용 |
|-------|------|----------|
| 메모리 사용 (베이스) | 500MB | 1GB |
| 메모리 사용 (100 워커) | 2GB | 4GB |
| 디스크 사용 | ~2GB/100만 블록 | ~5GB/100만 블록 |
| CPU 사용 | 200% (2 코어) | 400% (4 코어) |

---

## 8. 테스트 전략

### 8.1 단위 테스트

- **Coverage 목표**: >70%
- **도구**: Go testing package, testify
- **범위**:
  - Client 레이어 (mock RPC)
  - Storage 레이어 (in-memory DB)
  - 인코딩/디코딩 함수
  - 유틸리티 함수

### 8.2 통합 테스트

- **테스트넷 연결**: Stable-One testnet
- **시나리오**:
  - Genesis 블록 인덱싱
  - 연속 블록 인덱싱 (1-1000)
  - Gap 처리 (중간 블록 누락)
  - 재시작 후 복구

### 8.3 성능 테스트

- **벤치마크**:
  - 인덱싱 속도 측정
  - 메모리 프로파일링
  - CPU 프로파일링
- **부하 테스트**:
  - API 동시 요청 (100-1000 RPS)
  - WebSocket 연결 수 (100-1000)

### 8.4 E2E 테스트

- **시나리오**:
  - 전체 인덱싱 플로우
  - GraphQL 쿼리 → Storage → 응답
  - WebSocket 구독 → 새 블록 → 알림
  - 장애 복구 (노드 다운, DB 에러)

---

## 9. 위험 요소 및 대응

### 9.1 기술적 위험

| 위험 | 영향 | 확률 | 대응 방안 |
|------|------|------|----------|
| Stable-One RPC 불안정 | 높음 | 중간 | 재시도 로직, 에러 처리 강화 |
| Receipt 조회 느림 | 높음 | 높음 | 배치 요청, 병렬화, 캐싱 |
| 메모리 부족 | 중간 | 중간 | 청크 크기 조정, Worker 수 제한 |
| PebbleDB 성능 | 중간 | 낮음 | Write buffer 튜닝, Compaction 설정 |

### 9.2 일정 위험

| 위험 | 영향 | 확률 | 대응 방안 |
|------|------|------|----------|
| Phase 1 지연 | 높음 | 중간 | Buffer 시간 확보 (주말 작업) |
| Receipt 최적화 실패 | 중간 | 낮음 | 성능 목표 하향 조정 |
| GraphQL 복잡도 | 중간 | 중간 | 필수 기능 우선, 추가 기능은 Phase 4 |

---

## 10. 다음 단계

### 즉시 시작

1. ✅ 프로젝트 초기화
   ```bash
   cd /Users/wm-it-22-00661/workspace/indexer/indexer-go
   go mod init github.com/your-org/indexer-go
   ```

2. ✅ 디렉토리 구조 생성
   ```bash
   mkdir -p cmd client fetch storage events serve/jsonrpc serve/graph/schema/types types internal scripts
   ```

3. ✅ 의존성 설치
   ```bash
   go get github.com/ethereum/go-ethereum
   go get github.com/cockroachdb/pebble
   go get github.com/99designs/gqlgen
   go get github.com/go-chi/chi/v5
   go get go.uber.org/zap
   ```

4. ⏳ Sprint 1 시작
   - Client Layer 구현
   - Storage Layer 기본 구현

### 참고 문서

- 📄 [STABLE_ONE_TECHNICAL_ANALYSIS.md](./STABLE_ONE_TECHNICAL_ANALYSIS.md) - Stable-One 체인 기술 분석
- 📄 [TX_INDEXER_ANALYSIS.md](../TX_INDEXER_ANALYSIS.md) - Gno tx-indexer 분석

---

**문서 버전**: 1.0
**최종 업데이트**: 2025-10-16
**작성자**: Claude (SuperClaude)
**상태**: 승인 대기

---

## 부록 A: 체크리스트

### Phase 1 체크리스트
- [ ] Go 모듈 초기화
- [ ] 디렉토리 구조 생성
- [ ] Client Layer 구현
- [ ] Storage Layer 구현
- [ ] Fetcher 기본 구현
- [ ] Genesis 블록 인덱싱
- [ ] 단위 테스트 (>70%)

### Phase 2 체크리스트
- [ ] Worker Pool 구현
- [ ] 병렬 인덱싱 동작
- [ ] 배치 요청 구현
- [ ] Receipt 조회 최적화
- [ ] 인덱스 구현
- [ ] 성능 벤치마크

### Phase 3 체크리스트
- [ ] GraphQL Schema 정의
- [ ] GraphQL Resolvers 구현
- [ ] JSON-RPC API 구현
- [ ] WebSocket 구독
- [ ] HTTP 서버 구성
- [ ] API 문서

### Phase 4 체크리스트
- [ ] Event Manager 구현
- [ ] Address 인덱싱
- [ ] Log 필터링
- [ ] 프로덕션 설정
- [ ] 모니터링
- [ ] 전체 문서화

---

## 부록 B: 명령어 레퍼런스

### 개발
```bash
# 빌드
make build

# 테스트
make test

# GraphQL 코드 생성
make generate

# 실행
./indexer-go start --remote http://localhost:8545 --db-path ./data
```

### 배포
```bash
# Docker 빌드
docker build -t indexer-go:latest .

# Docker 실행
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/data:/data \
  indexer-go:latest \
  start --remote http://stable-one-node:8545 --db-path /data
```

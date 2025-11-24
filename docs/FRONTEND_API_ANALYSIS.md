# Frontend API Requirements Analysis

> indexer-go 백엔드의 현재 구현 상태와 프론트엔드 요구사항 비교 분석

**분석일**: 2025-11-24
**분석 대상**: Frontend API Requirements v1.0
**상태**: 검토 완료

---

## 📊 요약

| API | 우선순위 | 현재 상태 | 구현 정도 | 조치 필요 |
|-----|---------|-----------|-----------|----------|
| Search API | 🔴 높음 | ❌ 미구현 | 0% | ✅ 신규 개발 필요 |
| Top Miners API | 🟡 중간 | ✅ 부분 구현 | 60% | 🔧 필드 추가 필요 |
| Token Balance API | 🟡 중간 | ✅ 부분 구현 | 50% | 🔧 필드 추가 필요 |
| Contract Verification API | 🟢 낮음 | ❌ 미구현 | 0% | ⏳ 향후 개발 |

---

## 1. Search API (🔴 우선순위: 높음)

### 현재 상태
**❌ 미구현 (0%)**

현재 GraphQL schema에 `search` 쿼리가 없음. 개별 조회 API만 존재:
- `block(number: BigInt!)`: 블록 번호로 조회
- `blockByHash(hash: Hash!)`: 블록 해시로 조회
- `transaction(hash: Hash!)`: 트랜잭션 해시로 조회
- `transactionsByAddress(address: Address!)`: 주소로 트랜잭션 조회

### 요구사항
```graphql
type SearchResult {
  type: String!           # "block", "transaction", "address", "contract"
  value: String!
  label: String
  metadata: String        # JSON string
}

type Query {
  search(
    query: String!
    types: [String!]
    limit: Int = 10
  ): [SearchResult!]!
}
```

### 구현 계획
**예상 소요**: 1-2주

#### Phase 1: Storage Layer (5일)
1. **검색 인덱스 추가**
   - 블록 번호 → 블록 해시 매핑
   - 트랜잭션 해시 → 트랜잭션 데이터
   - 주소 → 타입(EOA/Contract) 매핑

2. **SearchResult 타입 정의** (`storage/search.go`)
```go
type SearchResult struct {
    Type     string // "block", "transaction", "address", "contract"
    Value    string
    Label    string
    Metadata map[string]interface{}
}
```

3. **Search 인터페이스 추가** (`storage/storage.go`)
```go
type SearchReader interface {
    Search(ctx context.Context, query string, types []string, limit int) ([]SearchResult, error)
}
```

4. **PebbleStorage 구현** (`storage/pebble.go`)
```go
func (s *PebbleStorage) Search(ctx context.Context, query string, types []string, limit int) ([]SearchResult, error) {
    // 1. 쿼리 타입 감지 (블록 번호, 해시, 주소)
    // 2. 타입별 검색 실행
    // 3. 결과 통합 및 정렬
    // 4. 메타데이터 구성
}
```

#### Phase 2: GraphQL Layer (3일)
1. **Schema 타입 추가** (`api/graphql/types.go`)
```go
var searchResultType = graphql.NewObject(graphql.ObjectConfig{
    Name: "SearchResult",
    Fields: graphql.Fields{
        "type":     &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
        "value":    &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
        "label":    &graphql.Field{Type: graphql.String},
        "metadata": &graphql.Field{Type: graphql.String},
    },
})
```

2. **Resolver 구현** (`api/graphql/resolvers_search.go`)
```go
func (s *Schema) resolveSearch(p graphql.ResolveParams) (interface{}, error) {
    query := p.Args["query"].(string)
    types := p.Args["types"].([]string)
    limit := p.Args["limit"].(int)

    // SearchReader 타입 캐스팅
    // Search 실행
    // 결과 반환
}
```

3. **Schema 등록** (`api/graphql/schema.go`)
```go
"search": &graphql.Field{
    Type: graphql.NewNonNull(graphql.NewList(searchResultType)),
    Args: graphql.FieldConfigArgument{
        "query": &graphql.ArgumentConfig{
            Type: graphql.NewNonNull(graphql.String),
        },
        "types": &graphql.ArgumentConfig{
            Type: graphql.NewList(graphql.String),
        },
        "limit": &graphql.ArgumentConfig{
            Type:         graphql.Int,
            DefaultValue: 10,
        },
    },
    Resolve: s.resolveSearch,
}
```

#### Phase 3: 성능 최적화 (2일)
1. **인덱싱 전략**
   - 블록 번호: B-tree 인덱스 (O(log n) 조회)
   - 해시: 해시 테이블 (O(1) 조회)
   - 주소: Prefix tree (부분 일치 지원)

2. **캐싱**
   - 최근 검색 결과 캐싱 (LRU, 1000개)
   - 메타데이터 캐싱 (블록/트랜잭션 요약 정보)

3. **응답 시간 목표**
   - 완전 일치: < 100ms
   - 부분 일치: < 500ms

#### 테스트 계획
1. **단위 테스트** (`storage/search_test.go`)
   - 블록 번호 검색
   - 블록 해시 검색
   - 트랜잭션 해시 검색
   - 주소 검색
   - 타입 필터링
   - Limit 동작

2. **통합 테스트** (`api/graphql/search_test.go`)
   - GraphQL 쿼리 테스트
   - 에러 케이스 처리

---

## 2. Top Miners API (🟡 우선순위: 중간)

### 현재 상태
**✅ 부분 구현 (60%)**

#### 구현된 부분
- ✅ GraphQL schema: `topMiners(limit: Int): [MinerStats!]!`
- ✅ Resolver: `resolveTopMiners` (api/graphql/resolvers_historical.go:359)
- ✅ Storage 구현: `GetTopMiners` (storage/pebble.go:1239)

#### 현재 타입 정의
```graphql
type MinerStats {
  address: Address!       # ✅ 구현됨
  blockCount: BigInt!     # ✅ 구현됨
  lastBlockNumber: BigInt! # ✅ 구현됨
}
```

### 요구사항 (누락된 필드)
```graphql
type MinerStats {
  address: Address!
  blockCount: Int!        # BigInt → Int로 변경 필요
  lastBlockNumber: BigInt!
  lastBlockTime: String!  # ❌ 누락
  percentage: Float!      # ❌ 누락
  totalRewards: BigInt    # ❌ 누락 (optional)
}

type TopMinersResult {   # ❌ 전체 타입 누락
  miners: [MinerStats!]!
  totalBlocks: Int!
  timeRange: String!
}
```

### 구현 계획
**예상 소요**: 3일

#### 1. Storage Layer 수정 (1일)
`storage/historical.go`:
```go
type MinerStats struct {
    Address          common.Address
    BlockCount       uint64
    LastBlockNumber  uint64
    LastBlockTime    uint64  // ← 추가
    Percentage       float64 // ← 추가
    TotalRewards     *big.Int // ← 추가 (optional)
}

type TopMinersResult struct { // ← 새 타입
    Miners      []MinerStats
    TotalBlocks uint64
    TimeRange   string
}

// 인터페이스 수정
GetTopMiners(ctx context.Context, limit int, timeRange string) (*TopMinersResult, error)
```

`storage/pebble.go` - 구현 수정:
```go
func (s *PebbleStorage) GetTopMiners(ctx context.Context, limit int, timeRange string) (*TopMinersResult, error) {
    // 1. timeRange 파싱 (24h, 7d, 30d, all)
    // 2. 블록 범위 계산
    // 3. 채굴자별 집계
    // 4. Percentage 계산
    // 5. TotalRewards 계산 (optional)
}
```

#### 2. GraphQL Schema 수정 (1일)
`api/graphql/types.go`:
```go
var minerStatsType = graphql.NewObject(graphql.ObjectConfig{
    Name: "MinerStats",
    Fields: graphql.Fields{
        "address":          &graphql.Field{Type: graphql.NewNonNull(addressType)},
        "blockCount":       &graphql.Field{Type: graphql.NewNonNull(graphql.Int)},
        "lastBlockNumber":  &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
        "lastBlockTime":    &graphql.Field{Type: graphql.NewNonNull(graphql.String)}, // ← 추가
        "percentage":       &graphql.Field{Type: graphql.NewNonNull(graphql.Float)}, // ← 추가
        "totalRewards":     &graphql.Field{Type: bigIntType}, // ← 추가 (nullable)
    },
})

var topMinersResultType = graphql.NewObject(graphql.ObjectConfig{ // ← 새 타입
    Name: "TopMinersResult",
    Fields: graphql.Fields{
        "miners":      &graphql.Field{Type: graphql.NewList(minerStatsType)},
        "totalBlocks": &graphql.Field{Type: graphql.Int},
        "timeRange":   &graphql.Field{Type: graphql.String},
    },
})
```

`api/graphql/schema.go`:
```go
"topMiners": &graphql.Field{
    Type: graphql.NewNonNull(topMinersResultType), // ← 반환 타입 변경
    Args: graphql.FieldConfigArgument{
        "limit": &graphql.ArgumentConfig{
            Type:         graphql.Int,
            DefaultValue: 10,
        },
        "timeRange": &graphql.ArgumentConfig{ // ← 새 인자
            Type:         graphql.String,
            DefaultValue: "all",
        },
    },
    Resolve: s.resolveTopMiners,
}
```

#### 3. Resolver 수정 (1일)
`api/graphql/resolvers_historical.go`:
```go
func (s *Schema) resolveTopMiners(p graphql.ResolveParams) (interface{}, error) {
    limit := 10
    if l, ok := p.Args["limit"].(int); ok {
        limit = l
    }

    timeRange := "all" // ← 새 파라미터
    if tr, ok := p.Args["timeRange"].(string); ok {
        timeRange = tr
    }

    result, err := histStorage.GetTopMiners(ctx, limit, timeRange)
    // lastBlockTime을 ISO 8601 포맷으로 변환
    // percentage 계산
    // 결과 반환
}
```

---

## 3. Token Balance API (🟡 우선순위: 중간)

### 현재 상태
**✅ 부분 구현 (50%)**

#### 구현된 부분
- ✅ GraphQL schema: `tokenBalances(address: Address!): [TokenBalance!]!`
- ✅ Resolver: `resolveTokenBalances` (api/graphql/resolvers_historical.go:399)
- ✅ Storage 구현: `GetTokenBalances` (storage/pebble.go:1307)
- ✅ ERC20/ERC721 Transfer 인덱싱 (Address Indexing 기능)

#### 현재 타입 정의
```graphql
type TokenBalance {
  contractAddress: Address! # ✅ 구현됨
  tokenType: String!        # ✅ 구현됨
  balance: BigInt!          # ✅ 구현됨
  tokenId: BigInt           # ✅ 구현됨
}
```

### 요구사항 (누락된 필드)
```graphql
type TokenBalance {
  contractAddress: Address!
  tokenType: String!
  balance: BigInt!
  name: String              # ❌ 누락
  symbol: String            # ❌ 누락
  decimals: Int             # ❌ 누락
  tokenId: String           # BigInt → String으로 변경
  metadata: String          # ❌ 누락 (NFT 메타데이터)
}

type Query {
  tokenBalances(
    address: Address!
    tokenType: String       # ❌ 필터 파라미터 누락
  ): [TokenBalance!]!
}
```

### 구현 계획
**예상 소요**: 1주

#### 1. Storage Layer 수정 (3일)
`storage/historical.go`:
```go
type TokenBalance struct {
    ContractAddress common.Address
    TokenType       string // "ERC20", "ERC721", "ERC1155"
    Balance         *big.Int
    Name            string   // ← 추가
    Symbol          string   // ← 추가
    Decimals        *int     // ← 추가 (ERC20만, nil for NFT)
    TokenID         string   // ← BigInt에서 String으로 변경
    Metadata        string   // ← 추가 (JSON string)
}

// 인터페이스 수정
GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]TokenBalance, error)
```

`storage/pebble.go` - 구현 수정:
```go
func (s *PebbleStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]TokenBalance, error) {
    // 1. Transfer 이벤트로 잔액 계산 (기존 로직)
    // 2. 각 토큰 컨트랙트에서 메타데이터 조회:
    //    - ERC20: name(), symbol(), decimals() 호출
    //    - ERC721: name(), symbol(), tokenURI(tokenId) 호출
    //    - ERC1155: uri(tokenId) 호출
    // 3. tokenType 필터링
    // 4. 결과 반환
}
```

**토큰 메타데이터 조회 방법**:
- ERC20 컨트랙트 호출 (read-only)
- ABI 사용: `name()`, `symbol()`, `decimals()`
- 결과 캐싱 (컨트랙트별)

#### 2. 메타데이터 캐싱 전략 (1일)
```go
// storage/token_metadata.go
type TokenMetadataCache struct {
    cache map[common.Address]*TokenMetadata
    mu    sync.RWMutex
}

type TokenMetadata struct {
    Name     string
    Symbol   string
    Decimals *int
    UpdatedAt time.Time
}

func (c *TokenMetadataCache) Get(address common.Address) *TokenMetadata
func (c *TokenMetadataCache) Set(address common.Address, metadata *TokenMetadata)
```

#### 3. GraphQL Schema 수정 (2일)
`api/graphql/types.go`:
```go
var tokenBalanceType = graphql.NewObject(graphql.ObjectConfig{
    Name: "TokenBalance",
    Fields: graphql.Fields{
        "contractAddress": &graphql.Field{Type: graphql.NewNonNull(addressType)},
        "tokenType":       &graphql.Field{Type: graphql.NewNonNull(graphql.String)},
        "balance":         &graphql.Field{Type: graphql.NewNonNull(bigIntType)},
        "name":            &graphql.Field{Type: graphql.String}, // ← 추가
        "symbol":          &graphql.Field{Type: graphql.String}, // ← 추가
        "decimals":        &graphql.Field{Type: graphql.Int},    // ← 추가
        "tokenId":         &graphql.Field{Type: graphql.String}, // ← 타입 변경
        "metadata":        &graphql.Field{Type: graphql.String}, // ← 추가
    },
})
```

`api/graphql/schema.go`:
```go
"tokenBalances": &graphql.Field{
    Type: graphql.NewNonNull(graphql.NewList(tokenBalanceType)),
    Args: graphql.FieldConfigArgument{
        "address": &graphql.ArgumentConfig{
            Type: graphql.NewNonNull(addressType),
        },
        "tokenType": &graphql.ArgumentConfig{ // ← 새 인자
            Type: graphql.String,
        },
    },
    Resolve: s.resolveTokenBalances,
}
```

#### 4. Resolver 수정 (1일)
`api/graphql/resolvers_historical.go`:
```go
func (s *Schema) resolveTokenBalances(p graphql.ResolveParams) (interface{}, error) {
    addr := common.HexToAddress(p.Args["address"].(string))

    tokenType := "" // ← 새 파라미터
    if tt, ok := p.Args["tokenType"].(string); ok {
        tokenType = tt
    }

    balances, err := histStorage.GetTokenBalances(ctx, addr, tokenType)
    // 결과 반환
}
```

---

## 4. Contract Verification API (🟢 우선순위: 낮음)

### 현재 상태
**❌ 미구현 (0%)**

이 API는 완전히 새로운 기능이며, 다음을 포함합니다:
- 소스 코드 저장 DB
- Solidity 컴파일러 통합
- 바이트코드 비교 로직
- 보안 고려사항

### 구현 제안
**예상 소요**: 2-3주

이 기능은 우선순위가 낮고 복잡도가 높으므로, 다음 단계에서 진행하는 것을 권장합니다:
1. Phase 1, 2, 3 (Search, Top Miners, Token Balance) 완료 후
2. 프론트엔드 팀과 상세 요구사항 재논의
3. 별도 프로젝트로 분리 고려 (verification service)

---

## 📋 구현 우선순위 및 일정

### Phase 1: 핵심 검색 기능 (2주)
- **Week 1-2**: Search API 구현
  - Storage layer (5일)
  - GraphQL layer (3일)
  - 성능 최적화 및 테스트 (2일)

### Phase 2: 기존 API 개선 (1주)
- **Week 3**: Top Miners API 개선 (3일)
- **Week 3-4**: Token Balance API 개선 (4일)

### Phase 3: 테스트 및 최적화 (1주)
- **Week 4**: 통합 테스트
- **Week 4**: 성능 테스트 및 튜닝
- **Week 4**: 문서 작성

**총 예상 기간**: 4주

---

## 🔧 기술 스택 (현재 사용 중)

### Backend
- **언어**: Go 1.21+
- **GraphQL**: github.com/graphql-go/graphql
- **Storage**: PebbleDB (github.com/cockroachdb/pebble)
- **Blockchain**: go-ethereum (geth)

### 성능 목표 (현재)
- API 응답 시간: < 1초 (대부분 < 500ms)
- 동시 요청 처리: 지원 (goroutine 기반)
- 캐싱: 부분적으로 구현됨

---

## ✅ 액션 아이템

### 즉시 시작 가능
1. [x] 현재 구현 상태 분석 완료
2. [ ] Search API 개발 시작
   - [ ] Storage layer 설계
   - [ ] 인덱싱 전략 수립
   - [ ] Resolver 구현

### Phase 2 준비
3. [ ] Top Miners API 개선
   - [ ] timeRange 파라미터 추가
   - [ ] percentage 계산 로직
   - [ ] lastBlockTime 필드 추가

4. [ ] Token Balance API 개선
   - [ ] 토큰 메타데이터 조회 로직
   - [ ] 캐싱 전략 구현
   - [ ] tokenType 필터 추가

### 향후 고려
5. [ ] Contract Verification API
   - [ ] 상세 요구사항 수집
   - [ ] 보안 검토
   - [ ] 별도 서비스 분리 검토

---

## 📞 협업 가이드

### 프론트엔드 팀 커뮤니케이션
1. **API 변경 알림**: 이 문서 업데이트 + GitHub Issue
2. **Staging 환경**: 개발 완료 시 배포 알림
3. **GraphQL Playground**: `/graphql` 엔드포인트에서 테스트 가능

### 개발 브랜치 전략
- `main`: Production
- `develop`: Staging
- `feature/search-api`: Search API 개발
- `feature/enhance-miners-api`: Top Miners 개선
- `feature/enhance-token-api`: Token Balance 개선

---

**문서 버전**: 1.0
**최종 수정**: 2025-11-24
**다음 업데이트**: Search API 개발 시작 시

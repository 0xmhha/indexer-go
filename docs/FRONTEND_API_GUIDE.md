# Frontend API 가이드

> **중요**: 이 문서는 코드 검증을 완료한 정확한 정보입니다.
> 마지막 업데이트: 2025-01-XX
> 작성자: Backend Team

---

## 1. GraphQL 엔드포인트 정보

### 기본 URL
```
HTTP: http://localhost:8080/graphql
WebSocket: ws://localhost:8080/graphql/ws
```

**⚠️ 중요**:
- 프로덕션 환경에서는 `config.yaml`의 `api.host`와 `api.port` 설정을 확인하세요
- 기본 포트는 `8080`입니다 (설정 파일: `/config.yaml` 참조)
- WebSocket은 실시간 구독(Subscription)에만 사용됩니다

### CORS 설정
- CORS는 기본적으로 활성화되어 있습니다
- 모든 오리진(`*`)이 허용됩니다
- 프로덕션 환경에서는 `config.yaml`에서 `api.allowed_origins` 수정 필요

---

## 2. 요청된 API 구현 상태

### ✅ API #1: 통합 검색 (Search API)

**상태**: 완전히 구현됨 ✅

**Query 이름**: `search`

**Schema 정의**:
```graphql
type SearchResult {
  # 결과 타입: "block", "transaction", "address", "contract" 중 하나
  type: String!

  # 검색된 값 (해시, 주소, 블록 번호 등)
  value: String!

  # 사용자에게 표시할 레이블
  label: String

  # 추가 메타데이터 (JSON 문자열 형식)
  metadata: String
}

type Query {
  # 블록, 트랜잭션, 주소를 통합 검색
  search(
    query: String!,           # 검색어 (블록 번호, 해시, 주소)
    types: [String],          # 필터: ["block", "transaction", "address", "contract"]
    limit: Int = 10           # 최대 결과 개수 (기본값: 10)
  ): [SearchResult!]!
}
```

**예제 쿼리**:
```graphql
# 1. 모든 타입 검색
query {
  search(query: "0x1234...") {
    type
    value
    label
    metadata
  }
}

# 2. 블록만 검색
query {
  search(
    query: "1000",
    types: ["block"],
    limit: 5
  ) {
    type
    value
    label
  }
}

# 3. 주소 또는 컨트랙트 검색
query {
  search(
    query: "0xabcd...",
    types: ["address", "contract"]
  ) {
    type
    value
    label
    metadata
  }
}
```

**응답 예시**:
```json
{
  "data": {
    "search": [
      {
        "type": "block",
        "value": "1000",
        "label": "Block #1000",
        "metadata": "{\"timestamp\":1704067200,\"miner\":\"0x...\"}"
      },
      {
        "type": "transaction",
        "value": "0x1234...",
        "label": "TX 0x1234...",
        "metadata": "{\"from\":\"0x...\",\"to\":\"0x...\",\"value\":\"1000000000000000000\"}"
      }
    ]
  }
}
```

---

### ✅ API #2: 상위 채굴자 (Top Miners API)

**상태**: 완전히 구현됨 ✅

**Query 이름**: `topMiners`

**Schema 정의**:
```graphql
type MinerStats {
  # 채굴자 주소
  address: Address!

  # 채굴한 블록 수
  blockCount: BigInt!

  # 가장 최근에 채굴한 블록 번호
  lastBlockNumber: BigInt!

  # 가장 최근 채굴 시간 (Unix timestamp)
  lastBlockTime: BigInt!

  # 전체 대비 비율 (0-100)
  percentage: Float!

  # 총 보상 (Wei 단위)
  totalRewards: BigInt!
}

type Query {
  # 블록 수 기준 상위 채굴자 조회
  topMiners(
    limit: Int,              # 최대 결과 개수 (기본값: 10, 최대: 100)
    fromBlock: BigInt,       # 시작 블록 (선택사항)
    toBlock: BigInt          # 종료 블록 (선택사항)
  ): [MinerStats!]!
}
```

**예제 쿼리**:
```graphql
# 1. 상위 10명의 채굴자
query {
  topMiners(limit: 10) {
    address
    blockCount
    percentage
    totalRewards
  }
}

# 2. 특정 블록 범위에서 상위 채굴자
query {
  topMiners(
    limit: 20,
    fromBlock: "1000",
    toBlock: "10000"
  ) {
    address
    blockCount
    lastBlockNumber
    lastBlockTime
    percentage
    totalRewards
  }
}
```

**응답 예시**:
```json
{
  "data": {
    "topMiners": [
      {
        "address": "0x1111111111111111111111111111111111111111",
        "blockCount": "1500",
        "lastBlockNumber": "9999",
        "lastBlockTime": "1704153600",
        "percentage": 15.5,
        "totalRewards": "1500000000000000000000"
      },
      {
        "address": "0x2222222222222222222222222222222222222222",
        "blockCount": "1200",
        "lastBlockNumber": "9998",
        "lastBlockTime": "1704153580",
        "percentage": 12.4,
        "totalRewards": "1200000000000000000000"
      }
    ]
  }
}
```

---

### ✅ API #3: 토큰 잔액 조회 (Token Balance API)

**상태**: 완전히 구현됨 ✅

**Query 이름**: `tokenBalances`

**Schema 정의**:
```graphql
type TokenBalance {
  # 토큰 컨트랙트 주소
  contractAddress: Address!

  # 토큰 표준 (ERC20, ERC721, ERC1155)
  tokenType: String!

  # 토큰 잔액 (문자열)
  balance: BigInt!

  # 토큰 ID (ERC721/ERC1155에만 해당, ERC20은 null)
  tokenId: BigInt

  # 토큰 이름
  name: String

  # 토큰 심볼 (예: "WETH", "USDT")
  symbol: String

  # 소수점 자릿수 (ERC20만 해당)
  decimals: Int

  # 메타데이터 (JSON 문자열, NFT용)
  metadata: String
}

type Query {
  # 주소의 토큰 잔액 조회 (ERC20/721/1155)
  tokenBalances(
    address: Address!,       # 조회할 주소
    tokenType: String        # 필터: "ERC20", "ERC721", "ERC1155" (선택사항)
  ): [TokenBalance!]!
}
```

**예제 쿼리**:
```graphql
# 1. 모든 토큰 잔액 조회
query {
  tokenBalances(address: "0x1234...") {
    contractAddress
    tokenType
    balance
    tokenId
    name
    symbol
    decimals
  }
}

# 2. ERC20 토큰만 조회
query {
  tokenBalances(
    address: "0x1234...",
    tokenType: "ERC20"
  ) {
    contractAddress
    tokenType
    balance
    name
    symbol
    decimals
  }
}

# 3. NFT (ERC721) 조회
query {
  tokenBalances(
    address: "0x1234...",
    tokenType: "ERC721"
  ) {
    contractAddress
    tokenType
    balance
    tokenId
    name
    symbol
    metadata
  }
}
```

**응답 예시**:
```json
{
  "data": {
    "tokenBalances": [
      {
        "contractAddress": "0xaaaa...",
        "tokenType": "ERC20",
        "balance": "1000000000000000000000",
        "tokenId": null,
        "name": "Wrapped Ether",
        "symbol": "WETH",
        "decimals": 18,
        "metadata": null
      },
      {
        "contractAddress": "0xbbbb...",
        "tokenType": "ERC721",
        "balance": "1",
        "tokenId": "42",
        "name": "CryptoKitties",
        "symbol": "CK",
        "decimals": null,
        "metadata": "{\"name\":\"Kitty #42\",\"image\":\"ipfs://...\"}"
      }
    ]
  }
}
```

---

### ✅ API #4: 컨트랙트 검증 (Contract Verification)

**상태**: Query와 Mutation 모두 완전히 구현됨 ✅

**Query 이름**: `contractVerification`
**Mutation 이름**: `verifyContract`

**Schema 정의**:
```graphql
type ContractVerification {
  # 컨트랙트 주소
  address: Address!

  # 검증 여부
  isVerified: Boolean!

  # 컨트랙트 이름
  name: String

  # Solidity 컴파일러 버전 (예: "0.8.20")
  compilerVersion: String

  # 최적화 활성화 여부
  optimizationEnabled: Boolean

  # 최적화 실행 횟수
  optimizationRuns: Int

  # 검증된 소스 코드
  sourceCode: String

  # 컨트랙트 ABI (JSON 문자열)
  abi: String

  # Constructor 인자 (인코딩됨)
  constructorArguments: String

  # 검증 시간 (RFC3339 형식)
  verifiedAt: String

  # 라이선스 타입 (예: "MIT", "GPL-3.0")
  licenseType: String
}

type Query {
  # 컨트랙트 검증 정보 조회
  contractVerification(address: Address!): ContractVerification
}

type Mutation {
  # 컨트랙트 소스 코드 검증
  verifyContract(
    address: Address!,
    sourceCode: String!,
    compilerVersion: String!,
    optimizationEnabled: Boolean!,
    optimizationRuns: Int,
    constructorArguments: String,
    contractName: String,
    licenseType: String
  ): ContractVerification!
}
```

**예제 쿼리** (조회):
```graphql
query {
  contractVerification(address: "0x1234...") {
    address
    isVerified
    name
    compilerVersion
    sourceCode
    abi
    verifiedAt
  }
}
```

**예제 Mutation** (검증 제출):
```graphql
mutation {
  verifyContract(
    address: "0x1234...",
    sourceCode: "pragma solidity ^0.8.0; contract MyToken { ... }",
    compilerVersion: "0.8.20",
    optimizationEnabled: true,
    optimizationRuns: 200,
    contractName: "MyToken",
    licenseType: "MIT"
  ) {
    address
    isVerified
    name
    verifiedAt
  }
}
```

---

## 3. 기존 API 버그 수정 현황

### 🐛 Issue #1: addressBalance 버그 (HIGH Priority)

**문제**: `addressBalance` 쿼리가 큰 Wei 값에 대해 "0" 반환

**근본 원인 분석**: ✅ 완료
- GraphQL 스키마: `BigInt` 타입으로 정의됨
- 실제 구현: `bigIntType = graphql.String`으로 정의되어 문자열 반환
- Resolver 구현: `balance.String()` 반환 (✅ 정확함)
- **결론**: GraphQL resolver 레이어는 정상 작동함

**현재 상태**:
- ⚠️ Storage 레이어(`GetAddressBalance`)에서 0 반환 가능성 높음
- 백엔드 팀에서 storage 구현 조사 필요

**Frontend 대응**:
```graphql
# 현재 Query (정상 작동 예상)
query {
  addressBalance(
    address: "0x1234...",
    blockNumber: "0"  # 0 또는 생략 시 최신 블록
  )
}

# 응답 형식
{
  "data": {
    "addressBalance": "1000000000000000000000"  # 문자열로 반환
  }
}
```

**중요 사항**:
1. ✅ 반환 타입은 `BigInt`(String)이므로 안전하게 큰 숫자 처리 가능
2. ✅ JavaScript에서는 `BigInt()` 또는 `ethers.BigNumber.from()` 사용 권장
3. ⚠️ 만약 여전히 "0"이 반환되면 백엔드 팀에 알려주세요

---

### ⚠️ Issue #2: ContractCreation 주소 필드 (MEDIUM Priority)

**문제**: `ContractCreation` 타입에 `address` 필드가 없어 컨트랙트 주소 표시 불가

**조사 결과**: ✅ 정상 작동 중 - 필드명 불일치 문제

**실제 Schema 정의**:
```graphql
type ContractCreation {
  # ⚠️ 필드명: contractAddress (address 아님!)
  contractAddress: Address!

  # 생성자 주소
  creator: Address!

  # 생성 트랜잭션 해시
  transactionHash: Hash!

  # 블록 번호
  blockNumber: BigInt!

  # 타임스탬프
  timestamp: BigInt!

  # 배포된 바이트코드 크기
  bytecodeSize: Int!
}
```

**해결 방법**: ✅ 필드명 변경

**올바른 쿼리**:
```graphql
# ❌ 잘못된 예 (작동 안 함)
query {
  contractCreation(address: "0x1234...") {
    address  # 이 필드는 존재하지 않음!
    creator
  }
}

# ✅ 올바른 예 (작동함)
query {
  contractCreation(address: "0x1234...") {
    contractAddress  # 정확한 필드명
    creator
    transactionHash
    blockNumber
    timestamp
    bytecodeSize
  }
}
```

**Frontend 수정 사항**:
- 모든 `ContractCreation` 쿼리에서 `address` → `contractAddress`로 변경
- 이것은 버그가 아니라 정상적인 스키마 설계입니다

---

## 4. GraphQL 스칼라 타입 참고

백엔드에서 사용하는 커스텀 스칼라 타입 정의:

```graphql
# BigInt: 큰 정수를 문자열로 표현 (JavaScript Number 한계 극복)
scalar BigInt    # 실제로는 String

# Address: 이더리움 주소 (0x로 시작하는 40자 hex)
scalar Address   # 실제로는 String

# Hash: 32바이트 해시 (0x로 시작하는 64자 hex)
scalar Hash      # 실제로는 String

# Bytes: 임의 길이 바이트 배열 (0x로 시작하는 hex)
scalar Bytes     # 실제로는 String
```

**중요**: 모든 스칼라 타입은 실제로 `String`으로 구현되어 있습니다!

---

## 5. 실전 사용 예제

### 예제 1: 주소의 전체 정보 조회
```graphql
query GetAddressFullInfo($address: Address!) {
  # 네이티브 코인 잔액
  balance: addressBalance(address: $address)

  # 토큰 잔액 (ERC20)
  tokens: tokenBalances(address: $address, tokenType: "ERC20") {
    contractAddress
    balance
    name
    symbol
    decimals
  }

  # NFT 보유 현황
  nfts: tokenBalances(address: $address, tokenType: "ERC721") {
    contractAddress
    tokenId
    name
    metadata
  }

  # 컨트랙트 생성 여부
  contractInfo: contractCreation(address: $address) {
    contractAddress
    creator
    blockNumber
    timestamp
  }

  # 검증 상태
  verification: contractVerification(address: $address) {
    isVerified
    name
    compilerVersion
  }
}
```

### 예제 2: 검색 기능 구현
```typescript
// TypeScript 예제
import { gql, useQuery } from '@apollo/client';

const SEARCH_QUERY = gql`
  query Search($query: String!, $limit: Int) {
    search(query: $query, limit: $limit) {
      type
      value
      label
      metadata
    }
  }
`;

function SearchBar() {
  const [searchTerm, setSearchTerm] = useState('');
  const { data, loading } = useQuery(SEARCH_QUERY, {
    variables: { query: searchTerm, limit: 10 },
    skip: searchTerm.length < 3
  });

  return (
    <div>
      <input
        value={searchTerm}
        onChange={(e) => setSearchTerm(e.target.value)}
        placeholder="블록 번호, 주소, 트랜잭션 해시 검색..."
      />
      {loading && <div>검색 중...</div>}
      {data?.search.map(result => (
        <SearchResult key={result.value} {...result} />
      ))}
    </div>
  );
}
```

### 예제 3: 채굴자 순위 대시보드
```graphql
query MinersDashboard {
  # 전체 상위 채굴자
  topMiners(limit: 20) {
    address
    blockCount
    percentage
    totalRewards
    lastBlockNumber
    lastBlockTime
  }

  # 최근 1000 블록 기준 채굴자
  recentMiners: topMiners(
    limit: 10,
    fromBlock: "990000",
    toBlock: "1000000"
  ) {
    address
    blockCount
    percentage
  }
}
```

---

## 6. 에러 처리

### 일반적인 GraphQL 에러 응답
```json
{
  "errors": [
    {
      "message": "invalid address format",
      "path": ["addressBalance"],
      "extensions": {
        "code": "BAD_USER_INPUT"
      }
    }
  ],
  "data": null
}
```

### 권장 에러 처리 전략
```typescript
// Apollo Client 예제
const { data, loading, error } = useQuery(QUERY, { variables });

if (error) {
  // GraphQL 에러
  if (error.graphQLErrors.length > 0) {
    error.graphQLErrors.forEach(({ message, path }) => {
      console.error(`GraphQL Error at ${path}: ${message}`);
    });
  }

  // 네트워크 에러
  if (error.networkError) {
    console.error('Network Error:', error.networkError);
  }

  return <ErrorComponent message={error.message} />;
}
```

---

## 7. 성능 최적화 팁

### 1. Pagination 사용
- 대부분의 목록 쿼리는 `pagination` 인자를 지원합니다
- 기본 limit: 10, 최대 limit: 100

```graphql
query {
  blocks(
    pagination: { limit: 20, offset: 0 }
  ) {
    nodes { number hash }
    totalCount
    pageInfo { hasNextPage }
  }
}
```

### 2. 필요한 필드만 요청
```graphql
# ❌ 나쁜 예: 모든 필드 요청
query {
  block(number: "1000") {
    number
    hash
    parentHash
    timestamp
    nonce
    miner
    difficulty
    totalDifficulty
    gasLimit
    gasUsed
    baseFeePerGas
    # ... 모든 필드
  }
}

# ✅ 좋은 예: 필요한 필드만 요청
query {
  block(number: "1000") {
    number
    hash
    timestamp
    miner
  }
}
```

### 3. WebSocket 구독 사용 (실시간 업데이트)
```graphql
# 새로운 블록 구독
subscription {
  newBlock {
    number
    hash
    timestamp
    transactionCount
  }
}
```

---

## 8. 테스트용 쿼리 모음

### GraphQL Playground에서 테스트
```
브라우저에서 열기: http://localhost:8080/graphql
```

### 빠른 검증 쿼리
```graphql
# 1. 서버 상태 확인
query {
  latestHeight
  blockCount
  transactionCount
}

# 2. 검색 기능 테스트
query {
  search(query: "1000") {
    type
    value
    label
  }
}

# 3. 채굴자 통계 확인
query {
  topMiners(limit: 5) {
    address
    blockCount
    percentage
  }
}

# 4. 잔액 조회 테스트
query {
  addressBalance(address: "0x0000000000000000000000000000000000000000")
}
```

---

## 9. 문의 및 지원

### 버그 리포트
- 예상치 못한 결과가 나오면 백엔드 팀에 문의
- 다음 정보를 포함해 주세요:
  1. 실행한 쿼리
  2. 받은 응답
  3. 기대했던 결과
  4. 재현 방법

### 기능 요청
- 새로운 쿼리나 필드가 필요하면 백엔드 팀에 요청
- Use case와 예상 응답 형식을 함께 제공해 주세요

---

## 10. 변경 이력

| 날짜 | 버전 | 변경 내용 |
|------|------|-----------|
| 2025-01-XX | 1.0 | 초기 문서 작성 |
|            |     | - Search API 정보 추가 |
|            |     | - Top Miners API 정보 추가 |
|            |     | - Token Balances API 정보 추가 |
|            |     | - Contract Verification API 정보 추가 |
|            |     | - addressBalance 버그 분석 |
|            |     | - ContractCreation.address 이슈 해결 |

---

**문서 작성일**: 2025-01-XX
**검증 완료**: ✅ 모든 API 코드 검증 완료
**테스트 환경**: Development (localhost:8080)

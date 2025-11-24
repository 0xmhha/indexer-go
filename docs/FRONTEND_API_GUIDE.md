# Frontend API Integration Guide

이 문서는 Indexer의 GraphQL API를 Frontend에서 사용하기 위한 가이드입니다.

## 목차
1. [API 엔드포인트](#api-엔드포인트)
2. [Search API](#search-api)
3. [Top Miners API](#top-miners-api)
4. [기타 Historical API](#기타-historical-api)
5. [에러 처리](#에러-처리)

---

## API 엔드포인트

### GraphQL Endpoint
```
POST http://localhost:8080/graphql
Content-Type: application/json
```

### GraphQL Playground
```
http://localhost:8080/graphql/playground
```

---

## Search API

통합 검색 API로 블록, 트랜잭션, 주소, 로그를 검색할 수 있습니다.

### Query

```graphql
query Search($query: String!, $types: [String!], $limit: Int) {
  search(query: $query, types: $types, limit: $limit) {
    ... on BlockResult {
      type
      block {
        number
        hash
        timestamp
        parentHash
        miner
        gasUsed
        gasLimit
        transactionCount
      }
    }
    ... on TransactionResult {
      type
      transaction {
        hash
        from
        to
        value
        gas
        gasPrice
        nonce
        blockNumber
        blockHash
        transactionIndex
      }
    }
    ... on AddressResult {
      type
      address
      transactionCount
      balance
    }
    ... on LogResult {
      type
      log {
        address
        topics
        data
        blockNumber
        transactionHash
        logIndex
      }
    }
  }
}
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| query | String | Yes | 검색어 (블록 번호, 해시, 주소 등) |
| types | [String] | No | 결과 타입 필터 ("block", "transaction", "address", "log") |
| limit | Int | No | 최대 결과 수 (기본값: 10, 최대: 100) |

### Request Examples

#### 1. 전체 검색 (모든 타입)
```json
{
  "query": "query Search($query: String!) { search(query: $query) { ... on BlockResult { type block { number hash } } ... on TransactionResult { type transaction { hash from to } } ... on AddressResult { type address } } }",
  "variables": {
    "query": "0x1234"
  }
}
```

#### 2. 블록만 검색
```json
{
  "query": "query Search($query: String!, $types: [String!]) { search(query: $query, types: $types) { ... on BlockResult { type block { number hash timestamp } } } }",
  "variables": {
    "query": "100",
    "types": ["block"]
  }
}
```

#### 3. 주소 검색
```json
{
  "query": "query Search($query: String!, $types: [String!]) { search(query: $query, types: $types) { ... on AddressResult { type address transactionCount balance } } }",
  "variables": {
    "query": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
    "types": ["address"]
  }
}
```

### Response Example

```json
{
  "data": {
    "search": [
      {
        "type": "block",
        "block": {
          "number": "100",
          "hash": "0x1234...",
          "timestamp": "1234567890",
          "parentHash": "0x5678...",
          "miner": "0xabcd...",
          "gasUsed": "5000000",
          "gasLimit": "8000000",
          "transactionCount": 10
        }
      },
      {
        "type": "transaction",
        "transaction": {
          "hash": "0x1234...",
          "from": "0xaaa...",
          "to": "0xbbb...",
          "value": "1000000000000000000",
          "gas": "21000",
          "gasPrice": "1000000000",
          "nonce": "5",
          "blockNumber": "100",
          "blockHash": "0x1234...",
          "transactionIndex": "0"
        }
      }
    ]
  }
}
```

### Frontend Integration Example (React + Apollo Client)

```typescript
import { useQuery, gql } from '@apollo/client';

const SEARCH_QUERY = gql`
  query Search($query: String!, $types: [String!], $limit: Int) {
    search(query: $query, types: $types, limit: $limit) {
      ... on BlockResult {
        type
        block {
          number
          hash
          timestamp
          miner
        }
      }
      ... on TransactionResult {
        type
        transaction {
          hash
          from
          to
          value
        }
      }
      ... on AddressResult {
        type
        address
        transactionCount
        balance
      }
    }
  }
`;

function SearchComponent() {
  const [searchQuery, setSearchQuery] = useState('');
  const [resultTypes, setResultTypes] = useState(['block', 'transaction', 'address']);

  const { loading, error, data } = useQuery(SEARCH_QUERY, {
    variables: {
      query: searchQuery,
      types: resultTypes,
      limit: 20
    },
    skip: !searchQuery
  });

  return (
    <div>
      <input
        type="text"
        value={searchQuery}
        onChange={(e) => setSearchQuery(e.target.value)}
        placeholder="Search blocks, transactions, addresses..."
      />

      {loading && <div>Loading...</div>}
      {error && <div>Error: {error.message}</div>}

      {data?.search.map((result, index) => (
        <div key={index}>
          {result.type === 'block' && (
            <div>Block #{result.block.number} - {result.block.hash}</div>
          )}
          {result.type === 'transaction' && (
            <div>Tx: {result.transaction.hash}</div>
          )}
          {result.type === 'address' && (
            <div>Address: {result.address} ({result.transactionCount} txs)</div>
          )}
        </div>
      ))}
    </div>
  );
}
```

---

## Top Miners API

채굴자 통계 및 랭킹을 조회하는 API입니다. **최근 개선사항으로 시간 범위 필터링과 추가 필드(보상, 비율, 타임스탬프)를 지원합니다.**

### Query

```graphql
query TopMiners($limit: Int, $fromBlock: BigInt, $toBlock: BigInt) {
  topMiners(limit: $limit, fromBlock: $fromBlock, toBlock: $toBlock) {
    address
    blockCount
    lastBlockNumber
    lastBlockTime
    percentage
    totalRewards
  }
}
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| limit | Int | No | 최대 결과 수 (기본값: 10, 최대: 100) |
| fromBlock | BigInt | No | 시작 블록 번호 (0 = genesis, 기본값: 전체) |
| toBlock | BigInt | No | 종료 블록 번호 (0 = latest, 기본값: 전체) |

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| address | Address | 채굴자 주소 |
| blockCount | BigInt | 채굴한 블록 수 |
| lastBlockNumber | BigInt | 마지막으로 채굴한 블록 번호 |
| lastBlockTime | BigInt | 마지막으로 채굴한 블록의 타임스탬프 (Unix timestamp) |
| percentage | Float | 전체 블록 대비 채굴 비율 (%) |
| totalRewards | BigInt | 총 채굴 보상 (Wei 단위, transaction fees 합계) |

### Request Examples

#### 1. 전체 기간 Top 10 채굴자
```json
{
  "query": "query TopMiners { topMiners { address blockCount lastBlockNumber lastBlockTime percentage totalRewards } }"
}
```

#### 2. 특정 블록 범위의 Top 20 채굴자
```json
{
  "query": "query TopMiners($limit: Int, $fromBlock: BigInt, $toBlock: BigInt) { topMiners(limit: $limit, fromBlock: $fromBlock, toBlock: $toBlock) { address blockCount percentage totalRewards } }",
  "variables": {
    "limit": 20,
    "fromBlock": "1000",
    "toBlock": "2000"
  }
}
```

#### 3. 최근 1000 블록의 채굴자 통계
```json
{
  "query": "query TopMiners($fromBlock: BigInt) { topMiners(fromBlock: $fromBlock) { address blockCount lastBlockNumber lastBlockTime percentage totalRewards } }",
  "variables": {
    "fromBlock": "9000"
  }
}
```

### Response Example

```json
{
  "data": {
    "topMiners": [
      {
        "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
        "blockCount": "1500",
        "lastBlockNumber": "9999",
        "lastBlockTime": "1709876543",
        "percentage": 15.5,
        "totalRewards": "5000000000000000000"
      },
      {
        "address": "0x1234567890abcdef1234567890abcdef12345678",
        "blockCount": "1200",
        "lastBlockNumber": "9998",
        "lastBlockTime": "1709876500",
        "percentage": 12.3,
        "totalRewards": "3800000000000000000"
      }
    ]
  }
}
```

### Frontend Integration Example (React + Apollo Client)

```typescript
import { useQuery, gql } from '@apollo/client';
import { formatEther } from 'ethers';

const TOP_MINERS_QUERY = gql`
  query TopMiners($limit: Int, $fromBlock: BigInt, $toBlock: BigInt) {
    topMiners(limit: $limit, fromBlock: $fromBlock, toBlock: $toBlock) {
      address
      blockCount
      lastBlockNumber
      lastBlockTime
      percentage
      totalRewards
    }
  }
`;

interface MinerStats {
  address: string;
  blockCount: string;
  lastBlockNumber: string;
  lastBlockTime: string;
  percentage: number;
  totalRewards: string;
}

function TopMinersComponent() {
  const [limit, setLimit] = useState(10);
  const [fromBlock, setFromBlock] = useState<string>('');
  const [toBlock, setToBlock] = useState<string>('');

  const { loading, error, data, refetch } = useQuery<{ topMiners: MinerStats[] }>(
    TOP_MINERS_QUERY,
    {
      variables: {
        limit,
        fromBlock: fromBlock || undefined,
        toBlock: toBlock || undefined
      }
    }
  );

  const formatTimestamp = (timestamp: string) => {
    return new Date(parseInt(timestamp) * 1000).toLocaleString();
  };

  return (
    <div>
      <h2>Top Miners Leaderboard</h2>

      {/* Filters */}
      <div className="filters">
        <label>
          Limit:
          <input
            type="number"
            value={limit}
            onChange={(e) => setLimit(parseInt(e.target.value))}
            min="1"
            max="100"
          />
        </label>

        <label>
          From Block:
          <input
            type="text"
            value={fromBlock}
            onChange={(e) => setFromBlock(e.target.value)}
            placeholder="Leave empty for all"
          />
        </label>

        <label>
          To Block:
          <input
            type="text"
            value={toBlock}
            onChange={(e) => setToBlock(e.target.value)}
            placeholder="Leave empty for latest"
          />
        </label>

        <button onClick={() => refetch()}>Apply Filters</button>
      </div>

      {loading && <div>Loading miners...</div>}
      {error && <div>Error: {error.message}</div>}

      {data && (
        <table>
          <thead>
            <tr>
              <th>Rank</th>
              <th>Address</th>
              <th>Blocks Mined</th>
              <th>Percentage</th>
              <th>Total Rewards (ETH)</th>
              <th>Last Block</th>
              <th>Last Activity</th>
            </tr>
          </thead>
          <tbody>
            {data.topMiners.map((miner, index) => (
              <tr key={miner.address}>
                <td>{index + 1}</td>
                <td>
                  <a href={`/address/${miner.address}`}>
                    {miner.address.slice(0, 10)}...
                  </a>
                </td>
                <td>{parseInt(miner.blockCount).toLocaleString()}</td>
                <td>{miner.percentage.toFixed(2)}%</td>
                <td>{formatEther(miner.totalRewards)} ETH</td>
                <td>
                  <a href={`/block/${miner.lastBlockNumber}`}>
                    #{miner.lastBlockNumber}
                  </a>
                </td>
                <td>{formatTimestamp(miner.lastBlockTime)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
```

### UI Design Recommendations

#### 1. Leaderboard View
- 순위, 채굴자 주소, 블록 수, 비율을 표시하는 테이블
- 주소 클릭 시 상세 페이지로 이동
- 페이지네이션 또는 무한 스크롤

#### 2. Time Range Filter
- 블록 번호 범위 선택 (from/to)
- 프리셋: "Last 1000 blocks", "Last 24 hours", "Last 7 days", "All time"
- 날짜를 블록 번호로 자동 변환 (타임스탬프 API 활용)

#### 3. Visualizations
- 파이 차트: 상위 채굴자들의 비율 시각화
- 막대 그래프: 채굴 블록 수 비교
- 타임라인: 시간대별 채굴 활동

#### 4. Miner Detail Page
- 총 채굴 블록 수와 보상
- 시간대별 채굴 그래프
- 최근 채굴한 블록 목록
- 평균 블록 타임

---

## 기타 Historical API

### 1. Block Count
```graphql
query {
  blockCount
}
```

### 2. Transaction Count
```graphql
query {
  transactionCount
}
```

### 3. Address Balance (Historical)
```graphql
query AddressBalance($address: Address!, $blockNumber: BigInt) {
  addressBalance(address: $address, blockNumber: $blockNumber)
}
```

### 4. Balance History
```graphql
query BalanceHistory($address: Address!, $fromBlock: BigInt, $toBlock: BigInt, $limit: Int, $offset: Int) {
  balanceHistory(
    address: $address
    fromBlock: $fromBlock
    toBlock: $toBlock
    limit: $limit
    offset: $offset
  ) {
    nodes {
      blockNumber
      balance
      delta
      txHash
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

---

## 에러 처리

### GraphQL Error Response

```json
{
  "errors": [
    {
      "message": "storage does not support historical queries",
      "locations": [
        {
          "line": 2,
          "column": 3
        }
      ],
      "path": ["topMiners"]
    }
  ],
  "data": null
}
```

### Common Errors

| Error Message | Cause | Solution |
|---------------|-------|----------|
| `storage does not support historical queries` | Historical 기능이 활성화되지 않음 | 백엔드 설정 확인 필요 |
| `invalid block number` | 잘못된 블록 번호 형식 | 숫자 형식 확인 |
| `fromBlock cannot be greater than toBlock` | 블록 범위 오류 | fromBlock ≤ toBlock 확인 |
| `limit exceeds maximum` | limit > 100 | limit를 100 이하로 설정 |

### Frontend Error Handling Example

```typescript
function handleGraphQLError(error: ApolloError) {
  if (error.graphQLErrors) {
    error.graphQLErrors.forEach((err) => {
      console.error(`GraphQL Error: ${err.message}`);

      // User-friendly error messages
      if (err.message.includes('storage does not support')) {
        showNotification('Historical data is not available', 'warning');
      } else if (err.message.includes('invalid block number')) {
        showNotification('Please enter a valid block number', 'error');
      } else {
        showNotification('An error occurred. Please try again.', 'error');
      }
    });
  }

  if (error.networkError) {
    console.error(`Network Error: ${error.networkError}`);
    showNotification('Network error. Please check your connection.', 'error');
  }
}
```

---

## Performance Optimization

### 1. Caching Strategy
```typescript
// Apollo Client setup with caching
const client = new ApolloClient({
  uri: 'http://localhost:8080/graphql',
  cache: new InMemoryCache({
    typePolicies: {
      Query: {
        fields: {
          topMiners: {
            // Cache by variables
            keyArgs: ['limit', 'fromBlock', 'toBlock'],
          },
          search: {
            // Cache by query and types
            keyArgs: ['query', 'types'],
          },
        },
      },
    },
  }),
});
```

### 2. Pagination
```typescript
// Implement offset-based pagination for large result sets
const [offset, setOffset] = useState(0);
const limit = 20;

const { loading, data } = useQuery(SEARCH_QUERY, {
  variables: { query, limit, offset },
});

// Next page
const nextPage = () => setOffset(offset + limit);
// Previous page
const prevPage = () => setOffset(Math.max(0, offset - limit));
```

### 3. Debouncing Search
```typescript
import { useDebouncedCallback } from 'use-debounce';

const debouncedSearch = useDebouncedCallback(
  (value: string) => {
    setSearchQuery(value);
  },
  500 // 500ms delay
);

<input
  type="text"
  onChange={(e) => debouncedSearch(e.target.value)}
  placeholder="Search..."
/>
```

---

## Testing

### GraphQL Playground 사용

1. 브라우저에서 `http://localhost:8080/graphql/playground` 접속
2. 왼쪽에 쿼리 입력
3. 하단에 Variables 입력
4. Play 버튼 클릭하여 실행
5. 오른쪽에서 응답 확인

### cURL 테스트

```bash
# Search API
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "query Search($query: String!) { search(query: $query) { ... on BlockResult { type block { number hash } } } }",
    "variables": {"query": "100"}
  }'

# Top Miners API
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "query TopMiners($limit: Int) { topMiners(limit: $limit) { address blockCount percentage } }",
    "variables": {"limit": 5}
  }'
```

---

## 다음 개선 예정 (Phase 2)

### Token Balance API 개선
- 추가 필드: name, symbol, decimals, metadata
- tokenType 필터 파라미터 추가
- ERC20/ERC721/ERC1155 타입별 필터링

### 예상 쿼리 (개발 예정)
```graphql
query TokenBalances($address: Address!, $tokenType: String) {
  tokenBalances(address: $address, tokenType: $tokenType) {
    contractAddress
    tokenType
    balance
    tokenID
    name
    symbol
    decimals
    metadata
  }
}
```

---

## 지원

문제가 발생하거나 추가 기능이 필요한 경우:
- GitHub Issues: [프로젝트 저장소]
- 백엔드 팀에 문의

**최종 업데이트:** 2025-01-24

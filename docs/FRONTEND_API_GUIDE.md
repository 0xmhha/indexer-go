# Frontend API Integration Guide

이 문서는 Indexer의 GraphQL API를 Frontend에서 사용하기 위한 가이드입니다.

## 목차
1. [API 엔드포인트](#api-엔드포인트)
2. [Search API](#search-api)
3. [Top Miners API](#top-miners-api)
4. [Token Balance API](#token-balance-api)
5. [Address Balance API](#address-balance-api)
6. [기타 Historical API](#기타-historical-api)
7. [에러 처리](#에러-처리)

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

## Token Balance API

토큰 잔액 조회 API로 ERC20, ERC721, ERC1155 토큰의 잔액과 메타데이터를 조회할 수 있습니다. **Phase 2에서 name, symbol, decimals, metadata 필드와 tokenType 필터가 추가되었습니다.**

### Query

```graphql
query TokenBalances($address: Address!, $tokenType: String) {
  tokenBalances(address: $address, tokenType: $tokenType) {
    contractAddress
    tokenType
    balance
    tokenId
    name
    symbol
    decimals
    metadata
  }
}
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| address | Address | Yes | 조회할 주소 (지갑 주소) |
| tokenType | String | No | 토큰 타입 필터 ("ERC20", "ERC721", "ERC1155") |

### Response Fields

| Field | Type | Description |
|-------|------|-------------|
| contractAddress | Address | 토큰 컨트랙트 주소 |
| tokenType | String | 토큰 표준 타입 (ERC20, ERC721, ERC1155) |
| balance | BigInt | 토큰 잔액 (ERC20: 소수점 없는 원본 값, ERC721: 1 또는 0, ERC1155: 수량) |
| tokenId | BigInt | 토큰 ID (ERC721/ERC1155만 해당, ERC20은 null) |
| name | String | 토큰 이름 (예: "Wrapped Ether") |
| symbol | String | 토큰 심볼 (예: "WETH") |
| decimals | Int | 소수점 자리수 (ERC20만 해당, 기본값: 18) |
| metadata | String | 토큰 메타데이터 JSON (ERC721/ERC1155의 경우 NFT 메타데이터) |

### Request Examples

#### 1. 모든 토큰 잔액 조회
```json
{
  "query": "query TokenBalances($address: Address!) { tokenBalances(address: $address) { contractAddress tokenType balance tokenId name symbol decimals metadata } }",
  "variables": {
    "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb"
  }
}
```

#### 2. ERC20 토큰만 조회
```json
{
  "query": "query TokenBalances($address: Address!, $tokenType: String) { tokenBalances(address: $address, tokenType: $tokenType) { contractAddress tokenType balance name symbol decimals } }",
  "variables": {
    "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
    "tokenType": "ERC20"
  }
}
```

#### 3. ERC721 NFT만 조회
```json
{
  "query": "query TokenBalances($address: Address!, $tokenType: String) { tokenBalances(address: $address, tokenType: $tokenType) { contractAddress tokenType tokenId name metadata } }",
  "variables": {
    "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
    "tokenType": "ERC721"
  }
}
```

#### 4. 특정 컨트랙트의 토큰만 조회 (클라이언트 필터링)
```typescript
// GraphQL에서는 contractAddress 필터를 지원하지 않으므로 클라이언트에서 필터링
const filteredTokens = data.tokenBalances.filter(
  token => token.contractAddress === "0x1234..."
);
```

### Response Example

```json
{
  "data": {
    "tokenBalances": [
      {
        "contractAddress": "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
        "tokenType": "ERC20",
        "balance": "5000000000000000000",
        "tokenId": null,
        "name": "Wrapped Ether",
        "symbol": "WETH",
        "decimals": 18,
        "metadata": null
      },
      {
        "contractAddress": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
        "tokenType": "ERC20",
        "balance": "10000000",
        "tokenId": null,
        "name": "USD Coin",
        "symbol": "USDC",
        "decimals": 6,
        "metadata": null
      },
      {
        "contractAddress": "0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D",
        "tokenType": "ERC721",
        "balance": "1",
        "tokenId": "1234",
        "name": "Bored Ape Yacht Club",
        "symbol": "BAYC",
        "decimals": null,
        "metadata": "{\"name\":\"Bored Ape #1234\",\"image\":\"ipfs://...\",\"attributes\":[...]}"
      },
      {
        "contractAddress": "0xd07dc4262BCDbf85190C01c996b4C06a461d2430",
        "tokenType": "ERC1155",
        "balance": "10",
        "tokenId": "5678",
        "name": "Rarible",
        "symbol": "RARI",
        "decimals": null,
        "metadata": "{\"name\":\"Artwork #5678\",\"image\":\"ipfs://...\",\"description\":\"...\"}"
      }
    ]
  }
}
```

### Frontend Integration Example (React + Apollo Client)

```typescript
import { useQuery, gql } from '@apollo/client';
import { formatUnits } from 'ethers';

const TOKEN_BALANCES_QUERY = gql`
  query TokenBalances($address: Address!, $tokenType: String) {
    tokenBalances(address: $address, tokenType: $tokenType) {
      contractAddress
      tokenType
      balance
      tokenId
      name
      symbol
      decimals
      metadata
    }
  }
`;

interface TokenBalance {
  contractAddress: string;
  tokenType: 'ERC20' | 'ERC721' | 'ERC1155';
  balance: string;
  tokenId: string | null;
  name: string;
  symbol: string;
  decimals: number | null;
  metadata: string | null;
}

function TokenBalancesComponent({ address }: { address: string }) {
  const [tokenTypeFilter, setTokenTypeFilter] = useState<string>('');

  const { loading, error, data } = useQuery<{ tokenBalances: TokenBalance[] }>(
    TOKEN_BALANCES_QUERY,
    {
      variables: {
        address,
        tokenType: tokenTypeFilter || undefined
      }
    }
  );

  const formatBalance = (token: TokenBalance) => {
    if (token.tokenType === 'ERC20' && token.decimals) {
      // ERC20: Format with decimals
      return formatUnits(token.balance, token.decimals);
    } else if (token.tokenType === 'ERC721') {
      // ERC721: Show token ID
      return `Token #${token.tokenId}`;
    } else if (token.tokenType === 'ERC1155') {
      // ERC1155: Show quantity and token ID
      return `${token.balance}x Token #${token.tokenId}`;
    }
    return token.balance;
  };

  const parseMetadata = (metadataJson: string | null) => {
    if (!metadataJson) return null;
    try {
      return JSON.parse(metadataJson);
    } catch {
      return null;
    }
  };

  return (
    <div>
      <h2>Token Balances</h2>

      {/* Token Type Filter */}
      <div className="filters">
        <label>
          Token Type:
          <select
            value={tokenTypeFilter}
            onChange={(e) => setTokenTypeFilter(e.target.value)}
          >
            <option value="">All Types</option>
            <option value="ERC20">ERC20 Tokens</option>
            <option value="ERC721">ERC721 NFTs</option>
            <option value="ERC1155">ERC1155 Tokens</option>
          </select>
        </label>
      </div>

      {loading && <div>Loading token balances...</div>}
      {error && <div>Error: {error.message}</div>}

      {data && (
        <div className="token-list">
          {data.tokenBalances.length === 0 ? (
            <div>No tokens found for this address</div>
          ) : (
            data.tokenBalances.map((token) => {
              const metadata = parseMetadata(token.metadata);

              return (
                <div key={`${token.contractAddress}-${token.tokenId || '0'}`} className="token-card">
                  {/* Token Header */}
                  <div className="token-header">
                    <h3>{token.name || 'Unknown Token'}</h3>
                    <span className="token-type-badge">{token.tokenType}</span>
                  </div>

                  {/* Token Info */}
                  <div className="token-info">
                    <div>
                      <strong>Symbol:</strong> {token.symbol || 'N/A'}
                    </div>
                    <div>
                      <strong>Balance:</strong> {formatBalance(token)}
                    </div>
                    <div>
                      <strong>Contract:</strong>{' '}
                      <a href={`/address/${token.contractAddress}`}>
                        {token.contractAddress.slice(0, 10)}...
                      </a>
                    </div>
                  </div>

                  {/* NFT Metadata (ERC721/ERC1155) */}
                  {metadata && (token.tokenType === 'ERC721' || token.tokenType === 'ERC1155') && (
                    <div className="nft-metadata">
                      {metadata.image && (
                        <img
                          src={metadata.image.replace('ipfs://', 'https://ipfs.io/ipfs/')}
                          alt={metadata.name}
                          className="nft-image"
                        />
                      )}
                      {metadata.description && (
                        <p className="nft-description">{metadata.description}</p>
                      )}
                      {metadata.attributes && (
                        <div className="nft-attributes">
                          {metadata.attributes.map((attr: any, idx: number) => (
                            <div key={idx} className="attribute">
                              <span className="trait-type">{attr.trait_type}:</span>
                              <span className="trait-value">{attr.value}</span>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
```

### UI Design Recommendations

#### 1. Token List View
- **ERC20 토큰**: 이름, 심볼, 포맷된 잔액, 달러 환산 가치
- **ERC721 NFTs**: 썸네일 이미지, 컬렉션 이름, 토큰 ID
- **ERC1155 토큰**: 썸네일, 수량, 토큰 ID
- 타입별 필터 탭 또는 드롭다운

#### 2. Token Card Design
- **헤더**: 토큰 이름 + 타입 뱃지 (ERC20/ERC721/ERC1155)
- **메인 정보**: 잔액, 심볼, 컨트랙트 주소
- **NFT 메타데이터**: 이미지, 설명, 속성 (ERC721/ERC1155만)
- **액션 버튼**: "View on Explorer", "Send Token" (future)

#### 3. Grouping and Sorting
- **그룹핑**: 토큰 타입별 (ERC20 / NFTs)
- **정렬 옵션**:
  - Balance (high to low)
  - Token name (A-Z)
  - Recently received
- **검색**: 토큰 이름, 심볼, 컨트랙트 주소로 필터링

#### 4. Performance Optimization
- **이미지 레이지 로딩**: NFT 이미지는 뷰포트에 들어올 때만 로드
- **메타데이터 캐싱**: IPFS 메타데이터는 로컬 캐시 활용
- **가상화**: 토큰이 많을 경우 react-window 사용

#### 5. Error Handling
- 메타데이터 로드 실패 시 기본 이미지 표시
- IPFS 게이트웨이 실패 시 대체 게이트웨이 시도
- 소수점 변환 오류 시 원본 값 표시

---

## Address Balance API

주소의 네이티브 ETH 잔액을 조회하는 API입니다. **트랜잭션 처리 시 자동으로 잔액 변화를 추적합니다.**

### Query

```graphql
query AddressBalance($address: Address!, $blockNumber: BigInt) {
  addressBalance(address: $address, blockNumber: $blockNumber)
}
```

### Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| address | Address | Yes | 조회할 주소 |
| blockNumber | BigInt | No | 특정 블록 높이의 잔액 조회 (생략 시 최신 잔액) |

### Response

반환값은 Wei 단위의 BigInt 문자열입니다 (1 ETH = 10^18 Wei).

### Balance Tracking Implementation

네이티브 잔액 추적은 블록 인덱싱 중 자동으로 수행됩니다:

- **송신자 잔액**: `-(value + gas cost)` (트랜잭션 값 + 가스 비용 차감)
- **수신자 잔액**: `+value` (트랜잭션 값 증가)
- **가스 비용 계산**: `gasUsed * gasPrice`
- **컨트랙트 생성**: 컨트랙트 주소로 값 이전

### Request Examples

#### 1. 최신 잔액 조회
```json
{
  "query": "query AddressBalance($address: Address!) { addressBalance(address: $address) }",
  "variables": {
    "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb"
  }
}
```

#### 2. 특정 블록의 과거 잔액 조회
```json
{
  "query": "query AddressBalance($address: Address!, $blockNumber: BigInt) { addressBalance(address: $address, blockNumber: $blockNumber) }",
  "variables": {
    "address": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb",
    "blockNumber": "1000"
  }
}
```

### Response Example

```json
{
  "data": {
    "addressBalance": "5000000000000000000"
  }
}
```

이 값은 5 ETH를 의미합니다 (5 * 10^18 Wei).

### Balance History Query

잔액 변화 내역을 조회하려면 `balanceHistory` 쿼리를 사용하세요:

```graphql
query BalanceHistory($address: Address!, $fromBlock: BigInt, $toBlock: BigInt, $limit: Int) {
  balanceHistory(
    address: $address
    fromBlock: $fromBlock
    toBlock: $toBlock
    limit: $limit
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

### Frontend Integration Example (React + Apollo Client)

```typescript
import { useQuery, gql } from '@apollo/client';
import { formatEther } from 'ethers';

const ADDRESS_BALANCE_QUERY = gql`
  query AddressBalance($address: Address!, $blockNumber: BigInt) {
    addressBalance(address: $address, blockNumber: $blockNumber)
  }
`;

const BALANCE_HISTORY_QUERY = gql`
  query BalanceHistory($address: Address!, $limit: Int) {
    balanceHistory(address: $address, limit: $limit) {
      nodes {
        blockNumber
        balance
        delta
        txHash
      }
      totalCount
    }
  }
`;

interface BalanceHistoryNode {
  blockNumber: string;
  balance: string;
  delta: string;
  txHash: string;
}

function AddressBalanceComponent({ address }: { address: string }) {
  const [selectedBlock, setSelectedBlock] = useState<string>('');

  // Current balance query
  const { loading: balanceLoading, data: balanceData } = useQuery<{ addressBalance: string }>(
    ADDRESS_BALANCE_QUERY,
    {
      variables: {
        address,
        blockNumber: selectedBlock || undefined
      }
    }
  );

  // Balance history query
  const { loading: historyLoading, data: historyData } = useQuery<{
    balanceHistory: {
      nodes: BalanceHistoryNode[];
      totalCount: number;
    };
  }>(BALANCE_HISTORY_QUERY, {
    variables: {
      address,
      limit: 20
    }
  });

  const formatBalance = (weiValue: string) => {
    return `${formatEther(weiValue)} ETH`;
  };

  const formatDelta = (deltaWei: string) => {
    const value = formatEther(deltaWei);
    const isPositive = !deltaWei.startsWith('-');
    return `${isPositive ? '+' : ''}${value} ETH`;
  };

  return (
    <div>
      <h2>Address Balance</h2>
      <p className="address-display">{address}</p>

      {/* Current Balance Display */}
      <div className="balance-card">
        <h3>Current Balance</h3>
        {balanceLoading ? (
          <div>Loading...</div>
        ) : balanceData ? (
          <div className="balance-value">
            {formatBalance(balanceData.addressBalance)}
          </div>
        ) : (
          <div>No balance data available</div>
        )}

        {/* Historical Balance Selector */}
        <div className="block-selector">
          <label>
            View balance at block:
            <input
              type="text"
              value={selectedBlock}
              onChange={(e) => setSelectedBlock(e.target.value)}
              placeholder="Leave empty for latest"
            />
          </label>
        </div>
      </div>

      {/* Balance History */}
      <div className="balance-history">
        <h3>Balance History</h3>
        {historyLoading ? (
          <div>Loading history...</div>
        ) : historyData && historyData.balanceHistory.nodes.length > 0 ? (
          <>
            <p>Total changes: {historyData.balanceHistory.totalCount}</p>
            <table>
              <thead>
                <tr>
                  <th>Block</th>
                  <th>Balance</th>
                  <th>Change</th>
                  <th>Transaction</th>
                </tr>
              </thead>
              <tbody>
                {historyData.balanceHistory.nodes.map((node) => (
                  <tr key={`${node.blockNumber}-${node.txHash}`}>
                    <td>
                      <a href={`/block/${node.blockNumber}`}>#{node.blockNumber}</a>
                    </td>
                    <td>{formatBalance(node.balance)}</td>
                    <td className={node.delta.startsWith('-') ? 'negative' : 'positive'}>
                      {formatDelta(node.delta)}
                    </td>
                    <td>
                      <a href={`/tx/${node.txHash}`}>
                        {node.txHash.slice(0, 10)}...
                      </a>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        ) : (
          <div>No balance history available</div>
        )}
      </div>
    </div>
  );
}
```

### UI Design Recommendations

#### 1. Balance Display
- **큰 글씨**: 현재 잔액을 눈에 띄게 표시
- **USD 환산**: ETH 가격 API 연동하여 달러 환산 표시
- **과거 잔액 조회**: 블록 번호 입력으로 과거 잔액 확인

#### 2. Balance History Timeline
- **타임라인 뷰**: 시간순으로 잔액 변화 표시
- **차트**: 잔액 변화를 그래프로 시각화 (Line chart)
- **색상 코딩**:
  - 증가(+): 녹색
  - 감소(-): 빨간색
- **트랜잭션 링크**: 각 변화의 원인이 된 트랜잭션으로 링크

#### 3. Statistics
- **Total received**: 총 수신 금액
- **Total sent**: 총 발신 금액
- **Net change**: 순 변화량
- **Transaction count**: 트랜잭션 수

### Important Notes

⚠️ **잔액 추적은 새로운 기능입니다.** 기존 인덱스된 블록에는 잔액 데이터가 없을 수 있습니다.

**잔액 데이터를 채우는 방법:**

1. **새로운 인덱싱 시작**:
   ```bash
   ./indexer --clear-data --start-height 0
   ```

2. **진행 중인 인덱싱**: 새로 인덱스되는 블록부터 자동으로 잔액 추적이 활성화됩니다.

3. **Production 환경**:
   - 기존 데이터를 유지하면서 잔액 추적을 활성화하려면 별도의 마이그레이션 스크립트가 필요합니다.
   - 또는 과거 블록을 재인덱싱하여 잔액 데이터를 채울 수 있습니다.

**쿼리 결과 확인:**
- 잔액이 "0"으로 반환되는 경우, 해당 블록이 아직 재인덱싱되지 않았을 수 있습니다.
- `balanceHistory` 쿼리로 잔액 변화가 추적되고 있는지 확인하세요.

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

### 3. Balance History
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

## 지원

문제가 발생하거나 추가 기능이 필요한 경우:
- GitHub Issues: [프로젝트 저장소]
- 백엔드 팀에 문의

**최종 업데이트:** 2025-01-24

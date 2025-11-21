# Frontend Integration Guide - Stable-One Chain

> indexer-go API에서 Frontend 개발에 필요한 새로운 필드 및 기능 안내

**Last Updated**: 2025-11-21
**Status**: Active

---

## 개요

이 문서는 Stable-One 체인 특화 기능 개발로 인해 추가되는 API 필드와 기능을 Frontend 개발팀에 전달하기 위한 가이드입니다.

---

## 새로 추가된 API 필드

### 1. Block 타입 - EIP-1559 필드

```graphql
type Block {
  # 기존 필드...

  # NEW: EIP-1559 Base Fee
  baseFeePerGas: BigInt          # 블록의 기본 가스 요금 (post-London)

  # NEW: Withdrawal 관련 (post-Shanghai)
  withdrawalsRoot: Hash          # Withdrawal 머클 루트

  # NEW: Blob 관련 (EIP-4844)
  blobGasUsed: BigInt            # 블록에서 사용된 Blob 가스
  excessBlobGas: BigInt          # 초과 Blob 가스
}
```

**사용 예시:**

```graphql
query GetBlockWithGasInfo {
  block(number: "1000") {
    number
    hash
    gasUsed
    gasLimit
    baseFeePerGas    # EIP-1559 기본 가스 요금
    transactions {
      hash
      maxFeePerGas
      maxPriorityFeePerGas
    }
  }
}
```

**Frontend 표시 권장:**
- `baseFeePerGas`를 Gwei 단위로 변환하여 표시 (1 Gwei = 10^9 wei)
- 가스 요금 차트에서 시간별 baseFee 추이 시각화

---

### 2. Transaction 타입 - Fee Delegation 필드

Stable-One 체인은 Fee Delegation (타입 0x16)을 지원합니다. 이를 통해 제3자가 트랜잭션 수수료를 대신 지불할 수 있습니다.

```graphql
type Transaction {
  # 기존 필드...

  # NEW: Fee Delegation 필드 (type = 22/0x16인 경우)
  feePayer: Address              # 수수료 대납자 주소
  feePayerSignatures: [FeePayerSignature!]  # 대납자 서명
}

type FeePayerSignature {
  v: BigInt!
  r: Bytes!
  s: Bytes!
}
```

**사용 예시:**

```graphql
query GetFeeDelegatedTransaction {
  transaction(hash: "0x...") {
    hash
    from
    to
    value
    type
    gasPrice

    # Fee Delegation 정보
    feePayer           # 수수료 대납자
    feePayerSignatures {
      v
      r
      s
    }
  }
}
```

**Frontend 표시 권장:**
- `type == 22 (0x16)`인 경우 "Fee Delegated" 뱃지 표시
- `from` (발신자)와 `feePayer` (대납자)를 구분하여 표시
- 트랜잭션 상세에서 "실제 수수료 지불자" 섹션 추가

---

### 3. Transaction Type 상수

Stable-One에서 지원하는 트랜잭션 타입:

| Type | Hex | 이름 | 설명 |
|------|-----|------|------|
| 0 | 0x00 | Legacy | 기본 트랜잭션 |
| 1 | 0x01 | AccessList | EIP-2930 접근 목록 |
| 2 | 0x02 | DynamicFee | EIP-1559 동적 수수료 |
| 3 | 0x03 | Blob | EIP-4844 Blob 트랜잭션 |
| 22 | 0x16 | FeeDelegateDynamicFee | Fee Delegation |

**Frontend 표시 권장:**
- 타입별 색상 또는 아이콘 구분
- 타입 22는 특별히 강조 (Fee Delegation 지원 UI 차별화)

---

## 가스 요금 계산

### EIP-1559 가스 요금 계산

```javascript
// 실제 지불 가스 요금 계산
const effectiveGasPrice = Math.min(
  maxFeePerGas,
  baseFeePerGas + maxPriorityFeePerGas
);

// 트랜잭션 총 비용
const totalCost = effectiveGasPrice * gasUsed;

// 발신자에게 환불되는 금액
const refund = (maxFeePerGas - effectiveGasPrice) * gasUsed;
```

### Fee Delegation 트랜잭션

```javascript
// Fee Delegation의 경우
if (transaction.type === 22) {
  // 실제 가스 비용은 feePayer가 지불
  const paidBy = transaction.feePayer;
  const paidAmount = effectiveGasPrice * gasUsed;

  // UI에서 표시
  console.log(`가스 비용 ${paidAmount} wei는 ${paidBy}가 대납`);
}
```

---

## System Contracts & Governance API

Stable-One 체인의 시스템 컨트랙트 이벤트 및 거버넌스 기능을 조회할 수 있는 API가 추가되었습니다.

### 시스템 컨트랙트 주소

```javascript
const SYSTEM_CONTRACTS = {
  NativeCoinAdapter: "0x0000000000000000000000000000000000001000",
  GovValidator:      "0x0000000000000000000000000000000000001001",
  GovMasterMinter:   "0x0000000000000000000000000000000000001002",
  GovMinter:         "0x0000000000000000000000000000000000001003",
  GovCouncil:        "0x0000000000000000000000000000000000001004",
};
```

### 1. NativeCoinAdapter - 토큰 발행/소각

#### GraphQL API

```graphql
# 총 공급량 조회
query GetTotalSupply {
  totalSupply  # String (BigInt)
}

# 활성 Minter 목록
query GetActiveMinters {
  activeMinters {
    address      # Address
    allowance    # BigInt
    isActive     # Boolean
  }
}

# 특정 Minter의 한도 조회
query GetMinterAllowance($minter: Address!) {
  minterAllowance(minter: $minter)  # BigInt
}

# Mint 이벤트 조회
query GetMintEvents($filter: SystemContractEventFilter!, $pagination: PaginationInput) {
  mintEvents(filter: $filter, pagination: $pagination) {
    nodes {
      blockNumber
      transactionHash
      minter
      to
      amount
      timestamp
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}

# Burn 이벤트 조회
query GetBurnEvents($filter: SystemContractEventFilter!, $pagination: PaginationInput) {
  burnEvents(filter: $filter, pagination: $pagination) {
    nodes {
      blockNumber
      transactionHash
      burner
      amount
      timestamp
      withdrawalId    # Optional
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

#### JSON-RPC API

```javascript
// 총 공급량 조회
const response = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getTotalSupply',
    params: {},
    id: 1
  })
});
// => { totalSupply: "1000000000000000000000000" }

// 활성 Minter 목록
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getActiveMinters',
    params: {},
    id: 2
  })
});
// => { minters: [{ address: "0x...", allowance: "...", isActive: true }] }

// Mint 이벤트 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getMintEvents',
    params: {
      fromBlock: 0,
      toBlock: 1000,
      minter: "0x...",  // Optional
      limit: 10,
      offset: 0
    },
    id: 3
  })
});
// => { events: [...], totalCount: 100 }
```

### 2. Governance - 제안 및 투표

#### Proposal Status

```javascript
const ProposalStatus = {
  NONE: 'none',
  VOTING: 'voting',
  APPROVED: 'approved',
  EXECUTED: 'executed',
  CANCELLED: 'cancelled',
  EXPIRED: 'expired',
  FAILED: 'failed',
  REJECTED: 'rejected'
};
```

#### GraphQL API

```graphql
# 제안 목록 조회
query GetProposals($filter: ProposalFilter!, $pagination: PaginationInput) {
  proposals(filter: $filter, pagination: $pagination) {
    nodes {
      contract
      proposalId
      proposer
      actionType
      callData
      memberVersion
      requiredApprovals
      approved
      rejected
      status              # ProposalStatus enum
      createdAt
      executedAt
      blockNumber
      transactionHash
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}

# 특정 제안 상세 조회
query GetProposal($contract: Address!, $proposalId: BigInt!) {
  proposal(contract: $contract, proposalId: $proposalId) {
    proposalId
    proposer
    status
    approved
    rejected
    requiredApprovals
    # ... 나머지 필드
  }
}

# 제안 투표 내역
query GetProposalVotes($contract: Address!, $proposalId: BigInt!) {
  proposalVotes(contract: $contract, proposalId: $proposalId) {
    contract
    proposalId
    voter
    approval            # Boolean (true=찬성, false=반대)
    blockNumber
    transactionHash
    timestamp
  }
}
```

#### JSON-RPC API

```javascript
// 제안 목록 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getProposals',
    params: {
      contract: SYSTEM_CONTRACTS.GovCouncil,
      status: 'voting',  // Optional
      limit: 10,
      offset: 0
    },
    id: 1
  })
});
// => { proposals: [...], totalCount: 50 }

// 특정 제안 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getProposal',
    params: {
      contract: SYSTEM_CONTRACTS.GovCouncil,
      proposalId: "1"
    },
    id: 2
  })
});

// 투표 내역 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getProposalVotes',
    params: {
      contract: SYSTEM_CONTRACTS.GovCouncil,
      proposalId: "1"
    },
    id: 3
  })
});
// => { votes: [{ voter: "0x...", approval: true, ... }] }
```

### 3. Validator & Council 관리

#### GraphQL API

```graphql
# 활성 Validator 목록
query GetActiveValidators {
  activeValidators {
    address
    isActive
  }
}

# Blacklist 주소 목록
query GetBlacklistedAddresses {
  blacklistedAddresses  # [Address!]!
}
```

#### JSON-RPC API

```javascript
// 활성 Validator 목록
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getActiveValidators',
    params: {},
    id: 1
  })
});
// => { validators: [{ address: "0x...", isActive: true }] }

// Blacklist 주소 목록
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getBlacklistedAddresses',
    params: {},
    id: 2
  })
});
// => { addresses: ["0x...", "0x..."] }
```

### Frontend 구현 권장사항

#### 1. Governance Dashboard

```javascript
// 제안 상태별 색상 코딩
const statusColors = {
  voting: '#3B82F6',     // 파란색 - 투표 진행 중
  approved: '#10B981',   // 녹색 - 승인됨
  executed: '#6B7280',   // 회색 - 실행 완료
  rejected: '#EF4444',   // 빨간색 - 거부됨
  cancelled: '#6B7280',  // 회색 - 취소됨
  expired: '#F59E0B',    // 주황색 - 만료됨
  failed: '#DC2626',     // 진한 빨간색 - 실패
};

// 제안 진행률 계산
function calculateProgress(proposal) {
  const total = proposal.requiredApprovals;
  const current = proposal.approved;
  return (current / total) * 100;
}

// 제안 상태 체크
function canVote(proposal) {
  return proposal.status === 'voting';
}
```

#### 2. Token Supply Dashboard

```javascript
// 총 공급량 표시 (wei → 이더 단위 변환)
function formatSupply(supplyWei) {
  const ether = BigInt(supplyWei) / BigInt('1000000000000000000');
  return ether.toLocaleString('ko-KR');
}

// Minter 별 발행 한도 차트
function getMinterStats(minters) {
  return minters.map(m => ({
    name: m.address.slice(0, 10) + '...',
    allowance: Number(BigInt(m.allowance) / BigInt('1000000000000000000')),
    isActive: m.isActive
  }));
}
```

#### 3. 이벤트 모니터링

```javascript
// 실시간 Mint 이벤트 폴링
async function pollMintEvents() {
  const latestBlock = await getLatestHeight();

  const response = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getMintEvents',
      params: {
        fromBlock: latestBlock - 100,  // 최근 100블록
        toBlock: latestBlock,
        limit: 50
      },
      id: 1
    })
  });

  const { result } = await response.json();
  return result.events;
}

// 3초마다 업데이트
setInterval(pollMintEvents, 3000);
```

### 페이지네이션 처리

```javascript
// GraphQL
const ITEMS_PER_PAGE = 10;

function fetchProposalsPage(page, contract, status) {
  return gqlClient.query({
    query: GET_PROPOSALS,
    variables: {
      filter: {
        contract: contract,
        status: status || undefined
      },
      pagination: {
        limit: ITEMS_PER_PAGE,
        offset: page * ITEMS_PER_PAGE
      }
    }
  });
}

// JSON-RPC
async function fetchMintEventsPage(page) {
  const response = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getMintEvents',
      params: {
        limit: ITEMS_PER_PAGE,
        offset: page * ITEMS_PER_PAGE
      },
      id: 1
    })
  });

  const { result } = await response.json();
  return {
    events: result.events,
    totalCount: result.totalCount,
    hasNextPage: result.totalCount > (page + 1) * ITEMS_PER_PAGE
  };
}
```

---

## 예정된 추가 기능

### WebSocket 구독 설정

#### 백엔드 엔드포인트

**WebSocket URL**: `ws://localhost:8080/graphql/ws`

백엔드는 GraphQL Subscriptions를 위한 WebSocket 서버를 `/graphql/ws` 경로에서 제공합니다.

#### Apollo Client 설정 (React/Vue/Angular)

**1. 필수 패키지 설치**

```bash
npm install graphql-ws
# 또는
yarn add graphql-ws
```

**2. Apollo Client 설정**

```javascript
import { ApolloClient, InMemoryCache, HttpLink, split } from '@apollo/client';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { getMainDefinition } from '@apollo/client/utilities';
import { createClient } from 'graphql-ws';

// HTTP 링크 (Query & Mutation)
const httpLink = new HttpLink({
  uri: 'http://localhost:8080/api/v1/graphql'
});

// WebSocket 링크 (Subscription)
const wsLink = new GraphQLWsLink(createClient({
  url: 'ws://localhost:8080/graphql/ws',
  retryAttempts: 5,
  shouldRetry: () => true,
  connectionParams: {
    // 필요시 인증 토큰 추가
    // authToken: localStorage.getItem('token')
  }
}));

// HTTP와 WebSocket 자동 라우팅
const splitLink = split(
  ({ query }) => {
    const definition = getMainDefinition(query);
    return (
      definition.kind === 'OperationDefinition' &&
      definition.operation === 'subscription'
    );
  },
  wsLink,      // Subscription → WebSocket
  httpLink     // Query/Mutation → HTTP
);

// Apollo Client 생성
const client = new ApolloClient({
  link: splitLink,
  cache: new InMemoryCache()
});

export default client;
```

**3. React에서 Subscription 사용 예시**

```javascript
import { useSubscription, gql } from '@apollo/client';

const NEW_BLOCK_SUBSCRIPTION = gql`
  subscription OnNewBlock {
    newBlock {
      number
      hash
      timestamp
      gasUsed
      transactionCount
    }
  }
`;

function BlockMonitor() {
  const { data, loading, error } = useSubscription(NEW_BLOCK_SUBSCRIPTION);

  if (loading) return <p>연결 중...</p>;
  if (error) return <p>에러: {error.message}</p>;

  return (
    <div>
      <h2>최신 블록: #{data?.newBlock?.number}</h2>
      <p>해시: {data?.newBlock?.hash}</p>
      <p>트랜잭션: {data?.newBlock?.transactionCount}개</p>
    </div>
  );
}
```

#### 연결 문제 해결

**WebSocket 연결 실패 시 체크리스트**:

1. **엔드포인트 경로 확인**: `/ws` ❌ → `/graphql/ws` ✅
2. **프로토콜 확인**: `http://` 대신 `ws://` 사용
3. **포트 번호 확인**: 백엔드 포트와 일치 (기본값: 8080)
4. **CORS 설정**: 개발 환경에서 origin 확인
5. **방화벽/프록시**: WebSocket 트래픽 허용 여부 확인

**브라우저 콘솔에서 직접 테스트**:

```javascript
const ws = new WebSocket('ws://localhost:8080/graphql/ws', 'graphql-transport-ws');
ws.onopen = () => console.log('✅ WebSocket 연결 성공');
ws.onerror = (err) => console.error('❌ WebSocket 에러:', err);
```

---

### WebSocket 구독 확장 (적용 완료)

#### 1. `newPendingTransactions`

```graphql
subscription PendingTxStream {
  newPendingTransactions {
    hash
    from
    to
    value
    nonce
    gas
    type
    gasPrice
    maxFeePerGas
    maxPriorityFeePerGas
  }
}
```

- `type`은 `0x0`, `0x2`, `0x16` 등 Ethereum typed transaction 값입니다.
- `gasPrice`는 Legacy/1559 공통, 1559 타입은 `maxFeePerGas`, `maxPriorityFeePerGas`를 함께 조회하세요.
- 트랜잭션이 아직 블록에 포함되지 않았으므로 `blockNumber` 대신 `nonce`와 `gas` 정보로 UI를 구성하면 됩니다.

#### 2. `logs` 구독 & 필터 변수 예시

필터는 GraphQL **variables**에 전달해야 하며, address/topic/블록 범위를 모두 지원합니다.

```graphql
subscription FilteredLogs($filter: LogFilterInput) {
  logs(filter: $filter) {
    address
    topics
    data
    blockNumber
    blockHash
    transactionHash
    logIndex
    removed
  }
}
```

```json
{
  "filter": {
    "address": "0x1111...",              // 단일 주소
    "addresses": ["0x2222..."],        // OR 조건 추가 가능
    "topics": [
      "0xddf252ad...",                 // topic0 - Transfer
      ["0x0000...", "0xffff..."],      // topic1 - 다중 OR
      null,                              // wildcard
      null
    ],
    "fromBlock": "0xA",                 // hex 또는 decimal
    "toBlock":  "100"                  // decimal 허용
  }
}
```

- `address`와 `addresses`를 함께 쓰면 모든 값이 OR 조건으로 추가됩니다.
- `topics` 내부 배열은 **eth_subscribe logs** 규칙과 동일: `null`은 와일드카드, 배열은 OR, 문자열은 단일 매치.
- 블록 범위는 생략 시 최신 블록 전체 스트림을 받습니다.

---

### 향후 추가 예정 기능

#### WBFT 메타데이터 파싱

블록 헤더 Extra 필드에서 파싱된 WBFT 정보:

```graphql
type WBFTMetadata {
  round: Int!
  validators: [Address!]!
  signatures: [BLSSignature!]!
  committedSeal: Bytes!
}

type Block {
  # 기존 필드...
  wbftMetadata: WBFTMetadata  # WBFT 합의 메타데이터
}
```

---

## 마이그레이션 가이드

### 기존 API와의 호환성

- 모든 새 필드는 **선택적(nullable)** 으로 추가됨
- 기존 쿼리는 변경 없이 동작
- 새 필드를 사용하려면 쿼리에 명시적으로 추가 필요

### Breaking Changes

**없음** - 모든 변경사항은 하위 호환성 유지

---

## WBFT 합의 메타데이터 API

Stable-One 체인의 WBFT (Weighted Byzantine Fault Tolerance) 합의 메타데이터를 조회할 수 있는 API가 추가되었습니다.

### WBFT란?

WBFT는 Stable-One 체인이 사용하는 BFT (Byzantine Fault Tolerance) 합의 알고리즘입니다. 블록 생성 시 검증자들이 서명한 정보와 에폭(Epoch) 정보가 블록 헤더에 포함됩니다.

### 1. WBFT 블록 메타데이터 조회

#### GraphQL API

```graphql
# 블록 번호로 WBFT 메타데이터 조회
query GetWBFTBlockExtra($blockNumber: BigInt!) {
  wbftBlockExtra(blockNumber: $blockNumber) {
    blockNumber
    blockHash
    randaoReveal        # BLS 서명
    prevRound          # 이전 블록 라운드 번호
    round              # 현재 라운드 번호
    gasTip             # 거버넌스로 합의된 가스 팁
    timestamp

    # Prepare 단계 서명
    preparedSeal {
      sealers          # 서명한 검증자 비트맵
      signature        # 집계된 BLS 서명 (96 bytes)
    }

    # Commit 단계 서명
    committedSeal {
      sealers
      signature
    }

    # 이전 블록 서명 (재시도 시 사용)
    prevPreparedSeal {
      sealers
      signature
    }

    prevCommittedSeal {
      sealers
      signature
    }

    # 에폭 정보 (에폭 마지막 블록에만 존재)
    epochInfo {
      epochNumber
      blockNumber
      candidates {
        address         # 검증자 후보 주소
        diligence       # 성실도 점수 (10^-6 단위)
      }
      validators        # 검증자 인덱스 목록
      blsPublicKeys     # BLS 공개키 목록
    }
  }
}

# 블록 해시로 WBFT 메타데이터 조회
query GetWBFTBlockExtraByHash($blockHash: Hash!) {
  wbftBlockExtraByHash(blockHash: $blockHash) {
    # 위와 동일한 필드
  }
}
```

#### 사용 예시

```javascript
// 특정 블록의 WBFT 메타데이터 조회
const response = await fetch('/api/v1/graphql', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    query: `
      query GetWBFTBlockExtra($blockNumber: BigInt!) {
        wbftBlockExtra(blockNumber: $blockNumber) {
          blockNumber
          round
          gasTip
          preparedSeal {
            sealers
            signature
          }
          committedSeal {
            sealers
            signature
          }
          epochInfo {
            epochNumber
            candidates {
              address
              diligence
            }
          }
        }
      }
    `,
    variables: {
      blockNumber: "1000"
    }
  })
});

const { data } = await response.json();
```

#### JSON-RPC API

**메서드**: `getWBFTBlockExtra`, `getWBFTBlockExtraByHash`

```javascript
// 블록 번호로 WBFT 메타데이터 조회
const response = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getWBFTBlockExtra',
    params: {
      blockNumber: 1000  // 또는 "1000" (문자열도 지원)
    },
    id: 1
  })
});

const { result } = await response.json();
/*
{
  blockNumber: "1000",
  blockHash: "0x...",
  randaoReveal: "0x...",     // BLS 서명 (hex)
  round: 5,
  prevRound: 4,
  timestamp: "1700000000",
  gasTip: "1000000000",       // wei 단위
  preparedSeal: {
    sealers: "0x...",         // 비트맵 (hex)
    signature: "0x..."        // BLS 집계 서명
  },
  committedSeal: {
    sealers: "0x...",
    signature: "0x..."
  },
  prevPreparedSeal: { ... },  // 재시도 시에만 존재
  prevCommittedSeal: { ... },
  epochInfo: {                // 에폭 마지막 블록에만 존재
    epochNumber: "10",
    blockNumber: "1000",
    candidates: [
      {
        address: "0x...",
        diligence: "1000000"  // 10^-6 단위
      }
    ],
    validators: [0, 1, 2],
    blsPublicKeys: ["0x...", "0x..."]
  }
}
*/

// 블록 해시로 조회
const response2 = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getWBFTBlockExtraByHash',
    params: {
      blockHash: "0x1234..."
    },
    id: 1
  })
});
```

### 2. 에폭(Epoch) 정보 조회

#### GraphQL API

```graphql
# 특정 에폭 정보 조회
query GetEpochInfo($epochNumber: BigInt!) {
  epochInfo(epochNumber: $epochNumber) {
    epochNumber
    blockNumber         # 에폭 정보가 저장된 블록 번호
    candidates {
      address
      diligence
    }
    validators          # 다음 에폭의 검증자 인덱스
    blsPublicKeys      # 다음 에폭의 BLS 공개키
  }
}

# 최신 에폭 정보 조회
query GetLatestEpochInfo {
  latestEpochInfo {
    epochNumber
    blockNumber
    candidates {
      address
      diligence
    }
    validators
    blsPublicKeys
  }
}
```

#### 사용 예시

```javascript
// 최신 에폭 정보 조회
const response = await fetch('/api/v1/graphql', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    query: `
      query {
        latestEpochInfo {
          epochNumber
          blockNumber
          candidates {
            address
            diligence
          }
        }
      }
    `
  })
});
```

#### JSON-RPC API

**메서드**: `getEpochInfo`, `getLatestEpochInfo`

```javascript
// 특정 에폭 정보 조회
const response = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getEpochInfo',
    params: {
      epochNumber: 10  // 또는 "10" (문자열도 지원)
    },
    id: 1
  })
});

const { result } = await response.json();
/*
{
  epochNumber: "10",
  blockNumber: "1000",
  candidates: [
    {
      address: "0x...",
      diligence: "1000000"
    }
  ],
  validators: [0, 1, 2],
  blsPublicKeys: ["0x...", "0x..."]
}
*/

// 최신 에폭 정보 조회 (파라미터 불필요)
const response2 = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getLatestEpochInfo',
    params: {},
    id: 1
  })
});
```

### 3. 검증자 서명 통계 조회

#### GraphQL API

```graphql
# 특정 검증자의 서명 통계 조회
query GetValidatorSigningStats(
  $validatorAddress: Address!
  $fromBlock: BigInt!
  $toBlock: BigInt!
) {
  validatorSigningStats(
    validatorAddress: $validatorAddress
    fromBlock: $fromBlock
    toBlock: $toBlock
  ) {
    validatorAddress
    validatorIndex

    # Prepare 단계 통계
    prepareSignCount    # 서명한 횟수
    prepareMissCount    # 누락한 횟수

    # Commit 단계 통계
    commitSignCount
    commitMissCount

    # 블록 범위
    fromBlock
    toBlock

    # 서명률 (%)
    signingRate
  }
}

# 모든 검증자의 서명 통계 조회 (페이지네이션)
query GetAllValidatorsSigningStats(
  $fromBlock: BigInt!
  $toBlock: BigInt!
  $pagination: PaginationInput
) {
  allValidatorsSigningStats(
    fromBlock: $fromBlock
    toBlock: $toBlock
    pagination: $pagination
  ) {
    nodes {
      validatorAddress
      validatorIndex
      prepareSignCount
      prepareMissCount
      commitSignCount
      commitMissCount
      signingRate
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

#### 사용 예시

```javascript
// 특정 검증자의 서명 통계 조회
const response = await fetch('/api/v1/graphql', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    query: `
      query GetValidatorStats(
        $validator: Address!
        $fromBlock: BigInt!
        $toBlock: BigInt!
      ) {
        validatorSigningStats(
          validatorAddress: $validator
          fromBlock: $fromBlock
          toBlock: $toBlock
        ) {
          validatorAddress
          prepareSignCount
          prepareMissCount
          commitSignCount
          commitMissCount
          signingRate
        }
      }
    `,
    variables: {
      validator: "0x1234...",
      fromBlock: "0",
      toBlock: "1000"
    }
  })
});

const { data } = await response.json();
console.log(`서명률: ${data.validatorSigningStats.signingRate}%`);
```

#### JSON-RPC API

**메서드**: `getValidatorSigningStats`, `getAllValidatorsSigningStats`

```javascript
// 특정 검증자의 서명 통계 조회
const response = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getValidatorSigningStats',
    params: {
      validatorAddress: "0x1234...",
      fromBlock: 0,      // 또는 "0"
      toBlock: 1000      // 또는 "1000"
    },
    id: 1
  })
});

const { result } = await response.json();
/*
{
  validatorAddress: "0x1234...",
  validatorIndex: 0,
  prepareSignCount: "950",
  prepareMissCount: "50",
  commitSignCount: "945",
  commitMissCount: "55",
  fromBlock: "0",
  toBlock: "1000",
  signingRate: 94.5
}
*/

// 모든 검증자의 서명 통계 조회 (페이지네이션)
const response2 = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getAllValidatorsSigningStats',
    params: {
      fromBlock: 0,
      toBlock: 1000,
      limit: 100,        // 선택적 (기본값: 100, 최대: 100)
      offset: 0          // 선택적 (기본값: 0)
    },
    id: 1
  })
});

const { result: allStats } = await response2.json();
/*
[
  {
    validatorAddress: "0x1111...",
    validatorIndex: 0,
    prepareSignCount: "950",
    prepareMissCount: "50",
    commitSignCount: "945",
    commitMissCount: "55",
    fromBlock: "0",
    toBlock: "1000",
    signingRate: 94.5
  },
  ...
]
*/
```

### 4. 검증자 서명 활동 내역 조회

#### GraphQL API

```graphql
# 특정 검증자의 블록별 서명 활동 조회
query GetValidatorSigningActivity(
  $validatorAddress: Address!
  $fromBlock: BigInt!
  $toBlock: BigInt!
  $pagination: PaginationInput
) {
  validatorSigningActivity(
    validatorAddress: $validatorAddress
    fromBlock: $fromBlock
    toBlock: $toBlock
    pagination: $pagination
  ) {
    nodes {
      blockNumber
      blockHash
      validatorAddress
      validatorIndex
      signedPrepare     # Prepare 단계 서명 여부
      signedCommit      # Commit 단계 서명 여부
      round
      timestamp
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

#### 사용 예시

```javascript
// 검증자의 최근 활동 내역 조회
const response = await fetch('/api/v1/graphql', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    query: `
      query GetValidatorActivity(
        $validator: Address!
        $fromBlock: BigInt!
        $toBlock: BigInt!
      ) {
        validatorSigningActivity(
          validatorAddress: $validator
          fromBlock: $fromBlock
          toBlock: $toBlock
          pagination: { limit: 100, offset: 0 }
        ) {
          nodes {
            blockNumber
            signedPrepare
            signedCommit
            round
          }
          totalCount
        }
      }
    `,
    variables: {
      validator: "0x1234...",
      fromBlock: "900",
      toBlock: "1000"
    }
  })
});
```

#### JSON-RPC API

**메서드**: `getValidatorSigningActivity`

```javascript
// 특정 검증자의 블록별 서명 활동 조회
const response = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getValidatorSigningActivity',
    params: {
      validatorAddress: "0x1234...",
      fromBlock: 900,
      toBlock: 1000,
      limit: 100,        // 선택적 (기본값: 100, 최대: 100)
      offset: 0          // 선택적 (기본값: 0)
    },
    id: 1
  })
});

const { result } = await response.json();
/*
[
  {
    blockNumber: "950",
    blockHash: "0x...",
    validatorAddress: "0x1234...",
    validatorIndex: 0,
    signedPrepare: true,
    signedCommit: true,
    round: 1,
    timestamp: "1700000000"
  },
  {
    blockNumber: "951",
    blockHash: "0x...",
    validatorAddress: "0x1234...",
    validatorIndex: 0,
    signedPrepare: true,
    signedCommit: false,  // Commit 단계 누락
    round: 1,
    timestamp: "1700000003"
  },
  ...
]
*/
```

### 5. 블록 서명자 조회

#### GraphQL API

```graphql
# 특정 블록에 서명한 검증자 목록 조회
query GetBlockSigners($blockNumber: BigInt!) {
  blockSigners(blockNumber: $blockNumber) {
    blockNumber
    preparers       # Prepare 단계에 서명한 검증자 주소 목록
    committers      # Commit 단계에 서명한 검증자 주소 목록
  }
}
```

#### 사용 예시

```javascript
// 특정 블록의 서명자 조회
const response = await fetch('/api/v1/graphql', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    query: `
      query GetBlockSigners($blockNumber: BigInt!) {
        blockSigners(blockNumber: $blockNumber) {
          blockNumber
          preparers
          committers
        }
      }
    `,
    variables: {
      blockNumber: "1000"
    }
  })
});

const { data } = await response.json();
console.log(`Prepare 서명자 수: ${data.blockSigners.preparers.length}`);
console.log(`Commit 서명자 수: ${data.blockSigners.committers.length}`);
```

#### JSON-RPC API

**메서드**: `getBlockSigners`

```javascript
// 특정 블록에 서명한 검증자 목록 조회
const response = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getBlockSigners',
    params: {
      blockNumber: 1000  // 또는 "1000"
    },
    id: 1
  })
});

const { result } = await response.json();
/*
{
  preparers: [
    "0x1111111111111111111111111111111111111111",
    "0x2222222222222222222222222222222222222222",
    "0x3333333333333333333333333333333333333333"
  ],
  committers: [
    "0x1111111111111111111111111111111111111111",
    "0x2222222222222222222222222222222222222222",
    "0x4444444444444444444444444444444444444444"
  ]
}
*/

// 서명자 수 확인
console.log(`Prepare 서명자 수: ${result.preparers.length}`);
console.log(`Commit 서명자 수: ${result.committers.length}`);
```

### Frontend 구현 권장사항

#### 1. 검증자 대시보드

```javascript
// 검증자 모니터링 대시보드
async function fetchValidatorDashboard(validatorAddress, blocks = 1000) {
  const latestBlock = await getLatestHeight();
  const fromBlock = Math.max(0, latestBlock - blocks);

  // 검증자 통계 조회
  const statsResponse = await fetch('/api/v1/graphql', {
    method: 'POST',
    body: JSON.stringify({
      query: `
        query($validator: Address!, $from: BigInt!, $to: BigInt!) {
          validatorSigningStats(
            validatorAddress: $validator
            fromBlock: $from
            toBlock: $to
          ) {
            prepareSignCount
            prepareMissCount
            commitSignCount
            commitMissCount
            signingRate
          }
        }
      `,
      variables: {
        validator: validatorAddress,
        from: fromBlock.toString(),
        to: latestBlock.toString()
      }
    })
  });

  const { data } = await statsResponse.json();

  return {
    validator: validatorAddress,
    blocks: blocks,
    stats: data.validatorSigningStats,
    health: calculateHealth(data.validatorSigningStats)
  };
}

function calculateHealth(stats) {
  if (stats.signingRate >= 99) return 'excellent';
  if (stats.signingRate >= 95) return 'good';
  if (stats.signingRate >= 90) return 'fair';
  return 'poor';
}
```

#### 2. 블록 상세 정보

```javascript
// 블록 상세 정보에 WBFT 메타데이터 추가
async function fetchBlockDetails(blockNumber) {
  const response = await fetch('/api/v1/graphql', {
    method: 'POST',
    body: JSON.stringify({
      query: `
        query($blockNumber: BigInt!) {
          block(number: $blockNumber) {
            number
            hash
            timestamp
            gasUsed
            transactionCount
          }

          wbftBlockExtra(blockNumber: $blockNumber) {
            round
            gasTip
            epochInfo {
              epochNumber
            }
          }

          blockSigners(blockNumber: $blockNumber) {
            preparers
            committers
          }
        }
      `,
      variables: {
        blockNumber: blockNumber.toString()
      }
    })
  });

  const { data } = await response.json();

  return {
    ...data.block,
    wbft: data.wbftBlockExtra,
    signers: data.blockSigners
  };
}
```

#### 3. 에폭 전환 모니터링

```javascript
// 에폭 전환 감지 및 새 검증자 목록 표시
async function monitorEpochChanges() {
  const response = await fetch('/api/v1/graphql', {
    method: 'POST',
    body: JSON.stringify({
      query: `
        query {
          latestEpochInfo {
            epochNumber
            blockNumber
            candidates {
              address
              diligence
            }
          }
        }
      `
    })
  });

  const { data } = await response.json();
  const epoch = data.latestEpochInfo;

  // 검증자를 diligence 점수로 정렬
  const sortedValidators = [...epoch.candidates].sort(
    (a, b) => Number(b.diligence) - Number(a.diligence)
  );

  return {
    epochNumber: epoch.epochNumber,
    epochBlock: epoch.blockNumber,
    validatorCount: sortedValidators.length,
    topValidators: sortedValidators.slice(0, 10)
  };
}
```

#### 4. 검증자 성능 차트

```javascript
// 검증자 서명률 히스토리 차트 데이터
async function fetchValidatorPerformanceHistory(
  validatorAddress,
  fromBlock,
  toBlock,
  interval = 100  // 블록 간격
) {
  const chartData = [];

  for (let block = fromBlock; block <= toBlock; block += interval) {
    const endBlock = Math.min(block + interval - 1, toBlock);

    const response = await fetch('/api/v1/graphql', {
      method: 'POST',
      body: JSON.stringify({
        query: `
          query($validator: Address!, $from: BigInt!, $to: BigInt!) {
            validatorSigningStats(
              validatorAddress: $validator
              fromBlock: $from
              toBlock: $to
            ) {
              signingRate
              prepareSignCount
              commitSignCount
            }
          }
        `,
        variables: {
          validator: validatorAddress,
          from: block.toString(),
          to: endBlock.toString()
        }
      })
    });

    const { data } = await response.json();

    chartData.push({
      blockRange: `${block}-${endBlock}`,
      signingRate: data.validatorSigningStats.signingRate,
      prepareCount: data.validatorSigningStats.prepareSignCount,
      commitCount: data.validatorSigningStats.commitSignCount
    });
  }

  return chartData;
}
```

### 페이지네이션 처리

```javascript
// 검증자 목록 페이지네이션
const VALIDATORS_PER_PAGE = 20;

async function fetchValidatorsPage(page, fromBlock, toBlock) {
  const response = await fetch('/api/v1/graphql', {
    method: 'POST',
    body: JSON.stringify({
      query: `
        query($from: BigInt!, $to: BigInt!, $limit: Int!, $offset: Int!) {
          allValidatorsSigningStats(
            fromBlock: $from
            toBlock: $to
            pagination: { limit: $limit, offset: $offset }
          ) {
            nodes {
              validatorAddress
              validatorIndex
              signingRate
              prepareSignCount
              commitSignCount
            }
            totalCount
            pageInfo {
              hasNextPage
              hasPreviousPage
            }
          }
        }
      `,
      variables: {
        from: fromBlock.toString(),
        to: toBlock.toString(),
        limit: VALIDATORS_PER_PAGE,
        offset: page * VALIDATORS_PER_PAGE
      }
    })
  });

  const { data } = await response.json();

  return {
    validators: data.allValidatorsSigningStats.nodes,
    totalCount: data.allValidatorsSigningStats.totalCount,
    currentPage: page,
    totalPages: Math.ceil(
      data.allValidatorsSigningStats.totalCount / VALIDATORS_PER_PAGE
    ),
    hasNextPage: data.allValidatorsSigningStats.pageInfo.hasNextPage,
    hasPreviousPage: data.allValidatorsSigningStats.pageInfo.hasPreviousPage
  };
}
```

---

## Address Indexing API

컨트랙트 생성, 내부 트랜잭션, ERC20/ERC721 토큰 전송 등 주소 관련 활동을 추적하는 API가 추가되었습니다.

### 1. Contract Creation - 컨트랙트 생성 추적

#### GraphQL API

```graphql
# 특정 컨트랙트의 생성 정보 조회
query GetContractCreation($contractAddress: Address!) {
  contractCreation(contractAddress: $contractAddress) {
    contractAddress
    creator              # 컨트랙트 생성자 주소
    transactionHash
    blockNumber
    timestamp
    bytecodeSize        # 바이트코드 크기
  }
}

# 특정 주소가 생성한 컨트랙트 목록
query GetContractsByCreator(
  $creator: Address!
  $pagination: PaginationInput
) {
  contractsByCreator(
    creator: $creator
    pagination: $pagination
  ) {
    nodes              # 컨트랙트 주소 목록
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

#### JSON-RPC API

```javascript
// 컨트랙트 생성 정보 조회
const response = await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getContractCreation',
    params: {
      address: "0x1234567890123456789012345678901234567890"
    },
    id: 1
  })
});
// => {
//   contractAddress: "0x1234...",
//   creator: "0xabcd...",
//   transactionHash: "0x...",
//   blockNumber: 1000,
//   timestamp: 1234567890,
//   bytecodeSize: 2048
// }

// 생성자가 만든 컨트랙트 목록
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getContractsByCreator',
    params: {
      creator: "0xabcd...",
      limit: 10,
      offset: 0
    },
    id: 2
  })
});
// => { contracts: ["0x1111...", "0x2222..."], totalCount: 25 }
```

### 2. Internal Transactions - 내부 트랜잭션 추적

내부 트랜잭션은 컨트랙트 간 호출(CALL, DELEGATECALL 등)을 추적합니다.

#### GraphQL API

```graphql
# 특정 트랜잭션의 내부 트랜잭션 목록
query GetInternalTransactions($txHash: Hash!) {
  internalTransactions(txHash: $txHash) {
    transactionHash
    blockNumber
    index              # 내부 트랜잭션 인덱스
    type               # CALL, DELEGATECALL, STATICCALL, CREATE 등
    from
    to
    value
    gas
    gasUsed
    input
    output
    error              # 실패 시 에러 메시지
    depth              # 호출 깊이
  }
}

# 특정 주소와 관련된 내부 트랜잭션
query GetInternalTransactionsByAddress(
  $address: Address!
  $isFrom: Boolean!   # true: from 주소, false: to 주소
  $pagination: PaginationInput
) {
  internalTransactionsByAddress(
    address: $address
    isFrom: $isFrom
    pagination: $pagination
  ) {
    nodes {
      transactionHash
      blockNumber
      type
      from
      to
      value
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

#### JSON-RPC API

```javascript
// 트랜잭션의 내부 트랜잭션 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getInternalTransactions',
    params: {
      txHash: "0xabcdef..."
    },
    id: 1
  })
});
// => { internalTxs: [{...}, {...}] }

// 주소별 내부 트랜잭션 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getInternalTransactionsByAddress',
    params: {
      address: "0x1234...",
      isFrom: true,    // from 주소로 필터링
      limit: 10,
      offset: 0
    },
    id: 2
  })
});
// => { internalTxs: [...], totalCount: 150 }
```

### 3. ERC20 Token Transfers - ERC20 토큰 전송 추적

#### GraphQL API

```graphql
# 특정 ERC20 전송 조회
query GetERC20Transfer($txHash: Hash!, $logIndex: Int!) {
  erc20Transfer(txHash: $txHash, logIndex: $logIndex) {
    tokenAddress       # ERC20 토큰 컨트랙트 주소
    from
    to
    value              # 전송량
    transactionHash
    blockNumber
    logIndex
    timestamp
  }
}

# 특정 토큰의 전송 내역
query GetERC20TransfersByToken(
  $tokenAddress: Address!
  $pagination: PaginationInput
) {
  erc20TransfersByToken(
    tokenAddress: $tokenAddress
    pagination: $pagination
  ) {
    nodes {
      from
      to
      value
      transactionHash
      timestamp
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}

# 특정 주소의 ERC20 전송 내역
query GetERC20TransfersByAddress(
  $address: Address!
  $isFrom: Boolean!   # true: from, false: to
  $pagination: PaginationInput
) {
  erc20TransfersByAddress(
    address: $address
    isFrom: $isFrom
    pagination: $pagination
  ) {
    nodes {
      tokenAddress
      from
      to
      value
      transactionHash
      timestamp
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

#### JSON-RPC API

```javascript
// 특정 ERC20 전송 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getERC20Transfer',
    params: {
      txHash: "0xabcd...",
      logIndex: 2
    },
    id: 1
  })
});
// => {
//   tokenAddress: "0x...",
//   from: "0x...",
//   to: "0x...",
//   value: "1000000000000000000",  // 1 token (18 decimals)
//   ...
// }

// 토큰별 전송 내역
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getERC20TransfersByToken',
    params: {
      tokenAddress: "0x...",
      limit: 20,
      offset: 0
    },
    id: 2
  })
});

// 주소별 ERC20 수신 내역
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getERC20TransfersByAddress',
    params: {
      address: "0x...",
      isFrom: false,   // to 주소로 필터링 (수신)
      limit: 20,
      offset: 0
    },
    id: 3
  })
});
```

### 4. ERC721 Token Transfers - NFT 전송 추적

#### GraphQL API

```graphql
# 특정 ERC721 전송 조회
query GetERC721Transfer($txHash: Hash!, $logIndex: Int!) {
  erc721Transfer(txHash: $txHash, logIndex: $logIndex) {
    tokenAddress       # NFT 컨트랙트 주소
    from
    to
    tokenId            # NFT 토큰 ID
    transactionHash
    blockNumber
    logIndex
    timestamp
  }
}

# 특정 NFT 컬렉션의 전송 내역
query GetERC721TransfersByToken(
  $tokenAddress: Address!
  $pagination: PaginationInput
) {
  erc721TransfersByToken(
    tokenAddress: $tokenAddress
    pagination: $pagination
  ) {
    nodes {
      from
      to
      tokenId
      transactionHash
      timestamp
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}

# 특정 주소의 NFT 전송 내역
query GetERC721TransfersByAddress(
  $address: Address!
  $isFrom: Boolean!   # true: from, false: to
  $pagination: PaginationInput
) {
  erc721TransfersByAddress(
    address: $address
    isFrom: $isFrom
    pagination: $pagination
  ) {
    nodes {
      tokenAddress
      from
      to
      tokenId
      transactionHash
      timestamp
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}

# 특정 NFT의 현재 소유자 조회
query GetERC721Owner(
  $tokenAddress: Address!
  $tokenId: BigInt!
) {
  erc721Owner(
    tokenAddress: $tokenAddress
    tokenId: $tokenId
  )  # 소유자 주소 반환
}
```

#### JSON-RPC API

```javascript
// 특정 ERC721 전송 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getERC721Transfer',
    params: {
      txHash: "0xabcd...",
      logIndex: 1
    },
    id: 1
  })
});
// => {
//   tokenAddress: "0x...",
//   from: "0x...",
//   to: "0x...",
//   tokenId: "42",
//   ...
// }

// NFT 컬렉션 전송 내역
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getERC721TransfersByToken',
    params: {
      tokenAddress: "0x...",
      limit: 20,
      offset: 0
    },
    id: 2
  })
});

// 주소별 NFT 수신 내역
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getERC721TransfersByAddress',
    params: {
      address: "0x...",
      isFrom: false,   // to 주소로 필터링 (수신)
      limit: 20,
      offset: 0
    },
    id: 3
  })
});

// NFT 소유자 조회
await fetch('/api/v1/jsonrpc', {
  method: 'POST',
  body: JSON.stringify({
    jsonrpc: '2.0',
    method: 'getERC721Owner',
    params: {
      tokenAddress: "0x...",
      tokenId: "42"
    },
    id: 4
  })
});
// => { owner: "0x1234..." }
```

### Frontend 구현 권장사항

#### 1. 주소 프로필 페이지

```javascript
// 특정 주소의 모든 활동 조회
async function fetchAddressProfile(address) {
  // 생성한 컨트랙트 조회
  const contracts = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getContractsByCreator',
      params: { creator: address, limit: 10, offset: 0 },
      id: 1
    })
  });

  // 내부 트랜잭션 (발신)
  const internalTxsFrom = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getInternalTransactionsByAddress',
      params: { address, isFrom: true, limit: 10, offset: 0 },
      id: 2
    })
  });

  // ERC20 토큰 전송 내역
  const erc20Transfers = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getERC20TransfersByAddress',
      params: { address, isFrom: false, limit: 10, offset: 0 },
      id: 3
    })
  });

  // NFT 보유 내역
  const nftTransfers = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getERC721TransfersByAddress',
      params: { address, isFrom: false, limit: 10, offset: 0 },
      id: 4
    })
  });

  return {
    address,
    contracts: (await contracts.json()).result,
    internalTxs: (await internalTxsFrom.json()).result,
    erc20: (await erc20Transfers.json()).result,
    nfts: (await nftTransfers.json()).result
  };
}
```

#### 2. 토큰 전송 히스토리

```javascript
// ERC20/ERC721 전송 내역 표시
function formatTokenTransfer(transfer, tokenInfo) {
  const isERC20 = 'value' in transfer;

  if (isERC20) {
    // ERC20: value를 토큰 단위로 변환
    const decimals = tokenInfo.decimals || 18;
    const amount = BigInt(transfer.value) / BigInt(10 ** decimals);

    return {
      type: 'ERC20',
      token: tokenInfo.symbol,
      from: transfer.from,
      to: transfer.to,
      amount: amount.toString(),
      txHash: transfer.transactionHash
    };
  } else {
    // ERC721: tokenId 표시
    return {
      type: 'NFT',
      collection: tokenInfo.name,
      from: transfer.from,
      to: transfer.to,
      tokenId: transfer.tokenId,
      txHash: transfer.transactionHash
    };
  }
}
```

#### 3. 컨트랙트 생성 타임라인

```javascript
// 생성자가 만든 컨트랙트 타임라인
async function fetchContractCreationTimeline(creator) {
  const response = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getContractsByCreator',
      params: {
        creator: creator,
        limit: 100,
        offset: 0
      },
      id: 1
    })
  });

  const { result } = await response.json();

  // 각 컨트랙트의 상세 정보 조회
  const details = await Promise.all(
    result.contracts.map(async (contractAddr) => {
      const detailResponse = await fetch('/api/v1/jsonrpc', {
        method: 'POST',
        body: JSON.stringify({
          jsonrpc: '2.0',
          method: 'getContractCreation',
          params: { address: contractAddr },
          id: 2
        })
      });

      const { result } = await detailResponse.json();
      return result;
    })
  );

  // 시간순 정렬
  return details.sort((a, b) => a.timestamp - b.timestamp);
}
```

#### 4. 내부 트랜잭션 트레이서

```javascript
// 트랜잭션의 내부 호출 트리 구성
async function buildInternalTxTree(txHash) {
  const response = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getInternalTransactions',
      params: { txHash },
      id: 1
    })
  });

  const { result } = await response.json();
  const internals = result.internalTxs;

  // depth 기반으로 트리 구성
  const tree = [];
  const stack = [];

  internals.forEach(tx => {
    const node = {
      ...tx,
      children: []
    };

    // depth 레벨에 맞게 부모 찾기
    while (stack.length > tx.depth) {
      stack.pop();
    }

    if (stack.length === 0) {
      tree.push(node);
    } else {
      stack[stack.length - 1].children.push(node);
    }

    stack.push(node);
  });

  return tree;
}

// 트리를 재귀적으로 렌더링
function renderInternalTxTree(nodes, indent = 0) {
  return nodes.map(node => (
    <div style={{ marginLeft: indent * 20 }}>
      <span>{node.type}</span>
      <span>{node.from} → {node.to}</span>
      <span>{node.value} wei</span>
      {node.error && <span className="error">{node.error}</span>}
      {node.children.length > 0 && renderInternalTxTree(node.children, indent + 1)}
    </div>
  ));
}
```

#### 5. NFT 소유권 추적

```javascript
// NFT 소유권 변경 히스토리 및 현재 소유자
async function fetchNFTOwnership(tokenAddress, tokenId) {
  // 현재 소유자
  const ownerResponse = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: 'getERC721Owner',
      params: {
        tokenAddress,
        tokenId
      },
      id: 1
    })
  });

  const { result: ownerResult } = await ownerResponse.json();
  const currentOwner = ownerResult.owner;

  // 전송 히스토리 (GraphQL)
  const historyResponse = await fetch('/api/v1/graphql', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      query: `
        query GetNFTTransfers($token: Address!, $limit: Int!) {
          erc721TransfersByToken(
            tokenAddress: $token
            pagination: { limit: $limit, offset: 0 }
          ) {
            nodes {
              from
              to
              tokenId
              transactionHash
              timestamp
            }
          }
        }
      `,
      variables: {
        token: tokenAddress,
        limit: 100
      }
    })
  });

  const { data } = await historyResponse.json();
  const transfers = data.erc721TransfersByToken.nodes;

  // 특정 tokenId만 필터링
  const tokenTransfers = transfers.filter(t => t.tokenId === tokenId);

  return {
    currentOwner,
    transferHistory: tokenTransfers,
    transferCount: tokenTransfers.length
  };
}
```

### 페이지네이션 처리

```javascript
// Address Indexing API 페이지네이션 유틸리티
const ITEMS_PER_PAGE = 20;
const MAX_ITEMS_PER_REQUEST = 100;  // API 최대 limit

async function fetchPaginatedData(method, params, page) {
  const response = await fetch('/api/v1/jsonrpc', {
    method: 'POST',
    body: JSON.stringify({
      jsonrpc: '2.0',
      method: method,
      params: {
        ...params,
        limit: Math.min(ITEMS_PER_PAGE, MAX_ITEMS_PER_REQUEST),
        offset: page * ITEMS_PER_PAGE
      },
      id: Date.now()
    })
  });

  const { result } = await response.json();

  return {
    items: result[Object.keys(result)[0]],  // contracts, internalTxs, 등
    totalCount: result.totalCount,
    currentPage: page,
    totalPages: Math.ceil(result.totalCount / ITEMS_PER_PAGE),
    hasNextPage: result.totalCount > (page + 1) * ITEMS_PER_PAGE,
    hasPreviousPage: page > 0
  };
}

// 사용 예시
async function loadERC20TransfersPage(tokenAddress, page) {
  return fetchPaginatedData(
    'getERC20TransfersByToken',
    { tokenAddress },
    page
  );
}
```

### 필터링 및 정렬

```javascript
// 토큰 전송 필터링
function filterTokenTransfers(transfers, filters) {
  return transfers.filter(transfer => {
    // 금액 필터 (ERC20)
    if (filters.minValue && transfer.value) {
      if (BigInt(transfer.value) < BigInt(filters.minValue)) {
        return false;
      }
    }

    // 날짜 범위 필터
    if (filters.fromDate && transfer.timestamp < filters.fromDate) {
      return false;
    }
    if (filters.toDate && transfer.timestamp > filters.toDate) {
      return false;
    }

    // 주소 필터
    if (filters.fromAddress && transfer.from !== filters.fromAddress) {
      return false;
    }
    if (filters.toAddress && transfer.to !== filters.toAddress) {
      return false;
    }

    return true;
  });
}

// 정렬
function sortTransfers(transfers, sortBy = 'timestamp', order = 'desc') {
  return [...transfers].sort((a, b) => {
    let comparison = 0;

    switch (sortBy) {
      case 'timestamp':
        comparison = a.timestamp - b.timestamp;
        break;
      case 'value':
        comparison = Number(BigInt(a.value || 0) - BigInt(b.value || 0));
        break;
      case 'blockNumber':
        comparison = a.blockNumber - b.blockNumber;
        break;
    }

    return order === 'desc' ? -comparison : comparison;
  });
}
```

---

## 구현 현황

| 기능 | 상태 | GraphQL | JSON-RPC | 비고 |
|------|------|---------|----------|------|
| baseFeePerGas | ✅ 완료 | ✅ | ✅ | Block 타입, EIP-1559 |
| withdrawalsRoot | ✅ 완료 | ✅ | ✅ | Post-Shanghai |
| blobGasUsed | ✅ 완료 | ✅ | ✅ | EIP-4844 |
| excessBlobGas | ✅ 완료 | ✅ | ✅ | EIP-4844 |
| feePayer | ✅ 완료 | ✅ | ✅ | Fee Delegation, go-stablenet 연동 완료 |
| feePayerSignatures | ✅ 완료 | ✅ | ✅ | Fee Delegation, go-stablenet 연동 완료 |
| newPendingTransactions | ✅ 적용 | WebSocket | GraphQL Subscription | 실시간 펜딩 트랜잭션 스트림 |
| logs subscription | ✅ 적용 | WebSocket | GraphQL Subscription | 주소 & 토픽 필터 지원 |
| **System Contracts** | | | | |
| totalSupply | ✅ 완료 | ✅ | ✅ | NativeCoinAdapter 총 공급량 |
| activeMinters | ✅ 완료 | ✅ | ✅ | 활성 Minter 목록 |
| minterAllowance | ✅ 완료 | ✅ | ✅ | Minter별 한도 조회 |
| mintEvents | ✅ 완료 | ✅ | ✅ | Mint 이벤트 조회, 페이지네이션 지원 |
| burnEvents | ✅ 완료 | ✅ | ✅ | Burn 이벤트 조회, 페이지네이션 지원 |
| **Governance** | | | | |
| proposals | ✅ 완료 | ✅ | ✅ | 거버넌스 제안 목록 |
| proposal | ✅ 완료 | ✅ | ✅ | 특정 제안 상세 조회 |
| proposalVotes | ✅ 완료 | ✅ | ✅ | 제안 투표 내역 |
| activeValidators | ✅ 완료 | ✅ | ✅ | 활성 Validator 목록 |
| blacklistedAddresses | ✅ 완료 | ✅ | ✅ | 블랙리스트 주소 목록 |
| **WBFT Consensus** | | | | |
| wbftBlockExtra | ✅ 완료 | ✅ | - | 블록 WBFT 메타데이터 (번호로 조회) |
| wbftBlockExtraByHash | ✅ 완료 | ✅ | - | 블록 WBFT 메타데이터 (해시로 조회) |
| epochInfo | ✅ 완료 | ✅ | - | 특정 에폭 정보 조회 |
| latestEpochInfo | ✅ 완료 | ✅ | - | 최신 에폭 정보 조회 |
| validatorSigningStats | ✅ 완료 | ✅ | - | 검증자 서명 통계 |
| allValidatorsSigningStats | ✅ 완료 | ✅ | - | 전체 검증자 서명 통계 (페이지네이션) |
| validatorSigningActivity | ✅ 완료 | ✅ | - | 검증자 서명 활동 내역 (페이지네이션) |
| blockSigners | ✅ 완료 | ✅ | - | 블록 서명자 목록 (Prepare/Commit) |
| **Address Indexing** | | | | |
| contractCreation | ✅ 완료 | ✅ | ✅ | 컨트랙트 생성 정보 조회 |
| contractsByCreator | ✅ 완료 | ✅ | ✅ | 생성자별 컨트랙트 목록 (페이지네이션) |
| internalTransactions | ✅ 완료 | ✅ | ✅ | 트랜잭션의 내부 트랜잭션 목록 |
| internalTransactionsByAddress | ✅ 완료 | ✅ | ✅ | 주소별 내부 트랜잭션 (페이지네이션) |
| erc20Transfer | ✅ 완료 | ✅ | ✅ | 특정 ERC20 전송 조회 |
| erc20TransfersByToken | ✅ 완료 | ✅ | ✅ | 토큰별 ERC20 전송 내역 (페이지네이션) |
| erc20TransfersByAddress | ✅ 완료 | ✅ | ✅ | 주소별 ERC20 전송 내역 (페이지네이션) |
| erc721Transfer | ✅ 완료 | ✅ | ✅ | 특정 ERC721 전송 조회 |
| erc721TransfersByToken | ✅ 완료 | ✅ | ✅ | NFT 컬렉션별 전송 내역 (페이지네이션) |
| erc721TransfersByAddress | ✅ 완료 | ✅ | ✅ | 주소별 NFT 전송 내역 (페이지네이션) |
| erc721Owner | ✅ 완료 | ✅ | ✅ | NFT 현재 소유자 조회 |

**Note**:
- 모든 Fee Delegation 필드는 go-stablenet의 `Transaction.FeePayer()` 및 `Transaction.RawFeePayerSignatureValues()` 메서드를 통해 실제 값을 추출합니다.
- System Contract 쿼리는 시스템 컨트랙트 주소 (0x1000-0x1004)의 이벤트 및 상태를 조회합니다.
- **WBFT API는 GraphQL과 JSON-RPC를 모두 지원합니다.** (2025-11-21 JSON-RPC 지원 추가)
- **Address Indexing API**는 컨트랙트 생성, 내부 트랜잭션, ERC20/ERC721 토큰 전송을 추적합니다. 모든 페이지네이션 API는 최대 100개 limit를 지원합니다.
- **WebSocket 엔드포인트**: GraphQL Subscriptions는 `ws://localhost:8080/graphql/ws` 경로에서 제공됩니다.

---

## 문의

추가 필드 요청이나 API 관련 문의는 백엔드 팀으로 연락해주세요.

---

## 변경 이력

| 날짜 | 버전 | 변경 내용 |
|------|------|----------|
| 2025-11-21 | 0.7.0 | WBFT JSON-RPC API 추가 - GraphQL과 동일한 8개 메서드 지원 (getWBFTBlockExtra, getEpochInfo 등) |
| 2025-11-21 | 0.6.0 | Address Indexing API 추가 (GraphQL, JSON-RPC) - 컨트랙트 생성, 내부 트랜잭션, ERC20/ERC721 토큰 전송 추적 (11개 쿼리) |
| 2025-11-21 | 0.5.0 | WBFT 합의 메타데이터 API 추가 (GraphQL) - 블록 메타데이터, 에폭 정보, 검증자 서명 통계 |
| 2025-11-21 | 0.4.0 | System Contracts & Governance API 추가 (GraphQL, JSON-RPC) |
| 2025-11-20 | 0.3.0 | go-stablenet 연동으로 Fee Delegation 실제 값 추출 구현 |
| 2025-11-20 | 0.2.0 | GraphQL 스키마 구현 완료 (EIP-1559, Fee Delegation) |
| 2025-11-20 | 0.1.0 | 초안 작성, EIP-1559 및 Fee Delegation 필드 정의 |

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

### Phase 4 완료 후 추가 예정

#### 1. NativeCoinAdapter 이벤트

베이스 코인(스테이블 토큰) 관련 이벤트:

```graphql
type NativeCoinEvent {
  eventType: String!        # Transfer, Mint, Burn
  from: Address
  to: Address
  value: BigInt!
  blockNumber: BigInt!
  transactionHash: Hash!
}

query GetNativeCoinEvents {
  nativeCoinEvents(
    address: "0x..."
    fromBlock: "0"
    toBlock: "latest"
  ) {
    eventType
    from
    to
    value
  }
}
```

#### 2. Gov 컨트랙트 정보

```graphql
type ValidatorInfo {
  address: Address!
  blsPublicKey: Bytes!
  isActive: Boolean!
  since: BigInt!
}

query GetValidators {
  validators {
    address
    blsPublicKey
    isActive
  }
}
```

#### 3. WBFT 메타데이터

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

**Note**:
- 모든 Fee Delegation 필드는 go-stablenet의 `Transaction.FeePayer()` 및 `Transaction.RawFeePayerSignatureValues()` 메서드를 통해 실제 값을 추출합니다.
- System Contract 쿼리는 시스템 컨트랙트 주소 (0x1000-0x1004)의 이벤트 및 상태를 조회합니다.

---

## 문의

추가 필드 요청이나 API 관련 문의는 백엔드 팀으로 연락해주세요.

---

## 변경 이력

| 날짜 | 버전 | 변경 내용 |
|------|------|----------|
| 2025-11-21 | 0.4.0 | System Contracts & Governance API 추가 (GraphQL, JSON-RPC) |
| 2025-11-20 | 0.3.0 | go-stablenet 연동으로 Fee Delegation 실제 값 추출 구현 |
| 2025-11-20 | 0.2.0 | GraphQL 스키마 구현 완료 (EIP-1559, Fee Delegation) |
| 2025-11-20 | 0.1.0 | 초안 작성, EIP-1559 및 Fee Delegation 필드 정의 |

# Frontend Integration Guide - Stable-One Chain

> indexer-go API에서 Frontend 개발에 필요한 새로운 필드 및 기능 안내

**Last Updated**: 2025-11-20
**Status**: In Progress

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

**Note**: 모든 Fee Delegation 필드는 go-stablenet의 `Transaction.FeePayer()` 및 `Transaction.RawFeePayerSignatureValues()` 메서드를 통해 실제 값을 추출합니다. type 0x16 (22) 트랜잭션에서 자동으로 feePayer 주소와 서명 값이 반환됩니다.

---

## 문의

추가 필드 요청이나 API 관련 문의는 백엔드 팀으로 연락해주세요.

---

## 변경 이력

| 날짜 | 버전 | 변경 내용 |
|------|------|----------|
| 2025-11-20 | 0.3.0 | go-stablenet 연동으로 Fee Delegation 실제 값 추출 구현 |
| 2025-11-20 | 0.2.0 | GraphQL 스키마 구현 완료 (EIP-1559, Fee Delegation) |
| 2025-11-20 | 0.1.0 | 초안 작성, EIP-1559 및 Fee Delegation 필드 정의 |

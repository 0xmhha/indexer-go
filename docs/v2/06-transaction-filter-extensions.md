# 06: 트랜잭션 필터 확장 (methodId, gasUsed, direction, time)

> **Priority**: Medium (P2-P3)
> **Effort**: Medium
> **Impact**: AdvancedTransactionFilters 나머지 필터 활성화
> **Blocking**: `components/transactions/AdvancedTransactionFilters.tsx`

## 현재 상태

### 지원되는 필터 (5개)
| Filter | GraphQL | Storage |
|--------|---------|---------|
| `fromBlock`/`toBlock` | BigInt! | uint64 |
| `minValue`/`maxValue` | BigInt | *big.Int |
| `txType` (0=all, 1=sent, 2=received) | Int | TransactionType |
| `successOnly` | Boolean | bool |

### 미지원 필터 (5개)
| Filter | 설명 | 복잡도 |
|--------|------|--------|
| `methodId` | 트랜잭션 input data의 첫 4바이트 | Medium |
| `minGasUsed`/`maxGasUsed` | receipt의 gasUsed 필터 | Medium |
| `direction` (SENT/RECEIVED) | 기존 txType과 중복, enum으로 표현 | Very Low |
| `fromTime`/`toTime` | 시간 기반 필터 → 블록 범위 변환 | Low |

## 각 필터 상세 분석

### 6-A: direction 필터

**현재**: `txType: Int` (0=all, 1=sent, 2=received)
**요구**: `direction: TransactionDirection` enum (SENT, RECEIVED, ALL)

이미 `storage.TransactionType`으로 동일 기능이 구현되어 있음. GraphQL 레벨에서 enum 매핑만 추가.

```go
// 기존
const (
    TxTypeAll      TransactionType = iota // 0
    TxTypeSent                            // 1
    TxTypeReceived                        // 2
)
```

**구현**: GraphQL `TransactionDirection` enum 정의 + 파서에서 enum → `TransactionType` 매핑

```graphql
enum TransactionDirection {
  SENT
  RECEIVED
  ALL
}

input HistoricalTransactionFilter {
  # ... 기존 필드
  direction: TransactionDirection  # txType의 enum 버전
}
```

```go
// parseHistoricalTransactionFilter에 추가
if direction, ok := args["direction"].(string); ok {
    switch direction {
    case "SENT":
        filter.TxType = storage.TxTypeSent
    case "RECEIVED":
        filter.TxType = storage.TxTypeReceived
    case "ALL":
        filter.TxType = storage.TxTypeAll
    }
}
```

**주의**: `txType`과 `direction`이 동시 지정되면 `direction` 우선.

### 6-B: methodId 필터

**목적**: 특정 function selector (0x + 8 hex chars)로 트랜잭션 필터링

**구현 위치**: `TransactionFilter`에 `MethodID string` 필드 추가

```go
type TransactionFilter struct {
    // ... 기존 필드
    MethodID string // function selector (first 4 bytes of input data)
}
```

**MatchTransaction 수정 (`historical.go`)**:

```go
// Check methodId
if f.MethodID != "" {
    inputData := tx.Data()
    if len(inputData) < 4 {
        return false // input이 4바이트 미만이면 매치 불가
    }
    txMethodID := fmt.Sprintf("0x%x", inputData[:4])
    if !strings.EqualFold(txMethodID, f.MethodID) {
        return false
    }
}
```

**또는 storage 레벨에서 처리** (pebble_historical.go의 `GetTransactionsByAddressFiltered` 내):

```go
// MatchTransaction 이후 추가 체크
if filter.MethodID != "" {
    inputData := tx.Data()
    if len(inputData) < 4 {
        continue
    }
    txMethodID := fmt.Sprintf("0x%x", inputData[:4])
    if !strings.EqualFold(txMethodID, filter.MethodID) {
        continue
    }
}
```

**추천**: `MatchTransaction` 메서드에 직접 추가 (순수 함수로 유지 가능, storage 접근 불필요)

### 6-C: minGasUsed / maxGasUsed 필터

**목적**: 트랜잭션의 실제 gas 사용량으로 필터링

**핵심**: gas 사용량은 `Receipt.GasUsed`에 있음. `MatchTransaction`은 receipt를 이미 파라미터로 받고 있음.

```go
type TransactionFilter struct {
    // ... 기존 필드
    MinGasUsed *uint64 // 최소 gas 사용량
    MaxGasUsed *uint64 // 최대 gas 사용량
}
```

**MatchTransaction 수정**:

```go
// Check gas used range
if f.MinGasUsed != nil && receipt != nil {
    if receipt.GasUsed < *f.MinGasUsed {
        return false
    }
}
if f.MaxGasUsed != nil && receipt != nil {
    if receipt.GasUsed > *f.MaxGasUsed {
        return false
    }
}
```

**주의**: receipt가 nil인 경우 gas 필터를 적용할 수 없으므로 스킵 (또는 false 반환). 현재 `GetTransactionsByAddressFiltered`에서는 receipt를 optional하게 조회하므로, gas 필터가 설정된 경우 receipt 조회를 필수로 변경해야 함.

### 6-D: fromTime / toTime 필터

**목적**: 시간 기반 트랜잭션 필터링

**구현 전략**: 04번 문서(feeDelegationStats)와 동일 패턴. resolver에서 timestamp→block 변환 후 기존 fromBlock/toBlock에 매핑.

```go
type TransactionFilter struct {
    // ... 기존 필드
    FromTime uint64 // Unix timestamp (resolver에서 변환 후 FromBlock에 매핑)
    ToTime   uint64 // Unix timestamp (resolver에서 변환 후 ToBlock에 매핑)
}
```

**실제로는 TransactionFilter에 time 필드를 추가할 필요 없음.** resolver에서 변환 후 `FromBlock`/`ToBlock`에 값을 설정하면 됨.

**parseHistoricalTransactionFilter 수정**:

```go
// 기존 fromBlock/toBlock 파싱 이후
// fromTime/toTime이 지정되면 덮어쓰기
if fromTimeStr, ok := args["fromTime"].(string); ok {
    // timestamp → block 변환은 resolver 레벨에서 처리
    // parseHistoricalTransactionFilter에서는 값만 저장
    ft, err := strconv.ParseUint(fromTimeStr, 10, 64)
    if err == nil {
        filter.FromTime = ft
    }
}
```

**그러나** `MatchTransaction`은 block timestamp 정보가 없으므로, `TransactionFilter`에 time 필드를 추가하기보다는 resolver에서 timestamp→block 변환 후 `fromBlock`/`toBlock`으로 전달하는 것이 깔끔.

## 통합 구현 계획

### Step 1: TransactionFilter 확장

**File**: `pkg/storage/historical.go`

```go
type TransactionFilter struct {
    FromBlock      uint64
    ToBlock        uint64
    MinValue       *big.Int
    MaxValue       *big.Int
    TxType         TransactionType
    SuccessOnly    bool
    // 02번에서 추가
    IsFeeDelegated *bool
    // 06번에서 추가
    MethodID       string   // function selector (0x + 8 hex chars)
    MinGasUsed     *uint64  // minimum gas used
    MaxGasUsed     *uint64  // maximum gas used
}
```

**Note**: `direction`은 `TxType`에 매핑, `fromTime`/`toTime`은 resolver에서 `FromBlock`/`ToBlock`로 변환. 별도 필드 불필요.

### Step 2: MatchTransaction 확장

**File**: `pkg/storage/historical.go`

```go
func (f *TransactionFilter) MatchTransaction(tx *types.Transaction, receipt *types.Receipt, location *TxLocation, targetAddr common.Address) bool {
    // ... 기존 체크 (block range, tx type, value range, success)

    // Check methodId
    if f.MethodID != "" {
        inputData := tx.Data()
        if len(inputData) < 4 {
            return false
        }
        txMethodID := fmt.Sprintf("0x%x", inputData[:4])
        if !strings.EqualFold(txMethodID, f.MethodID) {
            return false
        }
    }

    // Check gas used range
    if f.MinGasUsed != nil && receipt != nil {
        if receipt.GasUsed < *f.MinGasUsed {
            return false
        }
    }
    if f.MaxGasUsed != nil && receipt != nil {
        if receipt.GasUsed > *f.MaxGasUsed {
            return false
        }
    }

    return true
}
```

### Step 3: GraphQL Schema 확장

**File**: `pkg/api/graphql/schema.graphql`

```graphql
enum TransactionDirection {
  SENT
  RECEIVED
  ALL
}

input HistoricalTransactionFilter {
  fromBlock: BigInt!
  toBlock: BigInt!
  minValue: BigInt
  maxValue: BigInt
  txType: Int
  successOnly: Boolean
  # 02번에서 추가
  isFeeDelegated: Boolean
  # 06번에서 추가
  methodId: String
  minGasUsed: BigInt
  maxGasUsed: BigInt
  direction: TransactionDirection
  fromTime: BigInt
  toTime: BigInt
}
```

### Step 4: Schema Builder 수정

**File**: `pkg/api/graphql/schema.go`

HistoricalTransactionFilter input에 새 필드 추가 + TransactionDirection enum 정의.

### Step 5: Filter Parser 확장

**File**: `pkg/api/graphql/resolvers_historical.go` - `parseHistoricalTransactionFilter()`

```go
// Parse optional methodId
if methodId, ok := args["methodId"].(string); ok && methodId != "" {
    filter.MethodID = methodId
}

// Parse optional minGasUsed
if minGasUsedStr, ok := args["minGasUsed"].(string); ok && minGasUsedStr != "" {
    gasUsed, err := strconv.ParseUint(minGasUsedStr, 10, 64)
    if err == nil {
        filter.MinGasUsed = &gasUsed
    }
}

// Parse optional maxGasUsed
if maxGasUsedStr, ok := args["maxGasUsed"].(string); ok && maxGasUsedStr != "" {
    gasUsed, err := strconv.ParseUint(maxGasUsedStr, 10, 64)
    if err == nil {
        filter.MaxGasUsed = &gasUsed
    }
}

// Parse optional direction (overrides txType if both specified)
if direction, ok := args["direction"].(string); ok {
    switch direction {
    case "SENT":
        filter.TxType = storage.TxTypeSent
    case "RECEIVED":
        filter.TxType = storage.TxTypeReceived
    case "ALL":
        filter.TxType = storage.TxTypeAll
    }
}
```

### Step 6: Resolver에서 fromTime/toTime 처리

**File**: `pkg/api/graphql/resolvers_historical.go` - `resolveTransactionsByAddressFiltered()`

```go
// filter 파싱 후, fromTime/toTime으로 fromBlock/toBlock 덮어쓰기
if filterArgs != nil {
    histStorage, hasHist := s.storage.(storage.HistoricalReader)

    if fromTimeStr, ok := filterArgs["fromTime"].(string); ok && fromTimeStr != "" && hasHist {
        ft, err := strconv.ParseUint(fromTimeStr, 10, 64)
        if err == nil {
            block, err := histStorage.GetBlockByTimestamp(ctx, ft)
            if err == nil && block != nil {
                filter.FromBlock = block.NumberU64()
            }
        }
    }
    if toTimeStr, ok := filterArgs["toTime"].(string); ok && toTimeStr != "" && hasHist {
        tt, err := strconv.ParseUint(toTimeStr, 10, 64)
        if err == nil {
            block, err := histStorage.GetBlockByTimestamp(ctx, tt)
            if err == nil && block != nil {
                filter.ToBlock = block.NumberU64()
            }
        }
    }
}
```

### Step 7: Receipt 조회 필수화 (gas 필터 시)

**File**: `pkg/storage/pebble_historical.go` - `GetTransactionsByAddressFiltered()`

현재 receipt는 optional하게 조회됨. `MinGasUsed`/`MaxGasUsed` 필터가 설정된 경우 receipt 조회를 필수로 변경.

```go
// receipt 조회 로직
receipt, err := s.GetReceipt(ctx, txHash)
if err != nil {
    // gas 필터가 설정된 경우 receipt 없으면 스킵
    if filter.MinGasUsed != nil || filter.MaxGasUsed != nil {
        continue
    }
    // 그 외에는 receipt 없이 계속
    receipt = nil
}
```

## 수정 파일 목록

| File | Change Type |
|------|-------------|
| `pkg/storage/historical.go` | `TransactionFilter`에 `MethodID`, `MinGasUsed`, `MaxGasUsed` 추가, `MatchTransaction` 확장 |
| `pkg/storage/pebble_historical.go` | gas 필터 시 receipt 필수 조회 로직 |
| `pkg/api/graphql/schema.graphql` | `HistoricalTransactionFilter` 확장, `TransactionDirection` enum |
| `pkg/api/graphql/schema.go` | input 타입 + enum 정의 |
| `pkg/api/graphql/resolvers_historical.go` | `parseHistoricalTransactionFilter` 확장, timestamp→block 변환 |

## 테스트 매트릭스

| Filter | Test Case |
|--------|-----------|
| `methodId: "0xa9059cbb"` | ERC20 transfer 함수만 필터링 |
| `methodId: "0x"` (빈 input) | 단순 ETH 전송만 |
| `minGasUsed: "21000"` | 최소 gas 21000 이상 |
| `maxGasUsed: "100000"` | gas 100000 이하 |
| `direction: SENT` | 보낸 트랜잭션만 |
| `direction: RECEIVED` | 받은 트랜잭션만 |
| `fromTime` + `toTime` | 시간 범위 필터링 |
| 복합 필터 | `methodId + minGasUsed + direction` 조합 |

## 리스크

- **Low**: `MatchTransaction` 확장은 기존 필터에 영향 없음 (새 필드가 zero value이면 스킵)
- **Medium**: receipt 조회 필수화 시 gas 필터 없는 쿼리의 성능에 영향 없도록 조건부 적용
- **Low**: `strings.EqualFold`로 methodId 대소문자 구분 없이 비교

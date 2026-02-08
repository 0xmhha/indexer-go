# 02: isFeeDelegated 트랜잭션 필터

> **Priority**: Critical (P1)
> **Effort**: Medium
> **Impact**: AdvancedTransactionFilters 핵심 필터 활성화
> **Blocking**: `components/transactions/AdvancedTransactionFilters.tsx`

## 현재 상태

### HistoricalTransactionFilter (`schema.graphql:643-659`)
```graphql
input HistoricalTransactionFilter {
  fromBlock: BigInt!
  toBlock: BigInt!
  minValue: BigInt
  maxValue: BigInt
  txType: Int          # 0=all, 1=sent, 2=received
  successOnly: Boolean
}
```

### Storage TransactionFilter (`pkg/storage/historical.go:25-38`)
```go
type TransactionFilter struct {
    FromBlock   uint64
    ToBlock     uint64
    MinValue    *big.Int
    MaxValue    *big.Int
    TxType      TransactionType
    SuccessOnly bool
}
```

### Fee Delegation 감지 로직

현재 `pebble_fee_delegation.go`에서 `GetFeeDelegationTxMeta(txHash)` 호출로 fee delegation 여부를 확인:
- Key: `/data/feedelegation/{txHash}`
- 데이터가 존재하면 fee delegation 트랜잭션
- `FeeDelegationTxMeta.OriginalType == 0x16`

### MatchTransaction 로직 (`historical.go:248-291`)
```go
func (f *TransactionFilter) MatchTransaction(tx *types.Transaction, receipt *types.Receipt, location *TxLocation, targetAddr common.Address) bool {
    // Block range, TxType, Value range, SuccessOnly 필터 적용
}
```

## 구현 방안

### Option A: Filter에 isFeeDelegated 추가 + MatchTransaction 확장 (추천)

`TransactionFilter`에 `IsFeeDelegated *bool` 필드를 추가하고, `MatchTransaction` 또는 resolver 수준에서 fee delegation 체크.

**핵심 문제**: `MatchTransaction`은 `(tx, receipt, location, targetAddr)` 파라미터만 받으며, fee delegation meta 조회를 위한 storage 접근 불가.

**해결책**: `MatchTransaction` 시그니처를 변경하거나, resolver 레벨에서 필터링.

### Option B: Resolver 레벨 필터링

`resolveTransactionsByAddressFiltered`에서 기존 필터링 후 추가로 fee delegation 체크.

**장점**: `MatchTransaction` 시그니처 변경 불필요
**단점**: 페이지네이션과 함께 사용 시 정확도 이슈 (이미 limit에 도달한 후 필터링하면 결과 수가 줄어듦)

### Option C: Storage 레벨 통합 (추천)

`GetTransactionsByAddressFiltered`에서 필터를 적용할 때 fee delegation meta도 함께 확인.

## 상세 구현 (Option A + C 조합)

### Step 1: TransactionFilter 구조체 확장

**File**: `pkg/storage/historical.go`

```go
type TransactionFilter struct {
    FromBlock      uint64
    ToBlock        uint64
    MinValue       *big.Int
    MaxValue       *big.Int
    TxType         TransactionType
    SuccessOnly    bool
    // 신규 필드
    IsFeeDelegated *bool   // nil=무시, true=fee delegation만, false=non-fee delegation만
}
```

### Step 2: GetTransactionsByAddressFiltered 수정

**File**: `pkg/storage/pebble_historical.go`

현재 로직 (`GetTransactionsByAddressFiltered`):
```
1. AddressTransactionKeyPrefix(addr)로 이터레이션
2. 각 txHash에 대해 GetTransaction → GetReceipt
3. filter.MatchTransaction()으로 필터링
4. offset/limit 적용 후 반환
```

수정 로직:
```
1. (동일)
2. (동일)
3. filter.MatchTransaction()으로 기본 필터링
4. IsFeeDelegated 필터가 설정된 경우:
   - GetFeeDelegationTxMeta(txHash) 호출
   - meta != nil이면 fee delegation 트랜잭션
   - filter.IsFeeDelegated와 비교하여 매칭
5. offset/limit 적용 후 반환
```

```go
// MatchTransaction 이후 추가 필터링
if f.IsFeeDelegated != nil {
    meta, _ := s.GetFeeDelegationTxMeta(ctx, txHash)
    isFD := (meta != nil)
    if *f.IsFeeDelegated != isFD {
        continue // 필터 미매칭, 스킵
    }
}
```

**주의**: `MatchTransaction`은 순수 함수(storage 접근 불가)로 유지하고, fee delegation 체크는 `GetTransactionsByAddressFiltered` 내에서 별도로 수행.

### Step 3: GraphQL Schema 확장

**File**: `pkg/api/graphql/schema.graphql`

```graphql
input HistoricalTransactionFilter {
  fromBlock: BigInt!
  toBlock: BigInt!
  minValue: BigInt
  maxValue: BigInt
  txType: Int
  successOnly: Boolean
  # 신규 필드
  isFeeDelegated: Boolean
}
```

### Step 4: Schema Builder 수정

**File**: `pkg/api/graphql/schema.go` - HistoricalTransactionFilter input 정의에 필드 추가

```go
"isFeeDelegated": &graphql.InputObjectFieldConfig{
    Type:        graphql.Boolean,
    Description: "Filter by fee delegation status",
},
```

### Step 5: Filter Parser 수정

**File**: `pkg/api/graphql/resolvers_historical.go` - `parseHistoricalTransactionFilter()`

```go
// Parse optional isFeeDelegated
if isFeeDelegated, ok := args["isFeeDelegated"].(bool); ok {
    filter.IsFeeDelegated = &isFeeDelegated
}
```

### Step 6: pebble_historical.go 수정

`GetTransactionsByAddressFiltered` 내에서 fee delegation 체크 로직 추가. 이를 위해 함수가 `FeeDelegationReader`에도 접근 가능해야 하는데, `PebbleStorage`는 이미 `FeeDelegationReader`를 구현하므로 `s.GetFeeDelegationTxMeta(ctx, txHash)` 직접 호출 가능.

## 수정 파일 목록

| File | Change Type |
|------|-------------|
| `pkg/storage/historical.go` | `TransactionFilter` 구조체에 `IsFeeDelegated` 필드 추가 |
| `pkg/storage/pebble_historical.go` | `GetTransactionsByAddressFiltered` 내 fee delegation 필터 로직 추가 |
| `pkg/api/graphql/schema.graphql` | `HistoricalTransactionFilter` input에 `isFeeDelegated` 추가 |
| `pkg/api/graphql/schema.go` | input 타입 빌더에 필드 추가 |
| `pkg/api/graphql/resolvers_historical.go` | `parseHistoricalTransactionFilter` 파서 수정 |

## 성능 고려

- `IsFeeDelegated` 필터 적용 시 각 트랜잭션마다 `/data/feedelegation/{txHash}` 키 조회 추가
- PebbleDB의 point lookup은 매우 빠르므로(~μs) 성능 영향 미미
- 다만, 트랜잭션 수가 매우 많은 주소의 경우 누적 효과 있을 수 있음
- 필요 시 fee delegation 트랜잭션만을 위한 별도 인덱스 활용 가능 (`/index/feedelegation/payer/`)

## 테스트 포인트

1. `isFeeDelegated: true` → fee delegation 트랜잭션만 반환
2. `isFeeDelegated: false` → non-fee delegation 트랜잭션만 반환
3. `isFeeDelegated` 미지정 → 기존 동작 유지 (전체 반환)
4. `isFeeDelegated`와 다른 필터 조합 테스트 (e.g., `successOnly + isFeeDelegated`)
5. fee delegation 트랜잭션이 없는 주소에서 `isFeeDelegated: true` → 빈 결과

## 리스크

- **Low**: 기존 필터 동작에 영향 없음 (nil이면 무시)
- **Low**: PebbleDB point lookup 성능 우수
- **Medium**: 대규모 트랜잭션 목록에서 N번의 추가 조회 → offset 큰 값에서 느려질 수 있음

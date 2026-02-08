# 04: feeDelegationStats 시간 기반 필터링

> **Priority**: High (P2)
> **Effort**: Low
> **Impact**: FeeDelegationDashboard 시간 기간 셀렉터 활성화
> **Blocking**: `components/gas/FeeDelegationDashboard.tsx` (24H, 7D, 30D, ALL 필터)

## 현재 상태

### GraphQL 쿼리 (`schema.graphql:1267-1270`)
```graphql
feeDelegationStats(
  fromBlock: BigInt
  toBlock: BigInt
): FeeDelegationStats!
```

### Resolver (`resolvers_feedelegation.go:15-55`)
- `fromBlock`, `toBlock` 파라미터만 파싱
- `fdReader.GetFeeDelegationStats(ctx, fromBlock, toBlock)` 호출

### 기존 blockByTimestamp 쿼리 (`schema.graphql:979`)
```graphql
blockByTimestamp(timestamp: BigInt!): Block
```

이미 타임스탬프→블록 변환 쿼리가 존재. `HistoricalReader.GetBlockByTimestamp(ctx, timestamp)` 구현됨.

### 프론트엔드 요구사항

```graphql
feeDelegationStats(
  fromBlock: BigInt
  toBlock: BigInt
  fromTime: String    # ISO 8601 timestamp (신규)
  toTime: String      # ISO 8601 timestamp (신규)
): FeeDelegationStats!
```

## 구현 방안

### Option A: Resolver 레벨 timestamp→block 변환 (추천)

resolver에서 `fromTime`/`toTime`이 제공되면 `GetBlockByTimestamp()`를 호출하여 블록 번호로 변환한 후, 기존 `GetFeeDelegationStats(fromBlock, toBlock)` 로직을 그대로 사용.

**장점**: storage 레이어 변경 없음, 기존 로직 100% 재사용
**단점**: resolver에서 추가 I/O 2회 (fromTime, toTime 각각)

### Option B: Storage 인터페이스 확장

`GetFeeDelegationStats`에 time 파라미터를 추가.

**장점**: 깔끔한 인터페이스
**단점**: 불필요한 인터페이스 변경, storage 내부에서도 결국 timestamp→block 변환 필요

### 추천: Option A

timestamp→block 변환은 이미 구현된 기능이므로 resolver에서 처리하는 것이 가장 간단.

## 상세 구현

### Step 1: GraphQL Schema 확장

**File**: `pkg/api/graphql/schema.graphql`

```graphql
feeDelegationStats(
  fromBlock: BigInt
  toBlock: BigInt
  fromTime: BigInt    # Unix timestamp (기존 BigInt 패턴 유지)
  toTime: BigInt      # Unix timestamp
): FeeDelegationStats!
```

**Note**: 프론트엔드 요구사항은 ISO 8601 String이지만, 백엔드의 기존 타임스탬프는 모두 `BigInt` (Unix timestamp)로 처리됨 (`blocksByTimeRange`, `blockByTimestamp` 등). 일관성을 위해 `BigInt`로 통일.

프론트엔드에서 ISO 8601 → Unix timestamp 변환은 간단: `Math.floor(new Date(isoString).getTime() / 1000)`

### Step 2: Resolver 수정

**File**: `pkg/api/graphql/resolvers_feedelegation.go` - `resolveFeeDelegationStats()`

```go
func (s *Schema) resolveFeeDelegationStats(p graphql.ResolveParams) (interface{}, error) {
    ctx := p.Context

    var fromBlock, toBlock uint64

    // 기존: 블록 번호 직접 지정
    if fromBlockArg, ok := p.Args["fromBlock"].(string); ok && fromBlockArg != "" {
        if fb, success := new(big.Int).SetString(fromBlockArg, 10); success {
            fromBlock = fb.Uint64()
        }
    }
    if toBlockArg, ok := p.Args["toBlock"].(string); ok && toBlockArg != "" {
        if tb, success := new(big.Int).SetString(toBlockArg, 10); success {
            toBlock = tb.Uint64()
        }
    }

    // 신규: 시간 기반 → 블록 번호 변환
    // fromTime/toTime이 지정되면 fromBlock/toBlock보다 우선
    histStorage, hasHist := s.storage.(storage.HistoricalReader)

    if fromTimeArg, ok := p.Args["fromTime"].(string); ok && fromTimeArg != "" && hasHist {
        if ft, success := new(big.Int).SetString(fromTimeArg, 10); success {
            block, err := histStorage.GetBlockByTimestamp(ctx, ft.Uint64())
            if err == nil && block != nil {
                fromBlock = block.NumberU64()
            }
        }
    }
    if toTimeArg, ok := p.Args["toTime"].(string); ok && toTimeArg != "" && hasHist {
        if tt, success := new(big.Int).SetString(toTimeArg, 10); success {
            block, err := histStorage.GetBlockByTimestamp(ctx, tt.Uint64())
            if err == nil && block != nil {
                toBlock = block.NumberU64()
            }
        }
    }

    // 이후 기존 로직 동일
    fdReader, ok := s.storage.(storage.FeeDelegationReader)
    // ...
}
```

### Step 3: Schema Builder 수정

**File**: `pkg/api/graphql/schema.go` (또는 `resolvers_feedelegation.go`의 `buildFeeDelegationQueries()`)

```go
b.queries["feeDelegationStats"] = &graphql.Field{
    // ...
    Args: graphql.FieldConfigArgument{
        "fromBlock": &graphql.ArgumentConfig{
            Type: bigIntType,
        },
        "toBlock": &graphql.ArgumentConfig{
            Type: bigIntType,
        },
        // 신규
        "fromTime": &graphql.ArgumentConfig{
            Type:        bigIntType,
            Description: "Start time filter (Unix timestamp)",
        },
        "toTime": &graphql.ArgumentConfig{
            Type:        bigIntType,
            Description: "End time filter (Unix timestamp)",
        },
    },
    // ...
}
```

### Step 4: topFeePayers, feePayerStats에도 동일 적용

동일한 패턴으로 `topFeePayers`와 `feePayerStats` 쿼리에도 `fromTime`/`toTime` 추가.

## 수정 파일 목록

| File | Change Type |
|------|-------------|
| `pkg/api/graphql/schema.graphql` | `feeDelegationStats`, `topFeePayers`, `feePayerStats` 쿼리에 `fromTime`/`toTime` 인자 추가 |
| `pkg/api/graphql/resolvers_feedelegation.go` | 3개 resolver에 timestamp→block 변환 로직 추가 |

**Storage 변경 없음.**

## 테스트 포인트

1. `fromTime`만 지정 → 해당 시점 이후의 통계 반환
2. `toTime`만 지정 → 해당 시점까지의 통계 반환
3. `fromTime` + `toTime` 모두 지정 → 범위 통계
4. `fromBlock` + `fromTime` 모두 지정 → `fromTime`이 우선 (또는 에러)
5. 존재하지 않는 타임스탬프 → 가장 가까운 블록 사용
6. 기존 `fromBlock`/`toBlock` 동작 유지 확인

## 리스크

- **Very Low**: storage 변경 없음, resolver 레벨 변환만 추가
- **Low**: `GetBlockByTimestamp`가 정확한 블록을 반환하지 못하는 경우 → 가장 가까운 블록 반환 (이미 구현된 동작)

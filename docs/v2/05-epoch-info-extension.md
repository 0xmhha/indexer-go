# 05: EpochInfo - previousEpochValidatorCount 확장

> **Priority**: High (P2)
> **Effort**: Low
> **Impact**: EpochTimeline에서 validator 변화 표시
> **Blocking**: `components/consensus/EpochTimeline.tsx` (validatorChange 계산)

## 현재 상태

### GraphQL EpochInfo 타입 (`schema.graphql:1990-2006`)
```graphql
type EpochInfo {
  epochNumber: BigInt!
  blockNumber: BigInt!
  candidates: [Candidate!]!
  validators: [Int!]!
  blsPublicKeys: [Bytes!]!
}
```

### Storage EpochInfo (`pkg/storage/wbft.go:33-39`)
```go
type EpochInfo struct {
    EpochNumber   uint64
    BlockNumber   uint64
    Candidates    []Candidate
    Validators    []uint32
    BLSPublicKeys [][]byte
}
```

### 프론트엔드 요구사항

`latestEpochInfo`에 `previousEpochValidatorCount` 필드를 추가하여 validator 수 변화(delta)를 표시.

### 기존 인프라

- `GetEpochInfo(epochNumber)` → 특정 epoch 조회 가능
- `GetLatestEpochInfo()` → 최신 epoch number 조회 → `GetEpochInfo` 호출
- epoch는 순번이므로 N-1 epoch 조회가 가능

## 구현 방안

### Option A: Resolver 레벨 계산 (추천)

resolver에서 현재 epoch 조회 후 N-1 epoch도 조회하여 validator count를 비교.

**장점**: storage 타입 변경 없음, 단순한 구현
**단점**: 추가 I/O 1회 (이전 epoch 조회)

### Option B: Storage 타입에 필드 추가

`EpochInfo`에 `PreviousEpochValidatorCount` 필드를 추가하고, `SaveEpochInfo` 시점에 이전 epoch를 조회하여 저장.

**장점**: 조회 시 추가 I/O 없음
**단점**: 기존 저장된 데이터와 호환성 문제, migration 필요

### 추천: Option A

epoch 조회 빈도가 낮고, 이전 epoch 조회가 단순한 point lookup이므로 resolver에서 처리.

## 상세 구현

### Step 1: GraphQL Schema 확장

**File**: `pkg/api/graphql/schema.graphql`

```graphql
type EpochInfo {
  epochNumber: BigInt!
  blockNumber: BigInt!
  candidates: [Candidate!]!
  validators: [Int!]!
  blsPublicKeys: [Bytes!]!
  # 신규 필드
  validatorCount: Int!
  candidateCount: Int!
  previousEpochValidatorCount: Int
  timestamp: BigInt
}
```

추가 필드:
- `validatorCount`: `len(validators)` (프론트엔드 편의)
- `candidateCount`: `len(candidates)` (프론트엔드 편의)
- `previousEpochValidatorCount`: 이전 epoch의 validator 수 (nullable - epoch 0에서는 null)
- `timestamp`: epoch boundary 블록의 타임스탬프

### Step 2: Resolver (epochInfoToMap) 수정

**File**: `pkg/api/graphql/resolvers_wbft.go`

```go
func (s *Schema) epochInfoToMap(info *storage.EpochInfo) map[string]interface{} {
    // ... 기존 변환 로직

    m := map[string]interface{}{
        "epochNumber":   fmt.Sprintf("%d", info.EpochNumber),
        "blockNumber":   fmt.Sprintf("%d", info.BlockNumber),
        "candidates":    candidates,
        "validators":    validators,
        "blsPublicKeys": blsPublicKeys,
        // 신규 계산 필드
        "validatorCount": len(info.Validators),
        "candidateCount": len(info.Candidates),
    }

    return m
}
```

### Step 3: resolveEpochInfo / resolveLatestEpochInfo 수정

이전 epoch의 validator count를 조회하여 응답에 추가.

```go
func (s *Schema) resolveLatestEpochInfo(p graphql.ResolveParams) (interface{}, error) {
    ctx := p.Context

    wbftReader, ok := s.storage.(storage.WBFTReader)
    if !ok {
        return nil, fmt.Errorf("storage does not support WBFT metadata")
    }

    epochInfo, err := wbftReader.GetLatestEpochInfo(ctx)
    if err != nil {
        // ... 에러 처리
    }

    result := s.epochInfoToMap(epochInfo)

    // previousEpochValidatorCount 계산
    if epochInfo.EpochNumber > 0 {
        prevEpoch, err := wbftReader.GetEpochInfo(ctx, epochInfo.EpochNumber-1)
        if err == nil && prevEpoch != nil {
            result["previousEpochValidatorCount"] = len(prevEpoch.Validators)
        }
    }

    // timestamp 추가
    block, err := s.storage.GetBlock(ctx, epochInfo.BlockNumber)
    if err == nil && block != nil {
        result["timestamp"] = fmt.Sprintf("%d", block.Header().Time)
    }

    return result, nil
}
```

`resolveEpochInfo`에도 동일하게 적용.

### Step 4: Schema Builder 수정

**File**: `pkg/api/graphql/schema.go` - EpochInfo type에 필드 추가

```go
"validatorCount": &graphql.Field{
    Type:        graphql.NewNonNull(graphql.Int),
    Description: "Number of validators in this epoch",
},
"candidateCount": &graphql.Field{
    Type:        graphql.NewNonNull(graphql.Int),
    Description: "Number of candidates in this epoch",
},
"previousEpochValidatorCount": &graphql.Field{
    Type:        graphql.Int,
    Description: "Validator count from previous epoch (null for epoch 0)",
},
"timestamp": &graphql.Field{
    Type:        bigIntType,
    Description: "Timestamp of the epoch boundary block",
},
```

## 수정 파일 목록

| File | Change Type |
|------|-------------|
| `pkg/api/graphql/schema.graphql` | `EpochInfo` 타입에 4개 필드 추가 |
| `pkg/api/graphql/schema.go` | GraphQL 타입 빌더에 필드 추가 |
| `pkg/api/graphql/resolvers_wbft.go` | `epochInfoToMap`, `resolveEpochInfo`, `resolveLatestEpochInfo` 수정 |

**Storage 변경 없음.**

## 테스트 포인트

1. `latestEpochInfo` → `previousEpochValidatorCount`가 N-1 epoch의 validator 수와 일치
2. `epochInfo(epochNumber: 0)` → `previousEpochValidatorCount`가 null
3. `epochInfo(epochNumber: N)` → `previousEpochValidatorCount`가 N-1 epoch의 validator 수
4. `validatorCount` == `len(validators)` 확인
5. `candidateCount` == `len(candidates)` 확인
6. `timestamp` 값이 epoch boundary 블록의 실제 타임스탬프와 일치
7. 이전 epoch이 존재하지 않는 경우 (gap) → `previousEpochValidatorCount`가 null

## 리스크

- **Very Low**: storage 변경 없음, resolver 계산만 추가
- **Very Low**: 추가 I/O 1회 (이전 epoch point lookup) - 무시할 수 있는 수준

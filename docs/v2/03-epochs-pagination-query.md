# 03: epochs 페이지네이션 쿼리

> **Priority**: High (P2)
> **Effort**: Medium
> **Impact**: EpochTimeline 컴포넌트 히스토리 기능 활성화
> **Blocking**: `components/consensus/EpochTimeline.tsx`

## 현재 상태

### 기존 Epoch 쿼리

```graphql
# 특정 epoch 조회
epochInfo(epochNumber: BigInt!): EpochInfo

# 최신 epoch 조회
latestEpochInfo: EpochInfo
```

단일 epoch만 조회 가능. 리스트/페이지네이션 쿼리 없음.

### Storage 키 구조

```
/data/wbft/epoch/{20-digit zero-padded epochNumber}  → JSON (EpochInfo)
/meta/wbft/latest_epoch                               → uint64 (latest epoch number)
```

- `WBFTEpochKey(epochNumber)` → `/data/wbft/epoch/{20자리 zero-padded}` (예: `/data/wbft/epoch/00000000000000000042`)
- `WBFTEpochKeyPrefix()` → `/data/wbft/epoch/`
- `LatestEpochKey()` → `/meta/wbft/latest_epoch` (**다른 prefix**, epoch data prefix 스캔에 포함되지 않음)
- 키가 숫자 순으로 정렬되어 있으므로 역순 이터레이션으로 최신 epoch부터 조회 가능

### EpochInfo 구조체 (`pkg/storage/wbft.go:33-39`)

```go
type EpochInfo struct {
    EpochNumber   uint64
    BlockNumber   uint64      // epoch가 저장된 블록
    Candidates    []Candidate
    Validators    []uint32
    BLSPublicKeys [][]byte
}
```

### EpochData (Consensus Layer) (`pkg/types/consensus/wbft.go:60-66`)

```go
type EpochData struct {
    EpochNumber    uint64
    ValidatorCount int
    Validators     []ValidatorInfo
    CandidateCount int
    Candidates     []CandidateInfo
}
```

## 프론트엔드 요구 쿼리

```graphql
query GetEpochs($limit: Int!, $offset: Int!) {
  epochs(pagination: { limit: $limit, offset: $offset }) {
    nodes {
      epochNumber
      blockNumber
      validatorCount
      candidateCount
      timestamp       # epoch boundary 블록의 타임스탬프
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

## 구현 방안

### Option A: WBFTReader 인터페이스 확장 (추천)

`WBFTReader`에 `GetEpochsList` 메서드를 추가하고, PebbleDB의 epoch prefix 스캔으로 구현.

**장점**: 기존 패턴과 일관, 깔끔한 인터페이스
**단점**: 새 인터페이스 메서드 추가

### Option B: 기존 GetLatestEpochInfo + 순차 조회

latest epoch number를 얻은 후 `GetEpochInfo`를 반복 호출하여 리스트 구성.

**장점**: 인터페이스 변경 없음
**단점**: N번의 개별 조회 (비효율적), epoch number가 연속이 아닐 수 있음

## 상세 구현 (Option A)

### Step 1: WBFTReader 인터페이스 확장

**File**: `pkg/storage/wbft.go`

```go
type WBFTReader interface {
    // ... 기존 메서드들

    // GetEpochsList returns a paginated list of epochs, ordered by epoch number descending
    GetEpochsList(ctx context.Context, limit, offset int) ([]*EpochInfo, int, error)
}
```

반환: epoch 목록, 전체 개수, 에러

### Step 2: PebbleDB 구현

**File**: `pkg/storage/pebble_wbft.go`

```go
func (s *PebbleStorage) GetEpochsList(ctx context.Context, limit, offset int) ([]*EpochInfo, int, error) {
    if err := s.ensureNotClosed(); err != nil {
        return nil, 0, err
    }

    prefix := WBFTEpochKeyPrefix()
    upperBound := prefixUpperBound(prefix)

    // 역순 이터레이션 (최신 epoch부터)
    iter, err := s.db.NewIter(&pebble.IterOptions{
        LowerBound: prefix,
        UpperBound: upperBound,
    })
    if err != nil {
        return nil, 0, err
    }
    defer iter.Close()

    // 전체 개수 계산 + 데이터 수집
    // 참고: keyLatestEpoch는 "/meta/wbft/latest_epoch"로 다른 prefix에 있으므로
    // "/data/wbft/epoch/" prefix 스캔에 포함되지 않음. 별도 필터링 불필요.
    var allEpochs []*EpochInfo
    for iter.Last(); iter.Valid(); iter.Prev() {
        var epochInfo EpochInfo
        if err := json.Unmarshal(iter.Value(), &epochInfo); err != nil {
            continue
        }
        allEpochs = append(allEpochs, &epochInfo)
    }

    totalCount := len(allEpochs)

    // offset/limit 적용
    if offset >= len(allEpochs) {
        return []*EpochInfo{}, totalCount, nil
    }

    end := offset + limit
    if end > len(allEpochs) {
        end = len(allEpochs)
    }

    return allEpochs[offset:end], totalCount, nil
}
```

**참고**: `keyLatestEpoch`는 `/meta/wbft/latest_epoch`에 저장되며, epoch 데이터 prefix (`/data/wbft/epoch/`)와 다른 경로이므로 prefix 스캔 시 자동으로 제외됨. 별도 필터링 불필요.

**최적화**: 전체 카운트가 필요하므로 첫 번째 패스에서 카운트하고, 두 번째 패스에서 데이터 추출. 또는 단일 패스에서 처리.

### Step 3: Timestamp 보강

`EpochInfo`에는 `timestamp` 필드가 없다. epoch boundary 블록의 타임스탬프를 가져와야 한다.

**방법 1**: 응답 시 resolver에서 `GetBlock(epochInfo.BlockNumber)`으로 타임스탬프 조회
**방법 2**: `EpochInfo` 저장 시 타임스탬프도 함께 저장 (SaveEpochInfo 수정)

**추천**: 방법 1 (기존 데이터 호환성 유지). 신규 epoch부터는 방법 2로 전환 고려.

### Step 4: GraphQL Schema 추가

**File**: `pkg/api/graphql/schema.graphql`

```graphql
# Epoch 요약 (리스트 전용, BLS 키 등 무거운 데이터 제외)
type EpochSummary {
  epochNumber: BigInt!
  blockNumber: BigInt!
  validatorCount: Int!
  candidateCount: Int!
  timestamp: BigInt
}

type EpochSummaryConnection {
  nodes: [EpochSummary!]!
  totalCount: Int!
  pageInfo: PageInfo!
}

# Query 섹션에 추가
type Query {
  # ... 기존 쿼리

  # 페이지네이션된 epoch 목록
  epochs(pagination: PaginationInput): EpochSummaryConnection!
}
```

**`EpochSummary` vs `EpochInfo`**: 리스트용으로 경량 타입을 별도 정의. `EpochInfo`는 candidates, validators, blsPublicKeys 등 무거운 데이터를 포함하므로 리스트에 적합하지 않음.

### Step 5: Resolver 구현

**File**: `pkg/api/graphql/resolvers_wbft.go`

```go
func (s *Schema) resolveEpochs(p graphql.ResolveParams) (interface{}, error) {
    ctx := p.Context

    limit := constants.DefaultPaginationLimit
    offset := 0
    if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
        // limit, offset 파싱 (기존 패턴 따름)
    }

    wbftReader, ok := s.storage.(storage.WBFTReader)
    if !ok {
        return nil, fmt.Errorf("storage does not support WBFT metadata")
    }

    epochs, totalCount, err := wbftReader.GetEpochsList(ctx, limit, offset)
    if err != nil {
        s.logger.Error("failed to get epochs list", zap.Error(err))
        return nil, err
    }

    nodes := make([]interface{}, len(epochs))
    for i, epoch := range epochs {
        node := map[string]interface{}{
            "epochNumber":    fmt.Sprintf("%d", epoch.EpochNumber),
            "blockNumber":    fmt.Sprintf("%d", epoch.BlockNumber),
            "validatorCount": len(epoch.Validators),
            "candidateCount": len(epoch.Candidates),
        }

        // Timestamp: epoch boundary 블록에서 조회
        block, err := s.storage.GetBlock(ctx, epoch.BlockNumber)
        if err == nil && block != nil {
            node["timestamp"] = fmt.Sprintf("%d", block.Header().Time)
        }

        nodes[i] = node
    }

    return map[string]interface{}{
        "nodes":      nodes,
        "totalCount": totalCount,
        "pageInfo": map[string]interface{}{
            "hasNextPage":     len(epochs) == limit,
            "hasPreviousPage": offset > 0,
        },
    }, nil
}
```

### Step 6: Schema Builder에 쿼리 등록

**File**: `pkg/api/graphql/schema.go` - `buildConsensusQueries()` 내

```go
b.queries["epochs"] = &graphql.Field{
    Type:        epochSummaryConnectionType,
    Description: "Get paginated list of epochs",
    Args: graphql.FieldConfigArgument{
        "pagination": &graphql.ArgumentConfig{
            Type: paginationInputType,
        },
    },
    Resolve: b.schema.resolveEpochs,
}
```

## 수정 파일 목록

| File | Change Type |
|------|-------------|
| `pkg/storage/wbft.go` | `WBFTReader` 인터페이스에 `GetEpochsList` 추가 |
| `pkg/storage/pebble_wbft.go` | `GetEpochsList` 구현 |
| `pkg/api/graphql/schema.graphql` | `EpochSummary`, `EpochSummaryConnection` 타입 + `epochs` 쿼리 추가 |
| `pkg/api/graphql/schema.go` | 타입 정의 + 쿼리 빌더 |
| `pkg/api/graphql/resolvers_wbft.go` | `resolveEpochs` 함수 추가 |

## 성능 고려

- epoch 개수는 일반적으로 수백~수천 수준 (블록 수에 비해 매우 적음)
- 전체 스캔해도 성능 문제 없을 것으로 예상
- 각 epoch의 블록 타임스탬프 조회를 위한 추가 I/O: epoch 수만큼의 GetBlock 호출
  - 최적화: epoch 수가 많아지면 EpochInfo에 timestamp 필드를 추가하여 저장 시점에 캐싱

## 테스트 포인트

1. `epochs(pagination: {limit: 10, offset: 0})` → 최신 10개 epoch 반환
2. `epochs(pagination: {limit: 10, offset: 10})` → 다음 10개 epoch
3. epoch가 0개인 경우 → 빈 결과
4. `totalCount`가 전체 epoch 수와 일치
5. `pageInfo.hasNextPage`/`hasPreviousPage` 정확성
6. 각 epoch의 `validatorCount`, `candidateCount` 정확성
7. `timestamp` 필드 반환 확인

## 리스크

- **Low**: epoch 개수가 적으므로 전체 스캔 성능 문제 없음
- **Low**: 기존 epoch 관련 쿼리에 영향 없음
- **Very Low**: `keyLatestEpoch`는 `/meta/` prefix 하위에 있어 epoch data prefix 스캔에 포함되지 않음

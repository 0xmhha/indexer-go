# 01: ValidatorSigningStats - blocksProposed / totalBlocks 확장

> **Priority**: Critical (P1)
> **Effort**: Low
> **Impact**: ValidatorHeatmap 컴포넌트 완전 활성화
> **Blocking**: `components/consensus/ValidatorHeatmap.tsx`

## 현재 상태

### GraphQL Schema (`schema.graphql:2047-2078`)
```graphql
type ValidatorSigningStats {
  validatorAddress: Address!
  validatorIndex: Int!
  prepareSignCount: BigInt!
  prepareMissCount: BigInt!
  commitSignCount: BigInt!
  commitMissCount: BigInt!
  fromBlock: BigInt!
  toBlock: BigInt!
  signingRate: Float!
}
```

### Storage Type (`pkg/storage/wbft.go:47-62`)
```go
type ValidatorSigningStats struct {
    ValidatorAddress common.Address
    ValidatorIndex   uint32
    PrepareSignCount uint64
    PrepareMissCount uint64
    CommitSignCount  uint64
    CommitMissCount  uint64
    FromBlock        uint64
    ToBlock          uint64
    SigningRate       float64
}
```

### Consensus Type (`pkg/types/consensus/validator.go:8-26`)
```go
type ValidatorStats struct {
    Address           common.Address
    TotalBlocks       uint64   // ← 이미 존재
    BlocksProposed    uint64   // ← 이미 존재
    PreparesSigned    uint64
    CommitsSigned     uint64
    PreparesMissed    uint64
    CommitsMissed     uint64
    ParticipationRate float64
    LastProposedBlock uint64
    ...
}
```

## 핵심 발견

`consensustypes.ValidatorStats`에는 이미 `BlocksProposed`와 `TotalBlocks` **필드**가 존재하지만, `storage.ValidatorSigningStats`에는 없다.

현재 두 가지 경로가 병존:
1. `allValidatorsSigningStats` → `pebble_wbft.go`의 `GetAllValidatorsSigningStats()` → **blocksProposed/totalBlocks 필드 자체가 없음**
2. `validatorStats` (consensus resolver) → `ConsensusStorage.GetValidatorStats()` → `BlocksProposed`, `TotalBlocks` 필드 존재하나 **값이 항상 0**

### 현재 코드의 구체적 문제

**경로 1 (`GetAllValidatorsSigningStats`)**: `pebble_wbft.go:224-228`에서 `totalBlocks`는 **로컬 변수**로만 사용됨 (`stats.PrepareSignCount + stats.PrepareMissCount`). 이 값은 해당 validator의 활동 기록 수이며 `SigningRate` 계산에만 사용되고, 필드로 저장되지 않음.

**경로 2 (`ConsensusStorage.GetValidatorStats`)**: `consensus.go:133-136`에서 `TotalBlocks = uint64(len(activities))`로 설정 (해당 validator의 activity 레코드 수). **`BlocksProposed`는 어디에서도 설정하지 않으므로 항상 0.**

**경로 2 (`GetValidatorParticipation`)**: `consensus.go:184`에서 `wasProposer := false`로 하드코딩되어 있으며, block header Coinbase 조회를 하지 않음.

**핵심**: 두 경로 모두 proposer 판별을 구현하지 않아 `blocksProposed`가 사실상 사용 불가.

**Proposer 판별 방법**: `block.Header().Coinbase == validatorAddress`

## 구현 방안

### Option A: Storage 타입 확장 (추천)

`storage.ValidatorSigningStats`에 `BlocksProposed`, `TotalBlocks` 필드를 추가하고, 집계 로직에서 block Coinbase를 조회하여 proposer를 판별.

**장점**: 기존 `allValidatorsSigningStats` 쿼리를 그대로 사용하면서 필드만 추가
**단점**: 집계 시 각 블록의 header.Coinbase를 조회해야 하므로 I/O 증가

### Option B: Consensus Resolver 활용

프론트엔드에서 `validatorStats` (consensus resolver) 쿼리를 사용하도록 변경하고, consensus 쪽에서 proposer 판별 로직을 완성.

**장점**: 이미 `ValidatorStats.BlocksProposed` 필드와 `UpdateWithBlock` 로직이 존재
**단점**: 프론트엔드 쿼리 변경 필요, 기존 `allValidatorsSigningStats`와 중복

### Option C: Hybrid (추천)

`storage.ValidatorSigningStats`에 필드를 추가하되, `GetAllValidatorsSigningStats()` 집계 시 proposer 정보를 블록 헤더에서 가져와 계산.

## 상세 구현 (Option C)

### Step 1: Storage 타입 확장

**File**: `pkg/storage/wbft.go`

```go
type ValidatorSigningStats struct {
    ValidatorAddress common.Address
    ValidatorIndex   uint32
    PrepareSignCount uint64
    PrepareMissCount uint64
    CommitSignCount  uint64
    CommitMissCount  uint64
    FromBlock        uint64
    ToBlock          uint64
    SigningRate       float64
    // 신규 필드
    BlocksProposed   uint64  // 이 validator가 제안한 블록 수
    TotalBlocks      uint64  // 조회 범위의 전체 블록 수
    ProposalRate     float64 // BlocksProposed / TotalBlocks * 100
}
```

### Step 2: GetAllValidatorsSigningStats 로직 수정

**File**: `pkg/storage/pebble_wbft.go` (lines 151-244)

현재 로직:
1. `WBFTValidatorActivityAllKeyPrefix()`로 전체 activity 스캔
2. validator별로 prepare/commit 집계
3. `signingRate` 계산

수정 로직:
1. 기존 activity 스캔 유지
2. 블록 범위 `[fromBlock, toBlock]`에 대해 각 블록의 Coinbase(proposer) 조회
3. validator별 `blocksProposed` 카운트
4. `totalBlocks = toBlock - fromBlock + 1` (조회 범위의 전체 블록 수, 기존 `SigningRate`의 per-validator activity count와 다름)
5. `proposalRate = blocksProposed / totalBlocks * 100`

**주의**: 기존 `SigningRate` 계산의 분모(`PrepareSignCount + PrepareMissCount`)는 해당 validator의 활동 기록 수이고, 새로 추가하는 `TotalBlocks`는 조회 범위의 전체 블록 수(`toBlock - fromBlock + 1`)이다. 두 값은 다른 의미.

```go
// 블록 범위에서 proposer 통계 계산
totalBlocks := toBlock - fromBlock + 1
proposerCounts := make(map[common.Address]uint64)

for blockNum := fromBlock; blockNum <= toBlock; blockNum++ {
    block, err := s.GetBlock(ctx, blockNum)
    if err != nil {
        continue
    }
    proposer := block.Header().Coinbase
    proposerCounts[proposer]++
}

// 각 stats에 proposer 정보 추가
for _, stats := range statsMap {
    stats.BlocksProposed = proposerCounts[stats.ValidatorAddress]
    stats.TotalBlocks = totalBlocks
    if totalBlocks > 0 {
        stats.ProposalRate = float64(stats.BlocksProposed) / float64(totalBlocks) * 100
    }
}
```

**성능 고려**: 블록 범위가 큰 경우 I/O가 많아질 수 있다. 최적화 옵션:
- Proposer 인덱스 별도 관리 (`/index/proposer/{address}/{blockNumber}`)
- 범위 제한 (최대 10,000 블록)

### Step 3: GraphQL Schema 확장

**File**: `pkg/api/graphql/schema.graphql`

```graphql
type ValidatorSigningStats {
  validatorAddress: Address!
  validatorIndex: Int!
  prepareSignCount: BigInt!
  prepareMissCount: BigInt!
  commitSignCount: BigInt!
  commitMissCount: BigInt!
  fromBlock: BigInt!
  toBlock: BigInt!
  signingRate: Float!
  # 신규 필드
  blocksProposed: BigInt!
  totalBlocks: BigInt!
  proposalRate: Float
}
```

### Step 4: Schema Builder 수정

**File**: `pkg/api/graphql/schema.go` - `buildConsensusQueries()` 내 `ValidatorSigningStats` type 정의에 필드 추가

### Step 5: Mapper 수정

**File**: `pkg/api/graphql/resolvers_wbft.go` - `validatorSigningStatsToMap()`

```go
func (s *Schema) validatorSigningStatsToMap(stats *storage.ValidatorSigningStats) map[string]interface{} {
    return map[string]interface{}{
        "validatorAddress": stats.ValidatorAddress.Hex(),
        "validatorIndex":   int(stats.ValidatorIndex),
        "prepareSignCount": fmt.Sprintf("%d", stats.PrepareSignCount),
        "prepareMissCount": fmt.Sprintf("%d", stats.PrepareMissCount),
        "commitSignCount":  fmt.Sprintf("%d", stats.CommitSignCount),
        "commitMissCount":  fmt.Sprintf("%d", stats.CommitMissCount),
        "fromBlock":        fmt.Sprintf("%d", stats.FromBlock),
        "toBlock":          fmt.Sprintf("%d", stats.ToBlock),
        "signingRate":      stats.SigningRate,
        // 신규 필드
        "blocksProposed":   fmt.Sprintf("%d", stats.BlocksProposed),
        "totalBlocks":      fmt.Sprintf("%d", stats.TotalBlocks),
        "proposalRate":     stats.ProposalRate,
    }
}
```

## 수정 파일 목록

| File | Change Type |
|------|-------------|
| `pkg/storage/wbft.go` | 구조체 필드 추가 |
| `pkg/storage/pebble_wbft.go` | `GetAllValidatorsSigningStats`, `GetValidatorSigningStats` 로직 수정 |
| `pkg/api/graphql/schema.graphql` | ValidatorSigningStats 타입 필드 추가 |
| `pkg/api/graphql/schema.go` | GraphQL 타입 빌더에 필드 추가 |
| `pkg/api/graphql/resolvers_wbft.go` | `validatorSigningStatsToMap` 수정 |

## 테스트 포인트

1. `allValidatorsSigningStats` 쿼리에서 `blocksProposed`, `totalBlocks` 반환 확인
2. `validatorSigningStats` (단일 validator) 쿼리에서도 동일 필드 반환 확인
3. proposer가 없는 블록 범위 (빈 데이터) 처리
4. 대규모 블록 범위에서의 성능 확인
5. 기존 필드(signingRate 등)에 영향 없음 확인

## 리스크

- **Low**: 기존 필드에 대한 하위 호환성 유지 (신규 필드만 추가)
- **Medium**: 블록 범위가 클 때 proposer 조회 성능 → 범위 제한 또는 인덱스 추가로 완화

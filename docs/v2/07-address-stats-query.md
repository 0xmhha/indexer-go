# 07: addressStats 전용 쿼리

> **Priority**: Low (P3, Optional)
> **Effort**: Medium
> **Impact**: AddressStatsCard 성능 최적화
> **Blocking**: `components/address/AddressStatsCard.tsx` (현재 프론트에서 계산 중)

## 현재 상태

### 프론트엔드 현재 방식

`AddressStatsCard`는 트랜잭션 배열을 받아 프론트엔드에서 직접 통계를 계산:
- totalTransactions, sentCount, receivedCount
- successCount, failedCount
- totalGasUsed, totalGasCost
- totalValueSent, totalValueReceived
- contractInteractionCount, uniqueAddressCount
- firstTransactionTimestamp, lastTransactionTimestamp

### 기존 관련 쿼리

```graphql
# 주소별 트랜잭션 목록 (페이지네이션)
transactionsByAddress(address: Address!, pagination: PaginationInput): TransactionConnection!

# 주소별 필터링된 트랜잭션 목록
transactionsByAddressFiltered(address: Address!, filter: HistoricalTransactionFilter!, pagination: PaginationInput): TransactionConnection!

# 주소 개요 (address indexing)
addressOverview(address: Address!): AddressOverview!
```

### AddressOverview 타입 (`schema.graphql`)
```graphql
type AddressOverview {
  address: Address!
  balance: BigInt!
  nonce: BigInt!
  isContract: Boolean!
  codeSize: Int
  transactionCount: BigInt!
  # ... 기타 필드
}
```

이미 `transactionCount`는 있지만 sent/received 분류, gas 통계 등은 없음.

### 기존 Gas 통계 쿼리

```graphql
# 주소별 gas 통계 (이미 존재)
addressGasStats(address: Address!, fromBlock: BigInt!, toBlock: BigInt!): AddressGasStats!
```

```go
type AddressGasStats struct {
    Address          common.Address
    TotalGasUsed     uint64
    TransactionCount uint64
    AverageGasPerTx  uint64
    TotalFeesPaid    *big.Int
}
```

## 구현 방안

### Option A: 전용 addressStats 쿼리 (요구사항 그대로)

새로운 `addressStats` 쿼리를 추가하여 모든 통계를 한 번에 반환.

**장점**: 프론트엔드에서 단일 쿼리로 모든 데이터 취득
**단점**: 전체 트랜잭션 스캔 필요 (대규모 주소의 경우 느림), storage 인터페이스 확장

### Option B: 기존 쿼리 조합으로 해결

프론트엔드에서 기존 `addressOverview` + `addressGasStats` + `transactionsByAddressFiltered` 조합으로 필요한 데이터를 취득.

**장점**: 백엔드 변경 최소화
**단점**: 다중 쿼리 호출, 일부 데이터(sent/received count 등)는 여전히 계산 필요

### Option C: AddressOverview 확장 (추천)

기존 `AddressOverview` 타입을 확장하여 추가 통계 필드를 포함. `addressOverview` 쿼리는 이미 존재하므로 반환 타입만 확장.

**장점**: 기존 쿼리 재사용, 프론트엔드 변경 최소
**단점**: `addressOverview` resolver가 무거워질 수 있음

### 추천: Option A (전용 쿼리)

프론트엔드 요구사항에 정확히 맞추되, 성능을 위해 집계 로직을 최적화.

## 상세 구현

### Step 1: AddressStats 타입 정의

**File**: `pkg/storage/historical.go`

```go
// AddressStats represents pre-calculated statistics for an address
type AddressStats struct {
    Address                   common.Address
    TotalTransactions         uint64
    SentCount                 uint64
    ReceivedCount             uint64
    SuccessCount              uint64
    FailedCount               uint64
    TotalGasUsed              uint64
    TotalGasCost              *big.Int
    TotalValueSent            *big.Int
    TotalValueReceived        *big.Int
    ContractInteractionCount  uint64
    UniqueAddressCount        uint64
    FirstTransactionTimestamp  uint64  // Unix timestamp, 0 if no transactions
    LastTransactionTimestamp   uint64  // Unix timestamp, 0 if no transactions
}
```

### Step 2: HistoricalReader 확장

**File**: `pkg/storage/historical.go`

```go
type HistoricalReader interface {
    // ... 기존 메서드

    // GetAddressStats returns aggregated statistics for an address
    GetAddressStats(ctx context.Context, addr common.Address) (*AddressStats, error)
}
```

### Step 3: PebbleDB 구현

**File**: `pkg/storage/pebble_historical.go`

```go
func (s *PebbleStorage) GetAddressStats(ctx context.Context, addr common.Address) (*AddressStats, error) {
    if err := s.ensureNotClosed(); err != nil {
        return nil, err
    }

    stats := &AddressStats{
        Address:            addr,
        TotalGasCost:       big.NewInt(0),
        TotalValueSent:     big.NewInt(0),
        TotalValueReceived: big.NewInt(0),
    }

    uniqueAddresses := make(map[common.Address]bool)

    // 주소의 모든 트랜잭션을 이터레이션
    prefix := AddressTransactionKeyPrefix(addr)
    iter, err := s.db.NewIter(&pebble.IterOptions{
        LowerBound: prefix,
        UpperBound: prefixUpperBound(prefix),
    })
    if err != nil {
        return nil, err
    }
    defer iter.Close()

    for iter.First(); iter.Valid(); iter.Next() {
        txHash := common.BytesToHash(iter.Value())

        tx, location, err := s.GetTransaction(ctx, txHash)
        if err != nil {
            continue
        }

        receipt, _ := s.GetReceipt(ctx, txHash)

        // Sender 추출
        from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
        if err != nil {
            continue
        }

        stats.TotalTransactions++

        // Sent vs Received
        if from == addr {
            stats.SentCount++
            stats.TotalValueSent.Add(stats.TotalValueSent, tx.Value())
            if tx.To() != nil {
                uniqueAddresses[*tx.To()] = true
            }
        }
        to := tx.To()
        if to != nil && *to == addr {
            stats.ReceivedCount++
            stats.TotalValueReceived.Add(stats.TotalValueReceived, tx.Value())
            uniqueAddresses[from] = true
        }

        // Success vs Failed
        if receipt != nil {
            if receipt.Status == types.ReceiptStatusSuccessful {
                stats.SuccessCount++
            } else {
                stats.FailedCount++
            }
            stats.TotalGasUsed += receipt.GasUsed

            // Gas cost = gasUsed * effectiveGasPrice
            if receipt.EffectiveGasPrice != nil {
                cost := new(big.Int).Mul(
                    new(big.Int).SetUint64(receipt.GasUsed),
                    receipt.EffectiveGasPrice,
                )
                stats.TotalGasCost.Add(stats.TotalGasCost, cost)
            }
        }

        // Contract interaction
        if to != nil && len(tx.Data()) > 0 {
            stats.ContractInteractionCount++
        }

        // Timestamps
        if location != nil {
            block, err := s.GetBlock(ctx, location.BlockHeight)
            if err == nil && block != nil {
                ts := block.Header().Time
                if stats.FirstTransactionTimestamp == 0 || ts < stats.FirstTransactionTimestamp {
                    stats.FirstTransactionTimestamp = ts
                }
                if ts > stats.LastTransactionTimestamp {
                    stats.LastTransactionTimestamp = ts
                }
            }
        }
    }

    stats.UniqueAddressCount = uint64(len(uniqueAddresses))

    return stats, nil
}
```

**성능 우려**: 대규모 주소(수만 트랜잭션)의 경우 전체 스캔이 느릴 수 있음.

**최적화 옵션**:
1. 캐시: 결과를 PebbleDB에 캐싱 (TTL 기반)
2. 점진적 계산: 새 블록 인덱싱 시 통계 업데이트
3. 제한: 최대 트랜잭션 수 제한 (e.g., 최근 10,000건)

### Step 4: GraphQL Schema 추가

**File**: `pkg/api/graphql/schema.graphql`

```graphql
type AddressStats {
  address: Address!
  totalTransactions: Int!
  sentCount: Int!
  receivedCount: Int!
  successCount: Int!
  failedCount: Int!
  totalGasUsed: BigInt!
  totalGasCost: BigInt!
  totalValueSent: BigInt!
  totalValueReceived: BigInt!
  contractInteractionCount: Int!
  uniqueAddressCount: Int!
  firstTransactionTimestamp: BigInt
  lastTransactionTimestamp: BigInt
}

type Query {
  # ... 기존 쿼리

  # 주소별 집계 통계
  addressStats(address: Address!): AddressStats!
}
```

### Step 5: Resolver 구현

**File**: `pkg/api/graphql/resolvers_historical.go`

```go
func (s *Schema) resolveAddressStats(p graphql.ResolveParams) (interface{}, error) {
    ctx := p.Context

    addressStr, ok := p.Args["address"].(string)
    if !ok {
        return nil, fmt.Errorf("invalid address")
    }
    address := common.HexToAddress(addressStr)

    histStorage, ok := s.storage.(storage.HistoricalReader)
    if !ok {
        return nil, fmt.Errorf("storage does not support historical queries")
    }

    stats, err := histStorage.GetAddressStats(ctx, address)
    if err != nil {
        s.logger.Error("failed to get address stats",
            zap.String("address", addressStr),
            zap.Error(err))
        return nil, err
    }

    result := map[string]interface{}{
        "address":                  stats.Address.Hex(),
        "totalTransactions":        int(stats.TotalTransactions),
        "sentCount":                int(stats.SentCount),
        "receivedCount":            int(stats.ReceivedCount),
        "successCount":             int(stats.SuccessCount),
        "failedCount":              int(stats.FailedCount),
        "totalGasUsed":             fmt.Sprintf("%d", stats.TotalGasUsed),
        "totalGasCost":             stats.TotalGasCost.String(),
        "totalValueSent":           stats.TotalValueSent.String(),
        "totalValueReceived":       stats.TotalValueReceived.String(),
        "contractInteractionCount": int(stats.ContractInteractionCount),
        "uniqueAddressCount":       int(stats.UniqueAddressCount),
    }

    if stats.FirstTransactionTimestamp > 0 {
        result["firstTransactionTimestamp"] = fmt.Sprintf("%d", stats.FirstTransactionTimestamp)
    }
    if stats.LastTransactionTimestamp > 0 {
        result["lastTransactionTimestamp"] = fmt.Sprintf("%d", stats.LastTransactionTimestamp)
    }

    return result, nil
}
```

### Step 6: Schema Builder에 등록

historical queries 빌더에 `addressStats` 쿼리 등록.

## 수정 파일 목록

| File | Change Type |
|------|-------------|
| `pkg/storage/historical.go` | `AddressStats` 타입 추가, `HistoricalReader`에 `GetAddressStats` 추가 |
| `pkg/storage/pebble_historical.go` | `GetAddressStats` 구현 |
| `pkg/api/graphql/schema.graphql` | `AddressStats` 타입 + `addressStats` 쿼리 추가 |
| `pkg/api/graphql/schema.go` | 타입 정의 + 쿼리 빌더 |
| `pkg/api/graphql/resolvers_historical.go` | `resolveAddressStats` 구현 |

## 성능 고려

| 트랜잭션 수 | 예상 소요 시간 | 대응 |
|-------------|--------------|------|
| ~100 | < 100ms | 문제 없음 |
| ~1,000 | ~500ms | 허용 범위 |
| ~10,000 | ~3s | 캐싱 도입 고려 |
| ~100,000+ | ~30s+ | 점진적 계산 필수 |

**단기 대응**: 쿼리 타임아웃 설정 (10초), 프론트엔드에 로딩 표시
**중기 대응**: 결과 캐싱 (LRU cache 또는 PebbleDB에 저장, 블록 업데이트 시 무효화)
**장기 대응**: 인덱싱 시점에 통계 점진적 업데이트

## 테스트 포인트

1. 트랜잭션이 있는 주소 → 정확한 통계 반환
2. 트랜잭션이 없는 주소 → 모든 값 0/null
3. 컨트랙트 주소 → contractInteractionCount 정확성
4. fee delegation 트랜잭션이 포함된 주소 → gas 통계에 반영
5. sent + received 합계 == totalTransactions (자기 자신에게 보내는 경우 주의)
6. 대규모 트랜잭션 주소에서의 타임아웃 처리

## 리스크

- **Medium**: 대규모 주소에서의 성능 → 캐싱/점진적 계산으로 완화
- **Low**: 기존 쿼리에 영향 없음 (새 쿼리만 추가)
- **Low**: `HistoricalReader` 인터페이스 확장 → mock 구현 업데이트 필요

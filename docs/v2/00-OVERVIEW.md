# Backend API Extension - Implementation Overview

> **Date**: 2026-02-08
> **Source**: `indexer-frontend/docs/BACKEND_REQUIREMENTS.md`
> **Total Items**: 10 features across 3 priority phases

## Document Index

| # | Document | Feature | Priority | Effort |
|---|----------|---------|----------|--------|
| 01 | `01-validator-signing-stats-extension.md` | ValidatorSigningStats에 blocksProposed/totalBlocks 추가 | Critical | Low |
| 02 | `02-is-fee-delegated-filter.md` | 트랜잭션 isFeeDelegated 필터 | Critical | Medium |
| 03 | `03-epochs-pagination-query.md` | epochs 페이지네이션 쿼리 | High | Medium |
| 04 | `04-fee-delegation-time-filter.md` | feeDelegationStats 시간 기반 필터링 | High | Low |
| 05 | `05-epoch-info-extension.md` | EpochInfo previousEpochValidatorCount 추가 | High | Low |
| 06 | `06-transaction-filter-extensions.md` | methodId, gasUsed, direction, fromTime/toTime 필터 | Medium | Medium |
| 07 | `07-address-stats-query.md` | addressStats 전용 쿼리 | Low | Medium |

## Architecture Context

```
Frontend (GraphQL Query)
    ↓
Resolver Layer (pkg/api/graphql/resolvers_*.go)
    ↓
Storage Interface (pkg/storage/*.go - interfaces)
    ↓
PebbleDB Implementation (pkg/storage/pebble_*.go)
    ↓
PebbleDB (Key-Value Store)
```

### Key Patterns

- **Schema**: `schema.graphql` (GraphQL 타입/쿼리 정의)
- **Schema Builder**: `schema.go` (Go에서 GraphQL 스키마 빌드, `b.queries["name"] = ...`)
- **Resolver**: `resolvers_*.go` (쿼리 실행 로직)
- **Mapper**: `mappers.go`, `resolvers_*.go` 내 `*ToMap()` 함수
- **Storage Interface**: `wbft.go`, `historical.go` (인터페이스 정의)
- **Storage Impl**: `pebble_wbft.go`, `pebble_historical.go` (PebbleDB 구현)
- **Consensus Layer**: `consensus.go` (ConsensusStorage 래퍼 - 고수준 집계)
- **Type System**: `pkg/types/consensus/` (validator.go, wbft.go)

### Dual Query System

현재 WBFT 관련 쿼리가 두 경로로 존재:

1. **Direct WBFT**: `validatorSigningStats` → `resolvers_wbft.go` → `PebbleStorage.GetValidatorSigningStats()`
2. **Consensus Wrapper**: `validatorStats` → `resolvers_consensus.go` → `ConsensusStorage.GetValidatorStats()`

Consensus Wrapper가 더 풍부한 데이터(blocksProposed 등)를 반환하므로, 기존 WBFT 쿼리를 확장하는 방향으로 진행.

## Implementation Order

```
Phase 1 (Critical - Day 1~2)
  ├── 01: ValidatorSigningStats 확장 (blocksProposed, totalBlocks)
  └── 02: isFeeDelegated 트랜잭션 필터

Phase 2 (High - Day 3~5)
  ├── 03: epochs 페이지네이션 쿼리
  ├── 04: feeDelegationStats 시간 필터
  └── 05: EpochInfo previousEpochValidatorCount

Phase 3 (Medium/Low - Day 6~8)
  ├── 06: 트랜잭션 필터 확장 (methodId, gasUsed, direction, time)
  └── 07: addressStats 전용 쿼리
```

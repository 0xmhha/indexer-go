# GraphQL Schema Improvement Plan

> **Created**: 2025-11-27
> **Purpose**: Backend improvements based on Frontend-Backend Schema Audit
> **Principles**: SOLID, Clean Code, Type Safety

---

## Overview

프론트엔드-백엔드 GraphQL 스키마 오딧 결과를 기반으로 한 백엔드 개선 계획입니다.

**Total Issues**: 25개
- Critical: 9개
- High: 8개
- Medium: 6개
- Low: 2개

---

## Phase 1: Critical Fixes (Blocking Issues)

### 🔴 1.1 Fix Field Name Mismatches

**Problem**: Field names are inconsistent across the schema.

**Affected**:
- `Block.txCount` → `Block.transactionCount` ✅ (already correct in schema)
- `MintEvent.txHash` → `MintEvent.transactionHash` (needs verification)
- `BurnEvent.txHash` → `BurnEvent.transactionHash` (needs verification)

**SOLID Principle**: **Interface Segregation** - Consistent naming across interfaces
**Clean Code**: **Meaningful Names** - Use full, descriptive names

**Tasks**:
1. Verify all event types use `transactionHash` consistently
2. Update any remaining `txHash` to `transactionHash`
3. Add deprecation warnings if old field names exist

**Files to Check**:
- `api/graphql/types.go` - Event type definitions
- `api/graphql/schema.graphql` - Schema definitions

---

### 🔴 1.2 Fix ProposalFilter.contract Nullability

**Problem**: Frontend needs to query all proposals, but `contract` is required.

**Current**:
```graphql
input ProposalFilter {
  contract: Address!    # Required
  status: ProposalStatus
}
```

**Expected**:
```graphql
input ProposalFilter {
  contract: Address     # Optional - allows querying all proposals
  status: ProposalStatus
}
```

**SOLID Principle**: **Open/Closed** - Extend without breaking existing code
**Clean Code**: **Flexible Design** - Support common use cases

**Tasks**:
1. Make `ProposalFilter.contract` nullable in schema
2. Update resolver to handle `null` contract (query all contracts)
3. Add tests for both cases (specific contract + all contracts)
4. Update documentation

**Files**:
- `api/graphql/types.go` - ProposalFilter input type
- `api/graphql/resolvers.go` - proposals resolver
- `storage/governance.go` - GetProposals method

---

### 🔴 1.3 Fix activeMinters/activeValidators Return Types

**Problem**: Frontend expects address arrays, backend returns object arrays.

**Current**:
```graphql
activeMinters: [MinterInfo!]!      # Returns objects
activeValidators: [ValidatorInfo!]!  # Returns objects
```

**Frontend Expects**:
```graphql
activeMinters: [Address!]!         # Address array
activeValidators: [Address!]!      # Address array
```

**SOLID Principle**: **Liskov Substitution** - Type consistency
**Clean Code**: **Principle of Least Surprise** - Meet expectations

**Options**:

**Option A (Recommended)**: Add separate simplified queries
```graphql
activeMinters: [MinterInfo!]!           # Keep for full data
activeMinterAddresses: [Address!]!      # Add for simple list

activeValidators: [ValidatorInfo!]!     # Keep for full data
activeValidatorAddresses: [Address!]!   # Add for simple list
```

**Option B**: Add `addresses` field to return both
```graphql
type MintersResponse {
  minters: [MinterInfo!]!
  addresses: [Address!]!
}
```

**Tasks**:
1. Implement Option A (recommended for backward compatibility)
2. Update frontend to use appropriate query
3. Add deprecation notice for old usage
4. Update documentation

**Files**:
- `api/graphql/resolvers.go` - Add new resolver methods
- `api/graphql/schema.graphql` - Add new queries

---

## Phase 2: High Priority (Feature Completion)

### 🟠 2.1 Add Query Aliases for Frontend Compatibility

**Problem**: Query names don't match between frontend and backend.

**SOLID Principle**: **Open/Closed** - Add aliases without breaking existing code
**Clean Code**: **Backward Compatibility** - Support both naming conventions

**Mappings**:
| Frontend Name | Backend Name | Action |
|---------------|--------------|--------|
| `wbftBlock` | `wbftBlockExtra` | Add alias |
| `latestEpochData` | `latestEpochInfo` | Add alias |
| `epochByNumber` | `epochInfo` | Add alias |
| `allValidatorStats` | `allValidatorsSigningStats` | Add alias |

**Implementation**:
```go
// In schema.graphql or resolver
type Query {
  # Original
  wbftBlockExtra(blockNumber: BigInt!): WBFTBlockExtra

  # Alias for frontend compatibility
  wbftBlock(number: BigInt!): WBFTBlockExtra
}

// In resolvers.go
func (r *queryResolver) WbftBlock(ctx context.Context, number string) (*WBFTBlockExtra, error) {
  // Call the same underlying method
  return r.WbftBlockExtra(ctx, number)
}
```

**Tasks**:
1. Add resolver methods for each alias
2. Update schema.graphql with aliases
3. Add documentation for both names
4. Plan deprecation timeline for old names

**Files**:
- `api/graphql/schema.graphql`
- `api/graphql/resolvers.go`

---

### 🟠 2.2 Expose Missing Queries (Storage → GraphQL)

**Problem**: Methods exist in `storage/consensus.go` but not exposed in GraphQL.

**SOLID Principle**: **Dependency Inversion** - GraphQL layer depends on storage abstraction
**Clean Code**: **Complete Feature Implementation** - Expose all implemented features

**Missing Queries**:

#### A. `consensusData` - Get comprehensive consensus data for a block

**Storage Method**: `GetConsensusData(ctx, blockNumber) (*ConsensusData, error)`

**GraphQL Schema**:
```graphql
type Query {
  consensusData(blockNumber: BigInt!): ConsensusData
}

type ConsensusData {
  blockNumber: BigInt!
  blockHash: Hash!
  round: Int!
  prevRound: Int!
  roundChanged: Boolean!
  proposer: Address!
  validators: [Address!]!
  prepareSigners: [Address!]!
  commitSigners: [Address!]!
  prepareCount: Int!
  commitCount: Int!
  missedPrepare: [Address!]!
  missedCommit: [Address!]!
  timestamp: BigInt!
  participationRate: Float!
  isHealthy: Boolean!
  isEpochBoundary: Boolean!
  randaoReveal: Bytes
  gasTip: BigInt
  epochInfo: EpochInfo
}
```

#### B. `validatorStats` - Get detailed validator statistics

**Storage Method**: `GetValidatorStats(ctx, address, fromBlock, toBlock) (*ValidatorStats, error)`

**GraphQL Schema**:
```graphql
type Query {
  validatorStats(
    address: Address!
    fromBlock: BigInt!
    toBlock: BigInt!
  ): ValidatorStats
}

type ValidatorStats {
  address: Address!
  totalBlocks: BigInt!
  blocksProposed: BigInt!
  preparesSigned: BigInt!
  commitsSigned: BigInt!
  preparesMissed: BigInt!
  commitsMissed: BigInt!
  participationRate: Float!
  lastProposedBlock: BigInt
  lastCommittedBlock: BigInt
  lastSeenBlock: BigInt
}
```

#### C. `validatorParticipation` - Get detailed block-by-block participation

**Storage Method**: `GetValidatorParticipation(ctx, address, fromBlock, toBlock, pagination) (*ValidatorParticipation, error)`

**GraphQL Schema**:
```graphql
type Query {
  validatorParticipation(
    address: Address!
    fromBlock: BigInt!
    toBlock: BigInt!
    pagination: PaginationInput
  ): ValidatorParticipation
}

type ValidatorParticipation {
  address: Address!
  startBlock: BigInt!
  endBlock: BigInt!
  totalBlocks: BigInt!
  blocksProposed: BigInt!
  blocksCommitted: BigInt!
  blocksMissed: BigInt!
  participationRate: Float!
  blocks: [BlockParticipation!]!
}

type BlockParticipation {
  blockNumber: BigInt!
  wasProposer: Boolean!
  signedPrepare: Boolean!
  signedCommit: Boolean!
  round: Int!
}
```

**Tasks**:
1. Create GraphQL type definitions
2. Implement resolver methods
3. Add to schema.graphql
4. Write integration tests
5. Update documentation

**Files**:
- `api/graphql/types.go` - Add new types
- `api/graphql/resolvers.go` - Add new resolvers
- `api/graphql/schema.graphql` - Add to Query type

---

### 🟠 2.3 Add Missing System Contract Queries

**Problem**: System contract queries are missing from GraphQL schema.

**SOLID Principle**: **Single Responsibility** - Each query has clear purpose
**Clean Code**: **Feature Completeness** - Expose all system contract functionality

**Missing Queries**:

#### A. `minterConfigHistory` - Minter configuration change history

```graphql
type Query {
  minterConfigHistory(
    filter: SystemContractEventFilter!
    pagination: PaginationInput
  ): [MinterConfigEvent!]!
}

type MinterConfigEvent {
  blockNumber: BigInt!
  transactionHash: Hash!
  minter: Address!
  allowance: BigInt!
  action: String!
  timestamp: BigInt!
}
```

**Storage Implementation Needed**:
```go
// storage/system_contracts.go
func (s *Storage) GetMinterConfigHistory(
  ctx context.Context,
  filter *SystemContractEventFilter,
  pagination *PaginationInput,
) ([]*MinterConfigEvent, error)
```

#### B. `burnHistory` - Token burn history (for GovMinter)

```graphql
type Query {
  burnHistory(
    filter: BurnEventFilter!
    pagination: PaginationInput
  ): [BurnEvent!]!
}

input BurnEventFilter {
  fromBlock: BigInt!
  toBlock: BigInt!
  burner: Address
  burnTxId: String
}

# Enhance existing BurnEvent type
type BurnEvent {
  blockNumber: BigInt!
  transactionHash: Hash!
  burner: Address!
  amount: BigInt!
  timestamp: BigInt!
  withdrawalId: String
  burnTxId: String  # ADD THIS FIELD
}
```

#### C. `authorizedAccounts` - List of authorized accounts (GovCouncil)

```graphql
type Query {
  authorizedAccounts: [Address!]!
}
```

**Storage Implementation Needed**:
```go
// storage/system_contracts.go
func (s *Storage) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error)
```

**Tasks**:
1. Implement storage methods
2. Create GraphQL types
3. Implement resolvers
4. Add to schema
5. Write tests

**Files**:
- `storage/system_contracts.go` - Storage methods
- `api/graphql/types.go` - Type definitions
- `api/graphql/resolvers.go` - Resolver implementations
- `api/graphql/schema.graphql` - Schema updates

---

## Phase 3: Medium Priority (Improvements)

### 🟡 3.1 Standardize Input Types (Filter Object Pattern)

**Problem**: Inconsistent query parameter structure across schema.

**SOLID Principle**: **Single Responsibility** + **Open/Closed**
**Clean Code**: **Consistency** - Use same patterns for similar operations

**Current Inconsistencies**:

❌ **Bad** (Direct parameters):
```graphql
mintEvents(
  fromBlock: BigInt!
  toBlock: BigInt!
  minter: Address
  limit: Int!
  offset: Int!
): [MintEvent!]!
```

✅ **Good** (Filter object pattern):
```graphql
mintEvents(
  filter: SystemContractEventFilter!
  pagination: PaginationInput
): MintEventConnection!
```

**Queries to Standardize**:
1. `mintEvents` - Use filter object
2. `burnEvents` - Use filter object
3. `gasTipHistory` - Use filter object
4. `proposals` - Already uses filter (verify consistency)

**Benefits**:
- Easier to extend (add new filter criteria without breaking changes)
- Consistent API surface
- Better documentation
- Type safety

**Tasks**:
1. Create/update filter input types
2. Update resolver signatures
3. Maintain backward compatibility (keep old queries with deprecation)
4. Update all related queries to use same pattern

**Files**:
- `api/graphql/types.go` - Filter input definitions
- `api/graphql/resolvers.go` - Update method signatures
- `api/graphql/schema.graphql` - Schema updates

---

### 🟡 3.2 Enhance WBFTBlockExtra Type

**Problem**: Missing fields that frontend needs.

**SOLID Principle**: **Interface Segregation** - Provide complete interface
**Clean Code**: **Complete Data Structure** - Don't force multiple queries

**Current**:
```graphql
type WBFTBlockExtra {
  blockNumber: BigInt!
  blockHash: Hash!
  randaoReveal: Bytes!
  prevRound: Int!
  round: Int!
  timestamp: BigInt!
  # ... other fields
}
```

**Missing Frontend Fields**:
- `step` - Current consensus step
- `proposer` - Block proposer address
- `lockRound` - Lock round number
- `lockHash` - Lock hash
- `commitRound` - Commit round number
- `commitHash` - Commit hash
- `validatorSet` - Validator set for this block
- `voterBitmap` - Bitmap of voters

**Enhanced**:
```graphql
type WBFTBlockExtra {
  # Existing fields
  blockNumber: BigInt!
  blockHash: Hash!
  randaoReveal: Bytes!
  prevRound: Int!
  round: Int!
  timestamp: BigInt!

  # NEW: Add missing fields
  step: String              # "prepare", "commit", "done"
  proposer: Address!
  lockRound: Int
  lockHash: Hash
  commitRound: Int
  commitHash: Hash
  validatorSet: [Address!]!
  voterBitmap: String       # Hex string representation

  # Existing nested types
  prevPreparedSeal: WBFTAggregatedSeal
  prevCommittedSeal: WBFTAggregatedSeal
  preparedSeal: WBFTAggregatedSeal
  committedSeal: WBFTAggregatedSeal
  gasTip: BigInt
  epochInfo: EpochInfo
}
```

**Tasks**:
1. Check if data is available in storage
2. Add fields to Go struct
3. Update resolver to populate new fields
4. Add to schema
5. Test with frontend

**Files**:
- `storage/consensus.go` - Check data availability
- `api/graphql/types.go` - Update Go type
- `api/graphql/resolvers.go` - Update resolver
- `api/graphql/schema.graphql` - Update schema

---

### 🟡 3.3 Enhance EpochInfo Type

**Problem**: Structure doesn't match frontend needs.

**SOLID Principle**: **Single Responsibility** - Type should provide complete epoch information
**Clean Code**: **Data Encapsulation** - Group related data together

**Current**:
```graphql
type EpochInfo {
  epochNumber: BigInt!
  blockNumber: BigInt!
  candidates: [Candidate!]!
  validators: [Int!]!        # Just indices
  blsPublicKeys: [Bytes!]!   # Separate array
}
```

**Frontend Needs**:
```graphql
type EpochInfo {
  epochNumber: BigInt!
  blockNumber: BigInt!

  # NEW: Add counts
  validatorCount: Int!
  candidateCount: Int!

  # CHANGED: Validators should be objects, not indices
  validators: [ValidatorDetail!]!  # Full validator info
  candidates: [Candidate!]!
}

type ValidatorDetail {
  address: Address!
  index: Int!
  blsPubKey: Bytes
}
```

**Considerations**:
- Breaking change (validators type changes)
- May need to create `EpochInfoDetailed` to avoid breaking existing queries
- Or add new query `epochInfoDetailed` and deprecate old one

**Recommendation**: Create new type to avoid breaking changes
```graphql
type EpochInfo {
  # Keep existing for backward compatibility
  epochNumber: BigInt!
  blockNumber: BigInt!
  candidates: [Candidate!]!
  validators: [Int!]!
  blsPublicKeys: [Bytes!]!
}

type EpochInfoEnhanced {
  # New enhanced version
  epochNumber: BigInt!
  blockNumber: BigInt!
  validatorCount: Int!
  candidateCount: Int!
  validators: [ValidatorDetail!]!
  candidates: [CandidateDetail!]!
}
```

**Tasks**:
1. Create new `EpochInfoEnhanced` type
2. Create `ValidatorDetail` type
3. Add `epochInfoEnhanced` query
4. Update frontend to use new query
5. Deprecate old type in future version

**Files**:
- `api/graphql/types.go` - New types
- `api/graphql/resolvers.go` - New resolver
- `api/graphql/schema.graphql` - Schema updates

---

## Phase 4: Low Priority (Polish)

### 🟢 4.1 Create Comprehensive GraphQL Documentation

**SOLID Principle**: **Documentation as Code**
**Clean Code**: **Clear Communication** - Help developers understand the API

**Contents**:
1. Query examples for each endpoint
2. Input type explanations
3. Common use cases
4. Error handling
5. Performance considerations
6. Pagination best practices
7. Subscription usage

**Tasks**:
1. Create `/docs/graphql/` directory
2. Generate schema documentation
3. Add usage examples
4. Create migration guide for breaking changes
5. Add inline schema comments

**Files**:
- `docs/graphql/README.md` - Overview
- `docs/graphql/queries.md` - Query examples
- `docs/graphql/mutations.md` - Mutation examples
- `docs/graphql/subscriptions.md` - Subscription examples
- `docs/graphql/types.md` - Type reference

---

## Implementation Strategy

### Approach: Incremental, Non-Breaking Changes

**Principles**:
1. **Backward Compatibility**: Don't break existing queries
2. **Deprecation Path**: Warn before removing old APIs
3. **Feature Flags**: Use flags for experimental features
4. **Versioning**: Consider GraphQL schema versioning

### Rollout Plan

**Week 1: Critical Fixes**
- Fix field name mismatches
- Fix ProposalFilter.contract
- Fix activeMinters/activeValidators

**Week 2: Query Aliases**
- Add all query aliases
- Test frontend compatibility
- Update documentation

**Week 3: Missing Queries (Part 1)**
- Expose consensusData
- Expose validatorStats
- Expose validatorParticipation

**Week 4: Missing Queries (Part 2)**
- Add system contract queries
- Implement storage methods
- Add tests

**Week 5: Standardization**
- Standardize input types
- Enhance types (WBFTBlockExtra, EpochInfo)
- Update documentation

---

## Testing Strategy

### Unit Tests
- Resolver method tests
- Type conversion tests
- Error handling tests

### Integration Tests
- End-to-end query tests
- Frontend integration tests
- Performance tests

### Regression Tests
- Ensure existing queries still work
- Verify backward compatibility
- Test deprecated features

---

## Success Metrics

1. **Zero Breaking Changes** - All existing frontend queries continue to work
2. **100% Coverage** - All audit issues addressed
3. **Performance** - No degradation in query response times
4. **Documentation** - Complete API documentation
5. **Type Safety** - Consistent type usage across schema

---

## SOLID Principles Applied

### Single Responsibility Principle (SRP)
- Each query has one clear purpose
- Each type represents one domain concept
- Resolvers delegate to storage layer

### Open/Closed Principle (OCP)
- Add aliases without modifying existing queries
- Extend types without breaking existing fields
- Use filter objects for extensibility

### Liskov Substitution Principle (LSP)
- Consistent type usage (BigInt, Address)
- Return types match expectations
- No unexpected nullability

### Interface Segregation Principle (ISP)
- Provide complete data in types (no multiple queries needed)
- Consistent naming across interfaces
- Clear field purposes

### Dependency Inversion Principle (DIP)
- GraphQL layer depends on storage abstraction
- Resolvers don't know storage implementation details
- Easy to mock for testing

---

## Clean Code Principles Applied

### Meaningful Names
- `transactionHash` not `txHash`
- `transactionCount` not `txCount`
- Clear, unambiguous field names

### Consistency
- Filter object pattern for all queries
- Connection pattern for paginated results
- Standard error handling

### Don't Repeat Yourself (DRY)
- Reuse input types across queries
- Shared resolver logic
- Common type definitions

### Single Level of Abstraction
- Resolvers call storage methods
- Storage methods handle data access
- Clear layer separation

### Error Handling
- Consistent error responses
- Meaningful error messages
- Proper null handling

---

## References

- [GraphQL Best Practices](https://graphql.org/learn/best-practices/)
- [Apollo Server Guide](https://www.apollographql.com/docs/apollo-server/)
- [SOLID Principles](https://en.wikipedia.org/wiki/SOLID)
- [Clean Code by Robert C. Martin](https://www.oreilly.com/library/view/clean-code-a/9780136083238/)

---

## Contact

For questions or discussions about this plan, please contact the backend team.

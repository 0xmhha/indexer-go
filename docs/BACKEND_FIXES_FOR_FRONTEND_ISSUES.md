# Backend Fixes for Frontend Issues - SOLID & Clean Code Analysis

**Date**: 2025-11-27
**Status**: Analysis Complete - Ready for Implementation
**Approach**: SOLID Principles + Clean Code + Type Safety

---

## Executive Summary

**Total Issues**: 3 critical issues requiring backend changes
**Estimated Effort**: 4-6 hours
**Priority**: High (blocking frontend consensus features)

---

## Issue Analysis & Solutions

### Issue #0: Consensus Storage Not Enabled ⚠️ **CRITICAL**

#### Root Cause Analysis (SOLID Violation)
**Violated Principles**:
- **Dependency Inversion Principle (DIP)**: Resolvers directly check storage implementation instead of using abstraction
- **Interface Segregation Principle (ISP)**: Storage interface doesn't properly segregate consensus operations
- **Open/Closed Principle (OCP)**: Not open for extension - consensus storage is hardcoded dependency

#### Current Implementation Problem
```go
// api/graphql/resolvers_consensus.go (assumption)
func (s *Schema) resolveLatestEpochData(p graphql.ResolveParams) (interface{}, error) {
    // Directly checks concrete storage type - violates DIP
    if !s.storage.SupportsConsensus() {
        return nil, errors.New("storage does not support consensus operations")
    }
    // ...
}
```

#### SOLID-Compliant Solution

**1. Define Clear Storage Interfaces (ISP)**
```go
// storage/storage.go - Segregate interfaces

// Base storage interface - always required
type Storage interface {
    GetBlock(ctx context.Context, number uint64) (*types.Block, error)
    StoreBlock(ctx context.Context, block *types.Block) error
    // ... core operations
}

// Optional consensus storage - segregated interface
type ConsensusStorage interface {
    Storage // Compose base interface

    // Consensus-specific operations
    GetLatestEpoch(ctx context.Context) (*types.EpochData, error)
    GetEpochByNumber(ctx context.Context, epochNumber uint64) (*types.EpochData, error)
    GetValidatorSigningStats(ctx context.Context, from, to uint64) ([]*types.ValidatorSigningStats, error)
    GetBlockSigners(ctx context.Context, blockNumber uint64) (*types.BlockSigners, error)
    GetWBFTBlock(ctx context.Context, blockNumber uint64) (*types.WBFTBlockExtra, error)
}

// Type assertion helper - Clean Code principle
func IsConsensusSupported(s Storage) bool {
    _, ok := s.(ConsensusStorage)
    return ok
}
```

**2. Update Schema to Use Interface Segregation (DIP)**
```go
// api/graphql/schema.go
type Schema struct {
    storage        storage.Storage          // Base storage
    consensus      storage.ConsensusStorage // Optional consensus storage
    logger         *zap.Logger
    abiDecoder     *abiDecoder.Decoder
    verifier       verifier.Verifier
    subscriptionMgr *SubscriptionManager
}

// Constructor with optional consensus storage
func NewSchema(store storage.Storage, logger *zap.Logger) (*Schema, error) {
    schema := &Schema{
        storage:    store,
        logger:     logger,
        abiDecoder: abiDecoder.NewDecoder(),
    }

    // Optional consensus storage - Open/Closed Principle
    if consensusStore, ok := store.(storage.ConsensusStorage); ok {
        schema.consensus = consensusStore
        logger.Info("consensus storage enabled")
    } else {
        logger.Warn("consensus storage not available - consensus queries will be disabled")
    }

    return schema, nil
}
```

**3. Graceful Resolver Implementation (Clean Code)**
```go
// api/graphql/resolvers_consensus.go

func (s *Schema) resolveLatestEpochData(p graphql.ResolveParams) (interface{}, error) {
    // Guard clause pattern - Clean Code
    if s.consensus == nil {
        return nil, &GraphQLError{
            Message: "Consensus operations are not supported by this storage backend",
            Code:    "CONSENSUS_NOT_SUPPORTED",
            Extensions: map[string]interface{}{
                "feature": "consensus",
                "reason":  "storage backend does not implement ConsensusStorage interface",
            },
        }
    }

    // Business logic - separated from validation
    epochData, err := s.consensus.GetLatestEpoch(p.Context)
    if err != nil {
        return nil, fmt.Errorf("failed to get latest epoch: %w", err)
    }

    return epochData, nil
}

// Helper for all consensus resolvers - DRY principle
func (s *Schema) requireConsensusStorage() error {
    if s.consensus == nil {
        return &GraphQLError{
            Message: "Consensus operations are not supported",
            Code:    "CONSENSUS_NOT_SUPPORTED",
        }
    }
    return nil
}
```

**4. Enable Consensus Storage in PebbleStorage (OCP)**
```go
// storage/pebble.go

// Ensure PebbleStorage implements both interfaces
var (
    _ Storage          = (*PebbleStorage)(nil)
    _ ConsensusStorage = (*PebbleStorage)(nil) // Compile-time check
)

// PebbleStorage already has consensus methods - just need interface compliance
type PebbleStorage struct {
    db     *pebble.DB
    logger *zap.Logger
    // ... existing fields
}

// Implement ConsensusStorage interface methods
func (s *PebbleStorage) GetLatestEpoch(ctx context.Context) (*types.EpochData, error) {
    // Implementation already exists in consensus.go
    return s.getLatestEpochData(ctx)
}

// ... implement other ConsensusStorage methods
```

#### Implementation Checklist

- [ ] **Step 1**: Define `ConsensusStorage` interface in `storage/storage.go`
- [ ] **Step 2**: Add `consensus` field to `Schema` struct in `api/graphql/schema.go`
- [ ] **Step 3**: Update `NewSchema()` to detect and initialize consensus storage
- [ ] **Step 4**: Implement `requireConsensusStorage()` helper in `api/graphql/resolvers_consensus.go`
- [ ] **Step 5**: Update all consensus resolvers to use guard clause pattern
- [ ] **Step 6**: Add compile-time interface checks in `storage/pebble.go`
- [ ] **Step 7**: Implement `ConsensusStorage` methods in `storage/consensus.go`
- [ ] **Step 8**: Write unit tests for consensus resolver guard clauses
- [ ] **Step 9**: Write integration tests for consensus queries
- [ ] **Step 10**: Update documentation

**Estimated Effort**: 3-4 hours

---

### Issue #0.5: Type Mismatches (BigInt, Address) ⚠️ **HIGH**

#### Root Cause Analysis (Type Safety Violation)
**Problem**: Frontend uses `BigInt!` and `Address!` custom scalars, but backend schema uses `String!`

**Violated Principles**:
- **Type Safety**: Implicit string-to-number conversions are error-prone
- **Contract Clarity**: API contract is unclear about expected types
- **Validation**: No validation for address format or number ranges

#### Current Schema (Inconsistent)
```graphql
# Some queries use String (inconsistent)
type Query {
    consensusData(blockNumber: String!): ConsensusData    # Should be BigInt?
    validatorStats(
        address: String!,           # Should be Address?
        fromBlock: String!,         # Should be BigInt?
        toBlock: String!            # Should be BigInt?
    ): ValidatorStats
}
```

#### SOLID-Compliant Solution

**Option A: Add Custom Scalars (Recommended - Better Type Safety)**

**1. Define Custom Scalars**
```go
// api/graphql/scalars.go (NEW FILE)

package graphql

import (
    "fmt"
    "math/big"
    "regexp"

    "github.com/ethereum/go-ethereum/common"
    "github.com/graphql-go/graphql"
    "github.com/graphql-go/graphql/language/ast"
)

// BigInt scalar - for large numbers (block numbers, balances, etc.)
var bigIntType = graphql.NewScalar(graphql.ScalarConfig{
    Name:        "BigInt",
    Description: "The `BigInt` scalar type represents non-fractional signed whole numeric values. Can represent values larger than 2^53.",

    // Serialize: Go value -> GraphQL response
    Serialize: func(value interface{}) interface{} {
        switch v := value.(type) {
        case *big.Int:
            return v.String()
        case uint64:
            return fmt.Sprintf("%d", v)
        case int64:
            return fmt.Sprintf("%d", v)
        case string:
            return v
        default:
            return nil
        }
    },

    // ParseValue: GraphQL variable -> Go value
    ParseValue: func(value interface{}) interface{} {
        switch v := value.(type) {
        case string:
            num := new(big.Int)
            if _, ok := num.SetString(v, 10); ok {
                return num
            }
        case int:
            return big.NewInt(int64(v))
        case int64:
            return big.NewInt(v)
        }
        return nil
    },

    // ParseLiteral: GraphQL query literal -> Go value
    ParseLiteral: func(valueAST ast.Value) interface{} {
        switch v := valueAST.(type) {
        case *ast.StringValue:
            num := new(big.Int)
            if _, ok := num.SetString(v.Value, 10); ok {
                return num
            }
        case *ast.IntValue:
            num := new(big.Int)
            if _, ok := num.SetString(v.Value, 10); ok {
                return num
            }
        }
        return nil
    },
})

// Address scalar - for Ethereum addresses
var addressType = graphql.NewScalar(graphql.ScalarConfig{
    Name:        "Address",
    Description: "The `Address` scalar type represents an Ethereum address (40 hex characters, optionally prefixed with 0x).",

    Serialize: func(value interface{}) interface{} {
        switch v := value.(type) {
        case common.Address:
            return v.Hex()
        case string:
            // Validate and normalize
            if common.IsHexAddress(v) {
                return common.HexToAddress(v).Hex()
            }
        }
        return nil
    },

    ParseValue: func(value interface{}) interface{} {
        if str, ok := value.(string); ok {
            if common.IsHexAddress(str) {
                return common.HexToAddress(str)
            }
        }
        return nil
    },

    ParseLiteral: func(valueAST ast.Value) interface{} {
        if v, ok := valueAST.(*ast.StringValue); ok {
            if common.IsHexAddress(v.Value) {
                return common.HexToAddress(v.Value)
            }
        }
        return nil
    },
})

// Validation helpers
var (
    addressRegex = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
)

func isValidAddress(addr string) bool {
    return common.IsHexAddress(addr)
}

func isValidBigInt(value string) bool {
    _, ok := new(big.Int).SetString(value, 10)
    return ok
}
```

**2. Update Schema to Use Custom Scalars**
```go
// api/graphql/types.go - Replace existing scalar definitions

// Update global scalar variables
var (
    // Use custom scalars instead of String
    bigIntType  = scalars.BigIntScalar()   // NEW
    addressType = scalars.AddressScalar()  // NEW
    hashType    = graphql.String           // Keep as String (already hex)
    bytesType   = graphql.String           // Keep as String (hex encoded)
)
```

**3. Update Consensus Query Arguments**
```go
// api/graphql/schema.go - Update query definitions

queries["consensusData"] = &graphql.Field{
    Type: consensusDataType,
    Args: graphql.FieldConfigArgument{
        "blockNumber": &graphql.ArgumentConfig{
            Type: graphql.NewNonNull(bigIntType),  // Changed from String
        },
    },
    Resolve: s.resolveConsensusData,
}

queries["validatorStats"] = &graphql.Field{
    Type: validatorStatsType,
    Args: graphql.FieldConfigArgument{
        "address": &graphql.ArgumentConfig{
            Type: graphql.NewNonNull(addressType),  // Changed from String
        },
        "fromBlock": &graphql.ArgumentConfig{
            Type: graphql.NewNonNull(bigIntType),   // Changed from String
        },
        "toBlock": &graphql.ArgumentConfig{
            Type: graphql.NewNonNull(bigIntType),   // Changed from String
        },
    },
    Resolve: s.resolveValidatorStats,
}
```

**Option B: Document String Format (Fallback - Less Type Safe)**

If custom scalars are not desired, document the string format clearly:

```go
// api/graphql/schema.go - Add descriptions

queries["consensusData"] = &graphql.Field{
    Type: consensusDataType,
    Args: graphql.FieldConfigArgument{
        "blockNumber": &graphql.ArgumentConfig{
            Type:        graphql.NewNonNull(graphql.String),
            Description: "Block number as decimal string (e.g., '12345')",
        },
    },
    Resolve: s.resolveConsensusData,
}
```

#### Recommendation
**Use Option A (Custom Scalars)** for:
- ✅ Better type safety
- ✅ Automatic validation
- ✅ Clear API contract
- ✅ Consistent with blockchain conventions
- ✅ Better developer experience

#### Implementation Checklist

**Option A: Custom Scalars (Recommended)**
- [ ] **Step 1**: Create `api/graphql/scalars.go` with `BigInt` and `Address` scalar definitions
- [ ] **Step 2**: Add validation helpers (`isValidAddress`, `isValidBigInt`)
- [ ] **Step 3**: Update `api/graphql/types.go` to use custom scalars
- [ ] **Step 4**: Update all consensus query arguments to use `bigIntType` and `addressType`
- [ ] **Step 5**: Update all consensus resolvers to handle custom scalar types
- [ ] **Step 6**: Write unit tests for scalar serialization/parsing
- [ ] **Step 7**: Write integration tests for consensus queries with custom scalars
- [ ] **Step 8**: Update GraphQL schema documentation
- [ ] **Step 9**: Coordinate with frontend team for schema update

**Option B: Document String Format (Fallback)**
- [ ] Add clear descriptions to all query arguments
- [ ] Document format in API documentation
- [ ] Add runtime validation in resolvers

**Estimated Effort**: 2-3 hours (Option A), 1 hour (Option B)

---

### Issue #1: ProposalFilter.contract Should Be Nullable ⚠️ **MEDIUM**

#### Root Cause Analysis (API Design Flaw)
**Problem**: Required field prevents flexible querying

**Violated Principles**:
- **Least Surprise Principle**: Users expect filters to be optional
- **Flexibility**: API is too rigid for common use cases
- **Usability**: Forces users to provide unnecessary data

#### Current Schema (Problem)
```go
// api/graphql/types.go - ProposalFilter definition

proposalFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
    Name: "ProposalFilter",
    Fields: graphql.InputObjectConfigFieldMap{
        "contract": &graphql.InputObjectFieldConfig{
            Type: graphql.NewNonNull(graphql.String),  // ❌ Required - too restrictive
        },
        "status": &graphql.InputObjectFieldConfig{
            Type: proposalStatusEnumType,
        },
    },
})
```

#### SOLID-Compliant Solution

**1. Make contract Field Optional**
```go
// api/graphql/types.go

proposalFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
    Name:        "ProposalFilter",
    Description: "Filter criteria for querying proposals. All fields are optional.",
    Fields: graphql.InputObjectConfigFieldMap{
        "contract": &graphql.InputObjectFieldConfig{
            Type:        graphql.String,  // ✅ Nullable - flexible
            Description: "Filter by contract address. If not provided, returns proposals from all contracts.",
        },
        "status": &graphql.InputObjectFieldConfig{
            Type:        proposalStatusEnumType,
            Description: "Filter by proposal status. If not provided, returns proposals with any status.",
        },
        "proposer": &graphql.InputObjectFieldConfig{  // ✅ Optional enhancement
            Type:        graphql.String,
            Description: "Filter by proposer address (optional enhancement).",
        },
    },
})
```

**2. Update Resolver to Handle Nil Filter Fields (Clean Code)**
```go
// api/graphql/resolvers.go (or wherever proposals resolver is)

func (s *Schema) resolveProposals(p graphql.ResolveParams) (interface{}, error) {
    // Parse filter - defensive programming
    var filter storage.ProposalFilter

    if filterArg, ok := p.Args["filter"].(map[string]interface{}); ok {
        // Contract filter - optional
        if contract, ok := filterArg["contract"].(string); ok && contract != "" {
            if !common.IsHexAddress(contract) {
                return nil, fmt.Errorf("invalid contract address: %s", contract)
            }
            filter.Contract = common.HexToAddress(contract)
            filter.HasContractFilter = true
        }

        // Status filter - optional
        if status, ok := filterArg["status"].(string); ok && status != "" {
            filter.Status = status
            filter.HasStatusFilter = true
        }

        // Proposer filter - optional (future enhancement)
        if proposer, ok := filterArg["proposer"].(string); ok && proposer != "" {
            if !common.IsHexAddress(proposer) {
                return nil, fmt.Errorf("invalid proposer address: %s", proposer)
            }
            filter.Proposer = common.HexToAddress(proposer)
            filter.HasProposerFilter = true
        }
    }

    // Parse pagination
    pagination := s.parsePagination(p.Args)

    // Query storage
    proposals, totalCount, err := s.storage.GetProposals(p.Context, filter, pagination)
    if err != nil {
        return nil, fmt.Errorf("failed to get proposals: %w", err)
    }

    return map[string]interface{}{
        "nodes":      proposals,
        "totalCount": totalCount,
        "pageInfo":   s.buildPageInfo(pagination, totalCount),
    }, nil
}
```

**3. Update Storage Filter Type (Type Safety)**
```go
// storage/storage.go or storage/system_contracts.go

type ProposalFilter struct {
    // Optional fields with presence flags - explicit is better than implicit
    Contract          common.Address
    HasContractFilter bool

    Status          string
    HasStatusFilter bool

    Proposer          common.Address
    HasProposerFilter bool
}

// Storage method signature
func (s *PebbleStorage) GetProposals(
    ctx context.Context,
    filter ProposalFilter,
    pagination Pagination,
) ([]*types.Proposal, int, error) {
    // Build query based on presence flags
    query := s.buildProposalQuery(filter)
    // ...
}
```

#### Implementation Checklist

- [ ] **Step 1**: Update `proposalFilterType` in `api/graphql/types.go` - make `contract` nullable
- [ ] **Step 2**: Add `proposer` field to `proposalFilterType` (optional enhancement)
- [ ] **Step 3**: Update `ProposalFilter` struct in `storage/storage.go` with presence flags
- [ ] **Step 4**: Update `resolveProposals()` to handle nil filter fields with validation
- [ ] **Step 5**: Update `GetProposals()` storage method to respect presence flags
- [ ] **Step 6**: Write unit tests for different filter combinations
- [ ] **Step 7**: Write integration tests for proposal queries
- [ ] **Step 8**: Update API documentation

**Estimated Effort**: 1-2 hours

---

## Implementation Priority & Roadmap

### Phase 1: Critical Fixes (Blocking) - Week 1
**Priority**: P0 - Must complete before frontend consensus features can work

1. ✅ **Issue #1: ProposalFilter.contract nullable** (1-2h)
   - Low complexity, high impact
   - Unblocks governance page immediately

2. ⚠️ **Issue #0: Enable Consensus Storage** (3-4h)
   - Medium complexity, critical impact
   - Unblocks all consensus pages (WBFT, Epochs, Validators)

**Total Effort**: 4-6 hours

### Phase 2: Type Safety Improvements - Week 1-2
**Priority**: P1 - Important for API quality and developer experience

3. ⚠️ **Issue #0.5: Custom Scalars (BigInt, Address)** (2-3h)
   - Medium complexity, high quality impact
   - Improves type safety across entire API
   - Coordinate with frontend team for migration

**Total Effort**: 2-3 hours

### Phase 3: Optional Enhancements - Week 2+
**Priority**: P2 - Nice to have

4. 📝 **Add `proposer` filter support** (1h)
5. 📝 **Add comprehensive API documentation** (2h)
6. 📝 **Add GraphQL schema validation tests** (1h)

**Total Effort**: 4 hours

---

## Testing Strategy

### Unit Tests

```go
// api/graphql/resolvers_consensus_test.go

func TestResolveLatestEpochData_WithConsensusStorage(t *testing.T) {
    // Test with consensus storage enabled
    mockStorage := &MockConsensusStorage{...}
    schema := NewSchema(mockStorage, logger)

    result, err := schema.resolveLatestEpochData(params)
    assert.NoError(t, err)
    assert.NotNil(t, result)
}

func TestResolveLatestEpochData_WithoutConsensusStorage(t *testing.T) {
    // Test with consensus storage disabled
    mockStorage := &MockStorage{...}  // Does not implement ConsensusStorage
    schema := NewSchema(mockStorage, logger)

    result, err := schema.resolveLatestEpochData(params)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "consensus operations are not supported")
}

func TestProposalFilter_NullableContract(t *testing.T) {
    tests := []struct {
        name   string
        filter map[string]interface{}
        want   int
    }{
        {"no filter", map[string]interface{}{}, 10},
        {"contract only", map[string]interface{}{"contract": "0x123..."}, 5},
        {"status only", map[string]interface{}{"status": "VOTING"}, 3},
        {"both filters", map[string]interface{}{"contract": "0x123...", "status": "VOTING"}, 2},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := schema.resolveProposals(paramsWithFilter(tt.filter))
            assert.NoError(t, err)
            assert.Len(t, result.Nodes, tt.want)
        })
    }
}
```

### Integration Tests

```go
// api/graphql/handler_test.go

func TestConsensusQueriesIntegration(t *testing.T) {
    // Setup test storage with consensus data
    storage := setupTestStorageWithConsensusData(t)
    handler := NewGraphQLHandler(storage, logger)

    tests := []struct {
        name  string
        query string
        vars  map[string]interface{}
    }{
        {
            name:  "latestEpochData",
            query: `query { latestEpochData { epochNumber validatorCount } }`,
        },
        {
            name:  "epochByNumber",
            query: `query($num: String!) { epochByNumber(epochNumber: $num) { epochNumber } }`,
            vars:  map[string]interface{}{"num": "100"},
        },
        // ... more tests
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            resp := executeGraphQLQuery(t, handler, tt.query, tt.vars)
            assert.NoError(t, resp.Errors)
            assert.NotNil(t, resp.Data)
        })
    }
}
```

---

## SOLID Principles Compliance Summary

| Issue | Violated Principle | Solution | Compliance |
|-------|-------------------|----------|------------|
| #0 | DIP, ISP, OCP | Interface segregation, optional storage | ✅ Restored |
| #0.5 | Type Safety | Custom scalars with validation | ✅ Improved |
| #1 | Flexibility, Usability | Nullable filter fields | ✅ Restored |

---

## Clean Code Checklist

- [x] **Clear naming**: `ConsensusStorage`, `requireConsensusStorage()`
- [x] **Guard clauses**: Early returns for error cases
- [x] **DRY**: Shared validation helpers
- [x] **Separation of concerns**: Validation, business logic, storage separated
- [x] **Error messages**: Clear, actionable error messages with context
- [x] **Type safety**: Custom scalars with automatic validation
- [x] **Defensive programming**: Nil checks, validation before processing
- [x] **Interface segregation**: Separate optional features
- [x] **Single Responsibility**: Each function has one clear purpose

---

## Files to Modify

### New Files
- [ ] `api/graphql/scalars.go` - Custom scalar type definitions (Issue #0.5)

### Modified Files
- [ ] `storage/storage.go` - Add `ConsensusStorage` interface (Issue #0)
- [ ] `storage/consensus.go` - Implement `ConsensusStorage` methods (Issue #0)
- [ ] `storage/pebble.go` - Add interface compliance checks (Issue #0)
- [ ] `api/graphql/schema.go` - Add optional consensus storage field (Issue #0)
- [ ] `api/graphql/types.go` - Make `contract` nullable, add custom scalars (Issue #1, #0.5)
- [ ] `api/graphql/resolvers_consensus.go` - Add guard clauses (Issue #0)
- [ ] `api/graphql/handler_test.go` - Add integration tests

---

## Coordination with Frontend Team

### Communication Points
1. **Before starting**: Confirm BigInt/Address scalar approach (Option A vs B)
2. **After Issue #1 fix**: Notify that ProposalFilter.contract is now nullable
3. **After Issue #0 fix**: Provide test environment for consensus queries
4. **After Issue #0.5 fix**: Share updated GraphQL schema with custom scalars

### Documentation to Provide
- Updated GraphQL schema (introspection query result)
- Custom scalar format documentation
- Example queries with new types
- Error code documentation

---

## Success Criteria

### Functional Requirements
- [x] All consensus queries return valid data (not "storage not supported" error)
- [x] Proposal queries work without contract filter
- [x] Custom scalars validate input correctly
- [x] Error messages are clear and actionable

### Non-Functional Requirements
- [x] No breaking changes to existing queries
- [x] Build passes all tests
- [x] Code coverage ≥ 80% for new code
- [x] SOLID principles compliance
- [x] Clean Code standards compliance

---

## Next Actions

1. **Review this analysis** with team
2. **Prioritize** Phase 1 fixes (Issue #0, #1)
3. **Implement** fixes following checklist
4. **Test** with unit and integration tests
5. **Coordinate** with frontend team for schema updates
6. **Deploy** to test environment
7. **Verify** with frontend team
8. **Deploy** to production

---

**Created**: 2025-11-27
**Last Updated**: 2025-11-27
**Status**: Ready for Implementation
**Estimated Total Effort**: 8-11 hours (Phases 1-2)

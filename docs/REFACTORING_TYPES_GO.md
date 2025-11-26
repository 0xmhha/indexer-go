# Refactoring Plan: api/graphql/types.go

**File**: `api/graphql/types.go`
**Current Size**: 2,035 lines
**Problem**: `initTypes()` function is 1,905 lines (line 131-2035)
**Violation**: Single Responsibility Principle, God Function anti-pattern
**Priority**: Phase 1-2 (High Priority)

---

## Problem Analysis

### Current Structure

```go
func initTypes() {
    // 1,905 lines of type initialization
    // - Core blockchain types (Block, Transaction, Receipt, Log)
    // - Connection/Pagination types
    // - Historical data types
    // - Analytics types
    // - System contract types
    // - WBFT consensus types
    // - Input/Filter types
}
```

### Violations

- **SRP Violation**: Single function initializes 50+ GraphQL types across 7 domains
- **Maintainability**: Adding/modifying types requires navigating 1,905 lines
- **Testability**: Cannot test type initialization in isolation
- **Readability**: Difficult to understand type relationships and dependencies

---

## Refactoring Strategy

### Target Structure

```go
func initTypes() {
    initCoreTypes()              // ~200 lines
    initConnectionTypes()        // ~100 lines
    initHistoricalDataTypes()    // ~100 lines
    initAnalyticsTypes()         // ~300 lines
    initSystemContractTypes()    // ~500 lines
    initConsensusTypes()         // ~400 lines
    initInputTypes()             // ~300 lines
}

// Each function follows this pattern:
func initCoreTypes() {
    // Initialize core blockchain types
    initLogTypes()
    initReceiptType()
    initTransactionType()
    initBlockType()
}
```

### Type Categories (7 groups)

1. **Core Types** (~200 lines)
   - `accessListEntryType`
   - `feePayerSignatureType`
   - `decodedLogType`
   - `logType`
   - `receiptType`
   - `transactionType`
   - `blockType`

2. **Connection/Pagination Types** (~100 lines)
   - `pageInfoType`
   - `blockConnectionType`
   - `transactionConnectionType`
   - `logConnectionType`

3. **Historical Data Types** (~100 lines)
   - `balanceSnapshotType`
   - `balanceHistoryConnectionType`

4. **Analytics Types** (~300 lines)
   - `minerStatsType`
   - `tokenBalanceType`
   - `searchResultType`
   - `gasStatsType`
   - `addressGasStatsType`
   - `networkMetricsType`
   - `addressActivityStatsType`

5. **System Contract Types** (~500 lines)
   - `proposalStatusEnumType`
   - `mintEventType`
   - `burnEventType`
   - `minterConfigEventType`
   - `proposalType`
   - `proposalVoteType`
   - `gasTipUpdateEventType`
   - `blacklistEventType`
   - `validatorChangeEventType`
   - `memberChangeEventType`
   - `emergencyPauseEventType`
   - `depositMintProposalType`
   - `minterInfoType`
   - `validatorInfoType`
   - `mintEventConnectionType`
   - `burnEventConnectionType`
   - `proposalConnectionType`

6. **WBFT Consensus Types** (~400 lines)
   - `wbftAggregatedSealType`
   - `candidateType`
   - `epochInfoType`
   - `wbftBlockExtraType`
   - `validatorSigningStatsType`
   - `validatorSigningActivityType`
   - `blockSignersType`
   - `validatorSigningStatsConnectionType`
   - `validatorSigningActivityConnectionType`
   - `consensusDataType`

7. **Input/Filter Types** (~300 lines)
   - `blockFilterType`
   - `transactionFilterType`
   - `logFilterType`
   - `paginationInputType`
   - `historicalTransactionFilterType`
   - `systemContractEventFilterType`
   - `proposalFilterType`

---

## Implementation Steps

### Step 1: Extract Core Types

**Create**: `initCoreTypes()`

```go
// initCoreTypes initializes core blockchain type definitions
func initCoreTypes() {
    initLogTypes()
    initReceiptType()
    initTransactionType()
    initBlockType()
}

func initLogTypes() {
    // AccessListEntry type
    accessListEntryType = graphql.NewObject(graphql.ObjectConfig{
        Name: "AccessListEntry",
        Fields: graphql.Fields{
            "address": &graphql.Field{
                Type: graphql.NewNonNull(addressType),
            },
            "storageKeys": &graphql.Field{
                Type: graphql.NewList(graphql.NewNonNull(hashType)),
            },
        },
    })

    // FeePayerSignature type
    feePayerSignatureType = graphql.NewObject(graphql.ObjectConfig{
        Name:        "FeePayerSignature",
        Description: "Signature from fee payer in Fee Delegation transactions",
        Fields: graphql.Fields{
            "v": &graphql.Field{
                Type: graphql.NewNonNull(bigIntType),
            },
            "r": &graphql.Field{
                Type: graphql.NewNonNull(bytesType),
            },
            "s": &graphql.Field{
                Type: graphql.NewNonNull(bytesType),
            },
        },
    })

    // DecodedLog type
    decodedLogType = graphql.NewObject(graphql.ObjectConfig{
        Name: "DecodedLog",
        Fields: graphql.Fields{
            "eventName": &graphql.Field{
                Type:        graphql.NewNonNull(graphql.String),
                Description: "Name of the decoded event",
            },
            "args": &graphql.Field{
                Type:        graphql.String,
                Description: "Decoded event arguments as JSON",
            },
        },
    })

    // Log type
    logType = graphql.NewObject(graphql.ObjectConfig{
        Name: "Log",
        Fields: graphql.Fields{
            "address": &graphql.Field{
                Type: graphql.NewNonNull(addressType),
            },
            "topics": &graphql.Field{
                Type: graphql.NewList(graphql.NewNonNull(hashType)),
            },
            "data": &graphql.Field{
                Type: graphql.NewNonNull(bytesType),
            },
            "blockNumber": &graphql.Field{
                Type: graphql.NewNonNull(bigIntType),
            },
            "blockHash": &graphql.Field{
                Type: graphql.NewNonNull(hashType),
            },
            "transactionHash": &graphql.Field{
                Type: graphql.NewNonNull(hashType),
            },
            "transactionIndex": &graphql.Field{
                Type: graphql.NewNonNull(graphql.Int),
            },
            "logIndex": &graphql.Field{
                Type: graphql.NewNonNull(graphql.Int),
            },
            "removed": &graphql.Field{
                Type: graphql.NewNonNull(graphql.Boolean),
            },
            "decoded": &graphql.Field{
                Type:        decodedLogType,
                Description: "Decoded event log data (if ABI is available)",
            },
        },
    })
}

func initReceiptType() {
    // Receipt type initialization
    // ... (extract from current initTypes)
}

func initTransactionType() {
    // Transaction type initialization
    // ... (extract from current initTypes)
}

func initBlockType() {
    // Block type initialization
    // ... (extract from current initTypes)
}
```

### Step 2: Extract Connection Types

**Create**: `initConnectionTypes()`

```go
func initConnectionTypes() {
    // PageInfo type
    pageInfoType = graphql.NewObject(...)

    // BlockConnection type
    blockConnectionType = graphql.NewObject(...)

    // TransactionConnection type
    transactionConnectionType = graphql.NewObject(...)

    // LogConnection type
    logConnectionType = graphql.NewObject(...)
}
```

### Step 3-7: Extract Remaining Types

Follow the same pattern for:
- `initHistoricalDataTypes()`
- `initAnalyticsTypes()`
- `initSystemContractTypes()`
- `initConsensusTypes()`
- `initInputTypes()`

### Step 8: Update initTypes()

**Final Result**:

```go
func initTypes() {
    initCoreTypes()
    initConnectionTypes()
    initHistoricalDataTypes()
    initAnalyticsTypes()
    initSystemContractTypes()
    initConsensusTypes()
    initInputTypes()
}
```

---

## Testing Strategy

### Before Refactoring
1. Run full integration tests
2. Capture GraphQL schema introspection output
3. Document all type relationships

### During Refactoring
1. Extract one category at a time
2. Run tests after each extraction
3. Verify schema introspection matches baseline

### After Refactoring
1. Run full test suite
2. Compare schema introspection output
3. Verify API compatibility

---

## Expected Benefits

### Maintainability
- ✅ Each type category in dedicated function (~100-500 lines each)
- ✅ Easy to locate and modify specific types
- ✅ Clear type dependencies and initialization order

### Testability
- ✅ Can test each type category initialization independently
- ✅ Easier to mock and unit test

### Readability
- ✅ Function names clearly indicate type categories
- ✅ Reduced cognitive load (8 focused functions vs 1 massive function)
- ✅ Better code navigation and understanding

### Performance
- ⚠️ No performance impact (same initialization, just organized)

---

## Risks & Mitigation

### Risk 1: Type Initialization Order
**Issue**: Some types depend on others (e.g., logType referenced in receiptType)
**Mitigation**: Maintain initialization order, test thoroughly

### Risk 2: Breaking Changes
**Issue**: GraphQL schema change detection
**Mitigation**: Compare schema introspection before/after refactoring

### Risk 3: Merge Conflicts
**Issue**: Large file modification
**Mitigation**: Coordinate with team, refactor in isolated branch

---

## Completion Criteria

- ✅ `initTypes()` reduced to ~10 lines (7 function calls)
- ✅ Each extracted function < 500 lines
- ✅ All existing tests pass
- ✅ GraphQL schema introspection output identical
- ✅ API compatibility maintained
- ✅ Code review approved

---

## Alternative Approaches

### Approach A: Separate Files (More Aggressive)
Create separate files for each type category:
```
api/graphql/
├── types.go (main + initTypes())
├── types_core.go
├── types_connection.go
├── types_analytics.go
├── types_system_contract.go
├── types_consensus.go
└── types_input.go
```

**Pros**: Better file organization, easier navigation
**Cons**: More files to manage, requires careful coordination

### Approach B: Type Registry Pattern (Most Flexible)
```go
type TypeRegistry struct {
    types map[string]*graphql.Object
}

func (r *TypeRegistry) Register(name string, config graphql.ObjectConfig) {
    r.types[name] = graphql.NewObject(config)
}

func initTypes() {
    registry := NewTypeRegistry()
    registerCoreTypes(registry)
    registerConnectionTypes(registry)
    // ...
}
```

**Pros**: Most flexible, easier to extend
**Cons**: More complex, requires more refactoring

---

## Recommended Approach

**Phase 1-2a**: Extract into separate functions (as outlined in Steps 1-8)
**Phase 1-2b** (Optional): Move to separate files if team agrees

**Estimated Effort**: 4-6 hours
**Risk Level**: Low (mechanical refactoring, testable)
**Priority**: High (SRP violation, God Function)

---

**Status**: Ready for implementation
**Last Updated**: 2025-11-26
**Next Action**: Begin Step 1 (Extract Core Types)

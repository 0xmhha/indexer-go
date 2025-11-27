# Frontend-Backend Synchronization Guide

> **Last Updated**: 2025-11-27
> **Backend Version**: GraphQL Schema v2.0 (Post-Improvement)
> **Purpose**: Guide frontend team to synchronize with improved backend GraphQL schema

---

## 📋 Overview

This guide provides step-by-step instructions for the frontend team to synchronize their GraphQL queries with the improved backend schema. The backend has completed a comprehensive GraphQL schema improvement project addressing 25+ audit items.

**Backend Improvements Completed:**
- ✅ Field name standardization (`txHash` → `transactionHash`, `txCount` → `transactionCount`)
- ✅ ProposalFilter.contract made nullable
- ✅ Query aliases added for backward compatibility
- ✅ Missing queries exposed (consensusData, validatorStats, validatorParticipation)
- ✅ System contract queries added (minterConfigHistory, authorizedAccounts, burnHistory)
- ✅ Blocks pagination bug fixed
- ✅ Comprehensive documentation created

---

## 🚨 Critical Changes Required (Immediate Action)

### 1. Update Subscription Field Names

**Issue**: Subscription `newBlock` uses deprecated field name
**Priority**: Critical (Breaking Change)
**File**: `lib/apollo/queries.ts:253-264`

**Current (Incorrect):**
```graphql
subscription NewBlock {
  newBlock {
    number
    hash
    parentHash
    timestamp
    miner
    txCount        # ❌ Deprecated field name
  }
}
```

**Updated (Correct):**
```graphql
subscription NewBlock {
  newBlock {
    number
    hash
    parentHash
    timestamp
    miner
    transactionCount  # ✅ Correct field name
  }
}
```

**TypeScript Type Update:**
```typescript
// lib/types/block.ts
interface Block {
  number: string
  hash: string
  parentHash: string
  timestamp: string
  miner: string
  transactionCount: number  // ✅ Updated
}
```

---

### 2. Update System Contract Event Queries

**Issue**: MintEvent/BurnEvent queries use deprecated field name
**Priority**: Critical
**Files**: `lib/hooks/useSystemContracts.ts:28-70`

**Current (Incorrect):**
```graphql
query GetMintEvents(...) {
  mintEvents(...) {
    blockNumber
    txHash           # ❌ Deprecated
    minter
    to
    amount
    timestamp
  }
}
```

**Updated (Correct):**
```graphql
query GetMintEvents(...) {
  mintEvents(...) {
    blockNumber
    transactionHash  # ✅ Correct
    minter
    to
    amount
    timestamp
  }
}
```

**Apply to Both:**
- `GetMintEvents` query (line 28-51)
- `GetBurnEvents` query (line 54-70)

**TypeScript Type Update:**
```typescript
// lib/types/systemContracts.ts
interface MintEvent {
  blockNumber: string
  transactionHash: string  // ✅ Updated
  minter: string
  to: string
  amount: string
  timestamp: string
}

interface BurnEvent {
  blockNumber: string
  transactionHash: string  // ✅ Updated
  burner: string
  amount: string
  timestamp: string
}
```

---

### 3. Update ProposalFilter to Allow Null Contract

**Issue**: ProposalFilter.contract is now nullable but frontend doesn't handle it
**Priority**: High
**File**: `lib/hooks/useGovernance.ts:21-48`

**Current Query:**
```graphql
query GetProposals($contract: String, $status: ProposalStatus, ...) {
  proposals(
    filter: { contract: $contract, status: $status }
    pagination: { limit: $limit, offset: $offset }
  ) { ... }
}
```

**Updated Usage:**
```typescript
// Now you can query all proposals without specifying contract
const { data } = useQuery(GET_PROPOSALS, {
  variables: {
    // contract: undefined,  // ✅ Now optional!
    status: 'VOTING',
    limit: 20,
    offset: 0
  }
})

// Or query by specific contract
const { data } = useQuery(GET_PROPOSALS, {
  variables: {
    contract: '0x1234...',  // ✅ Optional
    status: 'VOTING',
    limit: 20,
    offset: 0
  }
})
```

**Benefit**: You can now query all proposals across all contracts without filtering by contract address.

---

## 🔧 High Priority Changes (Recommended)

### 4. Migrate to Backend Query Aliases

**Issue**: Frontend uses old query names that now have aliases
**Priority**: High (Improve maintainability)
**Benefit**: Frontend code becomes more readable and aligned with backend naming

**Migration Table:**

| Frontend Query | Backend Actual | Status | Action |
|----------------|----------------|--------|--------|
| `wbftBlock` | `wbftBlockExtra` | ✅ Alias exists | No change needed (alias works) |
| `latestEpochData` | `latestEpochInfo` | ✅ Alias exists | No change needed (alias works) |
| `epochByNumber` | `epochInfo` | ✅ Alias exists | No change needed (alias works) |
| `allValidatorStats` | `allValidatorsSigningStats` | ✅ Alias exists | No change needed (alias works) |
| `burnHistory` | `burnEvents` | ✅ Alias exists | No change needed (alias works) |

**Note**: All aliases are working. You can keep using old names or migrate to new names for consistency. **No immediate action required.**

---

### 5. Update BlockSigners Query Structure

**Issue**: Frontend expects different field structure than backend provides
**Priority**: Medium
**File**: `lib/hooks/useWBFT.ts:102-111`

**Current Frontend Expectation:**
```graphql
query GetBlockSigners($blockNumber: String!) {
  blockSigners(blockNumber: $blockNumber) {
    blockNumber
    signers      # ❌ Does not exist
    bitmap       # ❌ Does not exist
    timestamp    # ❌ Does not exist
  }
}
```

**Backend Actual Response:**
```graphql
type BlockSigners {
  blockNumber: BigInt!
  preparers: [Address!]!   # ✅ Prepare phase signers
  committers: [Address!]!  # ✅ Commit phase signers
}
```

**Frontend Update Required:**
```typescript
// lib/hooks/useWBFT.ts
const GET_BLOCK_SIGNERS = gql`
  query GetBlockSigners($blockNumber: String!) {
    blockSigners(blockNumber: $blockNumber) {
      blockNumber
      preparers   # ✅ Use these instead
      committers  # ✅ More accurate consensus info
    }
  }
`

// Usage
function BlockSignersDisplay({ blockNumber }: Props) {
  const { data } = useQuery(GET_BLOCK_SIGNERS, {
    variables: { blockNumber }
  })

  const preparers = data?.blockSigners?.preparers || []
  const committers = data?.blockSigners?.committers || []

  // Option 1: Show separately (recommended)
  return (
    <div>
      <div>Prepare Signers ({preparers.length}): {preparers.join(', ')}</div>
      <div>Commit Signers ({committers.length}): {committers.join(', ')}</div>
    </div>
  )

  // Option 2: Combine if needed
  const allSigners = [...new Set([...preparers, ...committers])]
  return <div>All Signers ({allSigners.length}): {allSigners.join(', ')}</div>
}
```

**Why**: Backend provides more accurate information by distinguishing prepare and commit phase signers in WBFT consensus.

---

### 6. Use Enhanced Consensus Queries

**Issue**: Frontend can now access richer consensus data
**Priority**: Medium (Feature Enhancement)
**File**: `lib/hooks/useWBFT.ts`, `lib/hooks/useConsensus.ts`

**New Query Available: `consensusData`**

```graphql
query GetConsensusData($blockNumber: String!) {
  consensusData(blockNumber: $blockNumber) {
    blockNumber
    blockHash
    round
    prevRound
    roundChanged

    # Consensus participants
    proposer         # ✅ Block proposer address
    validators       # ✅ Active validator set

    # Signing statistics
    prepareSigners   # ✅ Validators who signed prepare
    commitSigners    # ✅ Validators who signed commit
    prepareCount
    commitCount
    missedPrepare    # ✅ Validators who missed prepare
    missedCommit     # ✅ Validators who missed commit

    # Health metrics
    participationRate  # ✅ % of validators who participated
    isHealthy          # ✅ Boolean consensus health indicator
    isEpochBoundary    # ✅ Is this an epoch boundary block?

    # Additional data
    timestamp
    randaoReveal
    gasTip

    # Enhanced epoch info
    epochInfo {
      epochNumber
      validatorCount      # ✅ Total validator count
      candidateCount      # ✅ Total candidate count
      validators {        # ✅ Full validator details
        address
        index
        blsPubKey
      }
      candidates {
        address
        diligence
      }
    }
  }
}
```

**Usage Example:**
```typescript
// lib/hooks/useConsensus.ts
export function useConsensusData(blockNumber: string) {
  const { data, loading, error } = useQuery(GET_CONSENSUS_DATA, {
    variables: { blockNumber },
    skip: !blockNumber
  })

  return {
    consensusData: data?.consensusData,
    loading,
    error
  }
}

// components/consensus/ConsensusDetails.tsx
function ConsensusDetails({ blockNumber }: Props) {
  const { consensusData, loading } = useConsensusData(blockNumber)

  if (loading) return <LoadingSpinner />
  if (!consensusData) return <div>No consensus data</div>

  return (
    <Card>
      <CardHeader>
        <CardTitle>Consensus Data - Block #{consensusData.blockNumber}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-4">
          <div>
            <Label>Proposer</Label>
            <AddressDisplay address={consensusData.proposer} />
          </div>
          <div>
            <Label>Round</Label>
            <span>{consensusData.round}</span>
          </div>
          <div>
            <Label>Participation Rate</Label>
            <ProgressBar value={consensusData.participationRate} />
          </div>
          <div>
            <Label>Health Status</Label>
            <Badge variant={consensusData.isHealthy ? 'success' : 'destructive'}>
              {consensusData.isHealthy ? 'Healthy' : 'Unhealthy'}
            </Badge>
          </div>
        </div>

        <Separator />

        <div>
          <Label>Prepare Signers ({consensusData.prepareCount}/{consensusData.validators.length})</Label>
          <AddressList addresses={consensusData.prepareSigners} />
        </div>

        <div>
          <Label>Commit Signers ({consensusData.commitCount}/{consensusData.validators.length})</Label>
          <AddressList addresses={consensusData.commitSigners} />
        </div>

        {consensusData.missedPrepare.length > 0 && (
          <div>
            <Label className="text-destructive">Missed Prepare</Label>
            <AddressList addresses={consensusData.missedPrepare} variant="destructive" />
          </div>
        )}
      </CardContent>
    </Card>
  )
}
```

**Benefits:**
- ✅ Single query for comprehensive consensus data
- ✅ Enhanced epoch information with validator details
- ✅ Health and participation metrics
- ✅ Missed signer tracking

---

### 7. Use Enhanced Validator Queries

**New Queries Available:**

#### a. `validatorStats` - Individual Validator Statistics

```graphql
query GetValidatorStats(
  $address: String!
  $fromBlock: String!
  $toBlock: String!
) {
  validatorStats(
    address: $address
    fromBlock: $fromBlock
    toBlock: $toBlock
  ) {
    address
    totalBlocks
    blocksProposed
    preparesSigned
    commitsSigned
    preparesMissed
    commitsMissed
    participationRate
    lastProposedBlock
    lastCommittedBlock
  }
}
```

#### b. `validatorParticipation` - Detailed Block-by-Block Participation

```graphql
query GetValidatorParticipation(
  $address: String!
  $fromBlock: String!
  $toBlock: String!
  $limit: Int
  $offset: Int
) {
  validatorParticipation(
    address: $address
    fromBlock: $fromBlock
    toBlock: $toBlock
    pagination: { limit: $limit, offset: $offset }
  ) {
    address
    startBlock
    endBlock
    totalBlocks
    blocksProposed
    blocksCommitted
    blocksMissed
    participationRate

    # Detailed per-block participation
    blocks {
      blockNumber
      wasProposer      # Was this validator the proposer?
      signedPrepare    # Did they sign prepare phase?
      signedCommit     # Did they sign commit phase?
      round
    }
  }
}
```

**Usage Example:**
```typescript
// components/validators/ValidatorDetails.tsx
function ValidatorDetails({ address }: Props) {
  const fromBlock = '1000'
  const toBlock = '2000'

  const { data: stats } = useQuery(GET_VALIDATOR_STATS, {
    variables: { address, fromBlock, toBlock }
  })

  const { data: participation } = useQuery(GET_VALIDATOR_PARTICIPATION, {
    variables: { address, fromBlock, toBlock, limit: 100, offset: 0 }
  })

  return (
    <div>
      <ValidatorStatsCard stats={stats?.validatorStats} />
      <ValidatorParticipationTable blocks={participation?.validatorParticipation?.blocks} />
    </div>
  )
}
```

---

### 8. Use New System Contract Queries

#### a. `minterConfigHistory` - Minter Configuration Changes

```graphql
query GetMinterConfigHistory(
  $fromBlock: String!
  $toBlock: String!
) {
  minterConfigHistory(
    filter: {
      fromBlock: $fromBlock
      toBlock: $toBlock
    }
  ) {
    blockNumber
    transactionHash
    minter
    allowance
    action        # "added", "removed", "allowanceUpdated"
    timestamp
  }
}
```

#### b. `authorizedAccounts` - GovCouncil Authorized Accounts

```graphql
query GetAuthorizedAccounts {
  authorizedAccounts  # Returns [Address!]!
}
```

**Usage Example:**
```typescript
// components/governance/AuthorizedAccountsList.tsx
function AuthorizedAccountsList() {
  const { data } = useQuery(GET_AUTHORIZED_ACCOUNTS)

  const accounts = data?.authorizedAccounts || []

  return (
    <Card>
      <CardHeader>
        <CardTitle>Authorized Accounts ({accounts.length})</CardTitle>
      </CardHeader>
      <CardContent>
        <ul>
          {accounts.map(address => (
            <li key={address}>
              <AddressDisplay address={address} showCopy />
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}
```

---

## ⚙️ Backend Environment Configuration Required

### Consensus Storage Activation

**Issue**: Consensus-related queries return error "storage does not support consensus operations"
**Priority**: Critical (Backend Configuration)
**Affected Queries**: `consensusData`, `validatorStats`, `validatorParticipation`, `epochInfo`, `wbftBlock`

**Error Message:**
```
[GraphQL error]: Message: storage does not support consensus operations
```

**Solution**: Backend team needs to enable consensus storage in the indexer configuration.

**Configuration File**: `config.yaml` or environment variables

```yaml
# config.yaml
storage:
  type: "postgres"  # or "mysql"
  consensus:
    enabled: true   # ✅ Enable consensus operations
```

**Environment Variable:**
```bash
CONSENSUS_STORAGE_ENABLED=true
```

**Frontend Workaround (Current):**

While waiting for backend configuration, handle the error gracefully:

```typescript
// lib/hooks/useConsensus.ts
export function useConsensusData(blockNumber: string) {
  const { data, loading, error } = useQuery(GET_CONSENSUS_DATA, {
    variables: { blockNumber },
    errorPolicy: 'all',  // ✅ Continue even with errors
    skip: !blockNumber
  })

  const isUnsupported = error?.message?.includes('storage does not support consensus operations')

  return {
    consensusData: data?.consensusData,
    loading,
    error: isUnsupported ? undefined : error,
    isSupported: !isUnsupported  // ✅ Flag to show alternative UI
  }
}

// components/consensus/ConsensusDashboard.tsx
function ConsensusDashboard({ blockNumber }: Props) {
  const { consensusData, loading, isSupported } = useConsensusData(blockNumber)

  if (loading) return <LoadingSpinner />

  if (!isSupported) {
    return (
      <Alert variant="warning">
        <AlertTitle>Consensus Data Unavailable</AlertTitle>
        <AlertDescription>
          The backend does not currently support consensus operations.
          Please contact the backend team to enable consensus storage.
        </AlertDescription>
      </Alert>
    )
  }

  return <ConsensusDetails data={consensusData} />
}
```

---

## 📦 Migration Checklist

Use this checklist to track your migration progress:

### Critical Changes (Do First)
- [ ] Update `newBlock` subscription field: `txCount` → `transactionCount`
- [ ] Update `MintEvent` query field: `txHash` → `transactionHash`
- [ ] Update `BurnEvent` query field: `txHash` → `transactionHash`
- [ ] Update TypeScript types: `Block`, `MintEvent`, `BurnEvent`
- [ ] Test subscription updates in development environment

### High Priority Changes
- [ ] Update `ProposalFilter` to handle nullable `contract` field
- [ ] Update `BlockSigners` query to use `preparers` and `committers`
- [ ] Add error handling for unsupported consensus operations
- [ ] Test governance queries with and without contract filter

### Feature Enhancements (Optional)
- [ ] Migrate to `consensusData` query for richer consensus information
- [ ] Implement `validatorStats` query for validator dashboard
- [ ] Implement `validatorParticipation` query for detailed validator tracking
- [ ] Add `minterConfigHistory` query for minter audit trail
- [ ] Add `authorizedAccounts` query for governance dashboard
- [ ] Update UI components to display new consensus metrics

### Testing & Validation
- [ ] Test all critical queries in development
- [ ] Test pagination on blocks list (verify pages 1-5 work correctly)
- [ ] Test governance queries with null contract filter
- [ ] Test consensus queries (if consensus storage enabled)
- [ ] Update integration tests to use new field names
- [ ] Update E2E tests for subscription changes

---

## 🔍 Known Issues & Solutions

### Issue 1: Blocks Pagination Returns Empty Results

**Status**: ✅ Fixed in Backend
**Version**: Backend v2.0+

**Previous Behavior:**
```graphql
query GetBlocks($limit: Int, $offset: Int) {
  blocks(pagination: { limit: 20, offset: 60 }) {
    nodes       # [] - Empty!
    totalCount  # 0 - Wrong!
  }
}
```

**Current Behavior (Fixed):**
```graphql
query GetBlocks($limit: Int, $offset: Int) {
  blocks(pagination: { limit: 20, offset: 60 }) {
    nodes       # [Block, Block, ...] - Returns 20 blocks
    totalCount  # 12345 - Correct total count
    pageInfo {
      hasNextPage      # true
      hasPreviousPage  # true
    }
  }
}
```

**Frontend Action**: No changes needed. Pagination should work correctly now for all pages.

---

### Issue 2: Consensus Queries Not Working

**Status**: ⚠️ Backend Configuration Required
**Queries Affected**: `consensusData`, `validatorStats`, `validatorParticipation`, `epochInfo`, `wbftBlock`

**Error:**
```
storage does not support consensus operations
```

**Solution**: See [Backend Environment Configuration Required](#backend-environment-configuration-required) section above.

**Frontend Workaround**: Implement graceful error handling as shown in the configuration section.

---

### Issue 3: BigInt and Address Scalar Types

**Status**: ✅ No Action Needed (Backend Handles Serialization)
**Note**: Backend GraphQL schema uses `BigInt` and `Address` scalar types, but they serialize to/from strings in JSON.

**Frontend Usage:**
```typescript
// ✅ Correct: Use string types in TypeScript
interface Block {
  number: string      // BigInt serializes to string
  hash: string        // Hash serializes to string
  timestamp: string   // BigInt serializes to string
}

// ✅ Correct: Pass strings in variables
const { data } = useQuery(GET_BLOCK, {
  variables: {
    number: "1000"  // String, not number
  }
})

// ❌ Wrong: Don't use number type
const { data } = useQuery(GET_BLOCK, {
  variables: {
    number: 1000  // Number won't work
  }
})
```

---

## 📚 Additional Resources

- **GraphQL Schema Documentation**: `/docs/graphql/README.md`
- **Query Examples**: `/docs/graphql/queries.md`
- **Subscription Guide**: `/docs/graphql/subscriptions.md`
- **Best Practices**: `/docs/graphql/best-practices.md`
- **Audit Report**: `/docs/FRONTEND_BACKEND_AUDIT.md` (comprehensive analysis)

---

## 💬 Support & Questions

If you encounter issues during migration:

1. **Check Documentation**: Review `/docs/graphql/` for detailed examples
2. **Verify Backend Version**: Ensure backend is running v2.0+ with all improvements
3. **Check Consensus Storage**: Verify consensus storage is enabled if using consensus queries
4. **Contact Backend Team**: For configuration or schema questions

---

## ✅ Summary

**Backend Improvements Delivered:**
- 20+ schema improvements completed
- Field name standardization
- New queries and aliases
- Pagination bug fixed
- Comprehensive documentation

**Frontend Actions Required:**
- **Critical (3 items)**: Field name updates, type updates
- **High Priority (5 items)**: Query structure updates, error handling
- **Optional (6 items)**: Feature enhancements with new queries

**Estimated Migration Time:**
- Critical changes: 2-3 hours
- High priority changes: 4-6 hours
- Optional enhancements: 8-12 hours (depending on features)

**Benefits After Migration:**
- ✅ Consistent field naming across frontend and backend
- ✅ Richer consensus and validator data
- ✅ More flexible governance queries
- ✅ Fixed pagination for better UX
- ✅ Future-proof schema alignment

---

Good luck with the migration! 🚀

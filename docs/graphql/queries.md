# GraphQL Query Examples

## Table of Contents

- [Block Queries](#block-queries)
- [Transaction Queries](#transaction-queries)
- [Consensus Queries](#consensus-queries)
- [System Contract Queries](#system-contract-queries)
- [Validator Queries](#validator-queries)
- [Governance Queries](#governance-queries)

---

## Block Queries

### Get Latest Block

```graphql
query GetLatestBlock {
  latestBlock {
    number
    hash
    timestamp
    transactionCount
    gasUsed
    gasLimit
    miner
  }
}
```

### Get Block by Number

```graphql
query GetBlock($number: BigInt!) {
  block(number: $number) {
    number
    hash
    timestamp
    transactionCount
    transactions {
      hash
      from
      to
      value
      gasUsed
    }
  }
}
```

Variables:
```json
{
  "number": "1000"
}
```

### Get Block Range

```graphql
query GetBlockRange($from: BigInt!, $to: BigInt!) {
  blocks(
    filter: {
      fromBlock: $from
      toBlock: $to
    }
    pagination: {
      limit: 100
      offset: 0
    }
  ) {
    nodes {
      number
      hash
      timestamp
      transactionCount
    }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}
```

---

## Transaction Queries

### Get Transaction by Hash

```graphql
query GetTransaction($hash: Hash!) {
  transaction(hash: $hash) {
    hash
    blockNumber
    from
    to
    value
    gasUsed
    gasPrice
    input
    status
  }
}
```

### Get Internal Transactions

```graphql
query GetInternalTransactions($txHash: Hash!) {
  internalTransactions(transactionHash: $txHash) {
    from
    to
    value
    gas
    type
  }
}
```

**Note**: Field name changed from `txHash` to `transactionHash` in Phase 1.

### Get Address Transactions

```graphql
query GetAddressTransactions(
  $address: Address!
  $fromBlock: BigInt!
  $toBlock: BigInt!
) {
  address(address: $address) {
    transactions(
      filter: {
        fromBlock: $fromBlock
        toBlock: $toBlock
      }
      pagination: {
        limit: 50
        offset: 0
      }
    ) {
      nodes {
        hash
        blockNumber
        from
        to
        value
        timestamp
      }
      pageInfo {
        hasNextPage
        totalCount
      }
    }
  }
}
```

---

## Consensus Queries

### Get Consensus Data for Block

```graphql
query GetConsensusData($blockNumber: BigInt!) {
  consensusData(blockNumber: $blockNumber) {
    blockNumber
    blockHash
    round
    prevRound
    roundChanged
    proposer
    validators
    prepareSigners
    commitSigners
    prepareCount
    commitCount
    missedPrepare
    missedCommit
    participationRate
    isHealthy
    isEpochBoundary
    timestamp
    epochInfo {
      epochNumber
      validatorCount
      candidateCount
      validators {
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

### Get WBFT Block Extra (Alternative 1)

```graphql
query GetWBFTBlockExtra($blockNumber: BigInt!) {
  wbftBlockExtra(blockNumber: $blockNumber) {
    blockNumber
    blockHash
    randaoReveal
    prevRound
    round
    timestamp
    gasTip
    epochInfo {
      epochNumber
      blockNumber
      candidates {
        address
        diligence
      }
      validators
      blsPublicKeys
    }
  }
}
```

### Get WBFT Block (Alias - Alternative 2)

```graphql
query GetWBFTBlock($number: BigInt!) {
  wbftBlock(number: $number) {
    blockNumber
    blockHash
    round
    timestamp
  }
}
```

**Note**: `wbftBlock` is an alias for `wbftBlockExtra` (added in Phase 2).

---

## System Contract Queries

### Get Mint Events

```graphql
query GetMintEvents(
  $fromBlock: BigInt!
  $toBlock: BigInt!
  $minter: Address
) {
  mintEvents(
    filter: {
      fromBlock: $fromBlock
      toBlock: $toBlock
      minter: $minter
    }
    pagination: {
      limit: 100
      offset: 0
    }
  ) {
    nodes {
      blockNumber
      transactionHash
      minter
      to
      amount
      timestamp
    }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}
```

### Get Burn Events / Burn History

```graphql
query GetBurnHistory(
  $fromBlock: BigInt!
  $toBlock: BigInt!
  $burner: Address
) {
  burnHistory(
    filter: {
      fromBlock: $fromBlock
      toBlock: $toBlock
      burner: $burner
    }
    pagination: {
      limit: 100
      offset: 0
    }
  ) {
    nodes {
      blockNumber
      transactionHash
      burner
      amount
      timestamp
      withdrawalId
    }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}
```

**Note**: `burnHistory` is an alias for `burnEvents` (added in Phase 2).

### Get Minter Configuration History

```graphql
query GetMinterConfigHistory(
  $fromBlock: BigInt!
  $toBlock: BigInt!
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
    action
    timestamp
  }
}
```

**Note**: New query added in Phase 2. Returns configuration changes for ALL minters.

### Get Minter History (Specific Minter)

```graphql
query GetMinterHistory($minter: Address!) {
  minterHistory(minter: $minter) {
    blockNumber
    transactionHash
    minter
    allowance
    action
    timestamp
  }
}
```

**Note**: Returns history for a SPECIFIC minter only.

### Get Authorized Accounts

```graphql
query GetAuthorizedAccounts {
  authorizedAccounts
}
```

**Note**: New query added in Phase 2. Returns list of GovCouncil authorized account addresses.

**Response**:
```json
{
  "data": {
    "authorizedAccounts": [
      "0x1234...",
      "0x5678...",
      "0xabcd..."
    ]
  }
}
```

---

## Validator Queries

### Get Validator Statistics

```graphql
query GetValidatorStats(
  $address: Address!
  $fromBlock: BigInt!
  $toBlock: BigInt!
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

### Get Validator Participation (Detailed)

```graphql
query GetValidatorParticipation(
  $address: Address!
  $fromBlock: BigInt!
  $toBlock: BigInt!
) {
  validatorParticipation(
    address: $address
    fromBlock: $fromBlock
    toBlock: $toBlock
    pagination: {
      limit: 100
      offset: 0
    }
  ) {
    address
    startBlock
    endBlock
    totalBlocks
    blocksProposed
    blocksCommitted
    blocksMissed
    participationRate
    blocks {
      blockNumber
      wasProposer
      signedPrepare
      signedCommit
      round
    }
  }
}
```

### Get All Validator Stats / All Validator Signing Stats

```graphql
query GetAllValidatorStats(
  $fromBlock: BigInt!
  $toBlock: BigInt!
) {
  allValidatorStats(
    fromBlock: $fromBlock
    toBlock: $toBlock
    pagination: {
      limit: 50
      offset: 0
    }
  ) {
    nodes {
      validatorAddress
      prepareSignCount
      commitSignCount
      prepareMissCount
      commitMissCount
      signingRate
    }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}
```

**Note**: `allValidatorStats` is an alias for `allValidatorsSigningStats` (added in Phase 2).

### Get Active Validators (Addresses Only)

```graphql
query GetActiveValidatorAddresses {
  activeValidatorAddresses
}
```

**Note**: New query added in Phase 1. Returns simple address array.

**Response**:
```json
{
  "data": {
    "activeValidatorAddresses": [
      "0xvalidator1...",
      "0xvalidator2...",
      "0xvalidator3..."
    ]
  }
}
```

### Get Active Validators (Full Details)

```graphql
query GetActiveValidators {
  activeValidators {
    address
    blsPublicKey
    stakingBalance
    isActive
  }
}
```

**Note**: Returns full validator objects with detailed information.

---

## Governance Queries

### Get Proposals

```graphql
query GetProposals(
  $fromBlock: BigInt!
  $toBlock: BigInt!
  $contract: Address
  $status: String
) {
  proposals(
    filter: {
      fromBlock: $fromBlock
      toBlock: $toBlock
      contract: $contract
      status: $status
    }
    pagination: {
      limit: 20
      offset: 0
    }
  ) {
    nodes {
      contract
      proposalId
      proposer
      actionType
      requiredApprovals
      approved
      rejected
      status
      createdAt
      executedAt
    }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}
```

**Note**: `contract` field is now optional (changed in Phase 1). Omit to query ALL contracts.

### Get Specific Proposal

```graphql
query GetProposal(
  $contract: Address!
  $proposalId: BigInt!
) {
  proposal(
    contract: $contract
    proposalId: $proposalId
  ) {
    contract
    proposalId
    proposer
    actionType
    callData
    memberVersion
    requiredApprovals
    approved
    rejected
    status
    createdAt
    executedAt
    votes {
      voter
      support
      timestamp
    }
  }
}
```

---

## Epoch Queries

### Get Epoch Info (Basic)

```graphql
query GetEpochInfo($epochNumber: BigInt!) {
  epochInfo(epochNumber: $epochNumber) {
    epochNumber
    blockNumber
    candidates {
      address
      diligence
    }
    validators
    blsPublicKeys
  }
}
```

### Get Epoch by Number (Alias)

```graphql
query GetEpochByNumber($number: BigInt!) {
  epochByNumber(number: $number) {
    epochNumber
    blockNumber
    candidates {
      address
      diligence
    }
    validators
    blsPublicKeys
  }
}
```

**Note**: `epochByNumber` is an alias for `epochInfo` (added in Phase 2).

### Get Latest Epoch Data

```graphql
query GetLatestEpochData {
  latestEpochData {
    epochNumber
    blockNumber
    candidates {
      address
      diligence
    }
    validators
    blsPublicKeys
  }
}
```

**Note**: `latestEpochData` is an alias for `latestEpochInfo` (added in Phase 2).

---

## Advanced Patterns

### Batch Multiple Queries

```graphql
query GetDashboardData($blockNumber: BigInt!) {
  latestBlock {
    number
    timestamp
    transactionCount
  }

  consensusData(blockNumber: $blockNumber) {
    round
    participationRate
    isHealthy
  }

  latestEpochData {
    epochNumber
    validators
  }
}
```

### Pagination Example

```graphql
# Page 1
query GetProposalsPage1 {
  proposals(
    filter: {
      fromBlock: "1000"
      toBlock: "2000"
    }
    pagination: {
      limit: 20
      offset: 0
    }
  ) {
    nodes { ... }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}

# Page 2 (if hasNextPage is true)
query GetProposalsPage2 {
  proposals(
    filter: {
      fromBlock: "1000"
      toBlock: "2000"
    }
    pagination: {
      limit: 20
      offset: 20  # Skip first 20
    }
  ) {
    nodes { ... }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}
```

---

## Performance Tips

1. **Request Only What You Need**: Don't query all fields if you only need a few
2. **Use Pagination**: Always use `limit` to avoid large result sets
3. **Filter Early**: Use filter parameters instead of fetching all data and filtering client-side
4. **Batch Queries**: Combine multiple queries in one request to reduce round trips
5. **Cache Results**: Use block number as cache key for immutable historical data

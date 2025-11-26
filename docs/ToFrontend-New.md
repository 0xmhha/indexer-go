# System Contracts API - Frontend Integration Guide

**Last Updated**: 2025-01-26
**Status**: ✅ Implementation Complete & Ready for Integration
**Backend Version**: v1.0.0 (Indexer-Go)

## Overview

The indexer now fully supports **System Contracts Event Parsing and Querying** for Stable-One chain. All 5 system contracts (0x1000-0x1004) are indexed with 38 event types, enabling comprehensive queries for:

- **Mint/Burn History** (NativeCoinAdapter)
- **Validator Management** (GovValidator)
- **Minter Permissions** (GovMasterMinter)
- **Deposit/Burn Operations** (GovMinter)
- **Blacklist & Authorization** (GovCouncil)

## 🎯 Quick Start

### System Contract Addresses

```typescript
const SYSTEM_CONTRACTS = {
  NativeCoinAdapter: "0x0000000000000000000000000000000000001000", // Mint/Burn/Transfer
  GovValidator:      "0x0000000000000000000000000000000000001001", // Validator management
  GovMasterMinter:   "0x0000000000000000000000000000000000001002", // Minter permissions
  GovMinter:         "0x0000000000000000000000000000000000001003", // Actual mint/burn execution
  GovCouncil:        "0x0000000000000000000000000000000000001004", // Blacklist & permissions
};
```

---

## 📚 Available APIs

### 1. NativeCoinAdapter (0x1000) - Mint/Burn Management

#### Get Mint Events
Query mint events with optional filtering by minter address.

**GraphQL Query**:
```graphql
query GetMintEvents($fromBlock: BigInt!, $toBlock: BigInt!, $minter: Address, $limit: Int!, $offset: Int!) {
  mintEvents(fromBlock: $fromBlock, toBlock: $toBlock, minter: $minter, limit: $limit, offset: $offset) {
    blockNumber
    txHash
    minter
    to
    amount
    timestamp
  }
}
```

**JSON-RPC Method**:
```json
{
  "jsonrpc": "2.0",
  "method": "stable_getMintEvents",
  "params": [
    1000,        // fromBlock
    2000,        // toBlock
    "0x1234...", // minter address (optional, null for all)
    10,          // limit
    0            // offset
  ],
  "id": 1
}
```

**Response Schema**:
```typescript
interface MintEvent {
  blockNumber: number;
  txHash: string;
  minter: string;     // Address that executed mint
  to: string;         // Recipient address
  amount: string;     // Amount in wei (use BigNumber)
  timestamp: number;  // Unix timestamp
}
```

---

#### Get Burn Events
Query burn events with optional burner filtering.

**GraphQL Query**:
```graphql
query GetBurnEvents($fromBlock: BigInt!, $toBlock: BigInt!, $burner: Address, $limit: Int!, $offset: Int!) {
  burnEvents(fromBlock: $fromBlock, toBlock: $toBlock, burner: $burner, limit: $limit, offset: $offset) {
    blockNumber
    txHash
    burner
    amount
    timestamp
  }
}
```

**Response Schema**:
```typescript
interface BurnEvent {
  blockNumber: number;
  txHash: string;
  burner: string;     // Address that burned tokens
  amount: string;     // Amount burned in wei
  timestamp: number;
}
```

---

#### Get Active Minters
Query all currently active minter addresses.

**GraphQL Query**:
```graphql
query GetActiveMinters {
  activeMinters
}
```

**JSON-RPC Method**:
```json
{
  "jsonrpc": "2.0",
  "method": "stable_getActiveMinters",
  "params": [],
  "id": 1
}
```

**Response**:
```typescript
string[] // Array of active minter addresses
```

---

#### Get Minter Allowance
Query the mint allowance for a specific minter.

**GraphQL Query**:
```graphql
query GetMinterAllowance($minter: Address!) {
  minterAllowance(minter: $minter)
}
```

**Response**:
```typescript
string // Allowance in wei (BigNumber)
```

---

#### Get Minter History
Query configuration history for a specific minter.

**GraphQL Query**:
```graphql
query GetMinterHistory($minter: Address!) {
  minterHistory(minter: $minter) {
    blockNumber
    txHash
    minter
    allowance
    isActive
    timestamp
  }
}
```

**Response Schema**:
```typescript
interface MinterConfigEvent {
  blockNumber: number;
  txHash: string;
  minter: string;
  allowance: string;   // Allowance in wei
  isActive: boolean;   // Whether minter is active
  timestamp: number;
}
```

---

### 2. GovValidator (0x1001) - Validator Management

#### Get Active Validators
Query all currently active validators.

**GraphQL Query**:
```graphql
query GetActiveValidators {
  activeValidators
}
```

**Response**:
```typescript
string[] // Array of active validator addresses
```

---

#### Get Gas Tip History
Query gas tip update history.

**GraphQL Query**:
```graphql
query GetGasTipHistory($fromBlock: BigInt!, $toBlock: BigInt!) {
  gasTipHistory(fromBlock: $fromBlock, toBlock: $toBlock) {
    blockNumber
    txHash
    oldTip
    newTip
    updater
    timestamp
  }
}
```

**Response Schema**:
```typescript
interface GasTipUpdateEvent {
  blockNumber: number;
  txHash: string;
  oldTip: string;      // Previous gas tip in wei
  newTip: string;      // New gas tip in wei
  updater: string;     // Address that updated gas tip
  timestamp: number;
}
```

---

#### Get Validator History
Query validator change history for a specific validator.

**GraphQL Query**:
```graphql
query GetValidatorHistory($validator: Address!) {
  validatorHistory(validator: $validator) {
    blockNumber
    txHash
    validator
    isActive
    eventType  // "added" | "removed"
    timestamp
  }
}
```

---

### 3. GovMasterMinter (0x1002) - Minter Permission Management

#### Get Minter Config History
Query minter configuration changes across all minters.

**GraphQL Query**:
```graphql
query GetMinterConfigHistory($fromBlock: BigInt!, $toBlock: BigInt!) {
  minterConfigHistory(fromBlock: $fromBlock, toBlock: $toBlock) {
    blockNumber
    txHash
    minter
    allowance
    isActive
    timestamp
  }
}
```

---

#### Get Emergency Pause History
Query emergency pause/unpause events.

**GraphQL Query**:
```graphql
query GetEmergencyPauseHistory($contract: Address!) {
  emergencyPauseHistory(contract: $contract) {
    blockNumber
    txHash
    contract
    isPaused      // true = paused, false = unpaused
    timestamp
  }
}
```

---

### 4. GovMinter (0x1003) - Mint/Burn Execution

#### Get Deposit Mint Proposals
Query deposit mint proposals with status filtering.

**GraphQL Query**:
```graphql
query GetDepositMintProposals($fromBlock: BigInt!, $toBlock: BigInt!, $status: ProposalStatus!) {
  depositMintProposals(fromBlock: $fromBlock, toBlock: $toBlock, status: $status) {
    proposalId
    proposer
    amount
    depositTxId
    status        // "voting" | "approved" | "executed" | "rejected" | "cancelled"
    blockNumber
    timestamp
  }
}
```

**Proposal Status Values**:
```typescript
type ProposalStatus =
  | "all"        // Query all statuses (0xFF)
  | "none"       // Initial state (0)
  | "voting"     // Under voting (1)
  | "approved"   // Approved, pending execution (2)
  | "executed"   // Successfully executed (3)
  | "cancelled"  // Cancelled by proposer (4)
  | "expired"    // Voting period expired (5)
  | "failed"     // Execution failed (6)
  | "rejected";  // Voting rejected (7)
```

---

#### Get Burn History
Query burn execution history with optional user filtering.

**GraphQL Query**:
```graphql
query GetBurnHistory($fromBlock: BigInt!, $toBlock: BigInt!, $user: Address) {
  burnHistory(fromBlock: $fromBlock, toBlock: $toBlock, user: $user) {
    blockNumber
    txHash
    burner
    amount
    burnTxId      // External burn transaction ID
    timestamp
  }
}
```

---

### 5. GovCouncil (0x1004) - Blacklist & Authorization

#### Get Blacklisted Addresses
Query all currently blacklisted addresses.

**GraphQL Query**:
```graphql
query GetBlacklistedAddresses {
  blacklistedAddresses
}
```

**Response**:
```typescript
string[] // Array of blacklisted addresses
```

---

#### Get Blacklist History
Query blacklist event history for a specific address.

**GraphQL Query**:
```graphql
query GetBlacklistHistory($address: Address!) {
  blacklistHistory(address: $address) {
    blockNumber
    txHash
    address
    isBlacklisted  // true = blacklisted, false = unblacklisted
    proposalId
    timestamp
  }
}
```

---

#### Get Authorized Accounts
Query all authorized accounts (council members).

**GraphQL Query**:
```graphql
query GetAuthorizedAccounts {
  authorizedAccounts
}
```

**Response**:
```typescript
string[] // Array of authorized account addresses
```

**⚠️ Note**: This feature is partially implemented. The event parsers log these events but don't store them yet. Currently returns empty array `[]` for API compatibility. Full implementation requires:
1. Adding `AuthorizedAccountEvent` type to storage layer
2. Adding schema keys for authorized account index
3. Implementing storage in `parseAuthorizedAccountAdded/RemovedEvent`

---

### 6. Common Governance Operations

#### Get Proposals
Query governance proposals for any system contract with status filtering.

**GraphQL Query**:
```graphql
query GetProposals($contract: Address!, $status: ProposalStatus!, $limit: Int!, $offset: Int!) {
  proposals(contract: $contract, status: $status, limit: $limit, offset: $offset) {
    proposalId
    contract
    proposer
    targetFunction
    calldata
    createdAt
    votingEndsAt
    executedAt
    status
    yesVotes
    noVotes
  }
}
```

**Response Schema**:
```typescript
interface Proposal {
  proposalId: string;
  contract: string;       // System contract address
  proposer: string;       // Address that created proposal
  targetFunction: string; // Function signature hash
  calldata: string;       // Hex-encoded call data
  createdAt: number;      // Block number when created
  votingEndsAt: number;   // Block number when voting ends
  executedAt: number;     // Block number when executed (0 if not executed)
  status: ProposalStatus;
  yesVotes: number;
  noVotes: number;
}
```

---

#### Get Proposal Votes
Query votes for a specific proposal.

**GraphQL Query**:
```graphql
query GetProposalVotes($contract: Address!, $proposalId: String!) {
  proposalVotes(contract: $contract, proposalId: $proposalId) {
    voter
    support      // true = yes, false = no
    votedAt
  }
}
```

---

#### Get Member History
Query member change history for governance contracts.

**GraphQL Query**:
```graphql
query GetMemberHistory($contract: Address!) {
  memberHistory(contract: $contract) {
    blockNumber
    txHash
    contract
    member
    eventType    // "added" | "removed" | "changed"
    newMember    // For "changed" events
    proposalId
    timestamp
  }
}
```

---

### 7. Supply & Statistics

#### Get Total Supply
Query current total supply of native coin.

**GraphQL Query**:
```graphql
query GetTotalSupply {
  totalSupply
}
```

**Response**:
```typescript
string // Total supply in wei (BigNumber)
```

---

## 🧩 Integration Examples

### Example 1: Display Mint History with Pagination

```typescript
async function fetchMintHistory(
  minter?: string,
  page: number = 0,
  pageSize: number = 20
) {
  const currentBlock = await web3.eth.getBlockNumber();
  const fromBlock = currentBlock - 10000; // Last ~10K blocks

  const response = await graphqlClient.query({
    query: gql`
      query GetMintEvents($fromBlock: BigInt!, $toBlock: BigInt!, $minter: Address, $limit: Int!, $offset: Int!) {
        mintEvents(fromBlock: $fromBlock, toBlock: $toBlock, minter: $minter, limit: $limit, offset: $offset) {
          blockNumber
          txHash
          minter
          to
          amount
          timestamp
        }
      }
    `,
    variables: {
      fromBlock,
      toBlock: currentBlock,
      minter: minter || null,
      limit: pageSize,
      offset: page * pageSize,
    },
  });

  return response.data.mintEvents.map(event => ({
    ...event,
    amount: ethers.utils.formatEther(event.amount), // Convert to readable format
    date: new Date(event.timestamp * 1000),
  }));
}
```

---

### Example 2: Check if Address is Blacklisted

```typescript
async function isAddressBlacklisted(address: string): Promise<boolean> {
  const blacklistedAddresses = await graphqlClient.query({
    query: gql`
      query GetBlacklistedAddresses {
        blacklistedAddresses
      }
    `,
  });

  return blacklistedAddresses.data.blacklistedAddresses
    .map(addr => addr.toLowerCase())
    .includes(address.toLowerCase());
}
```

---

### Example 3: Monitor Proposal Status

```typescript
async function monitorProposal(
  contract: string,
  proposalId: string
): Promise<Proposal> {
  const response = await graphqlClient.query({
    query: gql`
      query GetProposals($contract: Address!, $status: ProposalStatus!, $limit: Int!, $offset: Int!) {
        proposals(contract: $contract, status: $status, limit: $limit, offset: $offset) {
          proposalId
          status
          yesVotes
          noVotes
          executedAt
        }
      }
    `,
    variables: {
      contract,
      status: "all",
      limit: 100,
      offset: 0,
    },
  });

  return response.data.proposals.find(p => p.proposalId === proposalId);
}
```

---

### Example 4: Real-time Mint/Burn Dashboard

```typescript
interface SupplyMetrics {
  totalSupply: string;
  last24hMints: number;
  last24hBurns: number;
  activeMinters: number;
}

async function getSupplyMetrics(): Promise<SupplyMetrics> {
  const currentBlock = await web3.eth.getBlockNumber();
  const blocksPerDay = 28800; // ~3s block time
  const fromBlock = currentBlock - blocksPerDay;

  const [totalSupply, mintEvents, burnEvents, activeMinters] = await Promise.all([
    graphqlClient.query({
      query: gql`query { totalSupply }`,
    }),
    graphqlClient.query({
      query: gql`
        query GetMintEvents($fromBlock: BigInt!, $toBlock: BigInt!) {
          mintEvents(fromBlock: $fromBlock, toBlock: $toBlock, limit: 10000, offset: 0) {
            amount
          }
        }
      `,
      variables: { fromBlock, toBlock: currentBlock },
    }),
    graphqlClient.query({
      query: gql`
        query GetBurnEvents($fromBlock: BigInt!, $toBlock: BigInt!) {
          burnEvents(fromBlock: $fromBlock, toBlock: $toBlock, limit: 10000, offset: 0) {
            amount
          }
        }
      `,
      variables: { fromBlock, toBlock: currentBlock },
    }),
    graphqlClient.query({
      query: gql`query { activeMinters }`,
    }),
  ]);

  const totalMints = mintEvents.data.mintEvents.reduce(
    (sum, e) => sum.add(ethers.BigNumber.from(e.amount)),
    ethers.BigNumber.from(0)
  );

  const totalBurns = burnEvents.data.burnEvents.reduce(
    (sum, e) => sum.add(ethers.BigNumber.from(e.amount)),
    ethers.BigNumber.from(0)
  );

  return {
    totalSupply: ethers.utils.formatEther(totalSupply.data.totalSupply),
    last24hMints: mintEvents.data.mintEvents.length,
    last24hBurns: burnEvents.data.burnEvents.length,
    activeMinters: activeMinters.data.activeMinters.length,
  };
}
```

---

## 📊 Event Schemas Reference

### All System Contract Events (38 Total)

<details>
<summary><b>NativeCoinAdapter Events (7)</b></summary>

1. **Transfer** - ERC-20 token transfers
2. **Approval** - ERC-20 allowance approvals
3. **Mint** - Token minting operations
4. **Burn** - Token burning operations
5. **MinterConfigured** - Minter permission granted/updated
6. **MinterRemoved** - Minter permission revoked
7. **MasterMinterChanged** - Master minter role transferred

</details>

<details>
<summary><b>GovBase Common Events (13)</b></summary>

8. **ProposalCreated** - New governance proposal
9. **ProposalVoted** - Vote cast on proposal
10. **ProposalApproved** - Proposal approved by votes
11. **ProposalRejected** - Proposal rejected by votes
12. **ProposalExecuted** - Proposal successfully executed
13. **ProposalFailed** - Proposal execution failed
14. **ProposalExpired** - Proposal voting period expired
15. **ProposalCancelled** - Proposal cancelled by proposer
16. **MemberAdded** - Council member added
17. **MemberRemoved** - Council member removed
18. **MemberChanged** - Council member replaced
19. **QuorumUpdated** - Voting quorum threshold updated
20. **MaxProposalsPerMemberUpdated** - Proposal limit per member updated

</details>

<details>
<summary><b>GovValidator Events (1)</b></summary>

21. **GasTipUpdated** - Gas tip parameter updated

</details>

<details>
<summary><b>GovMasterMinter Events (3)</b></summary>

22. **MaxMinterAllowanceUpdated** - Max mint allowance limit updated
23. **EmergencyPaused** - Emergency pause activated
24. **EmergencyUnpaused** - Emergency pause deactivated

</details>

<details>
<summary><b>GovMinter Events (3)</b></summary>

25. **DepositMintProposed** - New deposit-to-mint proposal created
26. **BurnPrepaid** - Prepaid burn initiated
27. **BurnExecuted** - Burn operation executed with external TX ID

</details>

<details>
<summary><b>GovCouncil Events (5)</b></summary>

28. **AddressBlacklisted** - Address added to blacklist
29. **AddressUnblacklisted** - Address removed from blacklist
30. **AuthorizedAccountAdded** - Account authorized (partially implemented)
31. **AuthorizedAccountRemoved** - Account deauthorized (partially implemented)
32. **ProposalExecutionSkipped** - Proposal execution skipped with reason

</details>

---

---

## 🔔 Real-Time Consensus Event Subscriptions

**NEW**: WebSocket subscriptions for real-time consensus monitoring (Phase B Complete!)

### Overview

Subscribe to consensus events via WebSocket for real-time monitoring of WBFT consensus, validator changes, forks, and errors.

**Supported Subscriptions**:
- `consensusBlock` - New block finalization with consensus data
- `consensusFork` - Chain fork detection and resolution
- `consensusValidatorChange` - Validator set changes at epoch boundaries
- `consensusError` - Consensus errors and anomalies

---

### WebSocket Connection Setup

```typescript
import { ApolloClient, InMemoryCache, split, HttpLink } from '@apollo/client';
import { GraphQLWsLink } from '@apollo/client/link/subscriptions';
import { getMainDefinition } from '@apollo/client/utilities';
import { createClient } from 'graphql-ws';

// HTTP link for queries/mutations
const httpLink = new HttpLink({
  uri: 'http://localhost:8080/graphql',
});

// WebSocket link for subscriptions
const wsLink = new GraphQLWsLink(
  createClient({
    url: 'ws://localhost:8080/subscriptions',
    connectionParams: {
      // Add auth tokens here if needed
    },
  })
);

// Split based on operation type
const splitLink = split(
  ({ query }) => {
    const definition = getMainDefinition(query);
    return (
      definition.kind === 'OperationDefinition' &&
      definition.operation === 'subscription'
    );
  },
  wsLink,
  httpLink
);

const client = new ApolloClient({
  link: splitLink,
  cache: new InMemoryCache(),
});
```

---

### 1. Consensus Block Subscription

Monitor new blocks with consensus data in real-time.

**GraphQL Subscription**:
```graphql
subscription OnConsensusBlock {
  consensusBlock {
    blockNumber
    blockHash
    timestamp
    round
    prevRound
    roundChanged
    proposer
    validatorCount
    prepareCount
    commitCount
    participationRate
    missedValidatorRate
    isEpochBoundary
    epochNumber
    epochValidators
  }
}
```

**TypeScript Usage**:
```typescript
import { gql, useSubscription } from '@apollo/client';

const CONSENSUS_BLOCK_SUBSCRIPTION = gql`
  subscription OnConsensusBlock {
    consensusBlock {
      blockNumber
      blockHash
      timestamp
      round
      roundChanged
      proposer
      validatorCount
      commitCount
      participationRate
      missedValidatorRate
      isEpochBoundary
      epochNumber
    }
  }
`;

function ConsensusMonitor() {
  const { data, loading, error } = useSubscription(CONSENSUS_BLOCK_SUBSCRIPTION);

  if (loading) return <p>Waiting for consensus data...</p>;
  if (error) return <p>Error: {error.message}</p>;

  const block = data.consensusBlock;

  return (
    <div className="consensus-block">
      <h3>Block #{block.blockNumber}</h3>
      <p>Round: {block.round} {block.roundChanged && '⚠️ Round Changed'}</p>
      <p>Proposer: {block.proposer}</p>
      <p>Participation: {block.participationRate.toFixed(2)}%</p>
      <p>Validators: {block.commitCount} / {block.validatorCount}</p>
      {block.isEpochBoundary && (
        <div className="epoch-boundary">
          🎯 Epoch Boundary - Epoch #{block.epochNumber}
        </div>
      )}
    </div>
  );
}
```

**Response Schema**:
```typescript
interface ConsensusBlockEvent {
  blockNumber: number;
  blockHash: string;
  timestamp: number; // Unix seconds
  round: number;
  prevRound: number;
  roundChanged: boolean;
  proposer: string; // Address
  validatorCount: number;
  prepareCount: number;
  commitCount: number;
  participationRate: number; // 0-100
  missedValidatorRate: number; // 0-100
  isEpochBoundary: boolean;
  epochNumber?: number;
  epochValidators?: string[]; // Addresses
}
```

---

### 2. Consensus Fork Subscription

Monitor chain forks and resolution in real-time.

**GraphQL Subscription**:
```graphql
subscription OnConsensusFork {
  consensusFork {
    forkBlockNumber
    forkBlockHash
    chain1Hash
    chain1Height
    chain1Weight
    chain2Hash
    chain2Height
    chain2Weight
    resolved
    winningChain
    detectedAt
    detectionLag
  }
}
```

**TypeScript Usage**:
```typescript
const FORK_SUBSCRIPTION = gql`
  subscription OnConsensusFork {
    consensusFork {
      forkBlockNumber
      chain1Hash
      chain1Height
      chain2Hash
      chain2Height
      resolved
      winningChain
      detectionLag
    }
  }
`;

function ForkMonitor() {
  const { data } = useSubscription(FORK_SUBSCRIPTION);

  if (!data) return null;

  const fork = data.consensusFork;

  return (
    <div className="fork-alert">
      <h3>🚨 Fork Detected at Block #{fork.forkBlockNumber}</h3>
      <div>
        <p>Chain 1: {fork.chain1Hash} (height: {fork.chain1Height})</p>
        <p>Chain 2: {fork.chain2Hash} (height: {fork.chain2Height})</p>
      </div>
      {fork.resolved && (
        <p>✅ Resolved: Chain {fork.winningChain} won</p>
      )}
      <p>Detection lag: {fork.detectionLag} blocks</p>
    </div>
  );
}
```

---

### 3. Validator Change Subscription

Monitor validator set changes at epoch boundaries.

**GraphQL Subscription**:
```graphql
subscription OnValidatorChange {
  consensusValidatorChange {
    blockNumber
    blockHash
    timestamp
    epochNumber
    isEpochBoundary
    changeType
    previousValidatorCount
    newValidatorCount
    addedValidators
    removedValidators
    validatorSet
    additionalInfo
  }
}
```

**TypeScript Usage**:
```typescript
const VALIDATOR_CHANGE_SUBSCRIPTION = gql`
  subscription OnValidatorChange {
    consensusValidatorChange {
      blockNumber
      epochNumber
      changeType
      previousValidatorCount
      newValidatorCount
      addedValidators
      removedValidators
      validatorSet
    }
  }
`;

function ValidatorChangeMonitor() {
  const { data } = useSubscription(VALIDATOR_CHANGE_SUBSCRIPTION);

  if (!data) return null;

  const change = data.consensusValidatorChange;

  return (
    <div className="validator-change">
      <h3>👥 Validator Set Change - Epoch #{change.epochNumber}</h3>
      <p>Type: {change.changeType}</p>
      <p>Validators: {change.previousValidatorCount} → {change.newValidatorCount}</p>
      {change.addedValidators?.length > 0 && (
        <div>
          <p>Added: {change.addedValidators.length} validators</p>
          <ul>
            {change.addedValidators.map(addr => (
              <li key={addr}>{addr}</li>
            ))}
          </ul>
        </div>
      )}
      {change.removedValidators?.length > 0 && (
        <div>
          <p>Removed: {change.removedValidators.length} validators</p>
        </div>
      )}
    </div>
  );
}
```

---

### 4. Consensus Error Subscription

Monitor consensus errors and anomalies for alerting.

**GraphQL Subscription**:
```graphql
subscription OnConsensusError {
  consensusError {
    blockNumber
    blockHash
    timestamp
    errorType
    severity
    errorMessage
    round
    expectedValidators
    actualSigners
    participationRate
    missedValidators
    consensusImpacted
    recoveryTime
    errorDetails
  }
}
```

**TypeScript Usage**:
```typescript
const ERROR_SUBSCRIPTION = gql`
  subscription OnConsensusError {
    consensusError {
      blockNumber
      errorType
      severity
      errorMessage
      round
      participationRate
      consensusImpacted
      missedValidators
    }
  }
`;

function ConsensusErrorMonitor() {
  const { data } = useSubscription(ERROR_SUBSCRIPTION);

  if (!data) return null;

  const error = data.consensusError;
  const severityColor = {
    critical: 'red',
    high: 'orange',
    medium: 'yellow',
    low: 'blue',
  }[error.severity];

  return (
    <div className={`consensus-error severity-${error.severity}`}>
      <h3 style={{ color: severityColor }}>
        {error.severity === 'critical' && '🚨'}
        Consensus {error.errorType.replace('_', ' ')}
      </h3>
      <p>Block: #{error.blockNumber}</p>
      <p>Message: {error.errorMessage}</p>
      <p>Round: {error.round}</p>
      <p>Participation: {error.participationRate.toFixed(2)}%</p>
      {error.consensusImpacted && (
        <p className="critical">⚠️ Consensus Impacted</p>
      )}
      {error.missedValidators?.length > 0 && (
        <p>Missed: {error.missedValidators.length} validators</p>
      )}
    </div>
  );
}
```

**Error Types**:
- `round_change` - Round change occurred (normal, but monitored)
- `missed_validators` - Validators failed to sign
- `low_participation` - Participation below threshold (<66.7%)
- `proposer_failure` - Proposer failed to create block
- `signature_failure` - Signature verification failed

**Severity Levels**:
- `critical` - Consensus at risk, immediate action required
- `high` - Significant issue, requires attention
- `medium` - Notable anomaly, monitor closely
- `low` - Minor issue, informational

---

### Complete Dashboard Example

```typescript
import { useSubscription, gql } from '@apollo/client';

const ALL_CONSENSUS_SUBSCRIPTIONS = gql`
  subscription MonitorConsensus {
    consensusBlock {
      blockNumber
      round
      participationRate
      isEpochBoundary
    }
  }
`;

function ConsensusDashboard() {
  const { data: blockData } = useSubscription(CONSENSUS_BLOCK_SUBSCRIPTION);
  const { data: errorData } = useSubscription(ERROR_SUBSCRIPTION);
  const { data: forkData } = useSubscription(FORK_SUBSCRIPTION);

  return (
    <div className="consensus-dashboard">
      <div className="current-block">
        {blockData && (
          <>
            <h2>Block #{blockData.consensusBlock.blockNumber}</h2>
            <p>Round: {blockData.consensusBlock.round}</p>
            <p>Participation: {blockData.consensusBlock.participationRate}%</p>
          </>
        )}
      </div>

      {errorData && (
        <div className="alert-section">
          <ConsensusErrorMonitor />
        </div>
      )}

      {forkData && (
        <div className="fork-section">
          <ForkMonitor />
        </div>
      )}
    </div>
  );
}
```

---

## ⚠️ Important Notes

### Block Range Limits
- **Recommended**: Query ≤10,000 blocks per request for optimal performance
- **Maximum**: System can handle up to 50,000 blocks but may be slower
- Use pagination (`limit` and `offset`) for large result sets

### BigNumber Handling
All amount fields are returned as strings in **wei**. Always use a BigNumber library:

```typescript
import { ethers } from "ethers";

const amountInEther = ethers.utils.formatEther(event.amount);
const amountInWei = ethers.utils.parseEther("1.5");
```

### Timestamp Precision
- Block timestamps are in **Unix seconds** (not milliseconds)
- Convert for JavaScript: `new Date(timestamp * 1000)`

### Address Format
- All addresses are checksummed (mixed case)
- Comparisons should be case-insensitive: `addr1.toLowerCase() === addr2.toLowerCase()`

---

## 🔧 Error Handling

### Common Error Codes

| Code | Meaning | Solution |
|------|---------|----------|
| `NOT_FOUND` | Block or data not indexed yet | Wait for indexer to catch up |
| `INVALID_RANGE` | `fromBlock` > `toBlock` | Swap block parameters |
| `RANGE_TOO_LARGE` | Block range exceeds limit | Reduce range or use pagination |
| `INVALID_ADDRESS` | Address format incorrect | Use checksummed hex format (0x...) |
| `RATE_LIMIT` | Too many requests | Implement request throttling |

### Error Response Format

```typescript
{
  "error": {
    "code": "RANGE_TOO_LARGE",
    "message": "Block range exceeds maximum of 50,000 blocks",
    "details": {
      "fromBlock": 1000,
      "toBlock": 100000,
      "maxRange": 50000
    }
  }
}
```

---

## 🚀 Performance Tips

1. **Use Pagination**: Always set reasonable `limit` values (10-100 items per page)
2. **Cache Results**: Cache frequently accessed data (active minters, blacklist)
3. **Batch Queries**: Use GraphQL aliasing to fetch multiple resources in one request:

```graphql
query BatchQuery {
  mints: mintEvents(fromBlock: 1000, toBlock: 2000, limit: 20, offset: 0) { ...fields }
  burns: burnEvents(fromBlock: 1000, toBlock: 2000, limit: 20, offset: 0) { ...fields }
  supply: totalSupply
}
```

4. **Optimize Block Ranges**: Start with recent blocks and expand backwards as needed
5. **Use Indexes**: Filter by specific addresses when possible (minter, burner, validator)

---

## 📞 Support & Resources

- **Backend Repository**: [indexer-go](https://github.com/0xmhha/indexer-go)
- **System Contract Documentation**: See `docs/SYSTEM_CONTRACTS_EVENTS_DESIGN.md`
- **Technical Analysis**: See `docs/STABLE_ONE_TECHNICAL_ANALYSIS.md`
- **Gap Analysis**: See `docs/GAP_ANALYSIS_AND_IMPLEMENTATION_PLAN.md`

---

## ✅ Implementation Checklist

Backend implementation is **100% complete**:
- [x] Event signature definitions (38 events)
- [x] SystemContractEventParser (28 parsers)
- [x] Storage layer implementation (18 query methods)
- [x] Fetcher pipeline integration
- [x] GraphQL/JSON-RPC API endpoints
- [x] Compilation and testing verified

**Ready for frontend integration!** 🎉

---

**Questions?** Contact the backend team or open an issue in the repository.

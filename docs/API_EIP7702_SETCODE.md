# EIP-7702 SetCode Transaction API Documentation

## Overview

EIP-7702 introduces SetCode transactions (type 0x04) that allow EOA (Externally Owned Account) addresses to temporarily delegate code execution to smart contracts. This enables features like:
- Account abstraction without deploying a separate contract
- Batch transactions from EOA
- Sponsored transactions
- Session keys

## Key Concepts

### Authorization
Each SetCode transaction contains an `authorizationList` - a list of signed authorizations that grant permission for EOAs to use specific contract code.

### Delegation
When an authorization is applied, the EOA's code is set to a special delegation designator: `0xef0100` + 20-byte address, pointing to the target contract.

### Roles
- **Authority**: The EOA that signs the authorization (the account being delegated)
- **Target**: The contract address whose code will be used for the authority's account

---

## GraphQL API

### Queries

#### 1. `setCodeAuthorization`
Get a specific SetCode authorization by transaction hash and authorization index.

```graphql
query GetSetCodeAuthorization($txHash: Hash!, $authIndex: Int!) {
  setCodeAuthorization(txHash: $txHash, authIndex: $authIndex) {
    txHash
    blockNumber
    blockHash
    transactionIndex
    authorizationIndex
    chainId
    address        # Target contract address
    authority      # Signer address (recovered from signature)
    nonce
    yParity
    r
    s
    applied        # Whether the authorization was successfully applied
    error          # Error message if not applied
    timestamp
  }
}
```

**Variables:**
```json
{
  "txHash": "0x1234...abcd",
  "authIndex": 0
}
```

---

#### 2. `setCodeAuthorizationsByTx`
Get all SetCode authorizations in a transaction.

```graphql
query GetSetCodeAuthorizationsByTx($txHash: Hash!) {
  setCodeAuthorizationsByTx(txHash: $txHash) {
    txHash
    blockNumber
    authorizationIndex
    address
    authority
    applied
    error
    timestamp
  }
}
```

**Variables:**
```json
{
  "txHash": "0x1234...abcd"
}
```

---

#### 3. `setCodeAuthorizationsByTarget`
Get all SetCode authorizations where a specific address is the delegation target (contract being delegated to).

```graphql
query GetSetCodeAuthorizationsByTarget($target: Address!, $pagination: PaginationInput) {
  setCodeAuthorizationsByTarget(target: $target, pagination: $pagination) {
    nodes {
      txHash
      blockNumber
      authorizationIndex
      address
      authority
      applied
      timestamp
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

**Variables:**
```json
{
  "target": "0xContractAddress...",
  "pagination": {
    "limit": 20,
    "offset": 0
  }
}
```

---

#### 4. `setCodeAuthorizationsByAuthority`
Get all SetCode authorizations where a specific address is the authority (signer/delegator).

```graphql
query GetSetCodeAuthorizationsByAuthority($authority: Address!, $pagination: PaginationInput) {
  setCodeAuthorizationsByAuthority(authority: $authority, pagination: $pagination) {
    nodes {
      txHash
      blockNumber
      authorizationIndex
      address
      authority
      applied
      timestamp
    }
    totalCount
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
  }
}
```

**Variables:**
```json
{
  "authority": "0xEOAAddress...",
  "pagination": {
    "limit": 20,
    "offset": 0
  }
}
```

---

#### 5. `addressSetCodeInfo`
Get comprehensive SetCode information for an address, including current delegation status and activity statistics.

```graphql
query GetAddressSetCodeInfo($address: Address!) {
  addressSetCodeInfo(address: $address) {
    address
    hasDelegation          # Whether the address currently has active delegation
    delegationTarget       # Current delegation target (if any)
    asTargetCount          # Number of times this address was used as delegation target
    asAuthorityCount       # Number of times this address delegated to others
    lastActivityBlock      # Last block with SetCode activity
    lastActivityTimestamp  # Last timestamp with SetCode activity
  }
}
```

**Variables:**
```json
{
  "address": "0x1234...abcd"
}
```

**Use Case:** Display on address detail page to show delegation status.

---

#### 6. `setCodeTransactionsInBlock`
Get all SetCode transactions in a specific block.

```graphql
query GetSetCodeTransactionsInBlock($blockNumber: BigInt!) {
  setCodeTransactionsInBlock(blockNumber: $blockNumber) {
    hash
    blockNumber
    from
    to
    value
    gasUsed
    type
    # Full transaction fields available
  }
}
```

**Variables:**
```json
{
  "blockNumber": "12345678"
}
```

---

#### 7. `recentSetCodeTransactions`
Get the most recent SetCode transactions.

```graphql
query GetRecentSetCodeTransactions($limit: Int) {
  recentSetCodeTransactions(limit: $limit) {
    hash
    blockNumber
    from
    to
    value
    timestamp
    type
  }
}
```

**Variables:**
```json
{
  "limit": 10
}
```

**Note:** Maximum limit is 100, default is 10.

---

#### 8. `setCodeTransactionCount`
Get the total count of SetCode transactions indexed.

```graphql
query GetSetCodeTransactionCount {
  setCodeTransactionCount
}
```

**Returns:** `Int!` - Total number of SetCode transactions

---

## JSON-RPC API

### Methods

#### 1. `getSetCodeAuthorization`
```json
{
  "jsonrpc": "2.0",
  "method": "getSetCodeAuthorization",
  "params": {
    "txHash": "0x1234...abcd",
    "authIndex": 0
  },
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "txHash": "0x1234...abcd",
    "blockNumber": "12345678",
    "blockHash": "0xabcd...1234",
    "transactionIndex": 5,
    "authorizationIndex": 0,
    "chainId": "1",
    "address": "0xTargetContract...",
    "authority": "0xSignerEOA...",
    "nonce": "42",
    "yParity": 0,
    "r": "0x...",
    "s": "0x...",
    "applied": true,
    "timestamp": "1704067200"
  },
  "id": 1
}
```

---

#### 2. `getSetCodeAuthorizationsByTx`
```json
{
  "jsonrpc": "2.0",
  "method": "getSetCodeAuthorizationsByTx",
  "params": {
    "txHash": "0x1234...abcd"
  },
  "id": 1
}
```

---

#### 3. `getSetCodeAuthorizationsByTarget`
```json
{
  "jsonrpc": "2.0",
  "method": "getSetCodeAuthorizationsByTarget",
  "params": {
    "target": "0xContractAddress...",
    "limit": 100,
    "offset": 0
  },
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "authorizations": [...],
    "count": 50,
    "limit": 100,
    "offset": 0
  },
  "id": 1
}
```

---

#### 4. `getSetCodeAuthorizationsByAuthority`
```json
{
  "jsonrpc": "2.0",
  "method": "getSetCodeAuthorizationsByAuthority",
  "params": {
    "authority": "0xEOAAddress...",
    "limit": 100,
    "offset": 0
  },
  "id": 1
}
```

---

#### 5. `getAddressSetCodeInfo`
```json
{
  "jsonrpc": "2.0",
  "method": "getAddressSetCodeInfo",
  "params": {
    "address": "0x1234...abcd"
  },
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "address": "0x1234...abcd",
    "hasDelegation": true,
    "delegationTarget": "0xContractAddress...",
    "asTargetCount": 150,
    "asAuthorityCount": 5,
    "lastActivityBlock": "12345678",
    "lastActivityTimestamp": null
  },
  "id": 1
}
```

---

#### 6. `getSetCodeTransactionsInBlock`
```json
{
  "jsonrpc": "2.0",
  "method": "getSetCodeTransactionsInBlock",
  "params": {
    "blockNumber": 12345678
  },
  "id": 1
}
```

---

#### 7. `getRecentSetCodeTransactions`
```json
{
  "jsonrpc": "2.0",
  "method": "getRecentSetCodeTransactions",
  "params": {
    "limit": 10
  },
  "id": 1
}
```

---

#### 8. `getSetCodeTransactionCount`
```json
{
  "jsonrpc": "2.0",
  "method": "getSetCodeTransactionCount",
  "params": {},
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "count": 12345
  },
  "id": 1
}
```

---

## Data Types

### SetCodeAuthorization

| Field | Type | Description |
|-------|------|-------------|
| `txHash` | `Hash` | Transaction hash containing this authorization |
| `blockNumber` | `BigInt` | Block number |
| `blockHash` | `Hash` | Block hash |
| `transactionIndex` | `Int` | Transaction index in block |
| `authorizationIndex` | `Int` | Index within transaction's authorization list |
| `chainId` | `BigInt` | Chain ID from authorization |
| `address` | `Address` | Target contract address (delegation target) |
| `authority` | `Address` | Signer address (recovered from signature) |
| `nonce` | `BigInt` | Authorization nonce |
| `yParity` | `Int` | Signature y-parity (0 or 1) |
| `r` | `Bytes32` | Signature r value |
| `s` | `Bytes32` | Signature s value |
| `applied` | `Boolean` | Whether authorization was successfully applied |
| `error` | `String` | Error code if not applied (see Error Codes) |
| `timestamp` | `BigInt` | Block timestamp |

### AddressSetCodeInfo

| Field | Type | Description |
|-------|------|-------------|
| `address` | `Address` | The queried address |
| `hasDelegation` | `Boolean` | Whether address currently has active delegation |
| `delegationTarget` | `Address?` | Current delegation target (null if none) |
| `asTargetCount` | `Int` | Times this address was used as delegation target |
| `asAuthorityCount` | `Int` | Times this address delegated to others |
| `lastActivityBlock` | `BigInt?` | Last block with SetCode activity |
| `lastActivityTimestamp` | `BigInt?` | Last timestamp with SetCode activity |

### Authorization Error Codes

| Code | Description |
|------|-------------|
| `none` | No error - authorization applied successfully |
| `invalid_chain_id` | Chain ID mismatch |
| `invalid_nonce` | Nonce mismatch |
| `invalid_signature` | Signature verification failed |
| `authority_not_eoa` | Authority address is not an EOA |
| `execution_reverted` | Execution reverted during application |

---

## Frontend Integration Examples

### 1. Address Detail Page - Delegation Status Badge

```typescript
const { data } = useQuery(GET_ADDRESS_SETCODE_INFO, {
  variables: { address }
});

// Show delegation badge
if (data?.addressSetCodeInfo?.hasDelegation) {
  return (
    <Badge color="blue">
      Delegated to: {data.addressSetCodeInfo.delegationTarget}
    </Badge>
  );
}
```

### 2. Transaction Detail Page - SetCode Authorizations Tab

```typescript
const { data } = useQuery(GET_SETCODE_AUTHORIZATIONS_BY_TX, {
  variables: { txHash }
});

// Only show tab for SetCode transactions (type 0x04)
if (transaction.type === 4) {
  return (
    <Tab label={`Authorizations (${data?.length || 0})`}>
      <AuthorizationsList authorizations={data} />
    </Tab>
  );
}
```

### 3. Block Detail Page - SetCode Transactions Filter

```typescript
const { data } = useQuery(GET_SETCODE_TRANSACTIONS_IN_BLOCK, {
  variables: { blockNumber }
});

// Filter option in transaction list
<TransactionFilter
  options={[
    { label: 'All', value: 'all' },
    { label: 'SetCode', value: 'setcode', count: data?.length }
  ]}
/>
```

### 4. Dashboard - Recent SetCode Activity Widget

```typescript
const { data: txCount } = useQuery(GET_SETCODE_TRANSACTION_COUNT);
const { data: recentTxs } = useQuery(GET_RECENT_SETCODE_TRANSACTIONS, {
  variables: { limit: 5 }
});

return (
  <Widget title="EIP-7702 SetCode Activity">
    <Stat label="Total Transactions" value={txCount} />
    <RecentList items={recentTxs} />
  </Widget>
);
```

### 5. Search by Authority/Target

```typescript
// When user searches for an address, show both roles
const { data: asAuthority } = useQuery(GET_SETCODE_BY_AUTHORITY, {
  variables: { authority: address, pagination: { limit: 10 } }
});

const { data: asTarget } = useQuery(GET_SETCODE_BY_TARGET, {
  variables: { target: address, pagination: { limit: 10 } }
});

return (
  <>
    <Section title="As Delegator (Authority)">
      {asAuthority?.nodes.map(auth => <AuthCard {...auth} />)}
    </Section>
    <Section title="As Delegation Target">
      {asTarget?.nodes.map(auth => <AuthCard {...auth} />)}
    </Section>
  </>
);
```

---

## Transaction Type Detection

SetCode transactions have `type = 4` (0x04). In existing transaction responses:

```json
{
  "type": "0x4",
  "authorizationList": [
    {
      "chainId": "0x1",
      "address": "0x...",
      "nonce": "0x0",
      "yParity": "0x0",
      "r": "0x...",
      "s": "0x..."
    }
  ]
}
```

The `authorizationList` field is already included in transaction responses for SetCode transactions.

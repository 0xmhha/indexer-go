# EIP-3091 Block Explorer Support Analysis

## Overview

EIP-3091 defines standard URL patterns for block explorers:
- `/block/{blockNumber}` or `/block/{blockHash}`
- `/tx/{txHash}`
- `/address/{address}`
- `/token/{tokenAddress}`

This document analyzes the current indexer's data availability for each endpoint.

---

## 1. Block Page (`/block/{blockNumber}` or `/block/{blockHash}`)

### Required Data & Indexer Support

| Data | API | Status | Notes |
|------|-----|--------|-------|
| Block Header | `GetBlock(height)`, `GetBlockByHash(hash)` | ✅ | number, hash, timestamp, miner, gasUsed, gasLimit, etc. |
| Parent Hash | Block header | ✅ | |
| Transactions List | Block.Transactions() | ✅ | |
| Transaction Count | len(Block.Transactions()) | ✅ | |
| Receipts | `GetReceiptsByBlockNumber(number)` | ✅ | |
| Total Gas Used | Block header | ✅ | |
| Block Reward | ❌ 계산 필요 | ⚠️ | Need to calculate from coinbase transfer |
| Uncle Blocks | Block.Uncles() | ✅ | |
| **SetCode Txs** | `setCodeTransactionsInBlock(blockNumber)` | ✅ | EIP-7702 |

### GraphQL Queries
```graphql
block(number: BigInt!)
blockByHash(hash: Hash!)
blocksRange(startNumber, endNumber) # Bulk fetch
receiptsByBlock(blockNumber: BigInt!)
setCodeTransactionsInBlock(blockNumber: BigInt!) # NEW
```

### Missing/Considerations
- Block Reward 계산 로직 (coinbase transfer + uncle rewards)
- MEV 관련 데이터 (priority fee revenue)

---

## 2. Transaction Page (`/tx/{txHash}`)

### Required Data & Indexer Support

| Data | API | Status | Notes |
|------|-----|--------|-------|
| Transaction Details | `GetTransaction(hash)` | ✅ | from, to, value, gas, input, nonce |
| Block Info | TxLocation | ✅ | blockNumber, blockHash, txIndex |
| Receipt | `GetReceipt(hash)` | ✅ | status, gasUsed, logs |
| Logs/Events | Receipt.Logs | ✅ | |
| Decoded Logs | `decodeLog` + ABI | ✅ | If ABI registered |
| Internal Txs | `GetInternalTransactions(txHash)` | ✅ | CALL, DELEGATECALL, etc. |
| ERC20 Transfers | `GetERC20Transfer(txHash, logIndex)` | ✅ | Parsed from logs |
| ERC721 Transfers | `GetERC721Transfer(txHash, logIndex)` | ✅ | Parsed from logs |
| Gas Price/Fee | Transaction fields | ✅ | gasPrice, maxFeePerGas, maxPriorityFeePerGas |
| **SetCode Auths** | `setCodeAuthorizationsByTx(txHash)` | ✅ | EIP-7702 type 0x04 |

### GraphQL Queries
```graphql
transaction(hash: Hash!)
receipt(transactionHash: Hash!)
internalTransactions(transactionHash: Hash!)
setCodeAuthorizationsByTx(txHash: Hash!) # NEW - for type 0x04
```

### Transaction Types Supported

| Type | Code | Status | Notes |
|------|------|--------|-------|
| Legacy | 0x00 | ✅ | |
| AccessList (EIP-2930) | 0x01 | ✅ | |
| DynamicFee (EIP-1559) | 0x02 | ✅ | |
| Blob (EIP-4844) | 0x03 | ✅ | |
| **SetCode (EIP-7702)** | 0x04 | ✅ | authorizationList indexed |
| FeeDelegation (StableNet) | 0x16 | ✅ | feePayer, feePayerSignature |

---

## 3. Address Page (`/address/{address}`)

### Required Data & Indexer Support

| Data | API | Status | Notes |
|------|-----|--------|-------|
| Transaction History | `GetTransactionsByAddress(addr, limit, offset)` | ✅ | |
| Filtered Tx History | `transactionsByAddressFiltered` | ✅ | by time, type, etc. |
| ERC20 Transfers | `GetERC20TransfersByAddress(addr, isFrom, ...)` | ✅ | Sent & Received |
| ERC721 Transfers | `GetERC721TransfersByAddress(addr, isFrom, ...)` | ✅ | Sent & Received |
| NFT Holdings | `GetNFTsByOwner(owner, limit, offset)` | ✅ | |
| Contract Info | `GetContractCreation(addr)` | ✅ | If contract |
| Contracts Created | `GetContractsByCreator(addr, ...)` | ✅ | |
| Internal Txs | `GetInternalTransactionsByAddress(addr, isFrom, ...)` | ✅ | |
| Token Balances | `tokenBalances(address)` | ✅ | Via metadata + transfer tracking |
| **SetCode Info** | `addressSetCodeInfo(address)` | ✅ | Delegation status |
| **As Authority** | `setCodeAuthorizationsByAuthority(authority)` | ✅ | Delegations made |
| **As Target** | `setCodeAuthorizationsByTarget(target)` | ✅ | Delegations received |
| Balance (ETH) | RPC `eth_getBalance` | ⚠️ | Not indexed, need RPC |
| Nonce | RPC `eth_getTransactionCount` | ⚠️ | Not indexed, need RPC |
| Code/Bytecode | RPC `eth_getCode` | ⚠️ | Not indexed, need RPC |
| Contract Source | `contractVerification(address)` | ✅ | If verified |
| ABI | `GetABI(address)` | ✅ | If registered |

### GraphQL Queries
```graphql
# Core
transactionsByAddress(address: Address!, pagination: PaginationInput)
transactionsByAddressFiltered(address, filter, pagination)

# Tokens
erc20TransfersByAddress(address, isFrom, pagination)
erc721TransfersByAddress(address, isFrom, pagination)
nftsByOwner(owner, pagination)
tokenBalances(address)

# Contracts
contractCreation(address: Address!)
contractsByCreator(creator: Address!, pagination)
contractVerification(address: Address!)

# Internal
internalTransactionsByAddress(address, isFrom, pagination)

# EIP-7702 SetCode (NEW)
addressSetCodeInfo(address: Address!)
setCodeAuthorizationsByAuthority(authority: Address!, pagination)
setCodeAuthorizationsByTarget(target: Address!, pagination)
```

### Address Type Detection

| Type | Detection | Display |
|------|-----------|---------|
| EOA | No contract creation | Basic address |
| Contract | Has ContractCreation | Show code, ABI |
| Token (ERC20) | TokenMetadata.Standard == "ERC20" | Token page link |
| NFT (ERC721) | TokenMetadata.Standard == "ERC721" | NFT collection |
| **Delegated EOA** | `hasDelegation == true` | Show delegation target |
| **Delegation Target** | `asTargetCount > 0` | Show delegators |

---

## 4. Token Page (`/token/{tokenAddress}`)

### Required Data & Indexer Support

| Data | API | Status | Notes |
|------|-----|--------|-------|
| Token Info | `GetTokenMetadata(address)` | ✅ | name, symbol, decimals |
| Token Standard | TokenMetadata.Standard | ✅ | ERC20, ERC721, ERC1155 |
| Total Supply | TokenMetadata.TotalSupply | ⚠️ | Snapshot, may be outdated |
| Transfer History | `GetERC20TransfersByToken(token, ...)` | ✅ | |
| NFT Transfers | `GetERC721TransfersByToken(token, ...)` | ✅ | |
| Contract Info | `GetContractCreation(token)` | ✅ | Creator, deploy tx |
| Verified Source | `contractVerification(address)` | ✅ | |
| Holders List | ❌ | ❌ | Not indexed |
| Holder Count | ❌ | ❌ | Not indexed |
| Balance of Address | RPC `balanceOf(address)` | ⚠️ | Need RPC call |

### GraphQL Queries
```graphql
# Token metadata (auto-detected on contract creation)
tokenMetadata(address: Address!) # via addressOverview or separate query

# Transfers
erc20TransfersByToken(token: Address!, pagination)
erc721TransfersByToken(token: Address!, pagination)

# Contract info
contractCreation(address: Address!)
contractVerification(address: Address!)
```

### Missing Features for Token Page

| Feature | Priority | Implementation Effort |
|---------|----------|----------------------|
| Holder Count | High | Medium - aggregate transfers |
| Holders List | Medium | High - maintain balance index |
| Real-time Total Supply | Low | Medium - periodic RPC sync |
| Price Data | Low | External API integration |

---

## 5. Search (`/search?q={query}`)

### Supported Search Types

| Query Type | Example | Status | API |
|------------|---------|--------|-----|
| Block Number | `12345678` | ✅ | `Search()` → block |
| Block Hash | `0xabc...` (66 chars) | ✅ | `Search()` → block |
| Tx Hash | `0xdef...` (66 chars) | ✅ | `Search()` → transaction |
| Address | `0x123...` (42 chars) | ✅ | `Search()` → address |
| Contract | Same as address | ✅ | Detected via ContractCreation |
| Token Name/Symbol | `USDT`, `Tether` | ✅ | `SearchTokens(query)` |
| ENS Name | `vitalik.eth` | ❌ | Not implemented |

### GraphQL
```graphql
search(query: String!, types: [String], limit: Int): [SearchResult!]!
```

---

## 6. EIP-7702 SetCode Integration Summary

### New URL Patterns (Recommended)

| URL Pattern | Description |
|-------------|-------------|
| `/tx/{txHash}#authorizations` | SetCode tx authorization tab |
| `/address/{address}#delegation` | Address delegation info |
| `/address/{address}#delegations-made` | As authority list |
| `/address/{address}#delegations-received` | As target list |

### API Endpoints Added

| Endpoint | Purpose |
|----------|---------|
| `setCodeAuthorization(txHash, authIndex)` | Single auth detail |
| `setCodeAuthorizationsByTx(txHash)` | All auths in tx |
| `setCodeAuthorizationsByTarget(target)` | Delegations to contract |
| `setCodeAuthorizationsByAuthority(authority)` | Delegations by EOA |
| `addressSetCodeInfo(address)` | Delegation status summary |
| `setCodeTransactionsInBlock(blockNumber)` | SetCode txs in block |
| `recentSetCodeTransactions(limit)` | Recent activity |
| `setCodeTransactionCount` | Total count |

---

## 7. Gap Analysis & Recommendations

### Currently Missing (Not Critical for EIP-3091)

| Feature | Impact | Recommendation |
|---------|--------|----------------|
| Real-time Balance | Medium | Use RPC proxy (already exists) |
| Real-time Nonce | Low | Use RPC proxy |
| Bytecode Storage | Low | Use RPC proxy or index on demand |
| Token Holders | Medium | Future: Add holder tracking index |
| ENS Resolution | Low | Future: External service integration |
| Block Rewards | Low | Calculate from coinbase + uncles |

### Strengths

1. **Complete Block/Tx/Receipt indexing** - Core EIP-3091 requirements met
2. **Rich Address Data** - Transactions, tokens, contracts, internal txs
3. **Token Detection** - Automatic ERC20/721/1155 detection
4. **Contract Verification** - Source code verification support
5. **EIP-7702 Support** - Full SetCode transaction indexing
6. **Unified Search** - Cross-entity search capability
7. **Fee Delegation** - StableNet-specific feature support

### Recommended Enhancements

1. **Token Holder Tracking** (Medium effort)
   - Track balance changes from Transfer events
   - Maintain holder count per token

2. **Address Labels** (Low effort)
   - System contracts, known addresses
   - User-defined labels

3. **Gas Analytics** (Low effort)
   - Average gas price history
   - Block gas utilization trends

---

## 8. Frontend Implementation Checklist

### Block Page
- [ ] Block header display
- [ ] Transaction list with pagination
- [ ] SetCode transactions filter/badge (NEW)
- [ ] Gas usage visualization
- [ ] Link to parent/child blocks

### Transaction Page
- [ ] Transaction details
- [ ] Receipt status & logs
- [ ] Decoded events (if ABI available)
- [ ] Internal transactions tab
- [ ] Token transfers tab
- [ ] **SetCode Authorizations tab** (NEW - for type 0x04)

### Address Page
- [ ] Balance display (via RPC)
- [ ] Transaction history with filters
- [ ] Token holdings tab
- [ ] NFT gallery
- [ ] Contract tab (if contract)
- [ ] Internal transactions
- [ ] **Delegation Status badge** (NEW - EIP-7702)
- [ ] **Delegations Made/Received tabs** (NEW - EIP-7702)

### Token Page
- [ ] Token metadata display
- [ ] Transfer history
- [ ] Contract info link
- [ ] (Future) Holder list

### Search
- [ ] Multi-type search results
- [ ] Token name/symbol search
- [ ] Result type icons/badges

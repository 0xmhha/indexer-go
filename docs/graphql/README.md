# GraphQL API Documentation

## Overview

This GraphQL API provides comprehensive access to blockchain indexer data including blocks, transactions, consensus information, and system contract events.

## Key Features

- **Blockchain Data**: Query blocks, transactions, and internal transactions
- **Consensus Information**: Access WBFT consensus data, validator statistics, and epoch information
- **System Contracts**: Query minter/burn events, proposals, and governance data
- **Real-time Updates**: Subscribe to new blocks and transactions
- **Flexible Filtering**: Consistent filter patterns across all queries
- **Pagination Support**: Efficient data retrieval with cursor-based pagination

## Quick Start

### GraphQL Endpoint

```
http://localhost:8080/graphql
```

### Basic Query Example

```graphql
query GetLatestBlock {
  latestBlock {
    number
    hash
    timestamp
    transactionCount
  }
}
```

### With Variables

```graphql
query GetBlock($number: BigInt!) {
  block(number: $number) {
    number
    hash
    timestamp
    transactionCount
    gasUsed
    gasLimit
  }
}
```

Variables:
```json
{
  "number": "1000"
}
```

## Documentation Structure

- [Queries](./queries.md) - Comprehensive query examples and usage patterns
- [Subscriptions](./subscriptions.md) - Real-time data subscription examples
- [Types](./types.md) - Type definitions and field descriptions
- [Best Practices](./best-practices.md) - Performance tips and common patterns

## Common Patterns

### Filter Object Pattern

Most queries use a consistent filter object pattern for flexibility:

```graphql
query GetMintEvents {
  mintEvents(
    filter: {
      fromBlock: "1000"
      toBlock: "2000"
      minter: "0x..."
    }
    pagination: {
      limit: 50
      offset: 0
    }
  ) {
    nodes {
      blockNumber
      transactionHash
      minter
      amount
    }
    pageInfo {
      hasNextPage
      totalCount
    }
  }
}
```

### Pagination

Use `limit` and `offset` for pagination:

```graphql
{
  proposals(
    filter: { ... }
    pagination: {
      limit: 20
      offset: 40  # Page 3 (20 * 2)
    }
  ) {
    nodes { ... }
    pageInfo {
      hasNextPage
      hasPreviousPage
      totalCount
    }
  }
}
```

## Error Handling

Errors are returned in standard GraphQL format:

```json
{
  "errors": [
    {
      "message": "invalid block number format",
      "path": ["block"],
      "extensions": {
        "code": "BAD_USER_INPUT"
      }
    }
  ],
  "data": null
}
```

## Performance Tips

1. **Request Only Needed Fields**: GraphQL allows precise field selection
2. **Use Pagination**: Limit result sets to avoid timeout
3. **Filter at Source**: Use filter parameters instead of client-side filtering
4. **Batch Queries**: Combine multiple queries in one request

## Recent Improvements

### Phase 1: Field Name Standardization
- `txHash` → `transactionHash`
- `txCount` → `transactionCount`

### Phase 2: New Queries
- `minterConfigHistory` - Minter configuration changes across all minters
- `burnHistory` - Token burn history (alias for burnEvents)
- `authorizedAccounts` - GovCouncil authorized accounts

### Phase 2: Query Aliases
- `wbftBlock` - Alias for `wbftBlockExtra`
- `latestEpochData` - Alias for `latestEpochInfo`
- `epochByNumber` - Alias for `epochInfo`
- `allValidatorStats` - Alias for `allValidatorsSigningStats`

## Support

For issues or questions:
- Check [queries.md](./queries.md) for detailed examples
- Review [best-practices.md](./best-practices.md) for optimization tips
- See [types.md](./types.md) for complete type reference

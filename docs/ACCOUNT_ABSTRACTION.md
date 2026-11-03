# Account abstraction queries

The ERC-4337 feature indexes configured EntryPoint receipt events into
UserOperations and bundler/paymaster/factory/smart-account statistics. Start with
[feature and EntryPoint configuration](CONFIG.md#account-abstraction-eip-4337).
The `aa.erc7579` feature separately indexes account modules.

## Query the indexed data

The current GraphQL names include:

| Data | List | Detail |
| --- | --- | --- |
| UserOperations | `userOperations`, `userOperationsBySender` | `userOperation(hash:)` |
| Bundlers | `bundlers` | `bundler(address:)` |
| Paymasters | `paymasters` | `paymaster(address:)` |
| Factories | `factories` | `factory(address:)` |
| Smart accounts | `smartAccounts` | `smartAccount(address:)` |

For example:

```graphql
query AccountAbstractionLists {
  bundlers(pagination: {limit: 20}) {
    nodes { address totalBundles totalOps }
    pageInfo { hasNextPage endCursor }
  }
  paymasters(pagination: {limit: 20}) {
    nodes { address totalOps }
    pageInfo { hasNextPage endCursor }
  }
}
```

When a page has a next cursor, repeat the same query with
`pagination: {limit: 20, after: "<endCursor>"}`. Follow [API pagination](API.md#pagination)
for supported cursor/offset behavior. Connections also expose `totalCount`.
Request runtime introspection for your linked features and profile.
The [runtime schema snapshot](../pkg/app/testdata/api/graphql-schema.txt),
[query registration](../pkg/api/graphql/schema_userops.go) and
[resolvers](../pkg/api/graphql/resolvers_userop.go) define the current contract.

## Coverage and integration

Statistics cover data actually indexed from the selected EntryPoints and range;
a recent UserOperation sample is not a complete bundler/paymaster list.
Event logs do not supply every calldata/gas field. Adding an EntryPoint affects
subsequent indexing; historical coverage requires the documented reindex path.
Schema field presence alone does not prove collection is enabled or complete.

Use these backend fields directly in a client. `allBundlers`/`allPaymasters` are
not the list field names, and an extra backend implementation is not required
for the existing `bundlers`/`paymasters` APIs. Changes in another frontend
repository remain a [separate integration task](analysis/open-items.md#ext-03).

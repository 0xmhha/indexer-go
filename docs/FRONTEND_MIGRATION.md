# Frontend integration

Integrate against the server's linked profile, enabled features and indexed
range. The backend contract is the current runtime schema and
[API reference](API.md); a separate frontend checkout's state is not inferred
from saved documents in this repository.

## GraphQL fields and collection status

Use runtime introspection and the [schema snapshot](../pkg/app/testdata/api/graphql-schema.txt).
Account abstraction clients use [the current AA queries](ACCOUNT_ABSTRACTION.md):

| Client query intent | Backend field |
| --- | --- |
| UserOperation by hash | `userOperation(hash:)` |
| UserOperation list | `userOperations(pagination:, sender:)` |
| Bundler list/detail | `bundlers(pagination:)`, `bundler(address:)` |
| Paymaster list/detail | `paymasters(pagination:)`, `paymaster(address:)` |

Cursor pages retain their documented query/filter and epoch/hash pins. Do not
turn unavailable/uncollected or incomplete coverage into an empty complete
result. Trace, blob, proof and source queries have separate collection contracts.

## Polling and subscriptions

Use the [REST endpoints](API.md#3-rest) for supported fixed queries. They execute
the same GraphQL resolvers and use the documented ETag/Cache-Control behavior.
Use `/graphql/ws` and the documented subscription protocol for live events;
reconnect and resume/reconcile through durable stream cursors as supported.
Notification streams use their own authentication/settings; they are not an
operator-alert destination. Multi-chain clients use the corresponding chain route.

## Validate a client change

Compare each document/variable shape with runtime introspection before deploying.
The repository's `TestFrontendDocuments` checks the frozen GraphQL documents in
[pkg/app/testdata/frontend](../pkg/app/testdata/frontend/) against the backend
schema and its explicit known-invalid list. This is fixture compatibility,
not a statement that another repository is currently deployed or fixed.
Run that focused check when changing API/client contracts; docs-only maintenance
does not require starting the server or rerunning runtime tests.

Keep secrets out of URL/query/log output, respect API limits, and distinguish
service health from indexing progress. Frontend implementation/deployment in
another project is [deferred work](analysis/open-items.md#ext-03).

# receipts: P07 on the indexer framework

The P07 receipt indexer (nu-54v-dk-toy `products/p07-indexer`) rebuilt on
the indexer SDK (`pkg/sdk`, docs/SDK.md) as a separate Go module that
changes nothing in the indexer (refactoring plan R6-2 and R6-3).

- Configuration does the ingestion: `indexer.mode: declared`,
  `indexer.finality: finalized` and a `receipts` table of the settlement
  contract's `PaymentSettled(address indexed merchant, bytes32 indexed
  orderId, address indexed device, uint256 amount, uint256 nonce)` with the
  key `[merchant, orderId]` (see `helpers_test.go`).
- `receipts.go` adds P07's API: `GET /receipts/{merchant}/{orderId}` (the
  earliest log, `duplicate` when the order settled more than once, 404
  `NOT_INDEXED`, 503 `RPC_STALE` while indexing is more than 60 blocks
  behind, 400 for malformed input) and `GET /healthz` (`cursor`, `lag`,
  `polled`). It also adds a feature of its own, `receipts.totals`, with
  `GET /merchants/{merchant}/totals`.
- `cmd/receipts-indexer` is the binary: the indexer plus these handlers.

```bash
go build -o receipts-indexer ./cmd/receipts-indexer
./receipts-indexer --config config.yaml
go test ./...   # builds the binary and runs it against a test chain
```

## P07 acceptance (acceptance_test.go)

| Requirement | Check |
|---|---|
| FR-01 only the settlement contract's PaymentSettled, finalized blocks | the other contract's same event and the settlement contract's other event are no receipts and are not counted; the database holds headers, records and the handler's keys only |
| FR-02 the cursor survives a restart | stopped part way and restarted: every payment once, nothing missed |
| FR-03 a log is stored once | restarting reads nothing twice; indexing the same range again is checked by `pkg/app` `TestRecordsFromDeclaredTables` |
| FR-04 lookup by (merchant, orderId), else 404 | 404 before the payment block is final, the receipt after |
| FR-05 the earliest log, marked duplicate | order 1 settled twice: the first log, `duplicate: true`; order 2 `false` |
| FR-06 the response fields | exactly merchant, orderId, device, amount, blockNumber, blockTime, txHashShort, duplicate, with the chain's values |
| NFR-02 RPC errors keep the cursor | `eth_getLogs` failing: the cursor stays; once the node answers the range is read again |
| NFR-03 no write API | POST is 405; no explorer JSON-RPC |
| design 4 stale | more than 60 blocks behind: a missing receipt is 503 `RPC_STALE` with the cursor |

## Differences from P07

- P07 reads `eth_getLogs` over up to 1,000 blocks per call; the declared
  mode reads each block's header and logs in one batch, two calls per
  block. Catching up from the deployment block takes more calls.
- P07's `start` is the cursor before the first block; `start_block` is the
  first block read.
- The lag is the node's head (under the finality policy) read every
  second while the live loop runs, minus the indexed block; it stays
  current while a batch keeps failing.
- P07 keeps its rows in PostgreSQL; the indexer stores records in Pebble or
  PostgreSQL (`database.driver`).

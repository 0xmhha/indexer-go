# receipts: an indexer with handlers of its own

An example of the indexer SDK (`pkg/sdk`, docs/SDK.md): the receipt lookup
of a payment settlement contract, built as a separate Go module that
changes nothing in the indexer.

- `receipts.go` registers `GET /receipts/{merchant}/{orderId}` (the
  earliest `PaymentSettled` log of the order, `duplicate` when it settled
  more than once, 404 `NOT_INDEXED` otherwise), the `receipts.totals`
  feature (each merchant's receipt count and amount, kept block by block)
  and `GET /merchants/{merchant}/totals`.
- `cmd/receipts-indexer` is the binary: the indexer plus these handlers.
- The indexer runs in the declared mode and stores the contract's
  `PaymentSettled` logs as the `receipts` table (see the configuration in
  `receipts_test.go`).

```bash
go build -o receipts-indexer ./cmd/receipts-indexer
./receipts-indexer --config config.yaml
go test ./...   # builds the binary and runs it against a test chain
```

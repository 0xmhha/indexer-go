# Arc balance oracles

The JSON fixtures retain independent node responses from
`https://rpc.testnet.arc.io` (chain ID 5042002). Block and receipt fields
are not synthesized from the accounting implementation. Balance files
contain decimal `eth_getBalance` results at `height - 1` and `height`.
The tests compare their differences with the profile's native deltas,
before the balance feature can reset a diverged account to node state.

| Height | Collected | Transactions | Oracle addresses | Covered movement |
|---|---|---:|---:|---|
| 66904957 | 2026-10-13 | 6 | 12 | Original regression against Ethereum fee/value rules |
| 67017992 | 2026-10-14 | 9 | 19 | Two native mints; eight native transfers |
| 67018549 | 2026-10-14 | 5 | 13 | Five native transfers; four have endpoints differing from the top-level transaction |
| 67018552 | 2026-10-14 | 4 | 10 | One failed transaction still pays execution gas |
| 67018564 | 2026-10-14 | 6 | 13 | One native burn; four native transfers |

Every new sample includes paid execution gas and checks the miner's full
fee credit. Oracle addresses include the miner, transaction senders and
recipients, and nonzero participants of the native system-emitter logs.
Mint/burn does not create a zero-address balance. The four new samples
compare 55 address/block deltas; this counts the same address again when
it appears in another block.

Endpoint differences demonstrate movements beyond the top-level
transaction's from/to pair; execution traces were not collected. These
bounded testnet fixtures do not establish all historical fork rules,
SELFDESTRUCT behavior, or mainnet conformance. Legacy authority events
remain covered by synthetic unit tests, not new historical state samples.

For a same-input Pebble/PostgreSQL comparison through production wiring,
see `TestLiveRPCStorageParity` in `pkg/app/rpc_parity_test.go` and
[the validation record](../../../../docs/analysis/work-log-2026-10.md).

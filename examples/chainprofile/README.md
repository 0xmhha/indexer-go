# A project's Arc testnet profile

This separate Go module builds its own indexer binary and registers
`project-arc` without changing the indexer. It is an example of the
[chain customization contract](../../docs/CHAIN_CUSTOMIZATION.md), not a
new chain implementation.

`pkg/chains/projectarc/profile.go` embeds `*evm.Profile` from `arc.New()`. The example overrides
the profile id and narrows detection to Arc testnet (5042002). It retains
the base profile's header and transaction verification, receipt decoding,
native accounting, native emitter and binary interfaces. Registering it
at priority 200 puts it before the built-in Arc profile (100); Arc mainnet
keeps the built-in profile, and unrelated EVM chains keep their profiles.
`project-arc-testnet` is an alias. Duplicate profile ids are rejected.

The main package imports this module and calls `cli.Main()` (`cmd/cli`). A project's
optional features and routes can be linked the same way; the
[receipts example](../receipts) demonstrates those SDK extensions.
The profile APIs imported here are outside the SDK compatibility promise;
pin the indexer version and run these tests when upgrading.
The Arc profile was added after tag `v0.2.15`; that tag alone cannot build
this example. This checkout uses `replace => ../..`. Outside the checkout,
pin a commit or release containing the Arc profile.

From this directory:

```bash
go build -o project-indexer ./cmd/project-indexer
go test -race -count=1 ./...
```

To read the public testnet, first choose a recent height whose preceding
state the RPC retains, set `ARC_START_HEIGHT` to that number, then run:

```bash
./project-indexer --config config.example.yaml \
  --adapter project-arc-testnet \
  --start-height "${ARC_START_HEIGHT:?set a retained recent start height}"
```

This creates a new database in `./data`. Do not reuse a database previously
indexed with generic Ethereum accounting. The normal startup state checks
remain enabled; the example does not bypass them. A recent start omits
earlier blocks, and the node must retain the state while indexing and
backfilling too. The binary only reads RPC data and sends no transactions.

The tests use local synthetic inputs, without public RPCs or Docker. They
verify profile priority and aliases, preservation of native accounting and
native-coin interfaces, and rejection of altered block/transaction hashes
and transaction senders. This example does not establish new mainnet,
historical accounting or live operation guarantees. Existing Arc testnet
measurements are recorded in `docs/analysis/work-log-2026-10.md` 3.5.

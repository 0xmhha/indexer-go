# Durable execution-result spool

`spool.Open` owns one private directory and persists validated `execwire.Result` values until the indexer returns an exact durable `Committed` ACK. It performs no RPC calls, starts no node and accesses no PostgreSQL database.

## Use

Supply a dedicated directory, chain ID/genesis, and immutable initial anchor. The final directory is created with mode `0700`; its parent must already exist. Files use `0600`. Linux and macOS process locks reject a second owner. Wrong identity, symlinks, hard links, unknown files and corrupt sealed metadata fail closed.

Call `Append` before transmission. It validates a contiguous generation/height/parent chain, writes a bounded result, then completes file sync, rename and directory sync before `Next` exposes it. `Read` validates length, digest and wire result before returning data. Retained duplicates return the original entry; conflicting content is rejected.

After the indexer's final publication commit, pass the matching chain/reference/digest `Committed` frame to `Acknowledge`. Only the next pending result can be acknowledged. A Hello cursor or a later ACK never deletes an unacknowledged prefix. The latest 128 committed identities support idempotent duplicate ACKs/appends; older unknown duplicates are rejected.

ACK cleanup first seals a durable ACK receipt, then removes the exact result, advances the durable checkpoint and removes the receipt. Reopening completes interrupted cleanup using that proof. If an operation returns `ErrRecovery`, close and reopen before further processing. `Close` joins active operations and releases ownership. Waiting operations accept cancellation; filesystem sync itself is not interruptible.

## Capacity and recovery

Defaults are 4 GiB and 262,144 retained result/temporary files; configure both explicitly for deployment. Bytes include physical allocated size where available, metadata, temporary and quarantined files. A 64 KiB metadata reserve keeps space for ACK/checkpoint work. Capacity pressure returns `ErrFull` and never evicts unacknowledged data.

Known incomplete temporary files are quarantined and charged to the quota. Corrupt sealed files remain untouched. An interrupted initial identity write requires explicit operator recovery: it cannot be rebound automatically, even to the same identity. No automatic cleanup of quarantined data is provided. These are functional safety limits, not storage sizing measurements.

Generation rewind requires the later coordinator's durable confirmation. This package does not infer it from a cursor and does not garbage-collect the node database. See the [E1 contract](../../../../docs/codemine/runs/20261023-e1-execution/05-spool-contract.md).

## Checks

```sh
# From x/stablenet/executor
go test -race -count=1 ./spool
GOMAXPROCS=1 go test -race -count=200 -timeout=5m \
  -run '^TestConcurrentCancellationAndCloseJoin$' ./spool
```

Tests use private temporary directories, inject failures at each ACK durability boundary, and verify recovery preserves later unacknowledged data. They also cover corruption, bounds, ownership, cancellation and joining. They start no node services or peers and measure no performance.

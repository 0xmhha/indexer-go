# E5 bounded call ranges and durable orphan observations

This is priority2 of the four ordered releases. Priority1 is released as
[#227](https://github.com/0xmhha/indexer-go/pull/227), squash
`449c31750bb4efb48d12b9264c1c1dc3fcb273aa`. Priorities3/4 start only after this
feature's exact-head CI5, squash and clean primary-main synchronization.

Implement the [public contract](../../../EXECUTION_CALL_TRACES.md) with a single
reserved PostgreSQL snapshot, bounded address/range/limit validation, strict
coverage and explicit partial opt-in, keyset pagination and coverage fencing.
Migration0021 retains published traces and uncollected markers in the same
rewind transaction before canonical purge. Historical epoch and original
ref/digest remain separate. Part-only rows never become orphan coverage.

Preserve ordinary ingest, native REST block/transaction pages, pool4/part2 and
seven writers, two EVM passes, shared RPC5, final NEW fresh and generation/ACK,
undo/outbox, full-baseline/checkpoint/permit/lease/intent/release/GC authorities.
No Rust/upstream changes. Held: nodes/peers/operator DB, disks/operational GC,
performance/SLO loops, tags/deploy and remote branch deletion.

See [checklist](checklist.md), [validation](validation.md),
[continuity](continuity.json) and [open items](../../../analysis/open-items.md).

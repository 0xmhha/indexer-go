# E4 feature writers

Base: E3 #216, main `a493be8`. The user authorized E4 with E1's RED/GREEN, current docs/continuity and CI-gated squash/main workflow. Accepted E1/E2/E3 contracts remain in force.

## Contract

One writer owns each resolved feature unit (including declared parts and multiple hooks). Preparation, ordered application and the unit's exact checkpoint belong to that writer. Every feature SQL mutation, undo, checkpoint, public height and outbox still commits in the **same** publication transaction, followed by uncached fresh and the post-response generation fence. Writers do not create pools or hold independent connections. Core seven queues, pool4/part2 and API reservation remain unchanged. This is responsibility separation, not parallel commits to mutable feature tables; dependency reads require the same candidate transaction. No throughput improvement is claimed without measurement.

Native execution balance queries/history use the durable account journal as their sole authority, bounded by the public snapshot. The balance feature verifies its input against the stored journal instead of writing a second balances/balance_history projection. Missing/partial state never becomes zero or RPC fallback. History uses anchor baseline plus actual nonzero changes in height/ordinal order (Finalize has a zero tx hash); its execution cursor is distinct from the old projection cursor. Existing projection rows remain untouched and ignored in execution mode. RPC-mode storage and capture summary/lookup/search workers remain unchanged. No schema/data deletion is authorized.

## Units

- U01: bounded block-bound feature writer plan; grouped hooks, per-unit checkpoints after application, errors/cancel/identity fences. RED then GREEN.
- U02: stored-journal balance verification and authoritative snapshot/history reads; uint256, tombstones, unchanged balances, baseline, coverage, pagination and poisoned old projection. Real fsync-on disposable PG RED/GREEN.
- U03: app integration: fresh failure/rollback, unit failure, retry/lost ACK, restart and rewind; existing frozen EVM/WBFT fixtures and API semantics. RED/GREEN for new requirements.
- U04: required full build/fmt/vet/lint/race/cache/PG/examples, new concurrency single GOMAXPROCS1/race/count200, staged/history secret scans, draft PR latest-head CI4 then ready/squash/ff main.

No Node.Start/live peers, provisioning/format, operational GC, performance/host-load tests or other-project changes. Explicit staging and English Conventional commits, no codex/attribution; preserve remote branches. E5/E6 and operations are reported, not started automatically.

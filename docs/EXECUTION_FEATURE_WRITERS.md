# Execution feature writers

E4 separates the existing feature tables into **one writer per resolved feature unit**. It applies to the opt-in StableNet/PostgreSQL execution path. Ordinary RPC/declared ingestion and capture summary/lookup/search workers retain their existing contracts.

## Publication contract

The seven core/state queues first persist unpublished parts. The final publication callback builds a block-bound `feature.WriterPlan` from the registered pipeline. Each writer owns its prepared inputs, all hooks of its unit and its exact `(definition, generation, height, hash, digest)` checkpoint. Declared feature parts remain separate units; multiple hooks of a unit complete together. Writers run in dependency order and can read earlier writers' uncommitted changes/checkpoints.

Feature SQL still runs in **one publication transaction** with undo, outbox and visible height. A unit/checkpoint failure, cancellation, changed block identity or failed final fresh rolls everything back. No individual feature commit grants visibility or a source ACK. Lost-ACK retries skip feature preparation/application and perform a new canonical check. Rewind restores feature data/checkpoints through existing undo and excludes stale reads/work through generation fences.

This separates responsibilities, not mutable SQL commits: arbitrary token/DEX/custom handlers can depend on each other, and committing their tables ahead would require a new versioned storage contract. E4 does not parallelize SQL on a single pgx transaction or open per-feature pools/goroutines. Core seven queues share two part permits, the existing pool remains **four connections per chain**, and API reservation remains within it. State-preparation payloads share `indexer.state_max_bytes`. RPC calls still use the shared **5 physical HTTP attempts/s** budget, including retries. Uncached canonical fresh remains after all queued writes/undo, immediately before commit, with the post-response generation fence.

## One native balance authority

In execution mode, PostgreSQL reads native balances from the account journal at the public snapshot height. The `balance.native` writer verifies its complete ordered input against durable account parts inside the candidate transaction; it performs no profile accounting, balance RPC repair or second `balances`/`balance_history` write. Wrong/missing entries, ordinal, transaction, before/after value or block binding abort publication.

`GetAddressBalance(..., 0)` means latest **public** balance. Historical reads cannot exceed visibility or silently fill missing partial coverage. Verified full E2 coverage can prove an unlisted account is zero. Read-only API processes use the same authority.

Execution balance history is the anchor baseline (delta zero) followed by actual nonzero balance changes ordered by height and ordinal. Nonce-only changes are excluded. Finalize changes have a zero transaction hash. Deletion is represented by the journal's zero after-balance. Requests before the anchor return a coverage error: a checkpoint is not pre-anchor history. Existing API fields and unsigned-256-bit precision are preserved; history now reports the journal rather than a synthetic initialization at the first changed block.

History cursors are opaque and scoped to execution format, address, range and generation. Old projection cursors and cursors crossing a rewind are rejected; restart pagination without a cursor. Ordinary RPC-mode balance histories/cursors are unchanged.

Upgrade execution API binaries together with, or before, the E4 ingest binary. A pre-E4 API process can keep reading the now-stale legacy projection after the new writer stops updating it; mixed execution versions are unsupported. No deployment was performed.

No migration or destructive cleanup is required. Previously stored execution projection rows are preserved and ignored by execution balance reads. New execution publications stop duplicating them. Generic RPC-mode balances and their undo remain unchanged. Disabling the balance feature does not remove the core account journal or make authoritative execution balances disappear; enabling/changing feature units after public progress still requires the existing explicit checkpoint contract.

## Verification and limits

Feature writer tests cover grouped hooks/checkpoint order, bounded inputs, dependent failure, block mutation, cancellation and concurrent independent plans. Real fsync-on PostgreSQL checks journal binding, poisoned old projections, visible history, pagination, precision and coverage. Frozen EVM/WBFT results cross actual seven-part writers, publication, API, IPC, restart, reorg and lost ACK, and assert no duplicate balance rows.

These are functional checks. No live peer/node service, disk provisioning, operational GC or performance measurement is part of E4. E1's two EVM executions, E2 bootstrap and E3 retention fences remain unchanged. See the [plan](codemine/runs/20261024-e4-feature-writers/plan.md), [checklist](codemine/runs/20261024-e4-feature-writers/checklist.md), [validation](codemine/runs/20261024-e4-feature-writers/validation.md) and [remaining work](analysis/open-items.md).

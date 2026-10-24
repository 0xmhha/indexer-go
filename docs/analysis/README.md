# Design and validation notes

Use [the documentation index](../README.md) for current usage and extension guides. This directory contains implementation plans, decisions, validation evidence and historical analysis. A design or a successful fixture test does not establish live support or a performance guarantee.

## Current work and evidence

| Question | Document |
| --- | --- |
| What is implemented, unresolved or deferred? | [Remaining work](open-items.md) |
| What changed, failed or was measured, and under which conditions? | [October 2026 work log](work-log-2026-10.md) |
| Do the draft Docker, capacity and recovery policies agree? | [Offline operations review](operations-offline-review.md) |
| How was the original framework plan reviewed? | [Plan](refactoring-plan.md), [completion audit](plan-audit.md) |
| Where are source-writer contracts and later verification details? | [Source facts](../SOURCE_FACTS.md) |

The work log preserves the state at each date. Later entries can close an earlier issue; use remaining work for the current status. Source facts combines a usage contract with dated implementation/validation sections, whose claims stay within their recorded scope.

## Architecture decisions

- [Command line and chain products](cli-and-products-design.md): cobra command line in cmd/, chain adapters under x/<chain>, per-chain binaries and the PR sequence.
- [Chain profiles](chain-profile-design.md): chain-specific decoding, accounting and extensions.
- [Feature registry](feature-registry-design.md): optional feature composition and dependencies.
- [Reorg recovery](reorg-design.md): rollback and orphan handling.
- [PostgreSQL undo](postgres-undo-design.md): storage-specific rollback design.
- [Subscriptions](subscriptions-design.md): outbox, replay, backpressure and notification contracts.
- [Phase 0](phase0-design.md): earlier correctness work and defect analysis.
- [Structure review](structure-review.md) and [code graph](graph/README.md): architecture analysis snapshots.

Check the corresponding maintained guide and code before relying on a proposed interface or an older package path.

## Feed and acquisition work

| Note | How to use it |
| --- | --- |
| [Execution ingest design](execution-ingest-design.md) | E1 execution ingest and E2 offline checkpoint preparation are implemented under their checklists. E3 node/raw deletion, other execution modules and operational evidence remain separate. |
| [Windowed feed design](windowed-feed-design.md) | Design goals, storage/processing boundaries and staged acceptance criteria. Implemented settings are documented in [feed application](../FEED_APP.md) and [window storage](../WINDOW_STORAGE.md). |
| [Arc peer capabilities](arc-peer-capabilities.md) | Input format and protocol audit; distinguish header/body/receipt supply from state, trace and blob support. |
| [Arc testnet identity](arc-testnet-identity.md) | Public genesis/network evidence and the remaining peer and fork verification requirements. |
| [Arc acquisition queue](arc-acquisition-queue.md) | RPC acquisition/processing split and its state-retention limits. |
| [Node-feed options](node-feed-options.md) | Earlier node/ExEx comparison; it is not the default lightweight peer architecture. |
| [Node-feed adapter](../NODE_FEED.md) | Optional historical ExEx/IPC implementation and validation limits. |

Actual Arc peer access, external-volume operation, independent finality evidence and sustained/24-hour operation remain separate operational requirements. Local TCP/RLPx, storage or worker tests do not complete those requirements.

## Other reference material

- [Performance analysis](performance-analysis.md): historical input/environment-specific analysis.
- [P07 reuse](p07-reuse.md): the background for the [receipts example](../../examples/receipts/README.md).
- [Frontend migration](../FRONTEND_MIGRATION.md): work for API consumers in a separate repository.
- [AA backend proposal](../AA_BACKEND_TASKS.md): a proposal rather than an approved implementation checklist.

Session handoff files are historical working notes, not public configuration or extension contracts. Use the task guides for a reproducible setup.

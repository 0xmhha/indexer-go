# E3 retention and offline collection

Base: E2 #215, main `ac6f0ac`. The user delegated the E1 RED/GREEN, documentation and CI-gated squash workflow. No operational GC is activated by this change.

## Contract

- Keep two EVM executions, the unmodified pinned StableNet client, one PostgreSQL pool4/part2, shared RPC5 physical HTTP attempts/s and uncached fresh immediately before public commit.
- Default retention is 86,400 acknowledged blocks, configurable upward and never below 129 (128-block rewind plus a parent). This is a block count, not a time/byte guarantee. Worker backlog is additional retained data; capacity pressure stops the owner rather than evicting pending work.
- Maintenance is explicitly offline. A v1 request binds chain, original anchor, exact durable source ACK/tail, node head/root and cutoff. A PostgreSQL permit durably pins the generation/public tip before any deletion, requires an E2-verified full baseline/digest and rejects rewind, missing baseline/parts or incomplete permanent data, and performs final uncached fresh before its commit. The fence has no TTL. Parts, publication, confirmation, rewind, API admission and application startup refuse it, including after process death.
- Spool reads materialize under the existing exclusive gate; collection shares that gate and the process lock. Only validated old-generation sealed results/ACK evidence at or below the approved cutoff can be removed. Pending results, protected rewind/window data, unknown/corrupt/temporary evidence and active transition/deletion journals are retained. Deletion intent and completion receipt bind the exact file set/digests. Interrupted work requires explicit same-permit recovery; normal Open must not finish GC.
- Node maintenance holds the actual chaindata/freezer locks, never constructs/starts a node. Keep genesis, every header/canonical mapping and live path state/code/preimages. Block bodies/receipts and their transaction lookup indices outside the window may be compacted only after the DB permit. Ancient table indices must remain aligned; never tail-truncate the chain freezer (the pinned client requires ancient genesis at index0). State-history pruning uses its explicit state IDs and block metadata, not `height == stateID`, and never drops a protected/current/persistent state dependency. Unsupported/corrupt layouts fail closed.
- All filesystem transitions are durable and fenced across restart. A completion artifact must prove the exact permit and successful postchecks before PostgreSQL releases its maintenance fence with a new fresh canonical check. Lost/uncertain responses are inspected/recovered by exact identity, never inferred from missing files. No automatic loop, arbitrary directory removal, lease TTL or latest-state fallback.
- Functional tests may delete their own temporary fixtures and use a disposable fsync-on PG. Live node/peer, formatting, production GC activation and performance measurements remain on hold. Other projects/resources remain untouched.

## Units

| Unit | Implementation and RED/GREEN acceptance |
| --- | --- |
| U01 | bounded versioned retention request/permit and PG durable maintenance fence; rollback/fresh/restore/rewind/read/part/publication gates |
| U02 | guarded spool quarantine collection, exact intent/receipt, cancellation/recovery and reader/close/generation exclusion |
| U03 | pinned node-store offline compaction, protected data/state and restart verification; fault boundaries and fail-closed unsupported layouts |
| U04 | explicit plan/authorize/apply/complete/inspect tools, quota halt/resume and cross-module integration; no runtime GC default |
| U05 | current guides/checklist/continuity, full required functional/static/race/PG/examples, single200 new concurrency cases, secrets, draft PR/CI4/squash/ff main |

The units share the same approval/receipt protocol and ship as one coherent PR. Update checklist and n/n-1/n-2 after each unit. Retain accepted E1/E2 designs; obsolete current status belongs in Git history. Actual pinned-client limitations found by RED tests must be reported explicitly, not hidden behind successful mocks.

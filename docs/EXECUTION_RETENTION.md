# Offline execution retention

E3 provides explicit maintenance for the StableNet execution producer and PostgreSQL archive. It does not schedule GC or start a node. Operational activation, disk provisioning, live peers and performance measurements remain on hold.

## What is retained

The default window is **86,400 acknowledged blocks**; `-keep-blocks` may increase it and must be at least 129. It is a block count, not a guaranteed number of hours or bytes. Collection ends inclusively at `durable source ACK - keep_blocks`. Unacknowledged backlog is additional retained data. A short reorg does not restore already expired history; if the proposed window moves behind a previous floor, maintenance fails until the ACK catches up or an explicit matched recovery is performed.

| Data | Maintenance behavior |
| --- | --- |
| PostgreSQL archive | No archive/history/feature/outbox rows are deleted. Require an E2-verified **full** baseline and bootstrap digest, exact canonical ACK, seven idle public cursors and complete canonical publication/part coverage. Partial/prepared-only baselines cannot authorize node GC. |
| Active result spool | Existing exact durable ACK cleanup remains unchanged. GC never removes pending active-generation results or advances ACK. |
| Quarantine | At most 128 validated expired old-generation result/ACK files per permit. Unknown/temporary/corrupt evidence remains; corruption fails collection. All retained evidence still counts against quota. |
| Node bodies/receipts | Retire only `original_anchor < height <= cutoff`. Data at/before a checkpoint anchor was not archived by E2 and stays. Headers, canonical mappings, genesis and total difficulties stay. |
| Native chain freezer | Rebuild aligned tables and extend positions through expired hot data. Preserve genesis at index 0; never tail-truncate the chain freezer. Expired payload positions are empty, and later native freezing can continue. Native `HasBody` may report an ancient position even when its retired payload is absent; use the indexer archive for historical queries. |
| Node state history | Use pinned v0 metadata and actual state IDs, not block heights. Keep the exact cutoff state, including an unchanged block's earlier state ID, current/persistent dependencies and every later history. Drop only older reverse histories/root lookups. |
| Current node state | Preserve live trie nodes, code and preimages. Current state, all headers/freezer indices and pre-anchor payloads can continue to grow. A window alone does not bound total disk bytes. |

The supported node layout is the dedicated native `<data_dir>/indexer-stablenet/chaindata` with internal `ancient/chain` and native path history. Bare, legacy, externally placed, symlinked, hardlinked, incomplete or unknown-version layouts fail closed. E2 read-only export remains separately usable for its supported inputs. A checkpoint without a complete compatible native header/history store is not a GC-ready node backup.

The native automatic state-history limit stays 0 (unlimited). Only offline ACK-fenced maintenance changes its floor. After a successful compaction, the assembly uses a one-block **auxiliary native transaction lookup** window to avoid background re-indexing of retired bodies. Replay uses block/hash/receipt paths; PostgreSQL remains the historical transaction API. No upstream client source is patched.

## Approval and recovery boundary

Stop the indexer (including API-only owners of that schema) and producer before planning. The maintenance coordinator alone opens one pool with maximum 4 connections; it creates no additional permanent pool. The source process lock and native DB/maintenance leases reject concurrent owners; no tool stops other projects for you.

1. Read existing stores into an exact request: chain/original anchor, source ACK/tail and metadata digest, node head/root, window and cutoff. This observation alone is no deletion authority.
2. PostgreSQL commits a random-token permit and **durable pending fence**, with a new uncached canonical read immediately before commit. The existing pool remains 4. Drain existing admission/read leases; then parts, publication, confirmation, rewind, API snapshots and app startup refuse maintenance until completion.
3. The DB coordinator checks the same currently pending permit under a live exclusive lease and launches the producer over a private inherited pipe with a new challenge. Saved permits/receipts cannot independently start a deletion. Parent EOF or cancellation stops work; there is no TTL that releases a pending fence.
4. The producer holds its source gate/process lock throughout native compaction and exact quarantine deletion. It persists intent before mutation and completion after file/directory sync and postchecks. Node/source fences remain pinned even after successful collection.
5. The coordinator verifies the supervised reply, commits the exact completion with another new fresh canonical read and post-response generation/public/baseline fence, then emits a release artifact. The producer verifies its own persisted receipt and that release before removing its local fences.

An uncertain response/commit, cancellation or crash means **inspect and retry the exact permit**. Do not infer completion from missing files, lower a generation, erase a fence, substitute latest state or start blind repair. A completed coordinator retry emits the same release without launching collection again. A different/new permit cannot take over unfinished work. After DB restore, a permit must still be the exact live pending row; otherwise collection is refused. Restore DB/node/spool as a matched set, or use the [E2 recovery procedure](EXECUTION_BOOTSTRAP.md#read-only-diagnosis-and-explicit-recovery).

## Copy space and capacity

Node maintenance creates and validates a full replacement before swapping directories. The original stays intact until the replacement is durable and reopened successfully. Rename/cleanup intents bind exact directory identities; explicit recovery handles each durable boundary. An incomplete owned candidate may be rebuilt; unknown/foreign data is preserved.

Require observed free space of at least **2 × logical chaindata bytes + retention_reserve_bytes** before copying. The default reserve is 1 GiB. This is a capacity check, not a filesystem reservation; concurrent writes can still exhaust space and leave the operation pinned for recovery. There is no in-place fallback. This deliberately conservative first implementation can require substantial copy time/space; no throughput result is claimed.

Optional executor settings, in addition to the [existing identity/path settings](EXECUTION_INGEST.md#executor-configuration-and-build):

```json
{
  "retention_reserve_bytes": 1073741824,
  "capacity": {
    "node_bytes": 274877906944,
    "stop_free_bytes": 128849018880,
    "resume_free_bytes": 150323855360,
    "interval_seconds": 30
  }
}
```

Receipt `Bytes` counts logical quarantine-file bytes, not physical disk reclaimed or node-copy traffic. The node budget/copy estimate uses logical file sizes; free-space checks observe the filesystem.

These are an example budget (256GiB, 120/140GiB free thresholds), not measured sizing or finalized disk allocation. Omission of `capacity` preserves the existing runtime setting. With it configured, take a new sample before node import starts, then at the configured interval (default 30 s, maximum 3,600 s). Unobservable storage, reaching the node budget or stop-free threshold stops the owned producer; it preserves raw data and starts no automatic restart/eviction. An explicit `resume-check` requires the higher free threshold, node usage below 90% and source bytes/records below 80% of their quota, and refuses generation/GC fences. It is a capacity observation, not startup or integrity authorization. Normal Hello/backlog/fresh gates still apply on the operator's next start.

A full spool remains bounded by both bytes and records, including quarantine and metadata. If no eligible data can be collected, increase verified capacity or perform matched checkpoint recovery. Do not delete pending input to make a quota error disappear. Existing application/PGDATA/mount identity/capacity procedures remain in the [operations runbook](OPERATIONS_RUNBOOK_DRAFT.md).

## Commands

Build both tools from the matching source revision. The executor is a separate Go module:

```sh
mkdir -p build
go build -o build/execution-retention ./x/stablenet/cmd/execution-retention
(cd x/stablenet/executor && go build -o ../../../build/stablenet-retention ./cmd/stablenet-retention)
```

Use a private `0700` artifact directory and new file names; output files are exclusive `0600` and synced. Supply the existing explicit executor config, schema and compiled producer paths. The following is a procedure template; it is **not operational activation approval**:

```sh
# Existing owners stopped; OPS is an existing private directory.
build/stablenet-retention -action plan -config "$EXECUTOR_CONFIG" \
  -keep-blocks 86400 -output "$OPS/request.json"

# DSN and HTTP(S) canonical endpoint are privately injected environment values:
# INDEXER_DB_POSTGRES_DSN and INDEXER_CHECKPOINT_RPC. Never put them in Git/flags.
build/execution-retention -action authorize -schema "$SCHEMA" \
  -request "$OPS/request.json" -output "$OPS/permit.json"

# Use absolute paths for producer/config. This coordinator launches only the
# maintenance binary; it strips DB/RPC credentials from the child's environment.
build/execution-retention -action collect -schema "$SCHEMA" \
  -permit "$OPS/permit.json" -producer "$PRODUCER_BINARY" \
  -producer-config "$EXECUTOR_CONFIG" -output "$OPS/release.json"

build/stablenet-retention -action release -config "$EXECUTOR_CONFIG" \
  -permit "$OPS/permit.json" -release "$OPS/release.json"
```

Do not invoke producer `apply` manually; it requires the coordinator's live FD3 supervisor. `inspect` is the default DB-tool action and reads no RPC. Producer `inspect` reads existing node/source maintenance observations. Neither initializes a store, recovers deletion or grants permission to start. Use `-action resume-check` with explicit capacity settings after resolving a quota/storage condition. If writing an output fails after a successful DB commit, retry with the same request/permit and a **new** output path; inspect the durable state rather than assuming rollback.

## Functional evidence and limits

[Plan](codemine/runs/20261024-e3-retention/plan.md), [checklist](codemine/runs/20261024-e3-retention/checklist.md), [continuity](codemine/runs/20261024-e3-retention/continuity.json) and [validation](codemine/runs/20261024-e3-retention/validation.md) track E3. Tests use owned temporary files, actual native storage/path history/freezers, a native `core.BlockChain` with a fake consensus engine, mock RPC and disposable fsync-on PostgreSQL. They cover real library reopen/import/short reorg/future freezing, state-ID recovery, crash boundaries, live-pipe cancellation and the shared 132-block producer/DB format.

This does not establish live WBFT peer/finality behavior, power-loss behavior on the chosen external device, production sizing, backup RPO/RTO or throughput. Existing two executions, writer7/pool4/part2, shared RPC5 physical HTTP attempts/s, parameter binding and final publication fresh stay in force. E4 feature writers, E5 other producers and E6 independent consensus/finality remain separate work.

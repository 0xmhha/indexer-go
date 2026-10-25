# Native replay call traces and internal calls

Use this path with `source.execution` and a matching Arc or StableNet producer. The producer observes the **existing second execution** of an imported block; there is no third EVM run, debug RPC request or upstream client patch. Header/receipt verification and the independently reconstructed state journal remain mandatory. The separate [RPC trace worker](CALL_TRACES.md) continues to report provider-supplied trees with its own provenance and completion cursor.

## What the records mean

The native replay includes transaction roots and observed `CALL`, `CALLCODE`, `DELEGATECALL`, `STATICCALL`, `CREATE`, `CREATE2` and `SELFDESTRUCT` frames. Each frame carries its exact transaction hash/index, parent ordinal, caller/target, optional uint256 value, gas, input/output and error. The consumer derives child-ordinal paths, `internal`, `ancestorReverted` and `reverted` without discarding attempted calls. A successful child inside a reverted parent remains queryable with both revert flags. A failed creation can have a null target; STATICCALL has a null value rather than an inferred zero transfer.

An **internal call** is any non-root frame, including failed, zero-value, static, delegate and creation attempts. It is not a separately signed transaction or evidence of a committed native transfer. Delegate/callcode values describe EVM context. SELFDESTRUCT does not independently prove account deletion or a native USDC transfer. The verified account/storage journal remains the balance/state authority; traces do not write a second balance projection or manufacture contract-creation state.

Arc uses its pinned official factory and logical `CallFrom` child identities. Transaction roots retain the original transaction identity and final execution output, including a directly rejected precompile call. A CallFrom completion rollback must not leave a successful child status: the adapter checks the resumed caller's final CALL outcome. System pre-block/finalization transitions, consensus operations, authorization processing and the private internals of native precompiles are not invented user call frames. Their state effects remain in the separate journal phases.

The response identifies `locally_replayed_evm_hooks_root_consistency`. Go verifies graph structure, profile, transaction roots and receipt consistency before writing. State/receipt roots do **not** independently commit to every child call; the audited producer and hook integration remain part of the trust boundary. This is not E6 consensus/finality verification, an opcode/memory/stack trace or an arbitrary provider's callTracer result. Raw revert output is retained; error strings are not a stable machine-readable EVM ABI.

## Storage and completion

EXR1 metadata adds optional `CallTraces` with `Version:1`, an exact profile label and a preorder `Frames` array. An absent field means **not collected**. A present empty array is valid only for a fully replayed transaction-empty block. Existing journal causes, frame transport and raw block/receipt encodings are unchanged. Old strict readers reject the extension: upgrade the indexer/schema before the producer. Old payloads remain readable and old publications retain `call_traces=false`; historical data is not silently backfilled or replaced by a new digest.

Migration0018 adds `exec_call_blocks` and `exec_call_frames` and an explicit collected marker on `exec_publications`. The existing **transactions writer** stores the exact payload-bound trace metadata and rows in its ordinary part transaction. There are still seven queues, one per-chain/process pool with max_conns4, at most two concurrent part transactions and the existing API reservation. No per-feature pool or eighth writer is introduced. PostgreSQL values are parameter-bound; filter SQL is fixed.

Durable parts remain invisible. Only final publication advances public height and can authorize an ACK. Before the NEW uncached fresh canonical read, publication and lost-ACK confirmation verify trace receipt identity, contiguous row count and bounded stored-row byte checksums. Missing or damaged collected data fails rather than becoming an empty result or a successful ACK. The post-response generation fence remains unchanged. Retry/restart reuse the original exact records; a retained canonical prefix keeps its original source generation.

Trace rows are permanent indexed data independent of the raw/native window. The execution coordinator removes abandoned heights, including unpublished trace rows, during rewind. Reader snapshots use the same public visible height and reject rewind/pending maintenance. A read-only API process can reopen the existing migrated schema without starting an app or invoking RPC. Pool4 is per chain/process, not a server-wide PostgreSQL limit.

## Query the database

These GET routes use the existing public query snapshot/reservation and return `Cache-Control: no-store`. In multi-chain mode prefix them with `/chains/{id}`.

```text
GET /execution/traces/blocks/{height}?limit=50
GET /execution/traces/transactions/{transactionHash}?internal=true&limit=50
```

Omit `internal` or use `false` to include roots. `internal=true` returns only non-root frames. `limit` is1..256, default50. A transaction page still returns the containing block identity and global frame ordinals; filtering never renumbers frames or paths.

Responses contain `epoch`, `ref`, exact payload `digest`, `profile`, `verification`, filtered `total`, `frames` and nullable `next`. `epoch` is the current public generation; `ref` is the original durable source reference (wire fields `Generation`, `Height`, `Hash`). They may differ for a surviving common prefix. Frame byte/value/gas fields use hexadecimal JSON encodings, with nullable target/value; the derived ordinal/path/flags are explicit.

`next` is the **first omitted global ordinal**, not an offset into a filtered list. Every continuation supplies both pins from the first response:

```text
GET /execution/traces/blocks/{height}?after=<next>&limit=50&epoch=<epoch>&hash=<ref.Hash>
GET /execution/traces/transactions/{transactionHash}?internal=true&after=<next>&limit=50&epoch=<epoch>&hash=<ref.Hash>
```

The client must retain its transaction/internal filter. Unknown/duplicate/malformed query fields, zero/incomplete pins and an unpinned continuation return400. A changed public generation/hash returns409, even if a reorg lowers the public tip below the requested height. Start that result again from its first page. Unknown, unpublished or legacy/not-collected data returns404. Collected empty data returns200 with an empty array. Disabled/uninitialized/rewinding/maintenance state returns503; storage corruption/I/O returns500 without private SQL/DSN/path details.

Pages authenticate only the requested rows after checking the bounded metadata/ordinal receipt; they do not load every trace body to return a small page. Publication/ACK checks authenticate the complete retained set. The APIs issue **zero RPCs**. The old GraphQL/JSON-RPC `InternalTransaction` model does not represent nullable targets/values, paths and this provenance, so these records are exposed through the richer native routes instead of lossy legacy rows. Existing APIs are unchanged.

## Resource and recovery boundaries

| Bound | Limit |
| --- | --- |
| Frames, including roots, per block | 4096 |
| Call path depth | 64 |
| Input/output, each frame | 64KiB each |
| Error, each frame | 4096bytes |
| Aggregate input/output/error | 4MiB |
| Whole EXR1 / metadata | existing64MiB /16MiB, plus existing JSON token limits |

Whichever bound is reached first rejects the **whole replay**. No truncated tree is marked collected and no block is silently skipped. Oversized blocks require an explicit format/limit design; missing parent state still requires matched recovery. Indexer/source/node checkpoint, ACK, lease and E3 retention authority are unchanged. Shared RPC5 physical attempts/s still covers every ordinary/retry/failover request and final fresh; recording adds no trace RPC budget.

Functional checks use actual pinned Arc/StableNet libraries, independently decoded frozen bytes, owned durable PostgreSQL, IPC and read-only APIs. They establish neither live peer/consensus behavior nor sustained throughput. Actual node operation, disk provisioning/formatting, operational GC and additional performance measurements remain held. See the maintained [plan/checklist](codemine/runs/20261025-e5-native-call-traces/plan.md) and [remaining work](analysis/open-items.md).

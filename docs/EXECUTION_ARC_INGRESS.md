# App-owned Arc native ingress

N3 adds the opt-in `arc.native` module to the Arc bundle. It borrows App's
prepared execution Store and canonical reader. It creates no SQL pool or RPC
client, starts no node and changes no upstream code. Normal processing still
uses two EVM passes: the native writer's first execution and the collector's
second replay. N4 implements the [managed replay/reorg bridge](EXECUTION_ARC_MANAGED.md);
permanent ingress bytes/authorized raw GC are N5; combined native/App/PostgreSQL acceptance is N6.
Until those are released, this is an implementation boundary, not an operating
configuration. APFS, official enode, authenticated checkpoint and private inputs
remain HOLD. No live execution or performance result is claimed.

## Configuration contract

`source.execution.ingress` is absent by default. Its module is registered only
by the Arc bundle; the generic engine has no chain-specific import or ID branch.
The existing execution configuration still requires PostgreSQL max_conns4,
part concurrency2, reserved API admission inside the same pool, seven logical
writers, shared RPC5 physical attempts/s, explicit original anchor and
`start_height = anchor_height + 1`. Legacy RPC/capture/execution paths remain
separate. Native ingress claims its own existing `window_storage` declaration.

The following is a field reference, **not a runnable operations configuration**:

```yaml
source:
  execution:
    # Existing socket, original anchor and execution queue settings also required.
    ingress:
      module: arc.native
      window: arc-raw
      initialize_volume: false
      create_window: false
      native:
        executable: /app/arc-native-import
        data_dir: /native/node
      collector:
        executable: /app/indexer-arc-executor
        spool_dir: /source/arc-spool
        max_bytes: 268435456
        max_records: 4096
      peer:
        network_id: 0      # HOLD: explicit verified network identity required
        fork_hash: HOLD   # HOLD: audited four-byte fork hash required
        fork_next: 0
        key_file: /run/private/peer-key
        peers: []         # HOLD: official EL enode required
        listen_addr: 127.0.0.1:0
      max_raw_blocks: 172800
      max_active_blocks: 8
      max_active_bytes: 268435456
      timeout: 1m
```

172800 retained blocks is a planning assumption of two blocks/s for24h, not a
measured Arc rate or a byte-capacity promise. The actual raw count includes
retained fork/partial datasets (three per complete block); disk quota, shared
volume budget and free-space floor are independent. Automatic GC remains off;
explicit [raw retention](EXECUTION_RAW_RETENTION.md) requires stopped owners,
exact source ACK and the existing live E3 permit.
Explicit initialization can initialize a new window on an already supplied
volume; it never provisions or formats a disk, adopts a legacy window or repairs
an incomplete owner. Current private configuration is unchanged.

Active count and bytes limit live capture/decoder/writer inputs independently
of the large disk backlog. Arc requires at least two active slots and bytes for
two reservations, each `8 * max_payload_bytes + 1MiB`; payloads are at most8MiB
each. This covers the declared encoded buffers/copies and bounded decoder item
limits, not total Go/VM/RSS/native-cache memory. Window read buffers have their
own existing reservation. A full disk/count quota stops admission and preserves
data; it never drops an unsaved block or fabricates completion.

## Preparation, capture and execution

Pure preflight validates settings and disjoint native/raw/socket paths before
resource startup, including separate writer/collector binaries and collector spool.
Preparation checks the private native layout, canonical N1
bootstrap receipt, immutable original anchor, pending/maintenance fences and
existing native/raw binding before API exposure. When N exceeds public P, the
original receipt at the current SQL P must still match its published hash/root
under the same shared native view. The SQL control is checked again afterward;
changed visibility or an active recovery refuses readiness. It starts no peer or child.
An absent native identity is allowed only for a clean explicit first window at
the SQL anchor. Partial identity/head/owner is HOLD, never regenerated.

App repeats recovery/checks before its listener. Only after publication of the
private receiver socket does it start the owned writer. The writer validates
actual native SDK identity/root and supplies AWH1. Only then can a new raw window
be bound to that real native instance. Its `owned-raw-v1` kind and bounded,
checksummed `OWNER.json` prevent legacy adoption. Control replacement uses exact
CAS, file/directory fsync and the existing volume quota. It is intake metadata,
not ACK/deletion authority; invalid control fences appends and leases.

The existing ETH/68 BatchStream supplies header/body/receipts. The Arc profile
verifies full dataset identity and commitments, height/parent continuity and
bounded cardinality. Full block RLP preserves the exact header and body fields,
including opaque header extensions. Capture fsyncs the datasets before updating
received height R. A coalescing one-element notification wakes the native worker;
the durable raw index is its queue. Capture can reach R3 while native execution
is blocked at N0, within the declared disk/active bounds.

The native worker selects the next exact raw hash after a fresh canonical read
through App's shared budget, verifies SQL generation/rewind fences after the
response, and sends one bounded AIN1 request. Its verified native parent/root,
request/import epoch and exact raw digest must match AWR1, identity/pins and
candidate state/receipt roots. The generation is checked again after completion.
Only then does native height N advance and may those raw references be served
to peers. R never advertises unexecuted blocks. One-at-a-time admission is within
the designed maximum8; no throughput guarantee follows from this choice.

Admission fresh is independent of E1's **NEW uncached canonical response →
post-response generation fence → COMMIT → exact ACK**. R/N, SQL public P,
collector seal S and source ACK A are distinct. The ingress owner never calls
the E1 publisher or manufactures ACKs. Native import/read epochs, raw epoch and
receiver generation are independent. [N4](EXECUTION_ARC_MANAGED.md) connects the
managed collector and existing E1 coordinator for reorg recovery; it adds no
generation authority, RPC client, SQL pool or writer queue.

## Restart and shutdown

On restart, the raw owner and exact last received bytes are checked before peer
startup. Native Ready alone does not skip queued work: a completed head above
the anchor must match its immutable native receipt, retained raw AIN1 digest
and state/receipt roots. Lost replies can therefore resume a proven completion;
partial or conflicting native stores remain HOLD. After reconciliation, capture
subscribes after recovered R while the worker drains existing N+1 concurrently.
It does not require N==R before resuming capture.

The owned child inherits only LANG/LC_ALL/TZ, no DSN, API key, HOME or loader
overrides. Metadata is at most16KiB and block/receipts each10MiB; response framing
checks length before allocation, magic/request ID, canonical JSON, exact fields
and all completion bindings. Stderr storage is capped at4KiB. Cancellation
interrupts child pipes, joins the process and request, then releases the native
maintenance lease. App cancels/joins listener and ingress before closing Store;
Stop is idempotent and releases prepared resources even if Run never starts.

An interrupted native multi-store mutation preserves its pending fence for
explicit recovery. Age, Hello, a raw height or a native receipt cannot clear it
or authorize deletion. Explicit [raw GC/recovery](EXECUTION_RAW_RETENTION.md)
is available; graceful drain and managed native maintenance wiring remain N5.
See the
[current plan](codemine/runs/20261030-arc-native-n5/plan.md),
[checklist](codemine/runs/20261030-arc-native-n5/checklist.md) and
[native writer](EXECUTION_ARC_NATIVE_WRITER.md).

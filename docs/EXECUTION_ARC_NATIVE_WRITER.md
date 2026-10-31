# Arc first-execution native writer

N2 adds `arc-native-import`, an explicit bounded stdin/stdout writer using the
pinned official Arc EVM and native-v2 storage libraries. It imports into an
[N1 verified bootstrap](EXECUTION_ARC_BOOTSTRAP.md), without NodeBuilder,
network/RPC/SQL clients, an upstream patch or a separate node service.
[App-owned ingress](EXECUTION_ARC_INGRESS.md) and the [N4 managed collector/reorg bridge](EXECUTION_ARC_MANAGED.md) are implemented; raw GC #252 and graceful drain #253 are released; App/E3 phase coordination and held deployment are implemented; [N6](EXECUTION_ARC_ACCEPTANCE.md) qualifies actual owned native/App/SQL and two normal execution entries.
Operational execution, APFS/enode/authenticated production inputs and performance
measurements remain **HOLD**.

## Ownership and readers

The writer accepts only the official pinned Arc testnet5042002/genesis. Synthetic
genesis injection and EVM database pause hooks exist only in `cfg(test)`. The
bootstrap receipt must be private, canonical and match network/pins, original
anchor and native state. Initial management writes a durable pending fence before
creating identity/head. Partial initialization is preserved, never regenerated
silently. The original bootstrap anchor is immutable; it does not track a later
retention cutoff or assign an E1 generation.

| Sibling of native directory `name` | Role |
| --- | --- |
| `.name.execution-retention-lock` | Existing shared run lease for writer lifetime and active read guards; E3 needs exclusive ownership |
| `.name.arc-native-import-writer` | Exclusive nonblocking sole-writer ownership for the writer lifetime |
| `.name.arc-native-import-turn` | Admission turnstile; readers cannot overtake a writer waiting for the view |
| `.name.arc-native-import-view` | Shared read snapshot / exclusive native publication |

Locks are same-UID regular single-link `0600` files opened without following a
final symlink. Native/parent control directories are private `0700`. Live MDBX
uses the pinned nonexclusive environment with durable synchronization; the old
exclusive maintenance opener is not reused for live import.

The legacy `execution-node-guard` also holds the sole-writer lock until its
child joins and refuses any first-import identity, orphan head/receipts/epoch,
bootstrap receipt or pending marker before starting a child. A malformed file,
directory or dangling identity link cannot turn managed storage into unmanaged
storage. This closes the interval before the first importer creates identity;
the shared run lease still permits existing unmanaged RO clients. It does not
authorize node execution or the remaining N5 managed maintenance path.

Execution holds a stable shared native view, releases it, then obtains exclusive
publication and rechecks parent/head/epoch before mutation. Queue waits, RPC,
SQL and stdio output waits hold no publication gate. An active Rust read guard
also retains the E3 run lease. A persistent owned reader must retain its run
lease for its entire native-provider lifetime and keep its view guard through
response verification or cancellation/join.

A managed identity blocks legacy unguarded replay and maintenance CLI access.
The N4 bridge supplies the explicit inherited read-view contract to the owned
collector; it never silently upgrades an unmanaged reader. Legacy managed
maintenance stays refused. The explicit [N5 inherited bridge](EXECUTION_MANAGED_MAINTENANCE.md)
connects run EX to COW; App/E3 phase coordination remains incomplete. Merely
stopping a writer is not a maintenance permit. Raw/native import epoch, native read epoch and
receiver-owned E1 generation are separate. N4 adds bounded receiver-authorized
epoch transitions and SDK unwind with immutable original-prefix receipts.

## Execution and durable completion

1. Decode bounded AIN1 and check exact chain/pins, epoch, next request/height,
   parent/hash/root, body/withdrawal/receipt commitments, bloom, type and gas.
2. Execute **once** with official ArcEvmConfig. Compare full native state root,
   actual receipts, gas/blob gas and requests with the input. No journal replay
   is performed to save this outcome. Existing collector replay remains the
   second normal EVM execution.
3. Under exclusive publication, recheck ownership/parent/completed head and
   durably write `.arc-native-import.pending` version2 with identity, previous
   head, exact metadata and original AIN1 SHA256 before native mutation.
4. Save the already computed SDK ExecutedBlock with Full mode. Pinned normal
   commit order is static files finalize/sync → synced RocksDB batches → durable
   MDBX. Read back header/body/receipts and root; input bytes must be preserved.
5. Durably publish the immutable receipt, atomically replace completed head and
   verify its matching receipt, then remove only the exact owned pending intent
   and sync the directory. Only then write the successful native response.

Control files are `.arc-native-import.identity.json`,
`.arc-native-import.head.json` and `.arc-native-import-receipts/EPOCH-HEIGHT.json`.
They use canonical JSON `{Data:<exact JSON>,Digest:<SHA256 of Data bytes>}` to
reject corruption, partial files, unknown/duplicate fields and changed tokens.
Head must match the immutable completed receipt and actual native head/root.
A checksum is integrity evidence, not external authentication or E1 authority.

Duplicate success requires the exact stored epoch/request/parent/input digest
and canonical native block identity. It performs no new EVM execution. A lost
reply after durable completion is retried with those same bytes. Changed bytes
under an old ID are rejected. Unknown/corrupt/pending state blocks admission;
there is no TTL clear, latest/zero fallback, automatic store repair or destructive
reset. A process killed before mutation can retry; an ambiguous partial commit
requires explicit inspection and a separately authenticated rebuild/new target.
Receipts and raw input are not pruned here.

## Bounded stdio

The command is an interface for the owning Go supervisor. This is not an
operational start recommendation:

```sh
cargo run --locked --manifest-path x/arc/executor/Cargo.toml --bin arc-native-import -- \
  --serve --datadir /ABSOLUTE/PRIVATE/verified-native
```

There is no RPC/DSN/peer command option. AIN1 input is the
[N0 contract](ARC_NATIVE_IMPORT_DESIGN.md#n0-구현과-검증-경계), including a16KiB
metadata cap and10MiB each for block/receipts. The writer reads one frame at a
time, processes in order and emits no text logs on stdout. Errors terminate the
session; original input retention belongs to the owner.

Every response has magic4 + request ID u64 big endian + JSON length u32 big
endian + bounded canonical PascalCase JSON (at most16KiB). `AWH1`, ID0, contains
Version/Identity/Head after verified startup. `AWR1`, the exact input RequestID,
contains the committed ImportReceipt. Ready/head/native receipt is **not** a
Hello-derived E1 ACK, finality, public completion or deletion permit. Output
failure leaves the completed receipt for an exact duplicate retry.

The N3 Go owner filters the child environment, bounds diagnostics and
cancels/joins its own child. It reuses App RPC5 and the same Store,
without another dialer/pool. N3 implements the [App ingress lifecycle](EXECUTION_ARC_INGRESS.md); N4 binds the managed collector.

## Evidence and limits

[Validation](codemine/runs/20261029-arc-native-n2/validation.md) records meaningful
RED→GREEN and owned-process tests. Real pinned SDK store boundaries are paused
using existing public tracing instrumentation, then the parent SIGKILLs only its
own child. EVM cancellation uses a transparent test-only database wrapper inside
the actual official executor. These are controlled process-kill tests, not power
loss, APFS unplug, real peer, throughput or operational restore evidence.

DB pool4/part2/API1/seven logical writers, shared5 physical HTTP attempts/s and
NEW uncached canonical response → post-response generation fence → COMMIT → exact
ACK remain unchanged. Native admission/completion never substitutes for final
E1 fresh. There is no new public API or SQL schema in N2.

The owned-process fairness test waits for an explicit post-execution rendezvous
before observing publication admission: the initial shared read admission uses
the same turnstile briefly. Child failures report the expected phase with a
bounded16KiB stdout/stderr capture after cancel/join; EOF alone does not diagnose
a storage failure. [CI correction](../verification/native-import-n2/ci-failure.json)
records the original failed run without treating it as release evidence.

# Checkpoint and GC inspection

`cmd/indexer-inspect` is a one-shot PostgreSQL and raw-window **metadata reader** for Linux/macOS. It reads existing state without starting the application, registering a feed, contacting RPC/peers, migrating a schema, opening a window writer or recovering a deletion. Disk formatting, provisioning, live execution and performance measurements remain on hold; the implementation is verified with an in-memory DB and temporary synthetic files.

Use it to record an observation before a later, explicitly authorized recovery. A successful observation is **not** permission to start, resume, clear a fence or delete files. `gcDecision` is always `not_authorized`.

## Inputs and future invocation

Build the separate command with the repository's pinned Go toolchain:

```bash
go build -o /tmp/indexer-inspect ./cmd/indexer-inspect
```

The following is a template for use after real inspection is authorized. The unresolved genesis/path values deliberately prevent deployment by copy/paste:

```bash
/tmp/indexer-inspect \
  --layout /path/to/window-layout.yaml \
  --window arc-testnet \
  --chain-id 5042002 \
  --genesis 0xREPLACE_WITH_VERIFIED_GENESIS_HASH \
  --schema exact_existing_chain_schema \
  --dsn-file /path/to/private/postgres-dsn
```

- `--layout` uses the standalone [window layout](WINDOW_STORAGE.md) with top-level `volumes`/`windows`. Copy the exact `window_storage` mapping from the completed private application configuration, without its wrapper. Do not pass the full application YAML or invent a new reservation/path. Required paths already exist; nothing is created.
- Paths must be visible in the same namespace as the stored binding. For Docker-written `/data/feed` bindings, inspect through that same path with existing read-only mounts; a host `/Volumes/...` alias is not an equivalent binding. The draft image recipe now includes `/app/indexer-inspect` for a separately authorized future invocation. No image was built or container started, and the regular Compose entrypoint still starts the indexer only. Do not edit DB bindings to make host paths pass.
- `--chain-id`/`--genesis` are explicit expected identities, never auto-detected from a node. `--schema` is the exact existing per-chain schema, not the multi-chain base schema. A missing/outdated schema fails; this command never migrates it.
- `--dsn-file` must be an owned regular file0600 with one hard link. Symlinks, public permissions, oversized files and embedded control bytes are refused. DSNs are not accepted inline or in application environment overrides; errors do not echo credentials or driver errors.
- The PostgreSQL adapter opens `ReadOnly: true`, no writable prefixes and at most **one short-lived management connection**. This consumes the existing management allowance; it does not change any chain's four-connection application pool. Run observations serially. RPC requests are zero; existing shared5 HTTP/s and pre-commit fresh checks are unchanged.
- `--timeout` defaults to5s, maximum30s, for the DB/metadata observation. Cancellation and query failures close the handle. Ordinary filesystem operations are bounded by file/count limits but cannot promise interruption of blocked kernel I/O.
- `--limit` selects1..100 feature records. Follow `projection.next` with `--after`. Each page is a separate observation; `unitsPageComplete` is true only for a complete, unpaginated enumeration and **does not mean the features have completed their work**.

This first command targets PostgreSQL **captured-feed/window** schemas such as Arc in the operations draft. A legacy RPC-only schema without capture state reports `record_missing`; it does not invent source/GC progress for the other three RPC chains. The reader library accepts storage ports, but no standalone Pebble opener is provided. It never uses ordinary Pebble/window Open as an inspection shortcut.

## Report and interpretation

| Output | Meaning |
| --- | --- |
| `checkpoint.state` | DB source generation, immutable plan/anchor, contiguous Through, finalized checkpoint/policy and raw consumers. Through equal to anchor means no captured prefix above bootstrap; it is not network latest. |
| `checkpoint.windowBinding` | Existing `app/feed-window/v1` volume/root/path/reservation. Null is unreported; the command does not initialize it. Supplied layout mismatches fail. |
| `projection` | Existing DB-only writer head, canonical match, feature definitions/gaps/continuous completion and configured independent worker state. Missing/stale/unreported values retain their meanings. Processing receipts are not raw ACKs. |
| `checkpoint.pendingRecord` / `pendingReferenceCount` | Exact pending DB token (in state), generation, raw StoreID/segment/reference digest and count. The report summarizes references rather than printing every raw location. |
| `checkpoint.rawACKs` | Raw-consumer ACKs for the captured Through block, or the first16 unique pending-deletion blocks. `ackScope` and `ackTruncated` describe this bounded scope. Missing ACKs are explicit; this is not a global consumer cursor. |
| `raw` | Logical volume/store identity, managed quota/store binding, durable `.gc` intents and the historical `GC.completed.json` receipt. Only metadata is read; raw payload/index/proof integrity is not verified. |
| `raw.pendingMatch` / `pendingFiles` | Exact receipt match and observed raw/idx/seal presence for the pending segment. Missing files alone give `no_matching_receipt`, never inferred completion. Even matching completion requires coordinated recovery; the tool cannot clear the DB fence. |
| `raw.leaseStatus` | Always `unknown`. A separate process cannot infer live in-memory leases or safe clock history. No writer lock is acquired or created. |
| `consistency` | `separate_snapshots_rechecked`: DB checkpoint/ACK and feature-page metadata are re-read; selected raw metadata is re-read and path identity checked. Detected changes fail rather than produce success. These are not a distributed atomic snapshot and do not exclude a change after observation. |
| `gcDecision` | Always `not_authorized`, including when no findings exist. |

The raw reader uses existing read-only file handles. It neither claims a quota nor creates a missing path/LOCK, seals a segment, truncates a tail, writes a marker, fsyncs, renames or unlinks. Metadata file and directory symlinks are refused. Logical volume/mount checks do not replace the Docker host preflight's physical APFS UUID checks or establish device durability.

## Exit status

| Code | Result |
| --- | --- |
| `0` | Bounded metadata observed with no listed findings. No operational or deletion approval. |
| `1` | Invalid input, unavailable/missing/corrupt/conflicting data, changed snapshot, cancellation/deadline or close/output failure. JSON contains a stable error code, not the underlying DSN/error string. |
| `2` | Metadata observed but attention is required: pending/orphaned deletion intent, missing/truncated ACK observations, unreported binding/projection scope or incomplete feature pagination. No repair was attempted. |

Retain each report with its observation time and exact code/config revision. Follow the [restart/GC runbook](OPERATIONS_RUNBOOK_DRAFT.md) for later recovery. A full payload/proof/lease GC preflight, capacity monitor, restart interlock, physical-device validation and actual database integration remain separate work.

## Remaining work and priority

The requested read-only metadata command is implemented and its offline functional checks are complete. No additional implementation is required to deliver that bounded scope.

1. **Before operational use: integration validation.** Verify read-only PostgreSQL access, no schema migration/writes, cancellation/connection return and Docker/APFS path/identity behavior. This is still deferred under the execution hold; mock success does not complete it.
2. **Optional deeper inspection.** Add bounded raw/index/source-byte and required-proof verification, complete ACK pagination and evidence from a running writer's read-only lease interface if a full GC-blocker assessment is needed. The current process cannot infer leases, independently prove finality or turn sampled ACKs into global completion.
3. **Optional additional backends/modes.** Add an independently safe Pebble reader and RPC-only schema diagnostics when those use cases are requested. The current command targets PostgreSQL captured-feed/window schemas.

Capacity monitoring and restart interlocks are the next separate operating features. Automatic repair, fence clearing, startup and deletion remain outside this diagnostic command.

## Offline functional checks

```bash
go test -race -count=1 -run '^(TestInspection|TestInspect|TestPrivateDSN)' \
  ./pkg/window ./pkg/opsinspect ./cmd/indexer-inspect
```

These checks use no PostgreSQL service, application, node, Docker daemon or public RPC. DB writes panic in the fake adapter. Synthetic files cover pending/partial/completed/missing deletion evidence, identity/digest conflicts, active-lease unknown status, quota/path/symlink refusal, source/page changes, missing/malformed ACKs, bounded observation/pagination, cancellation, connection close and secret redaction. File content/permissions/modification times are compared before and after observation. They do not establish live PostgreSQL/APFS behavior or a performance result.

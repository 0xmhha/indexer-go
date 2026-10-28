# Arc native checkpoints and retention

This guide covers the pinned Arc/Reth **v2** layout (`db`, `rocksdb`, `static_files`). Read the [Arc collector guide](EXECUTION_ARC.md), [bootstrap trust boundary](EXECUTION_BOOTSTRAP.md) and [E3 coordinator contract](EXECUTION_RETENTION.md) first. StableNet native commands/layouts do not apply to Arc.

The implementation is checked with owned native-library databases and synthetic blocks. Operator databases, actual node services/peers, external-disk formatting/provisioning, operational GC activation and additional performance measurements remain held. A successful functional fixture is not live-chain or capacity evidence.

## Build without starting services

```sh
cargo build --locked --bins --manifest-path x/arc/executor/Cargo.toml
go build -o /YOUR_BUILD/arc-execution-checkpoint ./x/arc/cmd/execution-checkpoint
go build -o /YOUR_BUILD/arc-execution-retention ./x/arc/cmd/execution-retention
go build -o /YOUR_BUILD/arc-native-retention ./x/arc/cmd/native-retention
go build -o /YOUR_BUILD/arc-execution-node-guard ./x/arc/cmd/execution-node-guard
```

The Rust maintenance binary is `x/arc/executor/target/debug/arc-native-maintenance`. It uses the same pinned official Arc and Reth sources as replay. It constructs storage providers, never a node, peer, downloader or RPC client. The guard is a separate explicit operator command; collection and maintenance never launch it.

## Full checkpoint export

For future operator use after the held native layout/backup checks:

```sh
/ABSOLUTE/arc-native-maintenance --chain testnet \
  --datadir /PRIVATE_MANAGED_PARENT/native --operation checkpoint \
  --height CHECKPOINT_HEIGHT --output /PRIVATE_CHECKPOINTS/full.ndjson
```

`--chain` is `testnet` or `mainnet`. Height is explicit, including zero. Native genesis, completed import height, exact historical state and canonical header must match. Generation in the exported baseline is1, as required by E2. The producer performs no RPC call and grants no import, deletion or publication authority.

Reth v2 current state uses hashed address/storage keys. Those hashes cannot recover the original keys. The exporter collects candidate raw keys from official genesis allocation and subsequent retained native changesets. With `--seed /PRIVATE_CHECKPOINTS/prior-full.ndjson`, it uses that canonical ancestor's full-stream keys and only later changesets. After managed retention it automatically uses the retained cutoff checkpoint when no explicit seed is given. Seed state values are ignored: the parser checks identity/header/full declaration/order/bounds, then the exporter reads actual native state and independently rebuilds every storage trie and the complete account trie. Only equality with the exact header root establishes full coverage. Partial, incomplete, missing-code or missing-history input never becomes a full baseline, zero-filled state or latest-state fallback.

Output uses the existing [NDJSON checkpoint format](../execwire/checkpoint/stream.go), in secure-key order with code on its account record. Private disk sort/deduplication replaces the former one-million-key catalog ceiling.

| Resource | Current bound |
| --- | --- |
| Sort chunk | 32,768 candidate records |
| Merge fan-in | 16 input streams, plus one output stream |
| Candidate inputs, including duplicates | 64 million per operation |
| Logical catalog and account-spool payload | 16 GiB combined |
| NDJSON stream | 64 million records and 64 GiB |
| Individual record / account code | 2 MiB / 1 MiB |

Every bound or I/O/capacity failure stops the operation; there is no unbounded-memory fallback. The complete output is staged in the chosen owned0700 directory as a0600 file, fsynced and published without overwrite only after root and current native canonicality checks. Account for temporary snapshot space in addition to native COW capacity. Failed export never publishes the requested output path; an interrupted process may leave private temporary evidence for diagnosis.

The sort workspace defaults to the output parent filesystem for standalone export. Set `--scratch-dir /PRIVATE_EXTERNAL/maintenance` to use a different existing owned0700 directory; native secondary files also use it, so it must be outside native storage. Supervised retention uses the Go owner’s private helper workspace beside the native directory, including cutoff verification. Keep scratch headroom on that filesystem in addition to the snapshot. Files are0600 under an owned0700 temporary directory; run identity/length/order/digest and strict EOF checks reject changed or incomplete files. Cleanup removes only the original workspace inode; a replaced directory is preserved. Intermediate catalogs are disposable and do not support restart/resume. A forced standalone exit can leave private evidence; the managed owner joins its child and removes its own workspace.

Seed parsing streams candidate keys. Export streams account/storage trie reconstruction and spools one account’s nonzero storage on disk until its root is known; the spool is checked by digest before publication. Empty/deleted-account candidates and zero slots are omitted only according to actual native state. No global account-root vector or per-account storage map is built. The bounds describe catalog buffers and open merge streams, not total process RSS: the official chain allocation, native provider/block changesets, code and wire-record allocations have their own costs. The format and importer trust boundary are unchanged.

Use `arc-execution-checkpoint` to independently verify against a separately trusted expected manifest before import. Import retains the shared pool4, RPC5 physical-attempt budget and final uncached fresh/generation gates. The cutoff snapshot stored by retention is a future key seed; it does not move the indexer's original anchor or authorize a new bootstrap by itself.

## Managed ownership

Native directories and their existing parent must be owned and0700 for maintenance. Use a dedicated canonical path; links, foreign owners, hard-linked files, reserved control names and mixed source/native trees are rejected. Collector/guard startup needs a writable owned parent for its persistent0600 lease file; this version does not relocate that control file for read-only mounts. Native DB reads still use read-only providers. Source spool and native data may be siblings, but neither may contain the other. Checkpoint export output must be outside native storage.

All native node owners must use `arc-execution-node-guard -executable /ABSOLUTE/OFFICIAL_ARC_NODE -datadir /PRIVATE_MANAGED_PARENT/native -chain arc-testnet -- OPTIONAL_NODE_FLAGS` when node execution is eventually authorized. The guard runs the **unmodified** official binary with the exact managed datadir/chain, holds a shared run lease until its child joins, and rejects path/config/chain overrides. Cancellation requests SIGTERM and allows30s to join before forced termination. Collector startup automatically takes the same shared lease and refuses a pending maintenance fence. Standalone replay/checkpoint commands also reject pending fences. Arbitrarily launched native clients or external tools writing native stores are outside this ownership contract.

The COW producer takes the exclusive run lease and a native MDBX exclusive read-only hold during source copy. This excludes normal native writers; the managed run lease excludes managed readers. Cooperative read-only handles can still open on some platforms and are outside this offline ownership contract. Native secondary logs live in a private separate workspace. The Go owner joins its helper before removing only its original scratch inode, including after forced exit; replaced/unknown directories are preserved. Pending control files are siblings of the native directory, so they survive replacement. There is no in-place fallback or automatic node restart. Before activation, verify that the upstream node's own pruning policy cannot remove protected/unacknowledged history; the guard does not rewrite that policy. The native directory must be a dedicated subdirectory inside the storage filesystem, not its mount point.

## E3 workflow

Copy [the configuration template](../configs/execution/arc-retention.json.example) into an owned private0600 file and fill every placeholder from the prepared chain/anchor. It contains paths and identity, never DSNs or RPC credentials. Keep original anchor generation/height/hash unchanged across restart; current ACK/generation belongs to the sealed source manifest.

The future operator sequence is:

1. Stop managed native owners and the source collector. Preserve matched DB/node/spool backups. `arc-native-retention -action plan -config /PRIVATE/arc.json -keep-blocks 86400 -output /PRIVATE/request.json` observes native head/root/original anchor and durable source ACK/tail. It authorizes no deletion.
2. `arc-execution-retention -action authorize -schema ARC_SCHEMA -request /PRIVATE/request.json -output /PRIVATE/permit.json` uses the existing private DSN/RPC environment injection and fresh DB canonical check. E3 requires a verified full baseline, complete seven-writer publication and safe ACK/window/reorg boundaries.
3. `arc-execution-retention -action collect -schema ARC_SCHEMA -permit /PRIVATE/permit.json -producer /ABSOLUTE/arc-native-retention -producer-config /PRIVATE/arc.json -output /PRIVATE/release.json` supplies the **live inherited DB supervisor**, not merely a cached permit. The producer's `apply` action refuses standalone use. Lease/cancellation loss stops only owned helpers and preserves pending evidence.
4. `arc-native-retention -action release -config /PRIVATE/arc.json -permit /PRIVATE/permit.json -release /PRIVATE/release.json` checks the exact durable source/node receipt and DB-issued release, then releases node followed by source fences. Only then may explicitly authorized managed owners restart and reconcile normal Hello/backlog/fresh gates.

`-action inspect` reads sealed node/source metadata without recovery, deletion, DB/RPC or service startup. It is diagnostic evidence, not ACK or start permission. DB-side `inspect` is the existing read-only E3 action. Keep private DSNs in `INDEXER_DB_POSTGRES_DSN` and RPC settings in the shared coordinator's environment as documented in E3; they are stripped before launching the producer/helper.

The retention boundary is `Through = ACK.Height - KeepBlocks`, with `KeepBlocks > 128`; default86400 is a **block count**, not a time/disk cap. At2 blocks/s it represents roughly12h;172800 represents24h under that assumption. No new chain-speed measurement was performed.

## What is reclaimed and what remains

The producer first writes and independently reconstructs a complete native cutoff checkpoint. Only then does it modify the sealed COW candidate. The original native directory stays intact until all native/source/DB verification and swap gates pass.

| Data | Retention boundary |
| --- | --- |
| Transaction, receipt and sender jars | Complete fixed ranges after the original anchor and at/below `Through` |
| Account/storage changeset jars | Complete fixed ranges after the anchor and **strictly below** `Through` |
| RocksDB account/storage history indices | Entries strictly below `Through`; suffix upper keys and final sentinels stay valid |
| Native account/storage history checkpoints | `Through - 1`, with `Before(Through)` |
| Adapter checkpoint/seed/replay state floor | `Through` |
| RocksDB `TransactionHashNumbers` | Only transaction ranges in the same complete expired transaction jars; delete before the payloads |
| Current hashed/plain state, code/preimages, headers, canonical number mappings, `TransactionBlocks`, `BlockBodyIndices` and trie indices | Retained |

Never delete the first/pre-anchor, boundary or highest jar. Retain the first transaction jar even when genesis is empty: Reth derives global history availability from it. History jars containing cutoff are also retained. Native format granularity can retain significantly more data than the requested window.

The one-block history guard is deliberate. In the pinned Reth build, basic state at height H uses changesets from H+1, but trie overlay/root calculations require H to be **above** the native prune checkpoint. Setting that checkpoint to `Through` rejected the protected cutoff root. Stop index/checkpoint pruning at `Through - 1`, keep the cutoff jar, and explicitly reject adapter requests below `Through`. Canonical header diagnostics can still read old headers. Some lower-level native basic reads may expose an extra older block; this is not an advertised replay/root boundary.

Index rows are streamed through pinned native codecs, with at most 2,000 block entries per shard and 64 row mutations per WAL-synced batch. Empty/oversized shards, bad finite bounds, overlapping ranges or missing sentinels abort the candidate. The native RocksDB flush/compaction API then reclaims eligible tombstones; it can physically rewrite other column families without logically deleting their data. This is not a measured space-saving or fixed-size guarantee. Latest state, headers, conservative jars and other indices can still grow. MDBX is compact-copied separately as described below; its configured file geometry may remain unchanged even when allocated disk blocks fall.

Postchecks require exact native history checkpoints, no index entries below the boundary, no further eligible jars, the full cutoff seed's digest/coverage and exact regenerated serialized values, canonical head/ACK/tail and roots at cutoff, its successor, ACK, tail and head. The cutoff seed remains usable after pruning and on the next retention pass. Unknown source/quarantine evidence stays protected and charged by the common spool; unknown native files are copied and preserved.

## History floor and compatibility

The private native floor metadata is version3 with `HistoryFloor = Through` and transaction-lookup postchecks. Version1 payload-only and version2 history floors remain readable and verifiable. Upgrade the Arc producer/collector and native binaries together before activating history retention. Older maintenance binaries reject the new metadata field/version; do not use an older reader to bypass the advertised boundary.

Checkpoint targets and seed starts must be at/above the maximum native history checkpoint plus one. Replay checks its **parent** against that boundary before reading/executing a block. Missing retained seeds, stale genesis/older seeds, insufficient raw history or an older restored source/DB never becomes latest/zero state or an automatic fast-forward. Use the retained full cutoff seed or a matching newer canonical full checkpoint. The original indexer anchor and source generation do not change, and earlier indexed data remains governed by the indexer's coverage/history contracts.

## Capacity and recovery

Reserve at least `2 × logical native bytes + ReserveBytes` free on the same filesystem (`ReserveBytes` defaults to1GiB), plus snapshot/catalog/normal-growth headroom. The logical catalog/account-spool bound is 16 GiB; filesystem overhead and native secondary scratch are additional. This is not a disk-space reservation; the staged output can separately reach 64 GiB. Check all relevant filesystems before activation. Directory entries are bounded; unknown/foreign/link layouts fail closed. Snapshot serialization can require more space than native state; ENOSPC leaves the original and pending candidate for diagnosis. This is functional admission, not a capacity benchmark.

The sealed journal binds exact permit and original/replacement directory device/inode identities. Phases are copy → swap → cleanup → done. Candidate files are newly copied, never hard-linked. Only a verified/fsynced candidate may replace the original; backup removal follows durable cleanup intent and native revalidation. The internal Rust prune operation accepts only the matching sealed COW candidate, never the operational native path.

After an index batch, jar deletion, checkpoint or compaction failure, keep the partially modified candidate fenced; never open it as the operational node. The ordinary copy-phase recovery rebuilds only that permit’s owned candidate from the preserved original. There is no independent per-shard resume authority.

After any failure, keep node/source/indexer pending fences and repeat supervised `collect` with the **same current DB permit**. Copy recovery removes only that permit's owned candidate; swap recovery handles either rename boundary; cleanup recovery removes only the recorded original backup inode. A different permit, replaced directory, corrupt/unknown candidate, missing history, stale native head, lost live lease or absent/mismatched release stops recovery. No normal startup performs GC or guessed repair. After DB completion, obtain/reuse the exact release and repeat producer `release`; completed DB collect does not launch deletion again.

The shared DB coordinator still uses four connections per chain/process and the shared RPC5 physical HTTP attempts/s limiter, including retries/failover. Final uncached fresh and post-response generation fences remain immediately before publication/completion. Native checks supplement these boundaries.

## Additional indices and physical MDBX reclaim

The source pins are official Arc `6e764023ee6515fe70573e123ed2db912a7207b4`
and Reth v2.2.0 `88505c7f`. Reth's
[`TransactionLookup`](https://github.com/paradigmxyz/reth/blob/88505c7f/crates/prune/prune/src/segments/user/transaction_lookup.rs)
uses RocksDB in storage v2, and
[`Bodies`](https://github.com/paradigmxyz/reth/blob/88505c7f/crates/prune/prune/src/segments/user/bodies.rs)
orders lookup pruning before deleting bodies. The adapter reads each selected
jar's native transaction range, reads payloads in batches of 64, verifies any
existing hash/number mapping, then deletes only those hash lookups. It retains
pre-anchor, first, boundary and highest jars and all their lookups. Every
surviving lookup must resolve to the exact native transaction hash. Its resume
checkpoint records the last block and transaction actually pruned, and the floor
binds that checkpoint so restart never resumes from genesis across deleted jars. It retains
`TransactionBlocks`: native ceiling seeks rely on its block-boundary keys.
No current state, bytecode, preimage, header or trie table is cleared.

After all writable native handles close, the sealed COW candidate is opened
exclusively using the pinned `reth-libmdbx` API. Its public native FFI performs
`mdbx_env_copy2fd(..., MDBX_CP_COMPACT)` into a new private file, omitting free
pages. The output is fsynced, the source environment closes, and only the
candidate's `db/mdbx.dat` is replaced atomically. The DB directory is fsynced;
native providers reopen and repeat full root/seed/lookup verification before
the existing inode-bound COW swap can proceed. A crash rebuilds that permit's
candidate from the preserved operational original. There is no operational
in-place fallback. The file geometry can retain a sparse minimum size; tests
check allocated disk blocks and reopen correctness, not a fixed DB-size promise.

## Matched backup and restore admission

`BackupOutput`, `IndexerReceipt` and `IndexerArtifact` in the private native
configuration opt into a matched backup during supervised collection. All three
are absolute paths. Use a new manifest path in a dedicated owned0700 directory,
separate from native/source inputs. `IndexerArtifact` is an **external** PostgreSQL
snapshot plus its required WAL recovery material, packed into one bounded private
file. `IndexerReceipt` is an externally authenticated private JSON receipt:

```json
{
  "Version": 1,
  "PermitDigest": "EXACT_RETENTION_PERMIT_DIGEST",
  "ArtifactDigest": "SHA256_OF_EXTERNAL_DATABASE_ARTIFACT",
  "PublicationDigest": "EXACT_VISIBLE_EXEC_PUBLICATIONS_DIGEST",
  "DatabaseIdentity": "DATABASE_NAME/SCHEMA_NAME",
  "SnapshotIdentity": "EXTERNALLY_AUTHENTICATED_SNAPSHOT_ID",
  "WALBoundary": "0/12345678"
}
```

The receipt's provenance is an external operational trust input, like the E2
anchor. The optional [offline backup adapter](EXECUTION_BACKUP_ADAPTER.md)
authenticates an externally signed receipt and exact packed bytes using a
separately pinned provider key/chain/DB scope; it can export this compatible
receipt. The receipt alone is not self-authenticating. Native retention does not
generate or replay a SQL dump/WAL, and the adapter does not verify its contents
or recovery success.
An operator must attest that the artifact restores the exact receipt publication
and contains all WAL required to that boundary. A hash of arbitrary SQL bytes is
insufficient operational evidence. Autonomous PostgreSQL snapshot/restore and
WAL archiving remain unimplemented and held.

Pass the same receipt to the DB coordinator using `-backup-receipt`. Under the
existing live E3 transaction, it compares current database/schema, canonical
publication digest, permit/full-baseline/generation/root and WAL boundary. The
inherited supervisor binds the digest of the exact decoded receipt validated in
that live DB transaction. It never rereads a pathname to select authority. A
backup request without nonzero validated authority fails closed; subsequent
child receipt reads must match that same digest, including after native work. The
supervisor binds the exact receipt digest; a producer cannot silently
substitute another receipt or omit the backup. The source gate and native
maintenance lease/library hold remain joined until both snapshots have been
copied. Source raw payloads are decoded and verified, not merely hashed. The
archive binds native chain/head/root/floor/checkpoint, exact original source
manifest/anchor/ACK/tail/generation/digest, native pending intent, and external
indexer publication identity through the same permit and component checksums.
Native roots and the indexer's exact visible block/root are independently read
through the native library before publication.

The manifest names a sibling `.components` directory containing native, source
and indexer copies. Every listed durable data file is owned/private, bounded, checksummed and
fsynced. The sole checksum exclusion is native `db/mdbx.lck`: the pinned MDBX API
recreates this coordination file and writes it even during read-only opens. It
still must be an owned private regular unlinked file of at most2MiB, matching
the pinned32000-reader bound. Data, source manifests, checkpoints, receipts and
pending intents have no such exclusion. Native exclusive opens and the E3
directory-inode/lease gates remain mandatory. A fully written temporary manifest is linked atomically without
replacement. No manifest is published for an incomplete copy. The intent records
permit, external receipt and component-directory inode. Same-permit recovery may
rebuild only that owned partial directory; foreign/replaced/corrupt or already
published evidence is preserved and rejected. A previously published exact
archive is idempotently verified. A supplied copy may have new filesystem inodes,
but must retain the manifest/component basenames and pass every checksum and
live DB/source/native identity check. The original component inode authorizes
only partial-publication cleanup, never destination restore activation. Pending E3/native/source intents remain until
the existing exact DB completion/release sequence finishes.

For **admission only**, set `RestoreManifest` to the existing matched manifest,
keep `IndexerReceipt` and `IndexerArtifact`, and run the DB coordinator with
`-action restore-admit -backup-receipt /PRIVATE/receipt.json` plus the existing
permit/producer/config/schema arguments. The external database must already be
restored and independently authenticated at the exact original database/schema,
publication and pending E3 permit; automatic database installation is absent.
The supervisor compares that live DB with the receipt, then the producer verifies
all supplied component checksums, native exact publication/root/full cutoff seed,
source completion and floor. A fresh uncached canonical read followed by the
post-response generation fence remains mandatory. Missing, corrupt, foreign,
mixed, stale and below-floor consumers fail closed. There is no latest-state
inference or silent fast-forward. Admission emits no release, activates no copied
directory, and starts no node. Applying restored components operationally remains
held; it requires separately validated source/DB installation and the existing
exact E3 completion/release authorities. Archived inode evidence must never be
reused as a destination swap authority. Both startup guards reject the embedded
`.execution-retention-pending` marker in a detached native snapshot; admission
does not clear it.

An archive's total copied component bytes are bounded at64GiB and the metadata
manifest at2MiB. Before copying, require its complete logical bytes plus1GiB free
on the archive filesystem, in addition to native COW/catalog and normal-growth
headroom. These are admission bounds, not reservations or measured capacity/RTO.
Interruptions preserve the source/native/indexer pending fences and owned partial
copy. Cancel/lease loss joins owned helpers before scratch cleanup.

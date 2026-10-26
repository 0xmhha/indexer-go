# E5 execution blob sidecars

Base main6c8dd2c (#223). This scope connects full EIP-4844 sidecars to E1, reusing existing evmblob KZG verification. E1/E2/E3/E4 and native-call contracts remain accepted. No third EVM or upstream edits. Full blobs and local verified retention do not establish global availability, beacon SSZ inclusion, PeerDAS/cell proofs or E6 finality. State inclusion proofs remain the next separate scope.

## Decisions

The pinned Arc pool disables blob admission; this is node policy, not a proof of every historical block. Preserve unsupported policy. Reject supplying positive blob references under that policy. An independently decoded no-reference block is distinct from unsupported, not-collected, missing or corrupt data. Legacy payloads remain readable with no implicit historical backfill.

Extend EXR1 with optional versioned/profile/provider-bound full-blob relay bytes. Reuse the existing canonical RLP envelope, exact block/chain identity, signed transaction reference order and KZG verification; enforce <=32 blobs and existing packet/metadata limits. Reject partial/reordered/extra/invalid proofs. Missing optional data is explicitly not collected; optional strict source.execution.require_blob_sidecars prevents positive-reference publication/ACK without full sidecars. Apply it before receiver confirmation as well as new submissions. Cancellation is observed before and between proofs.

Supply through an offline library/private-file composition command BEFORE export is admitted to a spool/ACK. Read retained inputs without rewriting, preserve generation, create a new immutable private output with fsync, no overwrite/delete/RPC. No invented live Arc/beacon endpoint. Actual peer/beacon/relay acquisition and remote-retention guarantees remain separately unverified and held.

Persist verified metadata/ordinal blobs in the existing transactions part with migration0019, parameter-bound values, no eighth writer. Publication and lost-ACK authenticate the complete retained sidecar set before final NEW uncached fresh and generation fence. Rewind removes abandoned published/unpublished data. Surviving prefix retains source generation/digest. Raw/node GC authority is unchanged; missing optional data is never reported fully available.

Bounded DB-only block/transaction pages use existing pool4/part2/API reservation, visible-height/rewind/maintenance gates, epoch+block pins and <=4 proofs/page. Verify requested proofs, reject corrupt data, distinguish legacy absence from present empty. Permanent sidecars survive raw-window deletion; no API/provider RPC. Full receipt checks remain before fresh, no SQL work is appended after it except existing fence/COMMIT.

## Units and release

- U01 RED→GREEN: actual type-3 replay requires EIP-4788 pre-block execution via the pinned library, independently reconstructed in a system journal without user call frames; preserve DAO/pre-fork rejection and cancellation. Optional bounded wire, execution KZG/profile/identity/reference validation, strict requirement and exact immutable pre-spool attachment.
- U02 RED→GREEN: same transactions part PG persistence, exact full receipt/fresh/ACK, missing/corruption/reorg/prefix/read-only cases.
- U03 RED→GREEN: DB-only pinned pages/private-file command/actual native blob fixture→seven writers→API/restart, cancellation/error/ownership/no-overwrite.
- U04: maintained guides/checklist/n/n-1/n-2; root/module build/fmt/vet/lint/functional/full make test-race, cache/examples/offline ops/owned fsync-on PG, new CPU1/race/single200, staged/history secret scans; commit/draft+attach/exact-head lint/test/postgres/examples/arc success, ready/squash/ff clean main, preserve source branch and clean only own caches.

Preserve per-chain/process DB4, part2, RPC5 physical HTTP/s including retries and final fresh, parameter binding, E3 exact ACK/lease/permit/retention fences, other projects. Actual nodes/peers/operator DB, disk formatting/provisioning, operational GC and extra performance measurements remain held. Unchanged Arc Rust source is not locally rebuilt; prior native evidence, unchanged-source review and required Arc CI remain authoritative. The changed StableNet library adapter requires all module build/vet/lint/full-race and new prelude cancellation single200 gates.

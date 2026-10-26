# Arc native proof and collector integration

Base main `dd41108` (#225). Preserve accepted E1/E2/E3/E4, execution proofs/blobs/native calls and two executions/upstream unchanged. Scope: opt-in selected standard-MPT Arc block-end proof/code from the retained native provider, exact replay/proof snapshot and canonical rejection, native EXR1 composition before Go spool admission, immutable source policy, existing collector/window/lease/restart/reorg/ACK/capacity boundaries. No RPC source or third EVM.

## Decisions

Use pinned native StateProofProvider.proof(TrieInput::default(), address, slots) and exact block-end history_by_block_hash in the same native read view as parent/block/receipts. Verify every native proof against the verified header root, full selected positions/code hash and existing bounds. Missing state/proof/code fails, never latest/zero fallback. Normalize proven empty-root representations for the existing Go codec without weakening nonempty-root paths. Append optional StateProofBundle (arc/arc-native) with exact old nil bytes preserved.

A new bounded opt-in replayproofs stdio operation carries explicit selection. Legacy operations remain unchanged. Go independently PrepareWithRequirements verifies exact chain/ref/profile/root/selection before any spool append; child errors/cancel/invalid replies stop/join and preserve originals. Existing owner lease spans replay/proof/cleanup; history floors protect parent and block end. No new native delete, FinishedHeight, node launch, endpoint or GC authority.

Pin an optional nonzero admission-policy digest in the source manifest on creation. Match it BEFORE transition/ACK recovery; reject enabling/disabling/changing the normalized scope on an existing spool, including a drained one. Legacy nil manifests stay byte-compatible, old readers fail closed on the new field. Same-policy explicit maintenance preserves the opaque binding; it never rebinds it or expands E3 permit authority. Source capacity/window includes enriched exact bytes, and rebind/lost-ACK preserves proof content/digests under existing rules.

Explicit private selection JSON loading/CLI configuration validates before IO/child/spool creation. Native one-file export and continuous collector use the same proof semantics. No automatic operator config changes.

## Units

- U01 RED→GREEN: native proof source/export/bounds/missing-code/actual MPT and retained-native provider evidence, optional wire parity and same-snapshot replay+proof service.
- U02 RED→GREEN: source admission pin before recovery, maintenance preservation, private selection loading/CLI, independent Go verification and child cancellation/join.
- U03 RED→GREEN: actual native fixtures across collector/spool/window/capacity/restart/reorg/ACK and fsync-on PostgreSQL/public reserved API/RO restart. Existing rows/legacy bytes preserved.
- U04: current guides/checklist/n/n-1/n-2/all remaining work; full local build/fmt/vet/lint/function/root+wire+StableNet race/PG/examples/cache/offline ops, changed Rust build/fmt/clippy/offline tests and cross-language fixtures, new single CPU1/race/count200, staged/history scans; draft PR+attach/exact-head CI5, ready/squash/main sync, branch preservation and own-cache cleanup.

Preserve chain/process pool4/part2/API reservation, seven writers, RPC5 physical attempts/s including retry/failover, final NEW uncached fresh/generation fence, parameter binding and E3 exact ACK/lease/permit/checkpoint/GC boundaries. Actual node/peer/operator DB, disk provisioning/formatting, operational GC and performance remain held. Only owned disposable native-library/PG/file/mock IPC functional resources are used; other projects/shared caches/Docker resources remain untouched.

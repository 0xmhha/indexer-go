# Arc history compaction validation

All required local checks are GREEN. The [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-history-compaction) resolves exact-head CI4+Arc and squash/main release status. Logs/build caches are owned by `/private/tmp/indexer-arc-history-20261025`; shared caches and other projects are preserved.

## Requirement evidence

- U01 RED: payload-only retention left native account/storage history checkpoints absent. GREEN: cutoff seed is durable before index/jar changes; typed native history indices are trimmed, native checkpoints are durable/monotone, and native flush/compaction is called in the COW candidate.
- Boundary experiment: setting history checkpoints at3 caused the native trie overlay to reject state3 with available range4..132. The pinned basic-state and trie-root boundaries differ. GREEN: checkpoints/index deletion stop at `Through - 1`, cutoff changeset jars stay retained and the adapter enforces floor `Through`. Native root validation was preserved; upstream was not modified.
- U02 RED: a parent below the root-safe floor was accepted before the explicit adapter guard. GREEN: checkpoint targets/seed starts and replay parents are rejected before pruned-state access. The actual replay CLI checks the boundary before block execution/output. Header diagnostics stay available.
- A synthetic owned native-v2 store contains account A creation/change/deletion/recreation and B creation/deletion, zero/nonzero slots, full native roots and genuine history indices/changesets. Cutoffs3→4, reopen, repeated maintenance, full cutoff byte equality, stale seed rejection and newer seed-based export agree on account/storage/root values. Eligible changeset jars are removed on the later cutoff; protected suffixes stay available. These are storage-library blocks, not consensus-valid peer transactions.
- U03 GREEN: multi-batch shard trimming, finite-upper/sentinel preservation, missing/overlapping/oversized shard rejection, v1 payload-floor compatibility with still-retained history jars, and v2 metadata/native checkpoint postconditions. Only the two history index tables are logically pruned; native compaction can physically rewrite other column families.

## Completed local checks

Native fmt/build/clippy warnings-as-errors and all-targets passed 26 library + 2 CLI tests, including the actual maintenance helper and actual replay adapter. Three regenerated Rust→Go fixtures are byte-identical. Root build/vet/lint/gofmt/cache/examples/offline operations/preflight/shell, separate wire/StableNet executor/AST module static, full root functional and complete root/wire/StableNet `make test-race` passed. Owned fsync-on PostgreSQL storage/app/API/StableNet bundle and its container/volume cleanup passed. No Go concurrency was introduced, so unchanged local single200 selectors are not restarted; existing CI selectors stay enabled.

The initial native build of new shard tests found a wrong test import path; it was corrected before the final complete native run. The two deliberate RED cases and the native boundary failure are distinct from build setup. No closed suite was restarted without a code change/failure.

## Release gates and limits

Current docs/format/relative links passed. Scan staged content before commit and feature history before push. Exact-head CI4+Arc is required before ready/squash/main sync; mutable release status is resolved through the source-branch PR. Required commands are in [the plan](plan.md). The source branch is retained.

No actual nodes/peers/operator DBs, disk provisioning/formatting, operational GC activation or performance measurements were run. Logical removal and a successful native compaction call do not establish physical savings, a fixed total native size, live peer/canonical consensus or external-disk durability. Accepted pool4/part2/API reservation, RPC5 physical attempts/s and final uncached fresh/generation checks are unchanged.

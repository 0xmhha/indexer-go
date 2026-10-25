# Arc history compaction checklist

- [x] U01 RED → GREEN cutoff history/index pruning and durable full seed
- [x] U02 RED → GREEN exact protected state, reopen/repeat, export/replay/seed floor and failure cases
- [x] U03 bounded/validated native indices, compatible metadata and current docs
- [x] U04 required local gates, final docs/static review and release preparation

Release authority: the [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-history-compaction). While open, release requires staged/history secret scans, draft attachment, exact-head lint/test/postgres/examples/arc success, ready/squash and clean main sync. MERGED with these checks completes this scope. Preserve the branch. [Plan and invariants](plan.md).

# Priority 1 checklist

- [x] Runtime RED: command lacked opt-in transport/head-window contract
- [x] GREEN: standard full-blob Engine transport, supported Cancun/dev contracts and exact proof core binding
- [x] Missing/pruned/proof/reference/fork/cancel/reorg/head-window rejection without mutable input
- [x] Immutable admission before spool recovery; restart, quota, Collector and exact ACK
- [x] Native replay -> real HTTP/JWT/private EXR1 -> fsync-on PG -> seven queues/reserved DB-only pages -> RO restart
- [x] Full required local gates and single count200 invocations
- [x] Semantic review; all feature behaviors reviewed against the immutable contract
- [x] Staged/history scans with exact reviewed false-positive identities; no real secret findings
- [x] Draft PR and task attachment
- [x] Exact-head lint/test/postgres/examples/arc success; ready/squash/clean main sync
- [x] Own cache cleanup after release

No later feature implementation before release. The [plan](plan.md), [validation](validation.md), [continuity](continuity.json) and live PR/check evidence are authoritative; unchecked items are not completion.

Release authority: [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-engine-blob-supply). Release checkboxes are not preclaimed before CI5/merge/sync; live PR and Git evidence establish completion.

Released through [PR #227](https://github.com/0xmhha/indexer-go/pull/227), exact head `f3b689d546b07209dedecc9e0f42a7a1a38c4cbc`, squash main `449c31750bb4efb48d12b9264c1c1dc3fcb273aa`. All five exact-head checks succeeded before ready/merge. Main was clean and synchronized before creating the next feature branch. [Synchronization evidence](../../../../verification/priority1-sync.json); owned go/lint caches and detached RED baseline removed, logs retained.

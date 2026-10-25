# Arc checkpoint catalog checklist

- [x] U01 RED → GREEN external catalog above one million keys; bounded private scratch/memory/fan-in and fault handling
- [x] U02 RED → GREEN streaming export/seed/trie reconstruction and retention verification
- [x] U03 native/CLI/Go interoperability, cleanup/failure coverage and current docs
- [x] U04 required local checks, final static/docs review and release preparation

Release authority: the [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-checkpoint-catalog). While open, require explicit staged/history secret scans, draft attachment, exact-head lint/test/postgres/examples/arc success, ready, squash and clean local main synchronized to origin/main. MERGED with these checks completes this scope. Preserve the source branch. Keep accepted prior plans and current n/n-1/n-2. Operational holds and resource/canonicality invariants are in [the plan](plan.md).

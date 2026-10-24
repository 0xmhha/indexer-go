# E5 checklist

- [x] U00 latest main/rules inspected; separate `refactor/e5-arc-execution` branch and plan
- [x] U01 pre-block journal cause RED/GREEN (wire/race; fsync-on PG rejects old check then accepts additive migration)
- [x] U02 official Arc replay/root completeness RED/GREEN; native empty/transfer/storage/revert/creation/code/destruction, independent omission/before rejection and private durable output
- [x] U03 one-file transport/publication/capability RED/GREEN; canonical Rust→Go fixtures, actual fsync-on PG/API and fresh failure/retry
- [x] U04 full local validation, native reproducibility, new concurrency200, docs/secrets; release through exact-head CI4 plus Arc/squash/main

Release authority: the [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-execution). Earlier phases are merged; do not repeat their closed checks without an affected contract.

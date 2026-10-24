# E3 checklist

- [x] U00 inspect E2 #215/main, rules and pinned client retention APIs; create feature branch and accepted implementation contract
- [x] U01 shared protocol and durable DB fence RED/GREEN
- [x] U02 spool collection/recovery RED/GREEN
- [x] U03 offline node-store retention RED/GREEN
- [x] U04 operational entry points and integration RED/GREEN
- [x] U05 full functional/build/fmt/vet/lint/race/cache/PG/examples and all new GOMAXPROCS=1/race/single count=200 checks
- [x] Staged/history secret scan and current documentation ready for compliant commit/PR

Release authority: [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe3-retention-gc). Require latest-head lint/test/postgres/examples, ready, squash and main sync. If that PR is merged, E3 is complete; do not repeat closed local checks. A source commit cannot certify its own future CI/merge.

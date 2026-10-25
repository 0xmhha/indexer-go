# E5 native call traces checklist

- [x] U01 bounded wire graph and consumer validation RED→GREEN
- [x] U02 native Arc/StableNet same-replay observers and actual fixtures RED→GREEN
- [x] U03 PostgreSQL visibility/recovery and API consumption RED→GREEN
- [x] U04 current documentation, required local gates and release preparation

The [plan](plan.md) is the contract; [continuity](continuity.json) retains n/n-1/n-2. Release authority: [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-native-call-traces). While open, staged/history scans, exact-head CI4+Arc, ready/squash and clean main sync are required. MERGED with these checks completes this scope. Preserve source branch `refactor/e5-native-call-traces`.

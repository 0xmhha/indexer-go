# Arc collector checklist

- [x] U00 cleanup completed task-owned build data; inspect main/rules and create `refactor/e5-arc-collector`
- [x] U01 shared durable spool/exporter RED/GREEN and StableNet compatibility
- [x] U02 bounded Arc native read-only service RED/GREEN
- [x] U03 owned Go backend/continuous collector/restart/reorg/ACK integration RED/GREEN
- [x] U04 local matrix, final affected/module checks, full serial PG, native reproducibility, new concurrency200, docs/secrets

Release gate: the source-branch PR must pass exact-head CI4+Arc, become ready and squash merge; synchronize main to its merged commit. The PR state supplies current release status, so this file does not predict a future merge.

The [plan](plan.md) is the scope. Release authority is the [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-collector).

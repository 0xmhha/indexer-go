# Arc checkpoint and retention checklist

- [x] U01 RED → GREEN native full export, key coverage/root checks, private publication and Go verifier interoperability
- [x] U02 RED → GREEN native retention + shared E3 permit/ACK/window/supervision/release
- [x] U03 RED → GREEN managed ownership, crash recovery, native functional fixtures and current docs
- [x] U04 required local checks, new concurrency single200, secret scan
Release authority: the [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-checkpoint-retention). While open, require draft attachment → exact-head lint/test/postgres/examples/arc success → ready → squash → clean local main synchronized to origin/main. If MERGED with these checks, this release scope is complete. The source branch is preserved; mutable GitHub state is not duplicated as a stale local checkbox.

Actual nodes/peers/operator DB maintenance, disk formatting/provisioning and additional performance measurements are held.

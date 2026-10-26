# E6 validation

Priority 3 E6 is released as [PR #229](https://github.com/0xmhha/indexer-go/pull/229).
Exact head `f89109a492fc9991fd8ec0f636d78a937a22ba97` passed all five CI jobs in
[run 40377905374](https://github.com/0xmhha/indexer-go/actions/runs/40377905374). Protected ready/squash produced
`f7c0fa26f6e2b62fa2dc50820369ea9a3b9a5818`. Primary main/origin-main are clean and equal to that
SHA; the one-parent squash tree equals the reviewed head tree.

The official pinned native library generated and cross-validated the development
certificates; its normal test regenerates and compares the frozen file. Root
`TestSeedPriority3E6CryptographicConsensus` executes actual BLS success and
signature/bitmap/quorum/domain/fork/trust/transition/ancestry/restore failures.
`TestIndependentTrustAnchorCommitteeConsistency` rejects otherwise valid keys in
the wrong authenticated anchor order before admission.

Actual fsync-on PG tests exercise Prepare/StoreBackend, all seven parts, atomic
publication/finality, failed-fresh retention, generation/cancellation rollback,
restart/exact ACK, corrupt/missing policy state, finalized rewind rejection and
same-tip pending-rewind recovery. The actual NewApp opt-in IPC test covers
publication, reconnect without repeated outbox, restart through a pending rewind,
ACK recovery and configuration downgrade rejection.

Runtime RED logs are retained for corrupt state, missing policy, same-tip recovery
and anchor committee mismatch. Each was followed by implementation and GREEN.
The full PG suite exposed the old v16 reconstruction helper retaining E6 schema;
its fixture reconstruction was updated, both upgrade tests passed, and the whole
suite passed. An earlier managed-source reorg timeout was separately reproduced
without code/timeout weakening; that isolated test and the final full suite passed.

## Completed local gates

- Root build, gofmt, vet and lint; nested execwire/StableNet/astgraph build/static checks.
- `make test-race`: full root + execwire + StableNet executor, not a selected subset.
- `make test-postgres`: full fsync-on storage + app + API + StableNet bundle.
- One complete `GOMAXPROCS=1 ./scripts/test-postgres.sh ./pkg/app ./pkg/storage/postgres -race -count=200 -timeout=40m -run '^TestSeedPriority3E6'`.
  App PASS 828.217s and storage PASS 332.443s; command exited 0 after owned cleanup.
  Interrupted earlier attempts are not summed or used as completion evidence.
- Examples, Pebble cache lifecycle and offline operations (40 Python tests and preflight/shell checks).
- Staged and whole-history secret scans, with only reviewed exact historical test/public-ID fingerprints excluded.

Evidence is retained under `/private/tmp/indexer-four.e6.z7M46d/`, including
`validated-race.log`, `validated-postgres-upgrade.log`,
`validated-concurrency200.log` and the RED/GREEN logs. See
[local receipt](../../../../verification/priority3-local-evidence.json).

Developer keys and offline library tests do not establish live operator-anchor
validation. Production requires externally authenticated trust. Priority 4 remains
for the parent after E6 release; actual nodes/peers/operator DBs, operational GC,
deployment and extra performance work remain held.

## Released stage receipt

- PR head: `f89109a492fc9991fd8ec0f636d78a937a22ba97`; reviewed tree: `33dc1174625932c7cef7280dce222a81358861ad`.
- Squash/main: `f7c0fa26f6e2b62fa2dc50820369ea9a3b9a5818`; sole parent: `ba285da31bfe105813e81ebbbb510e3ac2921f97`.
- CI5: lint/test/postgres/examples/arc all success on that exact head.
- [Sync receipt](../../../../verification/priority3-sync.json); source branch preserved.
- Pinned native module cache passed `go -C x/stablenet/executor mod verify`.
- Owned PG containers/volumes and interrupted-run temporary IPC/build fixtures were cleaned; shared caches were not removed. Logs/audit evidence remain in the execution-owned directory.
- Post-release completion docs/ledger/sync are handed off as a patch plus sync JSON, preserving the reviewed source head and clean primary main. Priority 4 remains with the parent.

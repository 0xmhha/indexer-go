# E6 validation

Priority 3 code and local verification are complete. Release still requires the
exact current PR head to pass CI5, protected squash, and clean main sync.

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

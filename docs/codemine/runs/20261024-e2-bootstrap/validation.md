# E2 verification contract and evidence

Base: `3ae27d5` (E1 #214). The feature tests below were added before their core implementation and failed for missing behavior, then passed after implementation. Source/build/environment failures are not counted as RED.

| Unit | Requirement-failing RED | GREEN / regression |
| --- | --- | --- |
| U01 | `TestCheckpointFullRootsAndExtra`: valid complete root fixture rejected with `execwire: incomplete result` by the initial unsupported verifier | Same test, root/slot/code/Extra/identity tampering, omission/end/trailing/cancel, partial inclusion/absence and stream limits pass |
| U02 | `TestBootstrapExecutionAtomicCoverage`: full and partial imports rejected with missing-baseline coverage error by the initial unsupported store entry | Same test; atomic rollback on fresh/identity/truncation/cancel, full-zero vs partial-unknown, full official state/code/slots, missing-zero-slot write/rewind, nonempty schema refusal and v14 upgrade preserving baseline pass |
| U03 | `TestExportFullActualStateWithExtraStorageAndCode`: actual committed state rejected with `execwire: incomplete result` by initial unsupported exporter | Full actual trie export, official WBFT full/partial frozen fixtures, read-only prepared path DB, no node/spool creation on genesis export, source metadata preservation and operator-tool/DB/publication/API integration pass |
| U03 follow-up | `TestExportPartialAbsentSlotsAreSecureSorted`: absent slots emitted in input order, violating secure-key order | Normalize selections for both existing and absent accounts; reject duplicates; same requirement now passes |

The initial full root race suite failed its architecture guard because the new contract subpackage imported its own wire module. The recursive guard now allows only the exact wire module namespace, with negative cases retaining rejection of storage/node/geth implementations. The focused boundary tests pass; the required full root/wire/executor race rerun passed. No data-race report was emitted.

Integration also found an actual report-encoding failure (`model.Extensions` is internal, not JSON) and unavailable preimage reads in the read-only adapter. Exclude the internal decoded block from JSON reports and enable read-only preimage lookup; the same CLI/PG and existing-path tests pass. A wrong test-helper name and empty-root state-scheme setup were test/environment corrections, not claimed feature REDs.

## Reproducible functional checks

From repository root, using Go 1.26.8:

```sh
go test -race -count=1 ./pkg/execution/checkpoint ./internal/executionops
(cd execwire && go test -race -count=1 ./...)
(cd x/stablenet/executor && go test -race -count=1 ./...)
./scripts/test-postgres.sh ./pkg/storage/postgres -race -count=1 -run '^TestBootstrap'
./scripts/test-postgres.sh ./pkg/app -race -count=1 -run '^TestCheckpointBootstrapToolThenExecutionPublication$'
```

No new goroutines, scheduler/connection-admission implementation or concurrency-specific tests were added by E2. Existing E1 count200/race/GOMAXPROCS1 CI gates remain unchanged. Full local functional, root/wire/executor race, PostgreSQL, build, vet, fmt, lint, cache lifecycle, example, offline deployment and staged/history secret checks passed. The checklist links the authoritative latest-head CI/merge record. Test-only PATH omits Anvil so optional live-node tests skip; no live RPC, workload/SLO or performance opt-ins are set. PostgreSQL uses only its own disposable fsync-on container/volume and cleans those up.

## Cross-module fixture identity

These are **test fixture** counts/digests, not production checkpoint identities or performance measurements. Normal executor tests regenerate and byte-compare each file from actual official WBFT state; root tests independently verify roots/proofs and import those bytes. Export does not change durable node KV state.

| File under testdata/execution/checkpoints | Accounts | Slots | SHA-256 |
| --- | ---: | ---: | --- |
| wbft-genesis.ndjson | 8 | 65 | `4e465c62923c641f09ed12d13256fc72f294ac2a91ce51dd266d03277cb3d178` |
| wbft-checkpoint.ndjson | 9 | 65 | `e4df98eeba39861b5aa524d3464baf3a7ae78cc488cfa1b79dc2528a9ce35d2a` |
| wbft-partial.ndjson | 2 | 4 | `dabe83f8ae02b194eb75967321cbbda21da5d5d7dc780f7e4eab8b9eb4bd233d` |

Actual node startup, peers, formatting, GC and performance remain on hold. A full indexer-state checkpoint does not restore node DB/consensus history or supply prior archive bodies/features. See [operator boundaries](../../../EXECUTION_BOOTSTRAP.md).

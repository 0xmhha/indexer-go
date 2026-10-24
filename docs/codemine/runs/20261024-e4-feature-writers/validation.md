# E4 validation

Functional evidence only. Record requirement-failing RED and the same GREEN per unit. No live nodes/peers, operational GC or performance measurements. Local logs are supporting evidence; test names and reproducible commands remain in this file.

## U01

`go test -race ./pkg/feature -count=1`: GREEN. Before implementation the three new TestExecutionWriter* tests failed valid plan construction with ErrInvalidData. They check grouped hooks/checkpoint order, byte bound, fork mutation, cancellation, unit failure and simultaneous independent block plans. Evidence: E4-u01/red.log and green.log.

## U02

`python3 /private/tmp/indexer-e1-20261024/run-pg-target.py E4-u02 green '^TestExecutionBalance' -race -count=1 -v`: actual fsync-on disposable PostgreSQL GREEN. RED first reproduced stale balances=123 overriding a uint256 journal value and absent exact stored-input validation. Baseline/nonzero/Finalize history, public-height filtering, pagination, partial coverage, read-only store and preservation of existing projection rows pass. An initial unused test import failed compilation and was corrected before recording the requirement-failing RED; it is not counted as RED.

## U03

`TestExecutionUnitCheckpointsAreOrderedAtomicAndRetryable` RED on the E3 fetch path: second unit observed NotFound instead of the first unit's checkpoint. A sandbox socket denial was rerun with the required local HTTP socket permission before recording this requirement-failing RED. GREEN with `go test -race ./pkg/fetch ./pkg/feature ./internal/features/balance -run 'TestExecution|TestNative' -count=1`. Unit failure rolls data/checkpoints back before fresh; retry and lost ACK preserve exactly-once effects.

Reproducible U02 command: `scripts/test-postgres.sh ./pkg/storage/postgres -race -count=1 -run '^TestExecutionBalance' -v`.

Actual fsync-on PG: `scripts/test-postgres.sh ./pkg/app -race -count=1 -run '^(TestExecutionFrozenEVMPostgresPublicationAndAPIParity|TestExecutionAppFrozenWBFTIPCReorgRestartAndLostACK|TestExecutionFeatureResumeRejectsMissingOrChangedDefinition)$' -v` passed. All four frozen scenarios retain API parity and now assert zero duplicate balances/balance_history rows. Reorg/restart/lost ACK and feature-definition admission pass. No node service is started.

`GOMAXPROCS=1 go test -race -count=200 -timeout=5m -run '^TestExecutionWriter' ./pkg/feature` passed as one invocation (not a sum). CI adds the independent-plan case without removing existing concurrency gates. The additional nonce-only/history cursor/generation/cancel PG edge cases pass under race.

## U04 final local checks

- Full root functional `go test -count=1 -timeout=30m ./...`: passed (551.0s).
- `make test-race`: full root/wire/executor passed (753.6s), using the pinned Go, executor module cache and bounded test parallelism.
- `make test-postgres`: full fsync-on disposable PG adapter/app/API/chain suite passed (540.3s); its owned container/volume were cleaned. Targeted new PG tests also passed with race.
- All15 static/build/vet/lint/cache/examples/offline operations checks passed. All1273 Go files are gofmt-clean. Current Markdown links exist and continuity JSON is valid.
- New ownership/concurrency tests passed one GOMAXPROCS1/race/count200 invocation. Existing CI concurrency gates remain and the new independent-plan case is added.
- betterleaks1.9.0 full-history scan found no leaks; staged scanning is a mandatory commit gate.

Functional validation used mock HTTP/IPC, frozen EVM/WBFT inputs and owned temporary databases. Live Anvil/RPC/SLO opt-ins were disabled. No live node/peer, provisioning, operational GC or performance evidence is claimed. Test duration is validation cost, not throughput.

Release authority is the source-branch PR linked by the checklist: require latest-head lint/test/postgres/examples, ready, squash and clean ff main. A pre-merge source record cannot certify future CI/merge; if that PR is merged, E4 is complete and closed checks must not repeat.

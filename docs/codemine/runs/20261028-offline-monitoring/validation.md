# Offline Native monitoring validation

Behavioral RED before implementation: `TestKnownCriticalSurvivesMissingStaleAndUnknownInputs`
failed because a known capacity stop returned HOLD/unknown. Subsequent RED
captured unknown-generation heights being treated as measured backlog and missing
original-anchor boundary diagnosis. GREEN covers these contracts without changing
operational E1–E6 implementation or configuration.

Functional cases cover exact zero/null distinction, inclusive free/capacity/gap/
lag boundaries, uint64-safe ratios, independent timestamps and stale/future inputs,
identity/generation/fence semantics, missing policy, strict bounds/schema, private
files/links, immutable inputs, CLI flags/cancellation/short output/redaction and
unknown drafts. Actual Arc/StableNet diagnostic producers read owned fake spool/
export metadata, and their entire JSON is round-tripped through the independent
consumer DTO. Files, bytes and inodes are preserved; no chain, operating DB/peer,
public RPC, native service, watcher or notification is run.

Final frozen-source contributor gates all exited0: functional product/consumer
contracts, pure-binary/runtime boundaries, tracked/new Go formatting, root/wire/
StableNet executor/astgraph build/vet/lint, full `make test-race` (root/wire/
executor) once, cache lifecycle, examples, release-sequence and current doc links.
[Source identity](../../../../verification/offline-monitoring/source.json) and
[exact commands/log hashes](../../../../verification/offline-monitoring/checks.json)
record the same unchanged8 Go files and2 input drafts throughout.

The actual built CLI separately passed no-input/help/argument redaction, private
draft HOLD and broken-output-pipe exit checks; its owned fixture was removed.
No new concurrent state or concurrency test was added, so no new local200-repeat
campaign was required. No local PostgreSQL rerun or Rust rebuild was needed for
unchanged SQL/storage/native implementation; actual final-head CI5 remains
mandatory. [Secret scans](../../../../verification/offline-monitoring/leak-scan.json)
distinguish staged findings from reviewed public history fixtures. No SQL/storage/operational source,
pool or RPC implementation is modified. No local operational PostgreSQL, Rust
node build/run, Docker execution, format, GC/restore activation or performance
test is part of this feature. Actual final-head CI postgres/arc remain mandatory
before merge; their temporary functional fixtures are not operational evidence.

The original [CI297](https://github.com/0xmhha/indexer-go/actions/runs/40691037595)
at `2f635a5e77c43cc4a7bf315fa590ae00528d2a00` passed lint/test/examples/arc,
but PostgreSQL exhausted the20-minute **package** timer for four state-proof
cases each repeated200 times. Its active test had run for1 second, waiting in
the5/s limiter during `ConfirmExecution`'s fresh read. No assertion or race
failure was reported. The prior successful identical group took1189.933 seconds
of1200; this is existing-log evidence, not a new performance measurement. The
subsequent DB-admission step was skipped and is not reported as passed.

The CI-only correction gives each existing case a separate sequential package
invocation with unchanged CPU1/race/count200/20m conditions. It neither removes
coverage nor changes the RPC cap, pool, fresh queries, individual-test deadlines,
assertions, PostgreSQL fixture ownership/cleanup or fsync. The offline coverage
contract first failed with1 invocation instead of4, then passed with the pinned
PyYAML6.0.3. An exact workflow comparison allows only the split and its guard's
CI invocation; all other jobs/steps and runtime code remain unchanged.

[Failure evidence](../../../../verification/offline-monitoring/ci-failure.json),
[CI configuration identity](../../../../verification/offline-monitoring/ci-fix-source.json),
[configuration checks](../../../../verification/offline-monitoring/ci-fix-checks.json)
and [RED](../../../../verification/offline-monitoring/ci-budget-red.txt) preserve
the distinction. All final Go/draft bytes still match the original source
manifest, so the already-passed full local Go race/build/vet/lint evidence is
reused without claiming a second execution. The correction has its own offline
checks and [staged/history scan](../../../../verification/offline-monitoring/ci-fix-leak-scan.json). Final corrected-head CI5 is required before
squash; release identity remains the PR/CI/Git metadata.

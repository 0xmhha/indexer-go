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
mandatory. [Secret scans](../../../../verification/offline-monitoring/secrets.json)
distinguish staged findings from reviewed public history fixtures. No SQL/storage/operational source,
pool or RPC implementation is modified. No local operational PostgreSQL, Rust
node build/run, Docker execution, format, GC/restore activation or performance
test is part of this feature. Actual final-head CI postgres/arc remain mandatory
before merge; their temporary functional fixtures are not operational evidence.

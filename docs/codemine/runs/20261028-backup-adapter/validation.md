# Offline backup adapter validation

Behavioral RED: `go test ./internal/executionbackup -run '^TestBackupAuthenticates' -count=1`
failed because a valid signed fixture was refused. The signature/permit/artifact
implementation then passed the functional suite. Development-only compiler
cleanup and sandbox cache-access failures were corrected; they are not counted
as behavioral RED evidence.

Synthetic tests cover accepted attribution, forged/wrong-domain signatures,
permit replay, chain/DB/key/version/size/WAL/digest conflicts, malformed strict
JSON, private rights and leaf/parent/hard links, input byte/inode preservation,
export compatibility, unsafe output parents, overwrite refusal, cancellation,
failed report writes and absence of partial exports. Fake snapshot/WAL bytes do
not establish PostgreSQL recovery or native data-directory conformance.

Final frozen-source checks all exited0: new functional suite, dependency/runtime
boundaries, all tracked/new Go formatting, root/wire/StableNet executor/astgraph
build/vet/lint, full `make test-race` (root/wire/executor), cache lifecycle,
examples, release-sequence fixtures and current documentation links.
[Source identity](../../../../verification/backup-adapter/source.json) and
[exact commands/log hashes](../../../../verification/backup-adapter/checks.json)
record the same unchanged final Go source throughout. The actual built command
also passed HOLD/help/argument/redaction/closed-pipe exit checks.

No new concurrent runtime state was introduced; cancellation tests are
sequential, so no new concurrency200 campaign was needed. No local PostgreSQL
rerun or Rust rebuild was needed for unchanged SQL/storage/native code; actual
final-head CI postgres and arc success remain required before merge.

[Secret scans](../../../../verification/backup-adapter/secrets.json) retain
staged versus previously reviewed public historical fixtures separately. No actual external snapshot/WAL creation,
operating DB/node, Docker build/run, disk format or performance test is performed.
No SQL/storage/pool/RPC/publication implementation is changed. Live receipt
validation and final fresh/generation fence retain their existing tests/contracts.

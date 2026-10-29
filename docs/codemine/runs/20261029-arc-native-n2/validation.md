# N2 validation

Meaningful RED cases precede implementation: missing native writer, missing stdio
protocol, canonical corrupted head token, active read guard missing maintenance
lease, well-checksummed head not bound to immutable completion, and managed legacy
maintenance admission. The synthetic ordinary-transaction fixture initially lacked
its test-only adapter; that is separate qualification evidence, not a production
defect. Compiler corrections are not behavioral RED.

Actual SDK Full persistence is read back as the original block/receipt bytes and
root. Exact duplicate after restart performs no EVM call. Synthetic funded
transaction/code/storage receipts are checked against the existing independent
second replay. Production frame decoder and writer still reject that synthetic
genesis; injection is cfg(test)-only with no runtime flag.

Owned child tests pause at before/during/after EVM, durable intent, save, the pinned
SDK entry/exit of a nonempty synced RocksDB commit batch, complete MDBX commit,
receipt, head and pending clear. Parent SIGKILL uses only its typed Child handle;
all pipes/processes are joined. SDK tracing is test-only and source-pinned:
normal commit finalizes/syncs static files before entering commit_batch, commits
synced RocksDB before span close, then commits durable MDBX. Pending states remain
fenced and byte-preserved; lost reply after pending clear retries exact receipt.
A separate RO process opened before the write observes the new state on its next
provider; an arriving reader cannot overtake the waiting writer.

These are controlled owned-process/native-library fixtures. They do not claim
power-loss/APFS/device-failure safety, real network/operational inputs or speed.
Final local and release evidence is recorded only after actual completion.

Final local all-target Rust tests passed72 library and4 command tests, no failures
or ignored tests. Build, fmt and clippy `-D warnings` passed on the reviewed source.
No Rust source changed during the final full run; the final per-file
[freeze](../../../../verification/native-import-n2/source.json) was verified after
gates. The final CI correction keeps the prior source hash and records the
explicit compact sorted file-hash map algorithm. [Checks](../../../../verification/native-import-n2/checks.json) and
[RED records](../../../../verification/native-import-n2/red.json) preserve actual
commands/log digests. Go/module source remains b8da4538; the retained successful
full race log19999d4f was verified and explicitly REUSED, not rerun. Rust tests
do not claim Go race checks detect native races. New final-head CI5 is required
before guarded squash/main synchronization and N3.

CI313 exposed a reader-fairness test failure with a generic child EOF. The old
test could observe either the writer's initial read admission or its later
publication wait; it had no post-execution rendezvous. The correction adds that
rendezvous and preserves bounded child errors/expected phases after joining all
owned pipes/processes. Both missing diagnostics and missing rendezvous were
RED before implementation. Native production lock/storage/VM code is unchanged.
The original failed run remains [evidence](../../../../verification/native-import-n2/ci-failure.json);
release still requires a new exact-head CI5.

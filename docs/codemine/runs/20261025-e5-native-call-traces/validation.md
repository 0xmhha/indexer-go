# E5 native calls validation

Scope: pinned native libraries, synthetic fixtures, local mock RPC/IPC and owned disposable PostgreSQL with fsync enabled. No actual node, peer, operator database, disk provisioning/formatting, operational GC or performance measurement. Preserve pool4/part2/API reservation, RPC5 physical attempts/s, final NEW uncached fresh read and generation fence. Scratch logs are under `/private/tmp/indexer-e5-traces-20261025/validation`; they are local evidence, not repository dependencies.

## RED → GREEN

| Contract | Observed RED | GREEN evidence |
| --- | --- | --- |
| Optional canonical wire | Old strict decoder rejects independently constructed canonical CallTraces metadata (`wire-red-canonical.log`) | Wire graph/order/bounds/legacy tests and consumer root/profile rejection tests |
| Consumer root consistency | Wrong sender, target, value, gas, input, receipt status or profile accepted (`consumer-red.log`) | All eight mutations rejected; ancestor-reverted paths retained |
| Same-replay native hooks | Actual Arc and StableNet replay omitted calls (`arc-red.log`, `stablenet-red.log`) | Native nested/all-seven-kind/bounds tests and deliberately regenerated producer fixtures |
| Arc CallFrom behavior | Direct logical call replaced the physical root; late precompile completion rollback was reported as a successful child (`arc-subcalls-red.log`) | Physical root plus logical child retained; `ArcCallFromCompletionReverted` recorded without another EVM execution |
| PostgreSQL persistence | No transactions-part call writer (`postgres-red.log`) | Same-part durability, publication visibility, paging, bound hostile input and rewind checks |
| Exact lost ACK | Missing row or damaged row bytes still confirmed (`trace-ack-red.log`, `trace-corruption-red.log`) | Complete ordinal coverage and row checksums verified before fresh/ACK |
| Reorg cursor | Stale pin returned missing after rewind (`trace-pin-red.log`) | Epoch checked before height lookup; stale cursor rejected |
| API | New route absent; explicit epoch zero silently accepted (`api-red.log`, `api-pin-red.log`) | Strict parameters/errors, existing reserved snapshot, multichain mount and compact no-store response |

Early Arc fixture calibration attempts did not reach the intended late completion failure; only the final authorized large-output fixture establishes that RED. An integration-test shallow-copy assertion was corrected before the final native→PostgreSQL/API matrix passed. Neither is evidence of an additional runtime defect.

## Targeted functional results

- Arc fmt/build/clippy and all-target native tests: 32 library + 2 CLI tests pass. Five native JSON outputs are independently consumed by Go; the checkpoint export remains byte-identical.
- StableNet native seven-kind, bounds and cancellation tests pass; six EVM/WBFT fixtures deliberately regenerated from the actual replay, including `evm-calls`.
- Ten actual EVM/WBFT/Arc fixture cases pass through PostgreSQL publication/API parity. Arc special-call fixtures pass independent Go wire/root checks.
- Actual native Arc call graph passes through seven writer queues into PostgreSQL, bounded block/transaction/internal pages, exact lost-ACK confirmation and read-only store reopen. API reads cause zero additional mock RPC requests.
- Collected empty graph is distinguishable from legacy absence. Missing/corrupt traces fail before fresh, while current-prefix identity and reorg cleanup remain intact.

## Upgrade-fixture regression found by full PostgreSQL verification

The initial complete PostgreSQL storage pass found two old upgrade tests that reconstructed only part of a historical schema. The v14 test retained migration18 tables/column while deleting its version, so rerunning migration18 collided. The v16 test deleted only version17 while max(version) remained18, so the intended constraint upgrade was skipped. The test-only reconstruction now removes the later call structures, restores all four original cause constraints and resets the complete later version suffix before reconstructing v14/v16. Production migrations and released SQL files are unchanged. Both upgrade/partition-preservation cases pass with actual fsync-on PostgreSQL and race enabled. The initial already-failed suite was interrupted only in its owned test process and its script cleaned up; the corrected complete PostgreSQL storage/app/API/StableNet-bundle run subsequently passed, including cleanup.

## Release gates

Full vet initially detected a test import cycle because PostgreSQL referenced the RPC trace package whose tests import PostgreSQL. The native root verifier now lives in an independent `evmtrace/native` package; no RPC dependency is introduced into storage.

Completed local gates:

- Root build/vet/lint, Go fmt, Rust fmt, changed-document relative links/anchors and `git diff --check`.
- Separate `execwire`, StableNet executor and AST module build/vet/lint; example modules, cache lifecycle, offline operations/preflight/shell checks.
- Full root functional tests and complete `make test-race` across root, wire and StableNet executor. Final test-only migration corrections also passed targeted actual PostgreSQL race plus final root lint/vet.
- Arc pinned Rust1.98.1 build/clippy warnings-as-errors/all-targets: 32 library + 2 CLI tests. Five actual JSON fixtures and the unchanged native checkpoint match generated bytes.
- Corrected full owned fsync-on PostgreSQL storage/app/API/StableNet bundle, including test container/volume cleanup.
- Single `GOMAXPROCS=1 -race -count=200` for actual native→seven-writer→PostgreSQL/reserved-API/restart/lost-ACK integration on the final runtime; separate single200 API route and StableNet call/bounds/cancellation selectors. Partial runs are not substituted. Surviving-prefix original generation/digest versus current epoch/fresh is additionally verified on actual PostgreSQL with race.

U04 covers implementation, local verification and release preparation. Staged/history secret scans, exact-head lint/test/postgres/examples/arc, ready/squash and clean main sync remain mandatory. The source-branch PR linked from the checklist is the mutable release authority: while open the release is pending; MERGED with these checks completes this scope. Preserve the source branch.

Native hook observations are trusted producer evidence checked against transaction roots/receipts, not a cryptographic child-list commitment. Blob sidecars/KZG/availability and independent consensus validation remain separate work; live operational and performance claims remain unverified.

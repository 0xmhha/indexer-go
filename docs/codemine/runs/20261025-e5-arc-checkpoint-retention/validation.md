# Arc checkpoint and retention validation

All required local checks are GREEN, including the CI cancellation correction below. The [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-checkpoint-retention) resolves exact-head CI4+Arc and squash/main release status. Owned synthetic/native-library fixtures are functional evidence; actual node services/peers/operator DB maintenance, disk provisioning and performance measurements remain held.

## Requirement evidence

- U01 RED: native full export returned the explicit unimplemented error. GREEN: complete native account/storage roots match, missing account/slot/code and incomplete seed fail closed, private output is durable/no-clobber, and Rust-generated LOCAL_DEV genesis NDJSON passes the independent Arc Go verifier. Invalid DB/RPC settings are unused during verification; a changed expected root is rejected.
- U02 RED: permitted native copy failed against the unsafe-maintenance stub. GREEN: owned copy/replacement and exact DB release, same-permit intent/copy/candidate/swap/old-rename/new-rename/cleanup/done recovery, active owners, insufficient reserved capacity and unknown candidate preservation. Actual owned Reth v2 MDBX/RocksDB/static files reopen with exact state/cutoff/retained payload after eligible-jar deletion. A native cutoff full seed and digest are retained.
- U03 RED: collector launched its fixture child despite a pending native fence. GREEN: startup rejects before launch; native helper cancellation/strict replies and managed guard join are checked. The durable source bridge acknowledges132 synthetic wire records and collects only expired quarantined evidence after native completion, retains node fencing until exact release, and returns the same durable receipt on retry. Public config was rejected after adding owned0600/hardlink checks. Sealed missing-current-path swap recovery is recognized; foreign/unfenced paths fail closed. Owned child scratch is removed after join, while a replaced directory is preserved. Go package child fixtures use the Go test executable, with no Python/node dependency.

## Confirmed native constraints

Deleting the first transaction jar made even genesis unavailable because upstream derives global history availability from that jar. First/pre-anchor/boundary jars therefore remain. Only eligible complete payload jars are reclaimed; all state history/current state/code/headers/indices stay retained. There is no fixed total-native-disk guarantee, and the candidate-key catalog is bounded to one million keys.

Same-process/cooperative RO reopen is not native writer exclusion. Actual separate exclusive-helper and normal RW native-library probes are rejected during the hold. Managed readers are excluded by the run lease; cooperative RO may still open on some platforms. All native owners must follow the managed offline contract. Tests use official testnet genesis plus synthetic storage blocks, not consensus-valid peer evidence.

## Required checks

| Check | Result |
| --- | --- |
| Root build, vet, lint, cache lifecycle, examples and offline operation drafts | GREEN |
| Full root functional | GREEN |
| `make test-race` for root, wire and StableNet executor | GREEN |
| New helper/guard/scratch `GOMAXPROCS=1 -race -count=200` in one invocation | GREEN |
| Affected collector/command `GOMAXPROCS=1 -race -count=200` in one invocation | GREEN |
| Complete owned fsync-on PostgreSQL storage/app/API/StableNet bundle; explicit container/volume cleanup | GREEN |
| Separate wire, StableNet executor and AST module build/vet/lint | GREEN |
| Rust fmt/build/clippy warnings-as-errors/all-targets native tests and actual maintenance CLI | GREEN |
| Three regenerated native fixtures byte-identical; Go format, relative links and staged secret scan | GREEN |

The full functional/race/PG/concurrency suites were each closed after their required run passed. No host-load/SLO or throughput experiment was added. Source packaging was corrected to explicit `src/maintenance.rs` because repository `bin/` ignore rules exclude implicit Rust bin sources; native build/lint/tests were rerun for that packaging change. The final Go-only child fixture passed affected vet/lint verification. A lint-only equivalent tagged-switch cleanup then passed the affected package race check; no closed full suite or single200 invocation was restarted. Optional live-node/Anvil paths were not activated. Secret history is scanned again after the feature commit and before push.

Local logs/manifests are under `/private/tmp/indexer-arc-checkpoint-20261025/validation` during release; required commands and limits above remain the durable report. Build caches are task-owned and disposable; shared caches, other projects and operator data are preserved.

## CI cancellation correction

CI run40143373888 passed lint/examples/arc and the earlier test stages, then the new native single200 caught `cancelled native child reported success`. A canceled command can race stdin EOF and exit0 before CommandContext's watcher kills it, so `Wait` alone did not preserve cancellation. The owner now retains its derived context and joins its error **before** cleanup's own cancel; ordinary successful close remains successful. One-shot operations preserve the same derived deadline/cancel reason.

`TestNativeSuccessfulExitStillReportsCanceledContext` deterministically uses a fixture command that exits0 after an already-canceled owner's EOF. It failed RED with nil, then passed GREEN with the cancellation error and joined scratch cleanup. This is a real semantic race, not a throughput experiment or a relaxed assertion. The regression is included in the single200 CI selection. Updated full `make test-race`, affected build/vet/lint and the five-case `GOMAXPROCS=1 -race -count=200` invocation all passed. Unchanged completed PG/native-format checks were not restarted; the new exact head still requires every CI4+Arc job before merge.

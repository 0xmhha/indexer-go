# Priority4 validation and release gate

Owned continuation directory: `/private/tmp/indexer-four.receipt.Rh9qzP`.
Preserved prior implementation/evidence: `/private/tmp/indexer-four.retention.wjtBlm`.
Base release: E6 #229, squash f7c0fa26f6e2b62fa2dc50820369ea9a3b9a5818.
Branch: `refactor/native-retention-matched-backup`.

## Immutable receipt continuation

The cancelled worktree remains untouched. The pending patch SHA256
`a9535d5cb9f4dac6ac42b10b5ed104ab416064f60f11e7cfe03ad16f4fc2437b`
and both untracked hashes were verified before applying to clean E6 main.
`prior-identity.json` records every Rust source/manifest and native binary hash,
plus prior complete RED/GREEN receipts. Rust and frozen fixtures are unchanged;
`frozen-parity.json` independently compares all seven fixtures byte for byte.

`receipt-red.log` records runtime subprocess failures in both directions:
validated A child rejected, unvalidated foreign B child accepted after an A→B
replacement inside the validation callback. The production coordinator now reads
once, validates that value in its live DB transaction and passes its immutable
digest into child supervision. Backup/restore requires nonzero authority.
`child-receipt-red.log` reproduces a later child reread accepting B; later reads
now also compare against the inherited digest. These tests use real inherited
pipes and challenged child receipts. Existing fsync-on PG tests separately
validate exact database/publication/permit/WAL and final fresh/generation gates.

Shared Go caches are preserved. The new owned `go-cache` is a filesystem clone
of the previous owned cache; the new `lint-cache` is isolated. After release,
the strict release observer checks both reviewed run generations; only proven
owned caches/temporary native fixtures in these two run directories
will be removed, preserving all logs, source branches and identity receipts.

## Runtime RED then GREEN

- `native-red.log`: three runtime assertion failures, not compile-only evidence:
  obsolete lookup survives, MDBX free pages remain, and changed checkpoint values
  with a recomputed outer checksum are accepted. Seven predecessor native tests
  passed in that same run.
- `lookup-resume-red.log`: a fourth runtime assertion demonstrated that a native
  lookup prune checkpoint without the exact transaction number is unsafe to resume.
- `backup-red.log`: the named root feature test rejected the absent implementation
  of mixed/missing receipt and component admission.
- `native-preservation-green.log`, `lookup-resume-green.log` and final
  `arc-release-final.log`: actual owned MDBX/RocksDB/static-file data APIs prove
  expired/retained lookup rows, cutoff/successor/ACK/tail/head roots, nonempty
  storage/code, exact cutoff values, compact-copy/reopen, interrupted compact-copy
  phases, sealed-candidate admission, retained proof export and old/mixed consumer
  rejection. The final full native gate passed 41 library and 2 binary tests.
- `go-native-snapshot-green.log` and `matched-component-final.log`: actual source
  spool/COW lifecycle, joined snapshots, same-permit recovery, every backup
  durable phase, native/source/indexer missing/corrupt/linked component rejection,
  atomic no-overwrite publication and pending-fence preservation.
- `pg-backup-green.log`: fsync-on temporary PostgreSQL rejects foreign DB,
  publication, permit and future WAL receipts; fresh/generation/cancel failures
  retain the exact fence in pool4.

## Local release gates

Passed: root build/vet/lint; formatting; nested wire/StableNet/astgraph build,
vet and lint; full root/wire/StableNet `make test-race`; examples; Pebble cache
lifecycle; 40 offline operation tests and preflight with zero services started;
observer receipt tests; relative documentation links; staged and whole-history
secret scans with only the exact previously reviewed fingerprints excluded.

`concurrency200.log` records one COMPLETE `GOMAXPROCS=1 go test -race -count=200`
invocation for the new native snapshot cancellation/fence test. No partial sums.
`arc-release-final.log` records offline pinned Rust fmt/build/clippy/all-target
checks. All seven regenerated frozen fixtures are byte-identical to the repo.
The prior full fsync-on PostgreSQL gate completed (storage860.858s, app1032.419s, API11.486s, StableNet15.595s). Final continuation gates and release are recorded below.

## Limits and operations

External PostgreSQL artifact provenance and WAL recovery are authenticated
operator inputs. This implementation validates matched receipts and supplied
artifacts against the current live E3 DB and native/source snapshots; it does not
install PostgreSQL or activate a restored node. No fixed total DB size, measured
savings/RPO/RTO, live chain or operational restore is claimed.

Pool4/part2/seven writers, two EVM passes, RPC5 physical attempts/s and final NEW
fresh plus post-response generation fences remain. No Rust/upstream execution
or consensus policy was changed. Original E1/E2/E3/E4, proof/blob/call and E6
contracts remain. Native latest state/code/preimages/headers/mappings/trie and
protected jars survive; only source-justified expired hash lookups are added to
logical pruning. Native physical reclaim uses MDBX compact-copy, separately
from RocksDB compaction.

All required local gates completed. Exact current-head CI5,
ready/squash, reviewed one-parent tree, clean primary main sync and the strict
all-four observer are subsequent required release evidence. Postrelease ledger,
Korean completion and cleanup receipts may remain as an identified local
managed-worktree handoff while primary main stays clean. Preserve all source
branches and evidence. Actual node/peer/provider/operator DB, disks, operational
GC/restore, performance/SLO, tags/deploy/artifact publication remain held.


## Continuation final-tree gates

`receipt-packages-final.log` passes all affected Go packages with race.
`concurrency200.log` is one complete CPU1/race/count200 invocation for both the
receipt subprocess mutation and native snapshot cancellation tests.
`real-native-backup-tagged.log` actually executes the arc_native-tagged native
library test (PASS507.00s), rather than treating zero matches as evidence.
`test-race-final.log` passes the complete root, execwire and StableNet target with
`GOFLAGS='-p=2 -timeout=20m'`; root app passes452.508s. This preserves all tests
and uses the repository's20m package allowance while bounding package workers.
Build/vet/root lint, nested build/vet/lint, formatting, examples/cache/offline,
observer tests, named priority4 and secret scans passed in this continuation.

Failed attempts remain preserved: the first broad race run hit existing blob
request and root default10m deadlines; the concurrent full PG run hit app20m
(storage972.482s, API10.552s, StableNet12.205s passed, but the command failed).
An early package run hit the existing native helper5s deadline; the complete
affected-package rerun passed. One lint invocation collided with the linter's
parallel-run lock; nested and then root lint passed. An initial native command
omitted the build tag and had zero matches; only the corrected tagged run is
counted. These failed/empty invocations are not passed release gates. The full
PG command reran alone and passed as `postgres-final.log`: storage479.591s,
app802.982s, API5.702s, StableNet9.702s, with successful owned container/volume
cleanup. No external test DSN was configured. Commit/PR local gates are satisfied;
exact-head CI5/squash/clean-main release remains pending.

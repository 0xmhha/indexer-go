# Priority2 validation

Execution-owned run directory: `/private/tmp/indexer-four.calls-6f7df360`.
Logs and RED/GREEN receipts remain in its `evidence/` directory. The predecessor
sync receipt is carried there unchanged; its ledger entry remains in
`verification/releases.json`. No cancelled worktree or `.ouroboros` data is staged.

Actual REDs: unknown GraphQL `executionCalls`; missing bounded range reader;
ambiguous duplicate `partial` JSON-RPC parameter accepted before strict parsing.
GREEN functional tests exercise real frame queries, strict/partial merged
missing ranges, known collected empty versus legacy absence, address matching,
original keyset ordinals, cross-block pagination, coverage and generation cursor
fences, archive failure rollback, original generation versus public epoch,
restart/cancellation and corrupted archived rows. API integration fills the
three non-reserved pool slots and serves GraphQL/JSON-RPC from the reserved fourth
slot, returns it on cancellation, rewinds and reopens read-only orphan queries.
All temporary PostgreSQL instances use the existing owned fsync-on script.

The named `TestSeedPriority2CallQueriesAndOrphanTraces` executes actual HTTP
GraphQL/JSON-RPC contracts with snapshot assertions, input bounds and compact
error semantics. PostgreSQL behavior is independently exercised by
`TestExecutionCallRangePostgresAPI` and `TestExecutionCallRange*` storage tests.
No live node/provider/peer, E6, native operational GC or performance claims.

Release is pending until exact-head CI5, squash tree and clean main evidence
are observed. The full `verification/verify_release_sequence.py` remains a
fail-closed final all-four gate; it is not a phase2 substitute.

## Completed local gates

- Build, gofmt, root vet and lint: passed. The one staticcheck switch-style
  finding was fixed and the full build/vet/lint command passed again.
- Full `make test-race`: root, execwire and StableNet passed (including root
  app/storage suites). Final API packages also passed race after parser/cost
  and style changes.
- Full fsync-on `make test-postgres`: PostgreSQL storage, app, API and StableNet
  bundle passed. Final verbose feature PostgreSQL tests all executed and passed
  without a skipped feature case, including cross-block cursors and replacement.
- One complete `GOMAXPROCS=1`, `-race`, `-count=200` invocation of the named
  feature/API and coverage/orphan/restart/archive-atomicity storage cases passed;
  API475.162s and PostgreSQL664.017s. These are complete package results from one
  invocation, not partial sums or performance measurements.
- `make test-examples test-cache-lifecycle test-execwire test-executor-stablenet`
  passed. Offline execution operations/inspection/checkpoint/proof commands passed.
- Build/vet/lint passed in execwire, StableNet executor and tools/astgraph.
- Relative guide links, schema snapshot and actual named-test PASS event checked.
- Staged and whole-history gitleaks scans passed with only exact previously
  reviewed fingerprints from `verification/secret-scan-reviewed.json` excluded.

Rust/upstream is unchanged. Local success is not release; exact-head CI5,
squash and clean primary main synchronization remain separate mandatory gates.

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

Release evidence is independently recorded below; local gates do not establish
GitHub release or main synchronization. The full `verification/verify_release_sequence.py` remains a
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
  reviewed fingerprints from `verification/leak-scan-reviewed.json` excluded.

Rust/upstream is unchanged. Local success is not release; exact-head CI5,
squash and clean primary main synchronization remain separate mandatory gates.

## Released priority2

- PR: https://github.com/0xmhha/indexer-go/pull/228 (draft → ready → squash).
- Exact reviewed head: `b0e58642562b6aefef7e0a5e3f6bb806d81e5443`.
- CI5: lint/test/postgres/examples/arc all success on run40347262650 at that head,
  completed before merge at2026-10-26T16:27:01Z. No bypass or verification exemption.
- Squash/main: `ba285da31bfe105813e81ebbbb510e3ac2921f97`, one parent and the exact reviewed head tree.
- Primary main/origin-main/HEAD are clean and equal; independent sync receipt:
  `verification/priority2-sync.json`. Source branch remains on origin.
- Only this execution's `go-cache` and `lint-cache` under the owned run directory
  were removed after release. Logs, scans, local/release audits and sync evidence
  remain in `evidence/`. Owned fsync-on PostgreSQL scripts exited successfully
  and cleaned their own containers/volumes; no shared resources were removed.
- This worktree preserves post-release docs/ledger/sync as uncommitted handoff
  evidence for the parent; the primary main is clean. No post-release code changes.

Priorities3/4 remain required for the parent. The final all-four release observer
is unchanged and still requires all four releases. Nodes/peers/operator DB,
disks/operational GC, performance/SLO/load, tags/deploy/artifact publication and
remote branch deletion remain held.

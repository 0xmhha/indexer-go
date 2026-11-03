# MON-02-D implementation and validation

## Behavioral RED → GREEN

Strict parser RED rejected the new disabled `operations.observation` namespace.
GREEN recognizes it while ignoring dormant HOLD values; unknown keys remain
rejected. Compiled RED also accepted default/large PostgreSQL pools and an
incompatible shared cadence; GREEN requires PostgreSQL pool4 and matching stats
intervals. App RED constructed no observation owner and accepted unsupported
storage; GREEN assembles the optional owner before starting resources.

Lifecycle RED left a never-started owner unstopped and marked missing checkpoint
history merely missing. GREEN closes idempotently and freezes StateLost/UNKNOWN
without manufacturing a recovery or replacing unknown history.

Owned PostgreSQL exposed a real concurrent-arrival defect: WAL completed after
the inbox drain's captured clock and was rejected as future input. A deterministic
channel-controlled RED reproduced it without SQL. GREEN takes a fresh consumer
clock for each dequeued component, preserving both acquisition timestamps. The
same owned PostgreSQL checks then passed.

## Implemented boundary

- `observeconfig` performs strict bounded validation without IO. Nil/off creates
  no observation owner, sampler, factory/file, RNG or goroutine. Existing capacity
  settings/default30s query/latch/Resume stay unchanged when observation is off.
- Enabled v1 grades explicit whole-database and server-WAL budgets only; opaque
  resource/series/policy bindings do not authenticate a database or allocate a
  shared database's usage to chain schemas. Unsupported native/backlog/lag/health
  operands are not synthesized. No schema migration or publication change.
- App shares exactly the existing capacity sampler or owns one standalone
  observation sampler: query2s/round5s, original component times, general pool4,
  no reserved API context, new pool or RPC. Shared cadence mismatch is rejected.
- The producer offers value metadata nonblocking to the bounded inbox. An
  independent worker serializes the C engine/checkpoint/sink. Slow/failing sink
  or persistence does not change producer returns, COMMIT or exact source ACK.
- Source selection and pure grading share a bounded fence; persistence and
  delivery are outside it. Detach retires queued results, cancels/joins the actual
  Store read and falls back to the earlier live source. Child Apps borrow the
  root runtime and stop only their source; root closure joins owned work before
  Store closure, including never-started, direct Shutdown and late provider IO.
- Missing/corrupt/unsafe startup metadata freezes StateLost/UNKNOWN, with no
  automatic overwrite/send. Explicit initialize can create only an absent fixed
  checkpoint in an existing private directory. Later save failure is UNKNOWN
  with interval-bounded retry. Lifetime inbox loss forces GAP and never resolves
  a known incident; safe restart restores incident metadata and fresh dwell.
- YAML permits disabled sink only. Enabled programmatic fake sinks require a
  separate exact operator identity; no chain destinations/credentials/outbox are
  borrowed. Stable IDs, max3 attempts and retained dead letters use C's contract.
  No real transport, HTTP endpoint, recipient or network message was added.

## Actual checks

Reviewed Go source: 12 files, SHA256 (sorted path, NUL, bytes, NUL):
`a8bf00cec2206c6580f05ce76eee799d5ce7f195b8370c8e89cfc2b74c59ec17`.

- Focused functionality and race: observation/config plus the affected App,
  existing inbox/sampler/config regression pass. Clock, source cancellation,
  slow checkpoint/sink, persistence failure/retry, source rebind, restored
  critical/loss, private initialization and operator binding are covered.
- One final targeted `GOMAXPROCS=1 go test -race -count=200` invocation passes
  11 new worker cases and2 new App owner cases (13 top-level cases). Old B/C
  campaigns, native SDK/full matrices and workload measurements were not repeated.
- Owned disposable PostgreSQL15, Unix socket only, fsync=on: new runtime pool4/API1
  general acquisition wait/cancel/return and actual DB/WAL qualification pass;
  actual multi-chain App construction/child Shutdown/fallback/root join and
  single-chain App.Run cancel/join pass.
  These3 new tests passed with race; normal non-PG runs explicitly skip them.
  The fixture was stopped and only its own directory removed on every outcome.
- Root build/vet and final lint pass; lint0. Scoped short CI is still required
  on the actual PR head and is not replaced by these local checks.
- Documentation links/anchors pass:14 selected documents/338 relative links/0
  errors. Existing CI policy24, whitespace/fmt pass. Staged/reachable-history
  secret checks pass: staged0, reachable history15 reviewed public fixtures,
  unreviewed0. Actual release identifiers
  and PR/CI/tree/squash/main identities are reconciled from Git/PR metadata;
  this precommit snapshot does not certify a future merge.

The actual private config.yaml hash remains
`870a6b815300bdda9a1ed9b49523e79e4c544580481efc2f2aab7197adbcd446`, mode0644.
Pool4/part2/API1, seven logical writers, physical RPC5 including retry/failover/
batch fallback, two normal EVMs and final NEW uncached canonical → generation
fence → COMMIT → exact ACK remain unchanged. No operating node/peer/deployment,
disk format/provision, operating GC/restore, external alert or extra performance
run; other projects/processes/containers/volumes/shared caches are unchanged.

## Release and next task

Base main `7c4e9f62c5e0babe823b093c552a0e0e48985030` (C #265), branch
`refactor/observation-app-assembly`. Require a normal PR, applicable exact-head
short CI, squash with identical reviewed tree/one base parent and clean main
synchronization. Preserve this branch and C/B contexts. Current release identities
are Git/PR evidence, not a reason to create a self-SHA documentation/CI loop.
After those gates, next is MON-03 fake process/parser/health, with all real Docker
probes, container execution, operating inputs and performance still HOLD.

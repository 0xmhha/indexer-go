# MON-03 implementation and validation

## Behavioral RED → GREEN

The compiled parser RED accepted invalid/permission/overflow output as known0
and returned0 instead of2048 for a valid KiB report. GREEN checks header/unit,
row/mount/decimal/ranges and checked byte conversion; known0 remains distinct
from missing or malformed data. A compiled health RED marked absent heartbeat
healthy. GREEN returns UNKNOWN and a separate MissingHeartbeat cause, only
reporting Healthy after qualified data and exact owned cleanup.

An additional deterministic RED reproduced a cancellation scheduling gap:
parent.Err was already cancelled while its AfterFunc delivery remained queued,
but late valid output was still published. GREEN checks the parent directly at
final publication, in addition to the copied context/deadline. No source timestamp
is refreshed to hide delay. Run's terminal fence also cancels/joins a previously
admitted external Sample instead of returning while that owned work is active.

## Implemented scope

New `internal/vmobserve` is an independent injected-runner library, with no OS/exec
or Docker client/CLI implementation, App/config/command activation, process launch,
container/network/DB/node call or operating authority. Fake callbacks model
Start, CLI Kill, actual Wait and exact resource cleanup as separate capabilities.
All library IO capabilities are opt-in programmatic injection; constructors and
health assessment do no IO. Existing scripts/configs/runtime/schema/workflows
remain unchanged.

- Bounded ASCII df -P -k root-report parsing, one recognized KiB header/row,
  unsigned integers/consistent usage/percent bounds and available×1024 overflow.
- Unique nonce/monotonic sequence, at most one active lifecycle, context-value
  stripping and bounded attempt/independent cleanup deadlines. A busy operation
  is not replaced; cancellation-ignoring Start/Wait/cleanup is honestly joined.
- Ambiguous launch, foreign/contradictory ownership or nonexact/missing/late
  cleanup receipt remains pending and blocks new attempts. CLI exit alone
  never confirms container removal. Partial start errors still join/clean
  the positively identified owned handle. No TTL/reset/prune escape.
- Stdout4096/stderr1024 byte limits cancel excessive output; stderr is counted
  and discarded. Seal fences late/concurrent writers; buffers/errors/credentials/
  filesystem names never enter returned status, only typed numbers and codes.
- Immutable sample acquisition time plus separate observer health/heartbeat:
  missing/stale/future/timeout/failure/pending is not known0/full/healthy.
  A healthy observer can truthfully measure a full disk (known0), leaving
  resource capacity severity to a separately qualified consumer.
- Optional serial Run and direct/never-started/concurrent Close preserve actual
  ownership until known work joins. Run termination fences new admission and
  joins externally admitted work too; overlapping Run is refused.

This is a cooperative capability contract, not Docker authentication/deletion
proof. Pending is retained by the in-memory owner; a new Probe/process is not
proof of prior cleanup. Before actual activation, a real adapter still needs
reviewed context/image/permissions, nonce labels/private cidfile verification,
durable crash/restart admission and owned-only cleanup, within OPS-03/05/06.
No operating implementation or activation is implicitly approved here.

## Actual checks

Reviewed Go source:6 new files, SHA256 (sorted path, NUL, bytes, NUL):
`ae57c75cce46b5517bf6b4fdbfa16149683e17470ade8e8400d466a3c2dbcd47`.

- Focused functional/race passes for the18 new top-level tests (including parser).
- One final `GOMAXPROCS=1 go test -race -count=200 -timeout=5m
  ./internal/vmobserve -run '^(TestProbe|TestCapture)'` passes16 new cases.
  They cover timeout/cancel, late Start/Wait/output, independent slow/failed
  cleanup, identity/pending fences, bounded concurrent output/seal, redaction,
  immutable health/data, partial failures and periodic/direct/concurrent closure.
- Root build/vet/lint pass, final changed-package lint0 and fmt clean. No existing
  SDK/DB/old concurrency campaigns or performance tests were repeated: no code
  in those integration/lifecycle paths changed. Their normal applicable CI
  remains required on the PR's exact head.
- Docs pass:13 selected documents/296 relative links/0 errors; existing CI
  policy24 pass. Staged secrets0, reachable history15 reviewed public fixtures,
  unreviewed0; whitespace/source/config checks pass. Current release
  identities and actual applicable short CI/squash/tree/main gates are read from
  Git/PR metadata; this precommit snapshot cannot pre-certify a future merge.

config.yaml remains SHA256
`870a6b815300bdda9a1ed9b49523e79e4c544580481efc2f2aab7197adbcd446`, mode0644.
Pool4/part2/API1, seven logical writers, physical RPC5 including retry/failover/
batch fallback, two EVMs, final NEW uncached canonical → generation fence → COMMIT
→ exact ACK, SQL binding and all retention/recovery authorities are unchanged.
Other projects/processes/containers/volumes/shared caches are untouched.
No Docker/host probe, operating node/peer/deployment, format/provision, operating
GC/restore, real sender or extra performance measurement ran.

## Release and next action

Base main `e16c337e515ed2280a9c6624a31d40edd37594cb` (D #266), branch
`refactor/docker-observer-lifecycle`. Require normal PR, actual applicable
exact-head short CI, squash/reviewed tree/one base parent and clean main sync;
preserve the branch. Keep D/C and accepted E1/first-import/N6 contexts linked.
After those gates, only existing operating-input/run HOLD and separately deferred
extensions remain. Next is OPS-01~04 preparation when inputs become available;
without them do not invent another probe, repeat completed tests or activate
actual Docker/health delivery, deployment or performance.

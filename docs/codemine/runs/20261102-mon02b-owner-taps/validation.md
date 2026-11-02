# MON-02-B implementation and validation

## Behavior and evidence

Compiled RED `TestStatsDetachCancelsJoinsAndDiscardsLateResult` showed detach
returning before cancellation/join of its owned Store read. GREEN removes the
source, retires its tap, cancels the actual round and waits for the provider to
return, discarding even a late apparent success. A query that ignores cancellation
is honestly waited for; no deadline is claimed to forcibly kill a driver.

Another compiled RED showed an ACK header without native/read epochs classified
as a known position conflict. GREEN reports `position_incomplete`; independent
known hash/epoch/anchor contradictions still remain visible. Incomplete epochs
are never copied from the binding or a newer native head.

One Arc test initially changed the fixture's in-memory immutable raw control
without changing its stored owner record and correctly received a conflict.
The fixture mutation was removed. This was test setup, not a production defect
or a separate feature RED. The final owner-hook tests pass.

## Implemented ownership

- `opsguard.StatsSampler` is the single sequential DB/WAL owner. The capacity
  monitor exposes that same sampler; a second periodic `Run` is rejected and
  an overlapping manual round returns false. Manual `Sample` is an explicit
  owner operation, never a consumer's extra poller. Latest attach/fallback remain.
- Legacy query timeout remains30s. `WithObservationStats` and the programmatic
  observation-only sampler opt into query2s/round5s context caps. Parent deadline
  and cancellation are preserved while context values (API reservation/DB
  transaction) are stripped. No immediate SQL retry, new pool or node RPC exists.
- Detach, rebind, close and direct App shutdown fence late results and join Store
  reads. Closing also wakes/joins an idle sampler and monitor run before journal/
  Store closure. Child Apps detach their own source and preserve the root owner.
  Source attachments are bounded64. Closed samplers admit no new read or Run.
- `nativeobserve.Inbox`/`Tap` accept bounded value-only observations with per-key
  sequence, nonblocking offer, sticky lifetime loss and permanent retirement.
  Retired queued values are filtered on dequeue. No payload, disk/network sink,
  callback, writer/GC/Resume capability or automatic loss recovery is added.
  A source owner must use a fresh registration after retirement, and distinct
  borrowed taps for unrelated owner lifetimes. New App activation is MON-02-D.
- Existing completed DB/WAL queries, Arc durable raw/import completion and source
  ACK, execution publication and legacy intake provide optional result hooks.
  Default pointers remain nil. Recorder stop and Arc program stop retire their
  own taps; the sampler retires detached sources. SQL backend taps are borrowed
  and their App owner must retire them after joining its receiver (D assembly).

## Time, coverage and limits

DB and WAL retain their own start/end times and zero is distinct from failure.
Fixed observation causes redact raw driver errors; legacy journal/log behavior
is preserved. Negative size is an unavailable statistic, not unsigned capacity.

Arc's first ACK callback can be a restart seed: `ACKClock` gives it no progress
time, retains the prior event time for duplicate refs, and timestamps only a new
same-generation advance observed in this run. Reset/reorg stays unknown. The
header has no native/read epochs, so that sample remains unverified/incomplete.

SQL publication success can confirm an earlier commit after lost ACK, so the hook
does not invent a commit event time or native/read epochs. Legacy intake retains
the actual delivered block's original window time and unverified/truncated
coverage; it does not invent a contiguous canonical head or capture generation.
These are useful partial facts, not proof that all cross-stage gaps are known.
No new query is made to fill missing progress metadata. D must validate declared
resource bindings; opaque IDs do not authenticate physical databases or chains.

## Actual validation

Final source digest (sorted relative path, NUL, bytes, NUL across24 Go files):
`9f8b6b04505c2d693e058813d1e8f46b48dc1d52cd7c050e72e4b84fe2770732`. The final edit after code checks only clarified the
Monitor.Close comment; tested behavior is unchanged.

- Focused functionality/race passes for observation/guard/telemetry/execution and
  App/Arc owner/stop cases. Scoped build/vet/lint pass; lint zero issues.
- Exactly one targeted `GOMAXPROCS=1 go test -race -count=200` command ran seven
  new concurrency cases across guard, inbox, App and Arc. All four packages pass.
  No full concurrency200/native qualification or performance run was requested.
- Owned PostgreSQL15 fixture, private Unix socket, `fsync=on`: the new real DB
  test passes with race. Pool remains4; three general connections may be held
  while the reserved API reader works; stats acquisition cancels/joins and does
  not steal/return another owner's connection. Fixture server stopped and its
  own temporary data directory was removed. No operating DB/Docker was used.
- Documentation12 files/274 relative links/zero errors; existing CI policy24
  pass. Whitespace/source/config checks pass. Secret checks pass: staged0; reachable history15 previously reviewed public
  fixtures, unreviewed0. Actual exact-head short CI and squash/main
  identities belong to Git/PR metadata and the subsequent session reconciliation.

Public RPC5 physical attempts/s, pool4/part2/API1, seven logical writers, two
normal EVM runs and final NEW canonical/fence/COMMIT/exact ACK stay unchanged.
Parameter-bound SQL, original anchors, epochs/leases/full-baseline/GC remain.
No configuration key or operating input is added. Other projects, APFS/enode/
checkpoint/private deployment inputs, formatting/operating/restore/GC/performance
HOLDs remain unchanged.

## Next

[MON-02-C](../20261102-mon01-observation-design/implementation.md#mon-02-c--경보-reducer와-private-checkpointqueue): pure alert transition reducer, bounded private alert/delivery
checkpoint and fake/disabled sink. It must not clear active alerts on missing,
stale, incomplete, loss, retirement or series reset. Actual App/config activation
remains D; actual senders and operating probes remain HOLD.

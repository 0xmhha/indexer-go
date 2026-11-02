# MON-02-C implementation and validation

## Behavioral RED → GREEN

Compiled reducer REDs showed unknown values and sequence gaps resolving a known
critical. GREEN retains the unresolved level and resets dwell. Further compiled
REDs caught reversed checkpoint delivery order, live restore erasing an unsent
critical, and another resource's normal values resolving this incident.

Owned file REDs caught missing critical level defaulting to normal, shortened
identity arrays silently padded by JSON decoding, and special permission bits
being accepted. GREEN requires every v1 field/array width and exact private
permissions. Further REDs covered unobserved metadata incorrectly marked invalid
and non-hour timezones failing checkpoint round-trip. Missing stays unknown;
time equality is semantic, independent of Go location pointers.

## Implemented contract

The single-owner `internal/nativealerts` module adds a pure reducer, bounded
incident/delivery state, explicit checkpoint operations and fake/disabled sinks.
No caller is started automatically. The real App/config/transport assembly stays
MON-02-D. Callers serialize Engine/store/sink operations off the source producer
path; the existing B inbox is the nonblocking handoff.

- Critical escalates immediately from known current evidence, including a
  known critical received across a loss gap. Warning/clear need both declared
  count and dwell; clear uses a separate threshold. Hysteresis, duplicates,
  missing/stale/invalid data, discontinuity and source/run/binding/policy resets
  never resolve an active level. Quality remains separate from incident severity.
- Rule keys bind resource, metric and rule kind. CapacitySignal reuses A's
  exact qualification and128-bit budget arithmetic without fake chain identity;
  an actual above-budget value is critical evidence, not a bad limit. Generic
  derived backlog/lag/health rule kinds require already assessed source facts;
  C does not supply their producer adapters or turn raw heights into backlogs.
  B's absent ACK/SQL epochs and capture generation remain unresolved.
- Transition IDs bind resource/key, retained incident incarnation and revision.
  Cooldown reminders keep the transition ID and use distinct stable delivery IDs.
  Unconfirmed queued transitions suppress reminder duplicates. Counter overflow
  is an explicit error, never a wrap or recovery.
- There are at most128 rule states across64 resources. Total pending capacity is
  256: queued128 plus at most one deferred opening critical per rule128. On full
  queue, retain that critical and its ID; pause further changes for that entry.
  Other overflow transitions are rejected with backpressure/unknown quality;
  resource recovery is not accepted without room for its event. No producer waits.
- Dispatch is explicit and serial. Persist the attempt reservation before sink
  IO; only an exact delivery receipt removes it. Persist failure before sending
  sends nothing. Post-receipt persistence failure restores the reserved delivery
  for possible replay with the same ID. Bounded retries/dead letters are retained;
  later resolution cannot overtake an unconfirmed/dead earlier event for that key.
  This permits duplicates and never claims network exactly-once or guaranteed
  success within the retry budget. A crash may consume a reserved attempt; its
  unconfirmed event remains retained/dead, never marked delivered.

## Checkpoint boundary

Version1 stores incident and delivery metadata only: no values/rate samples,
sequence/run/dwell history, SQL/outbox/block payload, cursor or source ACK. Restore
retains unresolved incidents, deferred criticals and delivery IDs/attempts, and
resets temporal baselines. Restore/Load initialize a fresh Engine only; live
reloading is refused. Missing/corrupt/unsafe/mismatched input is state-lost,
retains any known in-memory critical and blocks automatic apply/save/dispatch.
It is not a recovery event or permission to overwrite unknown history. A valid
initial checkpoint or explicitly fresh owner is required; no operational
reinitialization/recovery command is added.

The Unix file store holds a no-symlink directory descriptor, exact owner0700
directory/0600 regular single-link file checks and a nonblocking writer flock.
It creates no directory. `alerts.json` is fixed; paths are never read from input.
Strict bounded JSON rejects missing/unknown/duplicate/null scalar/trailing/deep
or malformed fields, oversized arrays/files and invalid event/counter/order data.
One existing checkpoint must be read before replacement; content changes are
fenced. Corrupt/linked input is not automatically replaced.

Publication uses an owned random exclusive temporary, file fsync, atomic rename
and directory fsync. Prepared/cancelled failure preserves old data and removes
only that temporary. Failure after rename is durability-unknown, not success.
No absolute path is reopened after descriptor admission. This is private local
metadata validation and cooperative ownership, not authentication against an
arbitrary same-UID editor or proof of physical disk/host behavior. Other platforms
fail closed. Blocking OS IO or a cancellation-ignoring sink is not forcibly killed;
future D owns the separate worker and its lifecycle, never the source writer.

## Actual checks

Final source digest (sorted relative path, NUL, file bytes, NUL across16 Go files):
`e500f2fe9822e7c90e77d9d34290225b608c2a44b86035b811ca0f3652309f3d`.

Focused functional/race checks pass for nativealerts and the connected observation
contract. Scoped build/vet/lint pass with zero final lint issues; a tagged-switch
staticcheck suggestion was corrected. One targeted CPU1/race/count200 invocation
passes the new private-directory writer-contention case only; B's concurrency
campaign is not repeated. The reducer/queues have no shared concurrent mutation
or new production goroutine/timer. All files, clocks, sinks and faults are owned
synthetic fixtures; no actual DB, source node, peer, sender or performance run.

Docs checks pass:12 selected documents/277 relative links/zero errors; existing
CI policy24 pass. Whitespace/source/config checks pass. Secret checks pass: staged0; reachable history15 previously reviewed public
fixtures, unreviewed0. Actual
exact-head short CI, squash/reviewed tree and clean-main identities are verified
in Git/PR metadata and on the next session reconciliation. Current E1/N6 plans and
n/n−1/n−2 context remain linked from SESSION_STATE.

Pool4/part2/API1, physical RPC5, seven logical writers, two normal EVM executions
and final NEW canonical/fence/COMMIT/exact ACK stay unchanged. No existing runtime,
schema, config, workflow or source logic is changed; the new module is not wired
into the App. Other projects, format/operating/GC/restore/performance HOLDs and
missing APFS/enode/authenticated checkpoint/private deployment inputs remain.

## Next

[MON-02-D](../20261102-mon01-observation-design/implementation.md#mon-02-d--opt-in-app-assembly와-sink-경계): disabled-by-default configuration/App assembly, qualified source binding,
worker cancellation/join and a separate operator transport boundary. Do not reuse
chain notification settings/outbox or enable real destinations/senders implicitly.

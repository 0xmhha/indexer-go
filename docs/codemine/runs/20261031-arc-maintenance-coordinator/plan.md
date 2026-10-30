# App/E3 maintenance phase coordinator

Current n follows managed COW/floor #254 (n−1) and graceful drain #253 (n−2).
Preserve [N5](../20261030-arc-native-n5/plan.md), [accepted first-import](../20261029-arc-first-import/plan.md)
and [E1](../20261023-e1-execution/02-design.md). No operational activation.

Expose optional explicit App maintenance/recovery methods and an Arc offline
recovery library accepting the caller's existing Store/canonical reader. No
CLI, timer, dialer, pool or eighth writer. Freeze and join via the existing drain.

Persist one bounded canonical sealed journal outside COW storage, under a
private ownership lock and atomic file+directory fsync. Bind data paths/volume,
SQL owner, native proof, exact ACK, binary digest and normalized bounds. Record
prepared → authorized → raw_complete (including zero deleted) → cow_complete
→ db_complete → complete. The final phase means files and SQL are released and
normal startup may revalidate; it is not a claim that a process actually resumed.
Retain the latest completed journal; replace only after a new verified drain.

Prepared stores the prior SQL token: uncertain authorization may adopt only the
exact newer pending request. SQL completion cannot be inferred from journal or
absent files. Raw recovery/collection precedes native/source mutation. Persist
raw_complete under the existing E3 live lease even for zero candidates. Native/
source COW uses that same current lease and real receipts. DB completion retains
NEW uncached fresh response and generation fence. Add a same-pool completed-E3
EX callback for exact file release; this excludes publication/reorg while files
and the final journal are released. Never run RPC in that callback.

App and receiver remain alive while parked. Serialize local resume/reorg and
maintenance; link cancellation to the owned Run and join all children before
return. Block public Resume/startup on an unfinished/corrupt journal. Offline
recovery starts no App, peer or node and resumes no worker. A complete journal
never authorizes another deletion or replay of an old native permit after head
advances. Missing or mismatched evidence remains HOLD.

RED→GREEN: phase/zero-GC durability, interrupted authorization/COW/completion/
file-release recovery, exact ownership, source admission, old permit refusal,
App forwarding, cancellation/join, actual PG completion lease. Tests use owned
files, protocol helpers and disposable fsync-on PG. SDK/App combined N6 remains
separate. Target only new concurrency for local CPU1/race/200; scoped short CI
unchanged. Build/fmt/vet/lint/docs/leak checks, CI-gated squash, clean main sync.

Preserve pool4/part2/API1, writers7, two EVM passes, physical RPC5 and final fresh
→ generation fence → COMMIT → exact ACK. Actual nodes/peers, format, deployment,
operational GC/restore and additional performance measurements stay HOLD.

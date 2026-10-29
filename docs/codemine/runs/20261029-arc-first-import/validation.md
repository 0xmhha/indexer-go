# Design validation

Base main: `934f9da34496cb526393ca2e7b9a0d30848c2fdb` (#238).
This change confirms design and a future N0-N6 implementation plan. It does not
add executable behavior, a migration, config keys, runtime mounts or a checker.
There is no feature RED/GREEN claim for documentation. Each future feature
requires its own requirement-failing test and GREEN implementation evidence.

[Source audit](source-audit.json) records exact pins, selected upstream blobs
and unchanged cached source checks; provider/bootstrap excerpts were also
checked against GitHub primary sources at the same commit. It proves the APIs
and multi-store ordering described in the design, not the proposed writer's
compile/bootstrap/recovery/RO behavior. Those remain explicit N0-N6 acceptance.
No upstream code or shared cache was changed. Current runtime declaration still
has no Arc first-import provider and is not silently upgraded by this document.

The unchanged Go/module hash is expected to remain
`b8da45385fbb6a218fb63f22b7d1517113acfbfa65f87c7323c5e98d08a98f92`.
The prior complete actual local race evidence on those exact bytes is
[offline-monitoring/checks.json](../../../../verification/offline-monitoring/checks.json),
race log SHA256
`19999d4fc634883bdecf2a8ac0e652371edb075f7a2abf445b9f06a15951bf89`.
This is reused evidence, not a new full Go/race/native/PG campaign. A new exact
PR head still needs all five actual CI jobs before guarded squash/main sync.

Local checks passed251 relative document links and4 anchors with no broken
targets; all JSON records parsed. Ignored config bytes/mode and five runtime/CI
implementation files matched the base. The exact Go source identity, retained
race log digest and successful race receipt above were checked. No new
concurrency test or local count200 campaign is introduced for documentation.

Staged secret scan found0 findings; reachable history had15 previously reviewed
public fixtures and0 unreviewed findings. The history scanner's raw exit1 is
preserved and is not a zero-findings claim. No new allowlist was added. Source
and secret verification receipts are retained in the task's private temporary
ledger. PR checks/merge metadata own the final CI/squash result; this pre-CI
document does not certify a future run.

Other projects, private settings and Docker resources remain unchanged. No
App/node/native DB, peer/RPC/SQL, Docker/image, provisioning/format, snapshot,
operational GC/restore or additional performance measurement was executed.

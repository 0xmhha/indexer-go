# MON-03: bounded Docker VM probe/health library

Base main `e16c337e515ed2280a9c6624a31d40edd37594cb`, branch
`refactor/docker-observer-lifecycle`. Follow the [accepted MON-03 contract](../20261102-mon01-observation-design/implementation.md#mon-03--docker-vm-source와-observer-health)
and preserve [D](../20261102-mon02d-app-assembly/validation.md)/[C](../20261102-mon02c-alert-state/validation.md).

Implement a caller-owned `internal/vmobserve` library with an injected Runner
and fake-only tests. No OS/exec/Docker implementation, App/config activation,
probe/container, provisioning or real network sink is added. No chain is run.

The result parser accepts exactly a bounded POSIX df -P -k root report with a
recognized KiB header and one numeric row. Strict decimal/range/usage/mount/unit
checks and checked multiplication distinguish known zero free bytes from malformed,
permission/daemon/timeout/overflow/missing observations. Never retain stderr/errors,
filesystem names, Docker context, endpoints or credentials in status/health.

A unique opaque job nonce and bounded output writers belong to one admitted
attempt. At most one Start/Wait/Kill/cleanup lifecycle is active; context values
are stripped. Cancellation/deadline/output overflow cancels the process, joins
its actual Wait, then independently checks exact owned cleanup confirmation.
An ambiguous start/identity/cleanup outcome fences subsequent attempts as pending;
CLI exit alone never confirms container deletion. No TTL/reset/prune escape.
Fake adapters must model those distinct capabilities; real adapters still require
owned labels/private cidfile/identity verification and durable crash admission
before operating activation. The in-memory library makes no cross-process claim.

Expose immutable sample and separate observer-health assessment: original data
acquisition time never freshens at dequeue/assessment; no heartbeat or elapsed
probe deadline is UNKNOWN/degraded, not known0/full/healthy. Caller-driven periodic
Run and direct Close cancel/join honestly, including late Start/Wait/cleanup and
never-started/concurrent Close. A cancellation-ignoring adapter remains occupied.
Health assessments have no halt/Resume/SQL/ACK/GC/send authority.

First compiled RED: malformed/permission/overflow df output must not become known
zero. Lifecycle RED: absent heartbeat must not be healthy and pending/overlap must
not start a second job. Then implement GREEN; test wrong nonce/receipt, late result,
stdout/stderr caps, exact cleanup, context stripping, cancellation and worker join.
One final new-case CPU1/race/count200 campaign, root build/vet/lint, docs/policy/
secrets, normal PR, actual exact-head short CI, squash and clean main are required.
No DB/native/workload/host probes are warranted by this independent library.
Preserve pool4/part2/API1/seven writers, physical RPC5, two EVM executions and
final NEW canonical/fence/COMMIT/exact ACK; other projects/resources stay unchanged.

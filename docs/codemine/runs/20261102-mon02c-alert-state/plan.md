# MON-02-C: alert transitions and private delivery state

Base `802d97751342bbc100387c09e2da818f01f1166f`, branch
`refactor/observation-alert-state`. Follow the [accepted C contract](../20261102-mon01-observation-design/implementation.md#mon-02-c--경보-reducer와-private-checkpointqueue)
and [observation design](../../../NATIVE_MONITORING_INTEGRATION.md).

Implement a pure reducer and single-owner bounded alert/delivery state. Critical
escalates immediately; warning and recovery require count plus dwell, with
distinct clear thresholds. Unknown, sequence gaps, out-of-order input, restart
or scope/series/policy changes never resolve an active incident. Stable transition
IDs and cooldown suppress duplicates. Keep quality distinct from resource level.

First compiled RED: an unknown sample must not resolve an active critical, and
a restart/sequence gap must not discard it. Add functional REDs for queue receipt
failure/backpressure and private checkpoint failure/validation before GREEN.

Use bounded value-only events/queues and a fake/disabled sink. Private atomic
checkpoint stores alert/delivery state only, never samples, block/SQL payloads or
source ACKs. Corrupt/missing files mean state lost/unknown. Restore resets dwell,
sequences and rate baselines; retry keeps transition IDs (at-least-once).

This slice is explicitly driven by its caller: no App/config activation, new
goroutine, timer, sampler, pool, RPC, network sender or authority over indexing.
Validate with fake time and owned private files, focused race/build/vet/lint,
docs/policy/secrets and applicable short CI; then squash and clean main. Do not
repeat B's concurrency200 or actual DB/native/performance suites. The new
private-directory writer flock gets its own one-case CPU1/race/count200 gate. Preserve pool4/part2/API1, RPC5 and final NEW
canonical/fence/COMMIT/exact ACK. All operating/input HOLDs remain unchanged.

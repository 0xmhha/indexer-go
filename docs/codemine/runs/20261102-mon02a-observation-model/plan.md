# MON-02-A: pure observation qualification

Base `f81f28873ba271ae9028d19f02ba20634c8a8ff0`, branch
`refactor/observation-model`. [Accepted design](../../../NATIVE_MONITORING_INTEGRATION.md)
and [slice contract](../20261102-mon01-observation-design/implementation.md#mon-02-a--구현-순수-관측-모델과-qualification)
remain authoritative.

Implement typed, value-only observations and a bounded immutable binding set.
Qualify each component using its own acquisition time, resource/measurement/budget
scope, source incarnation/run, sequence and explicitly distinct position domains.
Unobserved values never become zero, lower bounds never become exact progress,
and known conflicts remain errors when other fields are missing. Extract the
existing exact product predicate for shared use without changing v1 CLI behavior.

First compiled RED: database-wide bytes must not bind to two per-chain schema
budgets; a stale WAL sample must not inherit a current DB sample's time.
Additional tests cover caps, independent errors, numeric boundaries, copy/alias
safety, temporal pair resets and typed native/capture epoch separation.

No goroutines, queues, files, DB/RPC clients, configuration, App wiring or senders.
No actual deployment, formatting or performance measurement. Existing pool4,
RPC5 and final NEW canonical/fence/COMMIT/exact-ACK paths remain unchanged.

Run focused RED→GREEN and race, scoped build/vet/lint, documentation/policy/secret
checks, then the existing applicable exact-head short CI. No new concurrency200,
native rebuild or local DB campaign is needed for this pure layer. Squash and
clean-main sync precede MON-02-B.

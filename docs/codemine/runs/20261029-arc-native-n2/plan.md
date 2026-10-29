# N2 first-execution native writer

Implement N2 of the [accepted first-import plan](../20261029-arc-first-import/plan.md)
after N1 CI5/squash/main synchronization. Keep two normal EVM executions, fixed
pins, existing pool4/part2/API1/writers7, shared RPC5, final NEW fresh and exact
receiver ACK authority. Start no node/peer/App/SQL service, provision nothing and
perform no additional performance measurement.

Require one writer, E3 shared run lease, fair read/publication gates, one actual
Arc execution and Full outcome persistence, durable bound intent/completion,
exact duplicates and bounded owned stdio. Test real interrupted SDK multi-store
boundaries with owned children and fail closed on ambiguous state. Do not patch
upstream, self-repair partial stores, silently downgrade managed reads or grant
E1/GC authority from native completion.

Each feature uses RED→GREEN. Update current docs/checklist/n/n−1/n−2, verify
local gates and secret checks, require actual final-head CI5, expected-head squash
and clean main synchronization before N3. See [writer contract](../../../EXECUTION_ARC_NATIVE_WRITER.md)
and [validation](validation.md).

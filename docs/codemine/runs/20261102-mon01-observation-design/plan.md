# MON-01 observation and alert reuse design

Selected task: [MON-01](../../../analysis/open-items.md#mon-01), DESIGN.
Base main5250f4d3753e5321aae9736991922345b55b75c1. The user asked to proceed
from the tracked handoff. This task designs integration; it does not implement
or run a collector, watcher, sender, database/RPC probe or actuator.

Acceptance:

1. Audit existing capacity guard, attached PostgreSQL statistics, Store/read
   budgets, feed/pipeline diagnostics, offline evaluator and notification paths.
   Separate verified code facts, missing interfaces and untested operating facts.
2. Map each proposed value to its source, unit, resource/chain/schema scope,
   provenance/time, error/unknown meaning, ownership and budget. Never equate
   global DB/WAL bytes with a per-chain allocation or native/raw/SQL epochs.
3. Specify bounded acquisition → immutable observations → pure evaluation →
   alert transition/sink contracts, preserving existing guard actuation and
   public canonical/SQL/ACK/GC authority. Reuse available work before adding IO.
4. Define series resets, timestamps, stale/out-of-order/partial observations,
   backlog/ACK limits, cancellation/join/store-close, queue bounds, hysteresis,
   restart and redaction. Choose conservative recommended decisions without
   inventing deployment input values or claiming live validation.
5. Break MON-02 and MON-03 into concrete changes and behavioral RED→GREEN
   cases, with scoped local/CI validation and explicit activation dependencies.
6. Update current state/backlog/guide navigation and retain n/n−1/n−2 plus
   accepted E1/first-import plans. Validate docs/links/source consistency and
   existing CI policy, whitespace/secrets; docs-only requires no invented RED,
   compiler/DB/native/race200/performance run.
7. Explicit stage, Conventional Commit, normal PR, actual applicable exact-head
   docs-only CI, squash and clean main synchronization. Actual release IDs
   belong to PR metadata and the following handoff, never a pre-certified box.

Keep pool4/part2/API1/seven writers, physical RPC5 including retry/fallback,
two normal EVM passes and final NEW canonical/fence/COMMIT/exact ACK. Keep
parameter-bound SQL, original anchor/full-baseline/epoch/lease/GC boundaries.
APFS/enode/authenticated checkpoint/private deployment/backup inputs and format/
operating execution/extra performance remain HOLD. Other projects stay untouched.

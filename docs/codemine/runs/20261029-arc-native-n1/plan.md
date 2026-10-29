# N1 authenticated native bootstrap

Implement milestone N1 of the [accepted first-import plan](../20261029-arc-first-import/plan.md).
Require an independently trusted full anchor, exact state/code/storage/preimages,
a new private staged native-v2 DB, full root verification, original anchor/floor
and atomic no-overwrite publication. Require authentic contiguous header context
and actual anchor body/receipts above genesis; no invented pre-anchor coverage.

Use fixed Arc/Reth pins without upstream edits. Start no node, SQL application,
peer or operational restore; do no provisioning or performance measurement.
Temporary native library fixtures are the only executions in this feature.

Write a meaningful requirement-failing test, implement, prove GREEN; update
current docs/checklist/continuity. Review build/fmt/clippy/tests and exact-byte
Go race reuse when no Go source changes. Exact-head CI5, guarded squash and
clean main sync are required before N2. Release metadata must be actual evidence,
not a pre-certified checklist claim. See [validation](validation.md).

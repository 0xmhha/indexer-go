# Operations input recommendations

The user selected the recommended next step. Finish the choices that can be
made offline; preserve HOLD for unavailable external resources. The current
[decision table](../../../OPERATIONS_READINESS.md) is the operator-facing source
of truth, rather than another executable manifest or configuration schema.

Scope: bounded read-only host/config metadata, recommended disjoint paths,
per-Store and aggregate PostgreSQL budgets, block/time/spool distinctions,
capacity accounting and authenticated checkpoint startup. No runtime code or
config/default changes are needed. Feature RED→GREEN is not applicable to this
documentation-only change; do not invent a new feature test or performance run.

Acceptance:

1. Separate observed metadata, recommended plans and actual unknown inputs.
2. Preserve the current config and all draft placeholders. Do not inspect
   credentials or referenced database/user data.
3. Do not infer native capacity from the legacy470GiB comparison. State that
   86400 retention blocks are not24h at the assumed2blocks/s;172800 is a proposed
   24h block count, not a measured throughput or an in-memory queue size.
4. Keep pool4/part2/API1/seven logical writers, physical RPC5 attempts/s,
   final uncached canonical→generation fence→COMMIT and two EVM executions.
5. Preserve n−1/n−2 and the accepted E1 design/plan via
   [continuity](continuity.json); update the [backlog](../../../analysis/open-items.md).
6. Check changed-document links, arithmetic, boundaries, whitespace and secret
   scans. Verify unchanged executable/config bytes against main and retain the
   exact-source prior local race evidence. Require the new head's CI5 before
   guarded squash merge and clean main synchronization.

No formatting/provisioning, app/node/peer/DB/Docker execution, operational
GC/restore, additional performance measurements or changes to other projects.
Unavailable APFS/enode/checkpoint/private inputs remain HOLD without another
questionnaire or invented live-readiness evidence.

See [checklist](checklist.md) and [validation](validation.md).

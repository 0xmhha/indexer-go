# Designs and validation

Use the [documentation index](../README.md) for current usage. This directory
contains architecture decisions and source-identified analysis; it does not own
the current implementation backlog or imply deployment readiness.

## Current state

| Need | Canonical document |
| --- | --- |
| Outstanding work and input/run gates | [Remaining work](open-items.md) |
| Continue the current task | [Session state](../SESSION_STATE.md), [handoff protocol](../SESSION_HANDOFF.md) |
| Current deployment inputs and recommendations | [Operating readiness](../OPERATIONS_READINESS.md) |
| Developer/API usage | [Documentation index](../README.md) |

Completed implementation tasks stay out of the outstanding list. Superseded
working narratives belong in Git history, not new dated handoff documents or
append-only usage guides.

## Accepted contracts

- [E1 decision](../decisions/20261023-e1-design/decision.md),
  [design](../codemine/runs/20261023-e1-execution/02-design.md) and
  [implementation plan](../codemine/runs/20261023-e1-execution/03-plan.md).
- [Arc native import design](../ARC_NATIVE_IMPORT_DESIGN.md) and
  [first-import plan](../codemine/runs/20261029-arc-first-import/plan.md).
- [Native observation contract](../NATIVE_MONITORING_INTEGRATION.md).
- [Chain profiles](chain-profile-design.md), [feature registry](feature-registry-design.md),
  [reorg](reorg-design.md), [PostgreSQL undo](postgres-undo-design.md) and
  [subscription architecture](subscriptions-design.md).

These accepted designs remain available for intent and safety boundaries. Read
the corresponding current guide/code for the implemented API and configuration.

## Scoped evidence

The [recent functional contexts](../SESSION_STATE.md) link their exact plans,
checklists and validation under `docs/codemine/runs`. Preserve those required
contexts and E1/first-import/N6 evidence; do not copy their execution logs into
README, operating input tables or the remaining-task list.

[Arc peer capability](arc-peer-capabilities.md) and
[chain identity evidence](arc-testnet-identity.md) record the sources they audited.
They do not authenticate a current peer or replace an operating checkpoint.
The [graph index](graph/README.md) identifies the source behind its generated
analysis. Dated measurements/analysis describe their original inputs only;
current support and task status come from the maintained guides and backlog.

# Current continuity

- n: [operating-input handoff](plan.md) and [checklist](checklist.md).
- n−1: [N6 acceptance](../20261031-arc-native-n6/plan.md),
  [checklist](../20261031-arc-native-n6/checklist.md),
  [qualification](../20261031-arc-native-n6/validation.md),
  [PR258](https://github.com/0xmhha/indexer-go/pull/258),
  [CI334](https://github.com/0xmhha/indexer-go/actions/runs/41162469157).
  Squash/main a282758b92a40f8cd46fee34d3e648f7a6b855a7; all six actual jobs
  succeeded. The heavy SDK/App/PG qualification was local; it was correctly
  skipped by scoped short PR CI.
- n−2: [managed inspection](../20261031-managed-inspection/plan.md),
  [checklist](../20261031-managed-inspection/checklist.md),
  [PR257](https://github.com/0xmhha/indexer-go/pull/257),
  [receipt](../../../../verification/native-n6/inspection-release.json).
- Preserved: [runtime alignment](../20261031-native-runtime-alignment/plan.md),
  [first-import](../20261029-arc-first-import/plan.md),
  [N5](../20261030-arc-native-n5/plan.md),
  [E1 design](../20261023-e1-execution/02-design.md),
  [E1 implementation](../20261023-e1-execution/03-plan.md).

Keep Store4/part2/API1/seven logical writers, shared physical RPC5, two EVM
passes, immutable original anchor and final uncached canonical/generation
fence/COMMIT/exact ACK. Actual APFS/peer/checkpoint/private deployment/backup
inputs, operating execution, format and extra performance remain HOLD.

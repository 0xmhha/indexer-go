# Native diagnostic Docker wiring

Base main63fafea / Native diagnostics #233. Implement only offline Docker
diagnostic wiring; actual image/container/node/DB/provisioning/performance work
remains HOLD. Keep the legacy four-testnet composition and private DSN path.

1. Behavioral RED: unresolved draft declarations must not be PASS.
2. Separate fixed Arc/StableNet preflight and metadata services; network none,
   read-only roots/mounts, no secrets/dependencies, no host-path creation.
3. Bounded strict offline checker: FAIL dominates HOLD, no environment expansion,
   source traversal or Docker call. Known unsafe topology cannot hide in HOLD.
4. Build four actual host binaries and run synthetic composed bindings. Include
   Arc's sibling fence; preserve bytes/inodes and create no runtime records.
5. Fix local replace-module build input ordering and retain required CGO/BLS.
   Update current docs and continuity; freeze before contributor gates.
6. Explicit staging and staged/history secret checks; English Conventional
   commit/PR, final exact-head CI5, guarded squash and clean main synchronization.

[Current guide](../../../../deployments/native/README.md),
[checklist](checklist.md), [validation](validation.md), [continuity](continuity.json).
Preserve accepted E1 [design](../20261023-e1-execution/02-design.md) and
[plan](../20261023-e1-execution/03-plan.md), and the n−1/n−2 references.
Next: backup adapter, monitoring. Native runtime topology/activation stays held.

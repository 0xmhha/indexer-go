# Repository work instructions

## Start or resume

1. Work from this Git repository (`indexer-go`), not a sibling project.
2. Read [current state](docs/SESSION_STATE.md), then the selected ID in
   [remaining work](docs/analysis/open-items.md). Use the
   [handoff protocol](docs/SESSION_HANDOFF.md) for recovery and updates.
3. Inspect actual branch, HEAD, status and diff before editing. Recorded state
   is a snapshot; Git/PR/CI evidence determines what has actually completed.
   Preserve user changes and unrelated work. Never reset/clean to match a note.
4. Read the selected plan/checklist and previous two feature contexts linked
   from current state. Use [CLAUDE.md](CLAUDE.md) for architectural contracts and
   [CONTRIBUTING.md](CONTRIBUTING.md) for development/release checks.

## Continue within scope

- Record the task ID, intended behavior, acceptance/RED condition and next action
  before implementation. If only HOLD tasks remain, identify the missing input
  without repeating probes or inventing an optional extension.
- Keep one PostgreSQL pool4/part2/API reservation1 and seven logical writers;
  physical RPC5 includes retries/failover/batch fallback. Preserve two normal EVM
  executions and final NEW canonical → generation fence → COMMIT → exact ACK.
- Keep parameter-bound SQL and validated/quoted identifiers, original anchors,
  epoch/lease/ACK/full-baseline/GC gates. Offline diagnostics grant no authority.
- Disk format/provisioning, operating nodes/peers/deployment, operational
  GC/restore and extra performance are HOLD unless the user changes that scope.
  Do not change other projects or prune shared caches/resources.

## Verify and hand off

- Follow the relevant RED→GREEN and focused validation rules in CONTRIBUTING.
  Docs-only work uses links/anchors and existing CI policy tests, with no
  compiler/SDK/DB/race200 run. Do not repeat passed tests without a new reason.
- Stage explicit paths and use English Conventional Commits. With the user's
  existing release authorization, open a normal PR, wait for actual applicable
  exact-head short CI, squash merge and synchronize clean main. Preserve branches.
- Update current state and the task-ID backlog at checkpoints and before
  handing off. Keep n/n−1/n−2 and accepted E1/first-import plans linked.
- Store nonsecret status, release identities and validation scope in Git.
  Temporary files are optional diagnostics, never the sole continuation record.
  Do not commit DSNs, keys, tokens, private manifests or operating payloads.

# N5 held runtime composition alignment

Current feature n follows [App/E3 coordinator #255](../../../EXECUTION_MAINTENANCE_COORDINATOR.md)
(n−1) and [managed COW/floor #254](../../../EXECUTION_MANAGED_MAINTENANCE.md)
(n−2). Preserve [N5](../20261030-arc-native-n5/plan.md), the accepted
[first-import](../20261029-arc-first-import/plan.md) and
[E1](../20261023-e1-execution/02-design.md) plans.

Update only held declarations, their offline checker/tests and current guides.
Arc App owns ETH68 capture, raw window, Rust first execution and in-process
collector/owned Rust replay. Remove the external Arc collector service. Keep
StableNet App/executor ownership. Use existing same-pool4/part2/API1/writers7,
shared physical RPC5, two EVM passes and final fresh/fence/commit/exact ACK.

Declare Arc native parent RW (no nested node bind that pins a pre-COW inode),
source parent RW, independent raw volume RW and private peer key RO; App has
DB/RPC/peer egress. The current managed collector derives scratch from the source
spool parent, not a configurable /scratch mount. Charge that scratch separately
from source quota. Bind coordinator journal/run-lock siblings outside native
COW. Keep actual APFS/volume/UID/key/peer/fork/checkpoint/image/bytes HOLD.

Record raw + native/COW + source/scratch + DB/WAL + checkpoint/backup headroom
and R/N/P/source-ACK distinction. Diagnostics remain read-only partial evidence:
legacy collector file preflight does not certify managed App ingress, SQL E3 or
the coordinator journal. Do not imply such integration exists; N6 retains the
combined real SDK/App/SQL acceptance boundary. No production config is filled.

RED tests must fail against the current separate-collector/RO-node topology,
then GREEN verifies three services, exact mounts, limits, no child DSN/sidecar,
strict bounded schema, resolved synthetic HOLD, hostile files and no declared
path/process/network access. Run affected Python and existing owned host CLI
fixtures, docs/CI policy and leak checks. No new runtime concurrency or200 rerun.
After actual applicable short CI, squash and verify clean main exact tree.

No build/pull/start, node/peer/DB service, provisioning/format, operational GC/
restore, deployment, host-load screen or performance measurement. Existing
owned Go diagnostic binary builds for file fixtures are allowed; preserve other
projects, private config, shared caches and Docker resources.

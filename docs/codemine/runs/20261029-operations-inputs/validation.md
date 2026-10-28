# Validation

This is a documentation-only operating decision pass on base
`a49065d292258c0399296a5e1cc45989c295cbc8` (released #236).

2026-10-29 01:27–01:35 KST observations were separate read-only samples:
selected X31 diskutil/statvfs metadata; explicit path lstat; UID/GID; Docker
context selection; bounded regular current config parsed in memory with only
driver/path, pool size, RPC rate and indexer finality/workers/poll/start fields
emitted. Credentials/endpoints were not emitted or used; referenced stores and
directory contents were not opened.
No daemon/container probe, chain/RPC request, load sample or performance run.

Code checks: retention DefaultKeep86400, spool defaults4GiB/262144 records,
native COW free-space guard2×logical size+reserve and default reserve1GiB.
Planning arithmetic:2×86400=172800;86400/2=43200s=12h; four Store pools use16
connections; two additional single-Store pools use8, for24 within proposed32.
A four-chain API process adds16 instead of4 and requires a revised budget.

Whole Go/module source before this documentation change must match the released
`b8da45385fbb6a218fb63f22b7d1517113acfbfa65f87c7323c5e98d08a98f92`.
The [previous local checks](../../../../verification/offline-monitoring/checks.json)
include successful complete race on those exact bytes. Reuse is explicit;
this change does not claim another local full race or new RED→GREEN campaign.
No workflow, test, runtime config, draft or source bytes may change.

Current offline checks passed:135 relative links and2 anchors, continuity,
arithmetic, documentation-only scope and whitespace. Staged secret scan had0
findings; history had15 reviewed public fixtures and0 unreviewed findings
(raw history scanner exit1 remains distinct from a clean scan). The retained
complete race log SHA matched its published receipt. Current config bytes/mode,
all executable/test/workflow/config drafts and Go/module source stay unchanged.

The [checklist](checklist.md), PR/CI and Git metadata determine release status.
Actual final-head CI5 must complete before merge; prior CI is source evidence,
not a substitute for the new head's required checks.

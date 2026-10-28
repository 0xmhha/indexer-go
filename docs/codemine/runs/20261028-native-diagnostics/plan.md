# Native read-only diagnostics plan

Implement the next offline developer diagnostic after Native preflight #232,
using only this managed worktree. Inspect production APIs before choosing a safe
metadata path. Reuse source manifest/GC validators, native sealed fence/floor
schemas and typed checkpoint/retention exports; never run maintenance children,
open a DB, enumerate payloads or invoke mutating coordinators.

1. Compile a behavioral RED for unknown observations incorrectly returning ready.
2. Implement isolated Arc/StableNet metadata adapters and bounded strict sampling.
3. Compose original identity, ACK/generation, public parent/root, retained floor,
   pending transition and fence observations. Unknown HOLD never masks known FAIL.
4. Test positive/unknown/pending/conflicting/unsafe/changing observations, output,
   cancellation, actual binaries and byte/inode preservation; complete local review.
5. Freeze final Go sources, then serially run contributor gates with complete local
   logs and portable source/exit/log-hash receipts. Check documentation and secrets.
6. Stage only task files and make one English Conventional local commit. Hand off
   branch/commit/tree and receipts. No worker push, PR, CI poll, merge or main sync.

The [guide](../../../NATIVE_EXECUTION_DIAGNOSTICS.md),
[checklist](checklist.md), [validation](validation.md) and
[continuity](continuity.json) carry current behavior and honest evidence.
Accepted E1 [design](../20261023-e1-execution/02-design.md) and
[plan](../20261023-e1-execution/03-plan.md), E6 and call references remain preserved.
The parent owns exact-head CI5, guarded squash and clean main synchronization.

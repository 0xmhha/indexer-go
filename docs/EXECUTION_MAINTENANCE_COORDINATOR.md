# Explicit Arc execution maintenance

The opt-in coordinator connects the existing [worker drain](EXECUTION_DRAIN.md),
[raw retention](EXECUTION_RAW_RETENTION.md), [managed native COW](EXECUTION_MANAGED_MAINTENANCE.md)
and E3 completion/release protocols. It supports the audited Arc testnet5042002
layout. It is a library API, with no CLI, timer or automatic GC activation.
Actual nodes/peers, disk provisioning, operational GC/restore, deployment and
additional performance measurements remain HOLD.

## Entry points and ownership

`App.MaintainExecution(ctx, ingress.MaintenanceOptions)` forwards to the optional
`ingress.MaintenanceProgram`. Arc drains and joins native import and the
collector through the independently verified public/source ACK boundary N.
The receiver, API and existing Store stay open while ingress Run is parked.
The coordinator then performs explicit maintenance and verified resume.
`App.RecoverExecutionMaintenance` resumes an interrupted operation on that
parked owner. A failed operation preserves its journal and pending evidence;
ordinary Resume refuses unfinished or corrupt records.

For a stopped owner, `arcingress.Maintain` and `arcingress.RecoverMaintenance`
accept the caller's existing config, dependencies and raw-window options.
They start no App, receiver, importer, peer, RPC dialer or worker. Offline
recovery never resumes workers. Do not run these against another active owner:
the actual native run EX, source ownership and SQL E3 gates must all agree.

Supply an absolute owned maintenance executable, KeepBlocks, ReserveBytes and
Timeout. Defaults are172800 blocks,1GiB reserve and1hour; KeepBlocks must exceed
the128-block undo window and Timeout cannot exceed1hour. The executable must
be a regular owned executable, without hard links or group/world write access;
its SHA256 and normalized options are frozen for recovery. Paths must be
absolute, canonical and nonoverlapping. Volume root, identity and reservation
are bound too. These defaults neither size a production volume nor prove free
space; underlying quota and COW reserve checks still apply.

## Durable phases

One private sealed journal lives beside, **outside**, the native COW directory:
`<parent>/.<native-name>.arc-execution-maintenance`, with a sibling `.lock`.
The parent must be owned/private. Each update is bounded to16KiB, checksummed,
validated against the existing record and written with file fsync, atomic rename
and parent-directory fsync. Concurrent coordinators are excluded by the owned
lock inode. Missing or changed evidence is an error, never a repair instruction.

| Phase | Durable evidence and next action |
| --- | --- |
| `prepared` | Exact native I/head/root, SQL owner/policy, all-seven public boundary, actual source ACK, paths/options/binary digest and prior SQL token. Authorize E3 with a NEW uncached canonical response and post-response fence. |
| `authorized` | Exact new pending E3 permit. Recover an exact pending raw deletion ticket first, then collect eligible raw data. |
| `raw_complete` | Raw processing is complete, including **zero** deletion candidates. This phase is persisted under the current E3 live EX scope before native/source mutation. |
| `cow_complete` | Exact source/native COW completion receipt. Source ACK/digest and native proof must match the frozen boundary; COW runs under the existing E3 live scope. Complete SQL using another NEW fresh response and generation fence. |
| `db_complete` | SQL holds the exact completed permit/receipt. Enter a same-pool completed-E3 EX scope, check unchanged public/root/coverage and no raw pending ticket, then release exact native/source files. No RPC occurs inside this release scope. |
| `complete` | SQL completion and exact file release have finished. Normal startup/resume must still revalidate current state. This record alone does not mean workers actually resumed. |

An uncertain authorization adopts only the exact newer pending permit matching
the prepared request; an old completed token cannot be reused. An interruption
after SQL completion calls the existing idempotent completion check with fresh
validation. Partial native/source operations recover through their own exact
intents and receipts. Recovery never clears a fence by TTL, absent files,
`latest`, a fabricated zero value or a historical permit.

The latest completed journal is retained and can be replaced only by a new
independently verified operation. Re-reading a completed record after the head
advances is historical verification, not permission to rerun GC or old native
work. Recovery still requires the matching options/binary, owner and SQL receipt.
The read-only startup gate needs matching scope/owner/SQL completion; it does
not execute or require the historical maintenance binary. Normal prepared-state
validation separately checks the current raw/native/source state and floors.

## Cancellation and authority

App maintenance serializes local maintenance/resume and native reorg recovery.
Its context is linked to the owned Run. Stop cancels Run before joining; it does
not wait for the maintenance mutex while holding the recovery mutex. The owned
native child is canceled and joined before ownership is returned. Cancellation
preserves durable recovery evidence and never reports completion from an
unjoined child. A failure after drain leaves the owner parked or stopped.

No new pool, dialer, logical writer or EVM pass is introduced. Pool4/part2/API1,
seven logical writers, the two normal EVM passes and shared physical RPC5 remain.
App supplies the existing uncached canonical reader. E3 authorization/completion
use that reader and existing SQL fences; ordinary publication retains NEW final
canonical response → generation fence → COMMIT → exact ACK. All SQL data values
remain parameter-bound. The completed-release callback is synchronous, uses the
same bound transaction, performs no RPC and joins any owned children before return.

## Verification and remaining scope

Behavioral RED tests cover absent App delegation, missing release scope, zero-GC
phase persistence and startup/resume bypass after partial release. GREEN uses
owned Go protocol helpers, real source/raw files, actual ingress Run drain/resume
and Stop/join, plus disposable fsync-on PostgreSQL. The interruption matrix
covers every durable phase and uncertain side effect, including authorization,
raw/COW, SQL completion and file release. Changed binaries and corrupt journals
fail closed. The actual DB path checks pool4, two fresh calls and completed
recovery without another RPC/deletion; it does not claim a real Arc EVM run.

New live lifecycle/App and DB exclusion/cancellation cases receive targeted
local CPU1/race/single200 qualification. Default [CI](CI.md) remains scoped short;
this Go-only change does not rebuild the unchanged Rust SDK. The prior actual
SDK bridge qualification is recorded separately in
[managed maintenance](EXECUTION_MANAGED_MAINTENANCE.md). Combined App/mock ETH68/
actual SDK/SQL/GC acceptance remains N6. Held Docker/capacity/inspection
composition is the next N5 step. See the [plan](codemine/runs/20261031-arc-maintenance-coordinator/plan.md)
and [checklist](codemine/runs/20261031-arc-maintenance-coordinator/checklist.md).

# Maintenance coordinator validation

The [accepted plan](plan.md) precedes implementation. New tests first failed
behaviorally for absent App delegation, missing completed-E3 EX scope,
zero-candidate phase persistence and unfinished-release startup/resume gates;
no compile failure is used as RED proof. See
[RED receipts](../../../../verification/native-maintenance-coordinator/red.json).

Owned protocol fixtures pass every durable phase and uncertain side-effect
boundary: prepared, authorization side effect, authorized, raw side effect,
raw_complete, COW side effect, cow_complete, SQL completion side effect,
db_complete, file release side effect and complete. Recovery uses a new
coordinator instance, retains the exact permit and never restarts raw work
after COW. A completed record after a head advance performs no further GC.
Binary changes and corrupt journals are refused.

The real ingress Run fixture drains both owned workers, completes maintenance
and observes a newly started collector after resume. Stop during a blocked
owned native child cancels and joins the child/coordinator/Run, then offline
recovery completes without starting workers. The App test verifies optional
delegation while its receiver remains open.

Disposable fsync-on PostgreSQL verifies pending/wrong receipt refusal,
completed release exclusion against reorg, cancellation/join/lock return and
the complete coordinator file/SQL path using pool4. That integration observes
two fresh canonical calls and no raw deletion ticket for zero candidates;
recovery of complete state adds no RPC/deletion. SQL fixture public rows and a
Go protocol helper do not certify actual Arc EVM execution or combined SDK/App
acceptance. Existing E3/raw deletion DB regression passes once.

Affected Go short suites, root build and affected vet pass. Lint initially
reported redundant embedded field selectors in the test mock; only those
selectors were shortened and final lint passes. This does not change fields or
concurrency behavior. CI policy23 passes; default workflows remain unchanged.
New live ingress/App cases pass a single targeted CPU1/race/parallel1/count200
invocation (Arc728.352s/App1.984s). The new actual PostgreSQL release exclusion/
cancellation case passes its own single CPU1/race/parallel1/count200 invocation
(441.836s). These are functional race qualifications, not throughput results.
Changed documents plus entry points pass319 relative links with zero errors.
The [checklist](checklist.md) and [checks](../../../../verification/native-maintenance-coordinator/checks.json)
record local evidence; staged/history secret checks remain a required commit gate.

No Rust source/dependency changes: the actual SDK88 and inherited bridge
qualification from [n−1 #254](../../../../verification/native-maintenance-coordinator/managed-release.json)
is reused, not rerun or counted as this coordinator's combined SDK evidence.
Default CI is scoped short; actual applicable jobs must pass before squash.
The release head/tree/CI/main receipt is retained after merge; a pre-merge
document cannot contain its own final reviewed head without changing that head.

Pool4/part2/API1/writers7, two normal EVM passes, physical RPC5 and NEW final
fresh → post-response generation fence → COMMIT → exact ACK are unchanged.
Held Docker/capacity/inspection and N6 remain. No actual node/peer, format,
operational GC/restore, deployment or performance measurement occurred.

# Execution worker drain

`App.DrainExecution(ctx)` forwards to an optional `ingress.DrainProgram`.
`App.ResumeExecution(ctx)` resumes only after verification. Arc implements the
contract without a new RPC client, PostgreSQL pool, CLI flag or automatic GC.
Operational activation and performance measurements remain HOLD.

## Joined boundary

1. Serialize against native reorg recovery. Refuse an unstarted/stopped owner,
   another drain, SQL maintenance/rewind or an anchor without a real result ACK.
2. Fence input admission synchronously and cancel peer capture/watch work.
   An already admitted native import keeps its live round context and finishes.
   Join these workers before closing the peer/importer.
3. Read the actual sealed native head N, including its completion proof. Keep
   the collector and receiver alive while SQL publication catches up to N.
4. Observe the collector's durable source ACK. Exporter `OnCommitted` runs only
   after successful `Spool.Acknowledge`, or reports an existing durable source
   status. Hello/anchor without a digest and rejected ACKs never count. The
   round keeps an atomic latest observation, so no bounded notification queue
   can lose the final ACK. It performs no file/SQL/RPC operation in the callback.
5. Require matching chain, generation, height, hash and nonzero digest; SQL
   public root must equal N. All seven unique writer parts and idle cursors
   must agree on Ref/digest/parent. Reread SQL control to fence generation and
   publication changes. Missing/mixed/ahead evidence fails closed.
6. Cancel and join the collector, join origin loans and close the owned raw
   window. Park ingress Run without terminating App's receiver/API/Store.
   Return a `DrainPoint` only after these joins and resource release.

The request is bounded by its caller and the ingress timeout. Cancellation or
failure after the admission fence hard-stops and joins owned work. The join
occurs outside the recovery mutex, because Run cleanup uses that mutex too.
All durable pending evidence is preserved. A failed precondition does not
stop a running owner. Explicit Stop remains idempotent while parked.

## Maintenance and resume

A DrainPoint is an observation, **never deletion authority**. Native run EX,
source ownership, the existing E3 live lease, archive coverage and fresh
canonical checks are still required by [raw retention](EXECUTION_RAW_RETENTION.md)
and the managed native maintenance bridge. No permit is inferred from N alone.

Resume refuses a SQL maintenance/reorg fence, changed public Ref/root/parts,
pending native/raw intent, replaced source identity or mismatching native
controls. It repeats the prepared-state inspection while parked, reopens only
the verified raw window, then releases the admission fence. A failed inspection
keeps Run parked. Run cancellation and hard Stop still terminate it promptly;
maintenance parking has no reorg resume timer.

The receiver stays open throughout drain/parking. Pool4/part2/API1 and the
seven logical writers are unchanged. Normal processing still uses two EVM
passes; physical HTTP attempts share RPC5. Block publication still requires a
NEW canonical response immediately before its generation fence/COMMIT/exact
ACK. Drain adds neither a cached substitute nor a publication path.

## Verification and remaining scope

Behavioral RED reproduced the absent durable observer, missing drain behavior
and absent App delegation. Go fixtures exercise the real owned round, active
import completion, delayed publication/ACK, mixed writer refusal, verified
resume, parked Run and cancellation/join. They start no node or peer network.
Targeted new concurrency uses one local CPU1/race/count200 invocation per
scope; default CI stays [scoped short](CI.md).

This feature alone does not authorize native COW. The [inherited run lease/managed SDK bridge](EXECUTION_MANAGED_MAINTENANCE.md)
is implemented separately. The App/E3 coordinator, held Docker draft and
combined actual SDK/App/SQL N6 acceptance remain distinct work in the [N5 plan](codemine/runs/20261030-arc-native-n5/plan.md).

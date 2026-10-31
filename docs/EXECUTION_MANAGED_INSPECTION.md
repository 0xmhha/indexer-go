# Managed Arc execution inspection

This is an offline metadata diagnostic for developers operating the opt-in
`arc.native` App. It does not start an App, node or peer, open native/SQL stores,
run children, create locks or recover/delete anything. The report always stays
HOLD or FAIL and grants no start, ACK, GC, recovery or restore authority.

## File preflight

`arc-execution-preflight` now links the existing Arc ingress registration through
an optional product hook. It compares the App's selected native/import/replay,
source spool, raw window, socket, peer/network declarations and prepared-only
flags with the explicit native/retention/checkpoint/declaration files.
`native.json` remains a collector-shaped review declaration; it is not a second
runtime collector configuration.

Use the five files in
[`synthetic-managed-arc`](../configs/execution/preflight/synthetic-managed-arc/app.yaml)
with the flags in [file preflight](NATIVE_EXECUTION_PREFLIGHT.md). The synthetic
fork, loopback peer, quotas and anchor are test inputs, never operating values.
They return HOLD/2. No declared data path, executable, key or endpoint is opened.
Unresolved selected path, fork, network and peer values produce HOLD in detached
validation copies; the input files remain unchanged. Typed numeric budgets,
selected window/volume references, known conflicts and invalid limits fail.
`initialize_volume` and `create_window` must both be false.

The exact managed scratch directory is `dirname(collector.spool_dir)`. Only this
source-parent containment is permitted; other path overlaps still fail. Declared
backup headroom includes native + source + raw quota + indexer. Declared free
headroom includes two native copies, two source quotas, raw quota, reserve,
indexer, checkpoint, scratch and backup. These are overflow-safe declarations,
not measured APFS capacity or a 24-hour retention guarantee.

## SQL export from an existing Store

[`ingress.Inspector`](../pkg/execution/ingress/inspection.go) is an optional
interface implemented by the PostgreSQL Store. Call it through an already
prepared Store; there is no DSN-taking exporter command or new pool:

```go
snapshot, err := inspector.InspectExecutionIngress(ctx)
if err != nil {
    return err
}
encoded, err := json.Marshal(snapshot)
if err != nil {
    return err
}
// Save encoded to an explicitly chosen private artifact using the caller's
// existing private-file publication policy. Do not log its contents.
```

The method borrows an ordinary connection or the existing reserved query-reader
loan in `ctx`. It runs fixed/parameter-bound reads in one REPEATABLE READ,
READ ONLY transaction, then rolls back before returning the loan, including on
cancellation. Calls nested in an execution read/transaction context are refused.
It opens no pool, performs no migrations/DML, and takes no maintenance lease.
Pool4/part2/API1 and seven logical writers are unchanged.

The bounded export contains public control, current ingress policy/binding,
latest retention permit/completion and a validated raw deletion summary. It
never includes raw references or payloads. Absence is an explicit null/false
**within that SQL snapshot**. `DecodeInspection` accepts at most64KiB and requires
complete canonical `json.Marshal` output, including explicit null/false fields;
pretty printing, omitted fields, unknown fields and invalid digests fail closed.
The artifact has no external authentication or acquisition timestamp. Matching
it to files does not make it current, fresh or atomic across stores.

## Explicit metadata selection

```sh
build/arc-execution-diagnose \
  --native /native/node \
  --spool /source/spool \
  --raw /raw/arc-raw \
  --ingress-artifact /observations/ingress.json
# HOLD/2 for compatible partial evidence; FAIL/1 for known unsafe/conflicting data.
```

Either new flag selects the managed inspection callback. Existing flags retain
their legacy observations; StableNet refuses the Arc-only selections.
The command reads only fixed private native identity/head/bootstrap/current
receipt, raw STORE/OWNER and the sibling
`.<native-basename>.arc-execution-maintenance` journal. It does not follow paths
inside the journal. It samples source admission/ACK digests and the selected SQL
artifact. No writer/run lease is acquired and no payload/native tree is scanned.
Readers reject unsafe links/ownership, check canonical seals and resample opened
metadata. In-flight change or missing metadata stays HOLD; known corruption and
immutable chain/instance/anchor/bootstrap/source-policy/owner conflicts FAIL.

The report keeps received R, native N and SQL public P independent. Input refs
have height/hash/root without invented SQL generation. Raw epoch, native import
epoch, native read epoch and SQL generation are separate fields. A completed
journal's frozen point is historical; it is not the current native head or proof
of worker resume. Mutable epoch/progress/permit differences across samples are
HOLD, not permission to reconcile or repair them. An OWNER temporary file is
positive pending evidence; its absence does not establish global GC state.

Full SDK state, complete epoch lineage, payload coverage, quotas, SQL part
coverage and live canonical state are uninspected. Ordinary NEW uncached fresh
canonical query → post-response generation fence → COMMIT → exact ACK remains
mandatory. Two EVM executions and the shared five physical HTTP attempts/s,
including retries/failover/batch, are unchanged.

The [diagnostic Docker draft](../deployments/native/README.md) provides explicit
read-only raw/source/native/export bindings with network disabled and no DSN.
The [offline monitor](NATIVE_OFFLINE_MONITORING.md) accepts the added report
fields, but still evaluates only saved observations. Neither starts a chain.

## Qualification and next step

[Plan/checklist](codemine/runs/20261031-managed-inspection/plan.md) and
[validation](codemine/runs/20261031-managed-inspection/validation.md) retain
RED→GREEN and release scope. The discovered reserved-reader wait has a real
PostgreSQL regression and one CPU1/race/200 qualification. It reuses the existing
loan instead of waiting for a fifth connection.

[N6 combined App/mock ETH68/actual pinned SDK/SQL/GC acceptance](EXECUTION_ARC_ACCEPTANCE.md) is qualified using owned fixtures. Actual APFS/enode/authenticated checkpoint, operating nodes, format,
provisioning and additional performance measurements remain HOLD.

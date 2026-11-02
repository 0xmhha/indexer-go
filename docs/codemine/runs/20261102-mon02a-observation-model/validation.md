# MON-02-A validation and continuation

## Behavioral evidence

The first compiled `go test ./internal/nativeobserve -count=1` failed both
`TestDatabaseCannotBindToPerChainSchemaBudgets` (no `scope_mismatch`) and
`TestEachComponentRetainsItsOwnFreshness` (no `sample_stale`). The compiling
permissive scaffold admitted the bad bindings and stale sample. GREEN rejects
both. This was a behavior failure, not a dependency/cache/compiler failure.

Review found two additional gaps. Compiled RED cases rejected the implementation
that mislabeled an absent failed-query position as a known conflict and accepted
`MaxUint64` as a native height. GREEN distinguishes absent evidence from a known
position conflict and checks signed native height/anchor limits independently.
Another compiled RED showed a missing acquisition time masking a known ref
below/conflicting with its original anchor. GREEN records both findings.
A further RED caught valid history being incorrectly subjected to the current
sample's age window (one-minute history with ten-second current freshness).
GREEN checks original historical quality/time and the declared pair interval;
it never projects the old sample as a fresh current component.

## Implemented contract

[`nativeobserve`](../../../../internal/nativeobserve/types.go) contains value-only
resource/metric/unit/definition/budget/source/run/chain/epoch DTOs and a catalog
that copies at most1024 keys across64 resources. Opaque fixed-size IDs do not
provide authentication or physical-resource discovery. A resource ID cannot bind
to inconsistent ownership/budgets; duplicate keys fail even if identical.

Database logical bytes, cluster WAL, PGDATA allocated bytes, raw/native/spool,
filesystem free bytes and pipeline heights retain different definitions. Global
DB/cluster usage cannot have a chain/schema allocation. Schema measurement is
reserved and always unsupported. No sum/deduplication across physical stores is
inferred from a name or digest.

Each sample is checked against its exact immutable binding and its own acquisition
interval/time, sequence, quality, cause, numeric presence, coverage and typed
epochs. A restart ACK seed may retain its ref without an event time; observation
time never becomes progress time. Missing differs from zero; negative, fractional,
oversized and overflowing decimal inputs fail without echoing raw text. Missing
time/position does not invent a known contradiction; independent known errors
remain visible. Lower-bound/truncated/unverified progress is unusable as exact
evidence. There is no head substitution or cross-stage backlog arithmetic.

`CompareGrowth` independently checks current and previous samples, requiring the
same whole binding, consecutive per-key sequence, valid positive interval and
actual growth. Current age and historical pair interval are separate checks;
explicitly stale/failed history remains unusable. Scope/source/incarnation/run/series/policy/budget/epoch changes reset
the baseline. Stale/missing/flat/shrinking/invalid pairs yield no usable forecast,
never unlimited headroom or an alert resolution. It cannot compare height
metrics. `Growth.Within` returns `(reached, known)`, preserving unknown separately
from a valid result outside the horizon.

[`monitorpredicate.ProductGE`](../../../../internal/monitorpredicate/product.go)
is the unchanged128-bit comparison extracted from the v1 evaluator. Budget and
horizon checks reuse it without fake chain identities or rounded forecasts. An
independent `math/big` oracle covers full-width products; existing v1 monitor/CLI
and Arc/StableNet diagnostic compatibility cases pass.

## Verification scope

Final code digest (SHA256 over sorted relative paths, NUL, file bytes, NUL):
`a4a69738d134447cec0d8c89ea60fb9726153f672a844212462a37c3d7116002` across11 Go files.

Passed focused functionality and race checks cover `internal/nativeobserve`,
`internal/monitorpredicate`, `internal/nativemonitor` and the thin
`cmd/execution-monitor` package. Scoped build/vet/lint and formatting pass; lint reports zero issues.
Docs checks:12 selected documents/274 links/zero errors; existing CI policy24 pass.
Secret checks pass: staged0; reachable history15 reviewed public fixtures,
unreviewed0. Private config hash/mode are unchanged.
Exact-head release checks and squash/main identities are verified in Git/PR/CI;
this document does not pre-certify a future merge.

No new shared mutable state, goroutine, IO, file, queue, SQL/RPC query, pool,
configuration, schema, App wiring or sender is added. New concurrency200, local
PG/native SDK and performance campaigns are not applicable. Existing short CI
still exercises its durable core DB smoke; that is not operating validation.
Private config hash/mode, pool4/part2/API1, physical RPC5, two EVM passes and final
NEW canonical/fence/COMMIT/exact ACK remain unchanged. Other projects and all
format/deploy/node/peer/operational restore/GC/performance HOLDs remain intact.

## Next slice

MON-02-B: reuse completed owner results and the existing sampler, then test
nonblocking taps, source-token fencing, one sampler, deadline/acquire cancellation
and join-before-Store-close. No live configuration or sender is enabled by A.
Follow the [B plan](../20261102-mon01-observation-design/implementation.md#mon-02-b--기존-owner-결과-tap과-통계-sampler-소유권)
and [current state](../../../SESSION_STATE.md); preserve accepted E1/N6 plans.

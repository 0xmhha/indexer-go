# Priority2 checklist

- [x] Start from released priority1 main and preserve predecessor release evidence.
- [x] Meaningful API/schema and bounded-query RED before GREEN; strict scalar JSON-RPC ambiguity RED/GREEN.
- [x] Bounded address/range query with original ordinal order, coverage and explicit partial opt-in.
- [x] Separate orphan selection, epoch/ref/digest and durable archive before deletion.
- [x] Functional coverage/cursor/restart/atomic-failure/cancellation tests.
- [x] Fsync-on PostgreSQL/API reserved-fourth-slot integration; no API RPC client.
- [x] Full build/fmt/vet/lint, root+wire+StableNet race, fsync-on PostgreSQL, examples/cache/offline operations.
- [x] One complete GOMAXPROCS1/race/count200 run of new API/storage cases.
- [x] Explicit staging and reviewed staged/whole-history secret scans.
- [ ] Draft PR, exact-head lint/test/postgres/examples/arc success, ready/squash, clean primary main sync.
- [ ] Remove owned caches/fixtures after release, retaining evidence and branch.

Priorities3/4 remain for the parent. This checklist never establishes the final
four-feature acceptance gate.

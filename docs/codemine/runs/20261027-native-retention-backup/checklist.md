# Priority4 checklist

- [x] Audit pinned Arc/Reth v2 table dependencies and native MDBX API.
- [x] Runtime RED: expired lookup, MDBX free pages, exact seed values, mixed backup receipt.
- [x] Production COW lookup pruning, native compact-copy/reopen and exact seed verification.
- [x] Matched component sealing and restore admission with live E3/DB/source/native gates.
- [x] Immutable DB-validated receipt handoff and later child-read binding, runtime RED/GREEN.
- [x] Complete real native preservation/consumer tests and all local/Rust/PG/count200 gates.
- [ ] Staged/history secret scans; draft PR and exact current-head CI5.
- [ ] Ready/squash, one-parent reviewed tree and clean primary main sync.
- [ ] All-four strict observer, Korean completion report and owned-cache cleanup.

Operations, node startup/peers/providers, operator DBs, disk provisioning, operational
GC/restore activation and performance/SLO measurements remain held.

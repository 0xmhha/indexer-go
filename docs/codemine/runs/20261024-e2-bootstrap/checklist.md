# E2 checklist

- [x] U00: E1 #214 merged and clean main verified; scope/acceptance fixed in [plan](plan.md).
- [x] U01: streaming verifier RED (`TestCheckpointFullRootsAndExtra`: ErrIncomplete), then GREEN; roots, optional Extra, code, identity, omitted slot, missing end, trailing input and cancellation.
- [x] U02 core: real fsync-on PostgreSQL RED (missing baseline), then GREEN for full-zero/partial-unknown, pool4, duplicate refusal and atomic rollback on fresh/truncation/identity/cancel. Official full/partial state, unknown/absence, zero-slot write/rewind, nonempty schema refusal and E1 baseline compatibility passed. U04 tracks full regression.
- [x] U03: official state export RED/GREEN; full genesis/checkpoint and selected-proof fixtures; read-only existing path DB/spool inspection; start modes/preimages; generation/parent diagnosis. Operator tool → mock canonical RPC5 → PG → seven writers/API parity passed.
- [x] U04 implementation/local verification: full functional and root/wire/executor race, PostgreSQL, build/vet/fmt/lint, cache lifecycle, examples, offline operations and staged/history secret checks. Developer docs and current continuity updated.

[Functional evidence and commands](validation.md) record local acceptance checks. [PR #215](https://github.com/0xmhha/indexer-go/pull/215) passed all four latest-head required jobs, was squash-merged as `ac6f0acd96d769ccbd098e9211b59a5312d53fdf`, and main was synchronized. E2 is complete. Current requested work is [E3](../20261024-e3-retention/continuity.json); do not rerun E2 or performance screening merely to resume.

Evidence is recorded per unit here and in continuity; functional results do not establish performance or live peer readiness.

U03 integration exposed and fixed report JSON serialization (`model.Extensions` is internal and excluded), read-only preimage access and unsorted absent-slot selections. None required a node service or performance measurement.

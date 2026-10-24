# E2 checklist

- [x] U00: E1 #214 merged and clean main verified; scope/acceptance fixed in [plan](plan.md).
- [x] U01: streaming verifier RED (`TestCheckpointFullRootsAndExtra`: ErrIncomplete), then GREEN; roots, optional Extra, code, identity, omitted slot, missing end, trailing input and cancellation.
- [x] U02 core: real fsync-on PostgreSQL RED (missing baseline), then GREEN for full-zero/partial-unknown, pool4, duplicate refusal and atomic rollback on fresh/truncation/identity/cancel. Official full/partial state, unknown/absence, zero-slot write/rewind, nonempty schema refusal and E1 baseline compatibility passed. U04 tracks full regression.
- [x] U03: official state export RED/GREEN; full genesis/checkpoint and selected-proof fixtures; read-only existing path DB/spool inspection; start modes/preimages; generation/parent diagnosis. Operator tool → mock canonical RPC5 → PG → seven writers/API parity passed.
- [x] U04 implementation/local verification: full functional and root/wire/executor race, PostgreSQL, build/vet/fmt/lint, cache lifecycle, examples, offline operations and staged/history secret checks. Developer docs and current continuity updated.

[Functional evidence and commands](validation.md) and continuity record the acceptance checks. Release CI/merge are authoritative in the [PR for the source branch](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe2-checkpoint-bootstrap); require latest-head lint/test/postgres/examples before squash, then fast-forward main. The source commit cannot certify its own future merge. After that PR is merged, E3 is next; do not rerun E2 or performance screening merely to resume.

Evidence is recorded per unit here and in continuity; functional results do not establish performance or live peer readiness.

U03 integration exposed and fixed report JSON serialization (`model.Extensions` is internal and excluded), read-only preimage access and unsorted absent-slot selections. None required a node service or performance measurement.

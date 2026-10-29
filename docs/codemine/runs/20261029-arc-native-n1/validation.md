# N1 validation

The genesis requirement test first failed with `native bootstrap not implemented`
(exit101, one actual selected test). The checkpoint test then exposed the native
sequential change-file requirement: saving block2 expected block1. After adding
physical baseline positions and retained floor, genesis and checkpoint builds
reopen and continue correctly. A separate nonempty-withdrawal storage fixture then failed exact native block round-trip: the extra body data was absent. Delegating that part to the public pinned EthStorage writer and verifying body/receipts again after reopen fixes this omission. This storage-only synthetic anchor does not claim consensus validity. A legacy wire compatibility test also failed when nested strictness accidentally reached the old decoder; strict full-input types are now bootstrap-only, and wire/seed decoding is preserved. Compile/import corrections and an already-passing empty-body round-trip probe are not behavioral RED.

Functional fixtures cover official testnet genesis, synthetic funded code/storage,
independent expected commitments, altered or omitted values, duplicate/unknown
fields, partial/malformed/trailing input, absent target publication on failure,
retained pending fence and preserved inputs/targets. Checkpoints at1 and3 verify
next execution/save, reopen, original-anchor receipt, historical full reexport and
absence of fabricated intermediate body indices.

Only owned temporary native library DBs are used. No live node/peer, production
checkpoint, storage provisioning, SQL service or performance measurement is run.
N2–N6 remain separate work. Local and release receipts are recorded after their
actual completion; no CI/merge success is implied by this source document.

Final local Rust all-target tests passed57 library and3 command tests, zero failures
or ignored tests. Build, fmt and clippy with `-D warnings` passed on the frozen
[source](../../../../verification/native-import-n1/source.json). Portable
[checks](../../../../verification/native-import-n1/checks.json) and
[RED receipts](../../../../verification/native-import-n1/red.json) identify the
actual local evidence. No new Go/concurrency source was added; the retained full
Go race success was verified against the identical b8da4538 source and actual log
SHA25619999d4f, and is explicitly REUSED, not rerun. New final-head CI5 remains
required before squash/main sync.

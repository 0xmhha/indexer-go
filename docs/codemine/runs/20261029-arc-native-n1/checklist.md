# N1 checklist

- [x] Actual RED: valid genesis build fails with unimplemented bootstrap.
- [x] Actual RED: native-v2 H+1 save fails until change-file append position is established.
- [x] Actual RED: nonempty anchor withdrawal body omitted from native round-trip; use pinned body writer and reopened body/receipt verification.
- [x] Actual RED: preserve legacy wire decoding; isolate strict nested parsing to full bootstrap.
- [x] Separate full-value authentication from export candidate-key seed parsing.
- [x] Verify independent expected commitment, strict bounded rows, code/storage/account roots.
- [x] Keep all writes in new private staging, no source or existing-target modification.
- [x] Verify authentic headers and anchor block/receipt commitments; no fabricated older bodies.
- [x] Persist full preimages, immutable original anchor/floor/H+1 and input digests.
- [x] Reopen verification and atomic NOREPLACE publication; incomplete stages stay fenced.
- [x] Final local build/fmt/clippy/full Rust and document/secret checks.
- [ ] Actual final-head CI5, expected-head squash and clean main synchronization.

Operational APFS/enode/authenticated checkpoint/private deployment remain HOLD.

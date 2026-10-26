# E6 independent WBFT consensus

Current scope is priority 3 only. Priority 2 [#228](https://github.com/0xmhha/indexer-go/pull/228)
released at `ba285da31bfe105813e81ebbbb510e3ac2921f97`; priority 1
[#227](https://github.com/0xmhha/indexer-go/pull/227) released at
`449c31750bb4efb48d12b9264c1c1dc3fcb273aa`. Their completion evidence is retained.
Priority 4 starts only after E6's exact-head CI5, squash and clean main sync.

The [runtime contract](../../../EXECUTION_CONSENSUS.md) specifies the supported
pin, certificate messages, trust input, fork interval, bounds and operational holds.

## Native audit and integration decisions

The executor's existing replacement pins official go-stablenet
`4d17eea018c8dd579409db30bfd27de79c4f4121`. Audit sources are
`consensus/wbft/core/core.go` (PrepareSeal, prepare=0/commit=1),
`core/types/istanbul.go` (round/seal filtering and LSB bitmap),
`consensus/wbft/validator/default.go` (ceil(N-(N-1)/3) quorum),
`consensus/wbft/engine/engine.go` (current/previous certificates, epoch lookup,
RANDAO chain/version/height and replacement committee), and
`crypto/bls/blst/signature.go` (G1/G2 encoding and POP domain).

Root uses its existing blst dependency independently; the native module generates
and cross-validates deterministic development certificates. Chain-specific code
stays under x/stablenet. Neutral factory/value ports do not import any chain.

Persist the immutable policy and last authenticated committee/header/finality in
one monotonic coordinator row. Validate every new publication from that row in
the existing final transaction. No speculative verifier mutation, third EVM,
extra pool/writer/RPC or ACK authority. Missing proof fails before apply/fresh;
fresh, generation and cancellation still govern the final COMMIT. A reopened
store must enforce persisted policy without an in-memory verifier instance.

## Acceptance and release checklist

See [checklist](checklist.md), [validation](validation.md), and
[continuity](continuity.json). Full local gates, exact-head CI5 and actual squash
release are required; passing individual tests does not establish release.

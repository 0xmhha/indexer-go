# Native WBFT development certificates

`native.json` contains reproducible **development-only** keys' public components,
a genesis trust policy and four native RLP headers. There are four validators per
committee, three signatures per certificate, round 2, an epoch transition at
height 2, and the replacement committee signs heights 3 and 4. No live-network
anchor or operator keys are claimed. The private scalars in the generator are
public test material and must never be used outside tests.

Provenance: official `github.com/stable-net/go-stablenet`
`v1.0.1-0.20261013052903-4d17eea018c8`, full commit
`4d17eea018c8dd579409db30bfd27de79c4f4121`, pinned by the separate
[executor module](../../../x/stablenet/executor/go.mod).
The generator calls native `core.PrepareSeal`, BLS key derivation, signing,
aggregation and aggregate-public-key verification. Header RLP/hash and epoch
encoding also come from that pinned library. It starts no node and no EVM.

Regenerate explicitly from the repository root:

```sh
UPDATE_CONSENSUS_FIXTURES=1 go -C x/stablenet/executor test -run '^TestGenerateIndependentConsensusFixture$' -count=1
go -C x/stablenet/executor test -run '^TestGenerateIndependentConsensusFixture$' -count=1
```

Without the environment variable the native test regenerates in memory and
compares the entire file byte-for-byte. Root consensus tests independently
verify these signatures with blst and exercise signature/domain/round, quorum,
bitmap/key, chain/fork, authenticated committee transition, ancestry and recovery
failures. Root PostgreSQL integration verifies the publication/finality atomic
boundary, fresh failure, generation/cancellation, restart ACK and finalized
rewind floor. The guide explains [trust and limitations](../../../docs/EXECUTION_CONSENSUS.md).

# Independent execution consensus

The opt-in contract is `stablenet-wbft-4d17eea018c8`, using official
[go-stablenet commit 4d17eea018c8dd579409db30bfd27de79c4f4121](https://github.com/stable-net/go-stablenet/tree/4d17eea018c8dd579409db30bfd27de79c4f4121).
It authenticates WBFT finality certificates without executing a third EVM,
starting a node, or trusting an RPC assertion about validators. Existing WBFT
statistics and bitmap parsing do not establish independent verification.

## Trust and fork policy

`source.execution.consensus` is optional. When absent, existing publication
retains RPC head/fresh semantics and has no independent finality claim.
When present, configure `profile: stablenet`, `protocol:
stablenet-wbft-4d17eea018c8` and `parameters` containing a JSON TrustPolicy:

- `Protocol`: the same pinned protocol string.
- `Chain`: exact decimal `ID` and `Genesis` hash, matching execution storage.
- `Fork`: `anzeon-v1`. This supports the pinned WBFT seal encoding before
  Shanghai/Cancun and later header extensions. Anzeon RANDAO version is 1.
- `EpochLength`, `EpochOffset`, `BlockPeriod`, `MaxHeight`: an externally
  audited constant epoch/period interval. The offset is the start of that epoch
  schedule. MaxHeight is an explicit last supported height; choose it before
  any native configuration transition. Unsupported schedule changes stop ingest.
- `AnchorHeader`: base64 native RLP header at the original execution anchor.
- `Applied`, `Next`: ordered arrays of `{Address, PublicKey}` with base64
  compressed 48-byte BLS keys. Applied signed the anchor; Next signs its child.
  For an epoch anchor these can differ. At genesis both identify the initial set.

The operator must authenticate the entire initial policy, header, chain/genesis,
validator keys and their registration/ownership outside the RPC channel. The
indexer does not invent this trust or derive it from `eth_getBlockByNumber`.
The anchor hash/root/height must exactly match the prepared execution checkpoint.
A non-genesis anchor is an explicit trust assumption, not proof of its ancestors.

The YAML shape is:

```yaml
source:
  execution:
    # Keep the existing socket and prepared anchor settings here.
    consensus:
      profile: stablenet
      protocol: stablenet-wbft-4d17eea018c8
      parameters: '<complete externally authenticated TrustPolicy JSON>'
```

The placeholder is deliberately not an operator anchor. Keep the exact JSON
parameter string stable across restarts; whitespace changes also change the
pinned policy identity. The development fixture's `Policy` object shows field
encoding for offline tests only.

The policy is durably pinned before recovery/listener admission, including an
explicit disabled policy for ordinary execution sources. Every field is immutable;
changing parameters, disabling verification or upgrading an existing disabled
source requires a separately prepared source/database. There is no silent policy
migration. API-only reads never initialize or change a policy.

## Certificate and transition verification

The verifier uses subgroup-validated BLS12-381 G1 keys/G2 signatures and the native
`BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` domain. It requires both prepare and
commit certificates, native bitmap ordering, and quorum `ceil(N-(N-1)/3)`.
The message is Keccak256 of the round-filtered native WBFT header hash followed
by the prepare (0) or commit (1) byte. Round and current seals are filtered exactly
as native `WBFTFilteredHeaderWithRound`/`core.PrepareSeal` specify. The previous
header's prepare/commit certificates use its own applied committee. Duplicate
addresses/keys, invalid compressed points, absent certificates, out-of-range
bitmap bits and insufficient quorum fail closed.

Each verified block must extend the exact previous independently finalized hash
by one height. The verifier also authenticates the authorized proposer's native
chain/version/height RANDAO signature, parent mix and minimum block period.
Only epoch headers may advertise the next committee. The old committee's signed
header authenticates this replacement, which takes effect at the next block.
This certifies the committee's finalized statement; it does not independently
execute governance/staking contracts or replace the existing execution checks.

Work is bounded to one descendant header per publication, 256 KiB per header,
256 validators/candidates, and 1 MiB each for policy/state. Cancellation is checked
between validator and certificate operations. Pairing work is synchronous and
bounded; the verifier starts no goroutines. No network, HTTP budget, pool or writer
is added. Unsupported Arc independent consensus and unsupported fork/schedule
intervals remain closed. Blob support is a separate capability.

## Publication, ACK and recovery

The actual execution receiver prepares the original payload and uses the existing
seven part writers. The final PostgreSQL publication transaction reads persisted
policy/state, verifies the header's certificates against the last durable committee
and checks the output hash/root/height against the exact publication. It writes
finality and the next committee in that same transaction as visibility, feature
checkpoints, undo/outbox and the exact payload digest. A missing/invalid proof
cannot publish or gain a committed ACK. Unpublished parts remain retained.

The existing **new uncached fresh RPC immediately before generation fence and
COMMIT** is still mandatory. A failed fresh, cancellation or generation change
rolls back finality together with publication. A reopened store reconstructs the
verifier from persisted policy and state, verifies the saved header certificates
and committee consistency, and checks state/policy digests. The policy digest
is also pinned in execution control: deleting the consensus row cannot reopen
the source through the legacy path. Lost-ACK confirmation still compares the
complete original payload digest and performs a fresh canonical read. Every
successfully published block under this policy is independently finalized, so
rewinds below it, or a conflicting hash at its height, are rejected. Conflicting
RPC histories require operator investigation; they do not erase finality.

The existing one pool of four connections, two concurrent part transactions,
seven writers, reserved API slot, two EVM executions, shared five physical HTTP
attempts/second, and E1–E4 retention authorities remain in effect.

## Offline verification and release scope

[Development fixtures](../testdata/execution/consensus/README.md) reproduce native
signatures, replacement committees and previous seals without a node. Run:

```sh
go test ./x/stablenet/consensus -run TestSeedPriority3E6CryptographicConsensus -count=1
./scripts/test-postgres.sh ./pkg/storage/postgres -run TestSeedPriority3E6PostgresPublicationRecovery
```

These are development keys, not live mainnet/operator-anchor validation. Production
operation remains unconfigured/closed until an externally authenticated operator
policy exists. Live nodes/peers/provider DBs, operational GC, deployment and extra
performance tests remain held. Priority 4 native reclamation/backup is separate
and must follow this feature's exact-head CI5, squash and clean-main release.

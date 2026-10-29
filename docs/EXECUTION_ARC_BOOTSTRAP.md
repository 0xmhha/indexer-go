# Arc native full-state bootstrap

`arc-native-bootstrap` builds a **new** private native-v2 DB offline. It uses
pinned Arc/Reth storage libraries, starts no node, makes no RPC/SQL connection,
and performs no EVM execution. The continuing writer and App integration are
N2–N6 in the [first-import plan](codemine/runs/20261029-arc-first-import/plan.md).
Operational storage, enode and authenticated production inputs remain **HOLD**.

## Trust and required inputs

An operator must independently authenticate the expected anchor commitment.
The builder cannot establish the provenance of a file supplied by that operator.
It never copies its expected commitment from the checkpoint being verified.
This follows the [full checkpoint trust boundary](EXECUTION_BOOTSTRAP.md).

| Input | Contract |
| --- | --- |
| `--state` | Full version1 NDJSON checkpoint: manifest, secure-hash ordered account/storage records, end; complete code and nonzero storage values |
| `--expected` | Independently trusted JSON manifest: Version, Mode, Coverage, Chain, Ref, Root; Header may be omitted, or must equal the full input header |
| `--target` | Absent absolute path, within an existing same-UID `0700` directory; existing file, directory or symlink is refused |
| `--headers` | For checkpoint H>0, contiguous genesis..H NDJSON rows, each exactly `{"Header":"BASE64_HEADER_RLP"}`; no trailing rows |
| `--block` | For H>0, exact canonical anchor block RLP, at most10MiB |
| `--receipts` | For H>0, exact network-RLP receipt list, at most10MiB |

Every input is an absolute, same-UID regular `0600` file with one link. It is
opened without following a final symlink and copied once into private staging.
The source inode/size/timestamps are checked for concurrent changes. The copied
bytes and SHA256, rather than a later reopening of the external source, supply
validation and native writes. The source is never modified.

The only production profile is the pinned official Arc testnet5042002/genesis.
Synthetic genesis injection is test-only. The manifest requires generation1,
full coverage, genesis mode at0 or checkpoint mode above0, exact independent
anchor/hash/root and bounded signed64 height/time. Unknown/duplicate fields,
partial proofs, missing code/end, extraneous record fields, zero storage values,
and repeated or unordered secure keys fail closed.

Header archives are required because the pinned static-header storage uses
contiguous geometry and future EVM BLOCKHASH reads require exact ancestors.
Every link is checked back to the fixed genesis and forward to the independent
anchor hash. This stores authentic header context; it does **not** create old
bodies, old transactions, old state or pre-anchor feature coverage. Header/index
space must be budgeted separately from a raw block window.

## Verification and publication

The builder streams at most one bounded record at a time. A storage trie is
reconstructed per account and compared with its declared root; account leaves
then reconstruct the full state root. Code hashes are checked. Batched native
hashed-state writes are limited to256 rows or about4MiB plus one bounded row;
no whole snapshot or whole-account storage map is collected in memory. The
pinned native trie calculator flushes intermediate updates in bounded chunks.
Both the stream root and persisted native root must equal the trusted anchor.

For H>0 the actual anchor body and receipts are checked against transaction,
ommer, withdrawal and receipt roots, receipt types, bloom and gas. Historical
body indices below H stay absent (apart from the genuine genesis). Empty native
change-file positions establish the H+1 append position; the retained state floor
is H and prevents treating those positions as reconstructed earlier history.

The DB is closed and reopened read-only before publication; finished height,
canonical anchor, full root and floor must agree. The original full checkpoint
and preimages remain in `.arc-retention-checkpoint.ndjson`. The immutable
`.arc-native-bootstrap.json` records original anchor/floor/H+1, pins and exact
input digests. This receipt is neither E1 publication nor an ACK/GC permit.
Later retention must preserve the original-anchor receipt even when advancing a
retained export seed.

Only after verification are native files and directories synced and staging
atomically renamed with NOREPLACE, followed by parent fsync. Existing target
paths are never reset or merged. Errors preserve private staging and its
`.arc-native-bootstrap.pending` fence; `native::check_start` refuses that fence.
There is no TTL clear, automatic repair, destructive retry, latest/zero fallback
or implicit App startup. A failed stage requires explicit inspection; use a new
absent target for a separately reviewed rebuild.

## Explicit command

The following illustrates the interface; operating execution remains held until
authenticated inputs and storage are approved. A genesis build omits the last
three arguments. No command here obtains a checkpoint, contacts a peer, formats
a disk or starts the indexer.

```sh
cargo run --locked --manifest-path x/arc/executor/Cargo.toml --bin arc-native-bootstrap -- \
  --action build --target /ABSOLUTE/PRIVATE/new-native \
  --state /ABSOLUTE/PRIVATE/full.ndjson --expected /ABSOLUTE/PRIVATE/trusted.json \
  --headers /ABSOLUTE/PRIVATE/headers.ndjson \
  --block /ABSOLUTE/PRIVATE/anchor.rlp --receipts /ABSOLUTE/PRIVATE/receipts.rlp
```

The PostgreSQL checkpoint importer still initializes only the E2 baseline. This
native builder does not replace it, create an SQL baseline, assign a receiver
E1 generation, or permit replay below the original anchor. DB pool4/part2/API1,
seven logical writers, shared5 physical HTTP attempts/s and NEW final fresh →
post-response generation fence → COMMIT → exact ACK remain unchanged.

See [N1 validation](codemine/runs/20261029-arc-native-n1/validation.md) for offline
library evidence and its limits, and [native retention](EXECUTION_ARC_RETENTION.md)
for the separately supervised E3 path.

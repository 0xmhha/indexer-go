# Offline PostgreSQL backup attestation adapter

`cmd/execution-backup` authenticates an external provider's signed statement and
the exact supplied snapshot/WAL artifact. It exports the existing
`retention.IndexerBackup` receipt for the [matched backup contract](EXECUTION_ARC_RETENTION.md#matched-backup-and-restore-admission).
It opens no database, starts no services, and performs no restore, retention,
network request or live canonical check. Actual operations remain **HOLD**.

## Verify or export

Build with the repository's pinned Go toolchain:

```sh
go build -o build/execution-backup ./cmd/execution-backup
```

When operator inputs exist, verification alone reads four explicit files:

```sh
build/execution-backup \
  -trust /PRIVATE/provider-trust.json \
  -permit /PRIVATE/permit.json \
  -attestation /PRIVATE/signed-backup.json \
  -artifact /PRIVATE/database.backup
```

An optional `-output /PRIVATE/NEW-receipt.json` writes only the compatible
receipt. Its parent must already be owned0700; the destination must not exist.
Publication is private0600, fsynced and atomic without replacement. Verification
errors/cancellation before publication leave no receipt or partial file.
A failure after the publication boundary can leave a complete receipt; a failed
diagnostic output can also follow a completed export. Inspect that file without
assuming the command exit grants authority. Repeating an export never overwrites
existing evidence, even if identical.

All four inputs must be absolute, clean paths to owned0600 regular files with
one hard link. Every parent component is opened using held directory descriptors
with no symlink traversal; even system aliases such as `/tmp` must be supplied
as their resolved `/private/tmp` paths on macOS. JSON inputs are bounded to1MiB,
strictly typed, and reject duplicate/unknown/null-scalar/trailing/deep data.
Artifact hashing streams through a bounded buffer, checks the signed exact
nonzero length (at most64GiB), and checks private file identity/metadata on the
open descriptor. The matched archive's64GiB **total** budget is a separate,
potentially tighter limit including native/source components.

Exit status:

| Result | Code | Meaning |
| --- | --- | --- |
| PASS | 0 | Signature, pinned scope, permit and supplied bytes match |
| HOLD | 2 | Required file argument is absent or exactly `HOLD` |
| FAIL | 1 | Supplied evidence/arguments/export/I/O/cancellation failed |

JSON diagnostics contain fixed codes, no private paths/identities/keys/parser
errors, and always state `operational_readiness: not_authorized`,
`restore_verification: not_performed`, `live_freshness: not_checked`.
`-help` prints usage and exits0. There are no environment overrides, DSNs,
remote provider calls, signing keys or subprocesses. Only macOS/Linux implement
private-file admission; unsupported systems refuse verification/export.

## Provider trust and signed format

The operator independently authenticates and provisions **one** provider public
key scoped to one exact chain/genesis and database/schema. An embedded key in
the signed envelope never establishes trust. Key replacement/revocation and
custody are explicit operator actions; the tool never downloads keys or infers
trust from a matching checksum.

The private trust JSON has these fields (replace the illustrative placeholders;
they deliberately cannot verify):

```json
{
  "Version": 1,
  "KeyID": "LOWERCASE_SHA256_OF_RAW_32_BYTE_PUBLIC_KEY",
  "PublicKey": "LOWERCASE_HEX_32_BYTE_ED25519_PUBLIC_KEY",
  "Chain": {"ID": "CHAIN_ID", "Genesis": "EXACT_GENESIS_HASH"},
  "DatabaseIdentity": "DATABASE_NAME/SCHEMA_NAME"
}
```

The envelope has only `Statement` and `Signature`. `Statement` fields, in the
following canonical order, are `Version` (1), `KeyID`, `ArtifactBytes`, `Receipt`.
`Receipt` is the existing typed `retention.IndexerBackup`, with canonical order:
`Version`, `PermitDigest`, `ArtifactDigest`, `PublicationDigest`,
`DatabaseIdentity`, `SnapshotIdentity`, `WALBoundary`.

The signed message is the UTF-8 bytes of the exact domain
`indexer-go/postgres-backup-attestation/v1\n` (ending with one LF), followed by
Go `encoding/json.Marshal` of the **typed Statement**, without an extra newline.
This uses Go's string/HTML escaping and the existing wire hash text encoding;
it is not arbitrary input JSON or RFC8785. [SigningMessage](../internal/executionbackup/backup.go)
is the canonical reference. Envelope JSON whitespace and member order do not
change the signed typed statement. `Signature` is lowercase hex of the64-byte
Ed25519 signature; `PublicKey` is lowercase hex and `KeyID` is lowercase SHA-256
hex of its raw32 bytes. Provider signing occurs outside this command.

The adapter validates the full E3 permit and exact permit digest, chain/genesis,
DB identity, nonzero artifact/publication digests, bounded snapshot identity,
WAL LSN syntax, and visible height within the observed native head. A signature
for a different permit cannot be replayed. There is no time-based freshness
claim: matching an offline permit does not prove that it is still pending in
the live DB. JSON placeholders **inside** supplied files are invalid evidence,
not permission to skip checks.

## Existing authority boundaries

Retain the original signed envelope, the reviewed trust-key identity, artifact,
permit and exported receipt together under the operator's custody policy.
The compatible receipt itself contains no signature and must not be described
as self-authenticating. This adapter is an optional, explicit provenance step;
existing externally authenticated receipt inputs remain compatible. Do not
substitute a different receipt/artifact between verification and use; downstream
digest, private-file and live DB checks remain mandatory.

The existing DB coordinator still validates current database/schema, exact
publication digest, pending permit, full baseline, generation/root and WAL
boundary under its live E3 fence. The inherited producer handshake still binds
that validated receipt digest. Native/source snapshot checks, durable exact ACK,
window, lease, completion and release checks are unchanged. The final NEW
uncached canonical query and post-response generation fence immediately before
commit remain unchanged; RPC5 physical attempts/s, pool4/part2/API reservation
and seven logical writer queues are unchanged.

A trusted signer attests that the external packed artifact restores the stated
publication and contains the required WAL. The adapter proves attribution and
byte integrity; it does **not** parse PostgreSQL backup contents, inspect WAL
continuity, run `pg_verifybackup`, or establish recoverability/RPO/RTO. Provider
snapshot/WAL creation, actual PG restore and installation, APFS custody, live
restore admission and startup/GC remain held pending operator resources and
separate validation. No actual backup, node, container or performance trial is
part of this feature's synthetic functional tests.

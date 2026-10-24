# Execution wire v1

This standalone Go module defines the transport contract between verified execution producers and the indexer. The codec/checkpoint/retention contracts use only the standard library, so consumers keep their EVM dependencies isolated. Shared `spool` and `exporter` packages add durable source ownership and authenticated Unix supervision, also using only the standard library at runtime. Goleak is test-only; the original standard-library import-boundary check stays unchanged. The [StableNet executor](../x/stablenet/executor/README.md) and [Arc native adapter](../x/arc/executor/README.md) emit the same bounded encoding. E1 integration is tracked in the [implementation checklist](../docs/codemine/runs/20261023-e1-execution/checklist.md).

## Encoding

All lengths are unsigned 32-bit big-endian integers. A frame is:

```text
body length | kind byte | JSON header length | canonical JSON header | payload
```

The body is at most 8 MiB; the JSON header is at most 64 KiB. Version, chain ID, genesis, generation, height and block hash are mandatory. Heights and generations fit PostgreSQL `bigint`; generation starts at one. Hex values use fixed-width lowercase `0x` encoding. JSON rejects unknown or duplicate fields, alternate encodings, excessive nesting and more than 262,144 tokens before typed allocation.

| Kind | Meaning |
| --- | --- |
| `Hello` | Last durably published reference; no payload |
| `ResultPart` | One indexed fragment with total size, count, whole-result SHA-256 and fragment SHA-256 |
| `Committed` | Exact reference and result digest durably published; no payload |
| `Rewind` | New generation and common-parent reference; no payload |
| `Failure` | Bounded machine-readable error code; no free-form error or credential payload |

`MarshalResult` encodes the execution result as:

```text
"EXR1" | metadata length | block length | receipts length |
canonical JSON metadata | raw block RLP | raw receipts RLP
```

The whole result is at most 64 MiB; metadata is at most 16 MiB. Source block and receipt bytes remain unchanged. Journal ordinals are contiguous: optional first-only baseline/genesis or `PreBlock` (numeric cause 4), transactions in order, then Finalize (including empty blocks). `PreBlock` records protocol system changes before transactions with index -1 and zero transaction hash; it cannot follow a baseline or transaction. Accounts, slots and code are sorted and unique within each step. Account values retain existence, nonce, uint256 balance, code/storage hashes and StableNet Extra. A storage-clear marker and before/after values preserve deletion semantics. A result has at most 65,536 changes and a code item at most 1 MiB.

Existing StableNet cause numbers and canonical payloads are unchanged. Older readers reject cause 4; upgrade the indexer/API and PostgreSQL schema (migration 0017) before supplying a producer that emits it. The new schema retains existing partitioned history and still rejects unknown causes. This additive cause does not grant a chain execution capability or weaken native replay/root verification.

SHA-256 protects transport identity. The consumer must separately verify EVM block/receipt/state roots, code hashes, chain identity, canonicality, generation and journal completeness. A valid wire message is not a durable database ACK. Only final publication may send `Committed`; the wire module has no RPC or database access.

## Ownership and limits

`Split` reserves 64 KiB of each requested frame budget for metadata and copies the input once. Treat returned payloads as immutable. At most 4,096 parts describe one result.

`Assembler` has one owner. Configure at most 64 pending blocks and 256 MiB of declared payload, with a positive TTL up to one hour. A block reserves its entire declared size before any fragment copy. Identical fragments are idempotent; conflicting metadata, corrupt parts, invalid lengths and whole-result digest mismatches fail. Expiry uses the first fragment's deadline; duplicates do not extend it. Call `Expire` during idle periods and `Reset` on disconnect/cancellation. Completion temporarily constructs one extra copy of the completed block before releasing its reservation. Metadata parsing and result conversion need additional bounded working memory; the reservation is not a process RSS limit.

`Conn` allows one reader and one writer concurrently. Waiting for a direction gate and blocked socket I/O are cancellable. Deadline callbacks finish before the next operation starts. Cancellation before any bytes leaves the stream reusable; any error after a partial frame closes the connection. The owner closes and joins its session workers. `Close` also releases blocked operations.

## Check

Use the repository's Go 1.26.8 toolchain:

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
GOMAXPROCS=1 go test -race -count=200 -timeout=5m -run '^TestConn' ./...
```

The root `make test-execwire` target and required CI jobs cover this separate module. Tests use bytes, `net.Pipe`, owned temporary files and local Unix sockets; they start no nodes, live peers or databases. Runtime integration and operational evidence remain separate from this codec's checks.

## Offline checkpoint stream

`execwire/checkpoint` is a stdlib-only NDJSON format, separate from the IPC result protocol. Consumers must verify trusted identity, full trie roots or selected proofs and terminal EOF before committing staged records. See [bootstrap](../docs/EXECUTION_BOOTSTRAP.md). Diagnostic source observations authorize no ACK, generation change or deletion.

## Shared execution producer

[`spool`](spool/README.md) persists a contiguous source tail, exact ACK cursor, crash receipts, generation transitions and hard quotas. `exporter.Client` independently collects and sends a bounded in-flight window over an authenticated private same-UID socket. It reconnects after transport loss, drains retained startup data, requests recovery through the indexer coordinator and rebinds only to a checked newer generation/canonical floor. Hello never acknowledges or deletes source results.

The original StableNet package paths remain aliases, including native E3 permit/recovery APIs. Arc uses the same source format and supervision through [its collector](../docs/EXECUTION_ARC.md#continuous-collector). This reuse grants no Arc-native GC/checkpoint capability.

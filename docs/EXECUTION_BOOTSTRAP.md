# Execution checkpoint preparation

E2 adds offline preparation to [execution ingest](EXECUTION_INGEST.md). It does not start an indexer or node. The official StableNet client remains unmodified; ordinary import and journal replay still execute a block twice. PostgreSQL uses **four connections per chain/process**, parts use two permits, and public commits retain uncached fresh under the shared **5 physical HTTP attempts/s** budget.

## Choose a starting boundary

| Mode | Initial state and node requirement | Coverage |
| --- | --- | --- |
| `genesis` | Official genesis allocation, exact genesis hash/root, height 0. The runtime can initialize a new node DB using the same official genesis. | Full state from height 0. Ordinary node full sync imports later blocks; it is not executed by preparation. |
| `checkpoint` | Explicit trusted height/hash/root and a compatible prepared **path** node DB or matched node backup. The exact parent state must be readable. | Full snapshot, or selected account/slot proofs marked partial. No state or historical coverage before the anchor. |

A full snapshot rebuilds every account's storage root and the global account root, including StableNet's optional `Extra`, uint256 balances, code hashes and existence. Partial snapshots prove every supplied account/slot, including absence. A partial snapshot cannot assert full coverage. Unknown partial keys remain errors; proven empty/full-state absent keys return zero. Use partial coverage for deliberately bounded recorded ranges with all required touched keys. It does not guarantee ongoing head ingestion: an uncovered existing Before value stops processing and preserves the pending result. E1 baselines migrate as partial, with their data unchanged.

**A PostgreSQL snapshot is not a node DB snapshot.** The tools do not fabricate node headers, consensus snapshots, trie history or a node head from selected proofs. A checkpoint start requires a compatible node DB with its chain/consensus context; restore that separately from a trusted matched backup, or choose genesis synchronization. No blind `SetHead` or latest-root substitution is provided. Full preimages are required to export all addresses/slots. New executor settings retain preimages; this does not retroactively recover missing ones in an old DB.

Checkpoint preparation authenticates header and **state** at the anchor. It supplies no historical block body, receipts, logs or feature history at/before that boundary; the anchor row is a header checkpoint. Archive block/transaction/feature coverage begins with anchor+1. Do not use the checkpoint header's empty transaction list as evidence that its original body was empty. Normal RPC indexing remains available independently.

## Build the tools

From the repository root (build only):

```sh
go build -o /YOUR_BUILD_DIRECTORY/execution-checkpoint ./x/stablenet/cmd/execution-checkpoint
(cd x/stablenet/executor && go build -o /YOUR_BUILD_DIRECTORY/stablenet-checkpoint ./cmd/stablenet-checkpoint)
```

`execution-checkpoint` defaults to `--action verify`. It never starts the app, binds an API/socket, dials peers or performs deletion recovery. The exporter defaults to inspection and never calls `node.Start`, `node.New` or `eth.New`.

## Export and verify

First prepare the explicit executor configuration described in [execution ingest](EXECUTION_INGEST.md#executor-configuration-and-build). `start_mode` accepts `genesis` (requires height0) or `checkpoint`; omission preserves E1 prepared-checkpoint behavior. Both retain unlimited state/transaction history pending E3 and preserve preimages. Choose the anchor explicitly; no command silently selects `latest`.

Examples below describe **operator actions**, not actions performed during this implementation:

```sh
# No node service or data-directory creation: builds genesis state in memory.
stablenet-checkpoint --action genesis --config /PRIVATE/executor.json --output /PRIVATE/genesis.ndjson

# Stop the node owner first. Specify the existing real paths; do not guess freezer placement.
stablenet-checkpoint --action node --config /PRIVATE/executor.json --chaindata /MOUNT/node/indexer-stablenet/chaindata --ancients /MOUNT/node/indexer-stablenet/chaindata/ancient --output /PRIVATE/checkpoint.ndjson

execution-checkpoint --action verify --input /PRIVATE/checkpoint.ndjson --expected /PRIVATE/expected.json
```

Omit `--ancients` only when that DB has no freezer. The node exporter opens existing chaindata and path state **read-only**, checks genesis/chain/canonical header/root, then iterates full state. Missing nodes/preimages fail. For partial state, add `--selection /PRIVATE/selection.json`, an array of `{"Address":"0x…40 hex digits…","Slots":["0x…64 hex digits…"]}`; an explicit empty array still means partial. Code is always verified for supplied accounts. Genesis export permits only full coverage.

`expected.json` is an **independently trusted manifest**, not an automatically trusted copy of the input file's first record. Pin identity from the official genesis/checkpoint agreement. Its shape is:

```json
{
  "Version": 1,
  "Mode": "checkpoint",
  "Coverage": "full",
  "Chain": {"ID": "DECIMAL_CHAIN_ID", "Genesis": "0x64_HEX_DIGITS"},
  "Ref": {"Generation": 1, "Height": 123, "Hash": "0x64_HEX_DIGITS"},
  "Root": "0x64_HEX_DIGITS"
}
```

Placeholders must be replaced; they are not runnable values. Verification binds the profile-decoded header to the trusted identity/root. It verifies execution-state commitments, not independent consensus/finality (E6). Output includes SHA-256 of the exact file and account/slot counts. NDJSON records are bounded to 2 MiB, code to 1 MiB, proofs to 128 nodes with 64 KiB/node, header to 64 KiB, total records to 64 million. Full verification streams secure-key order and retains bounded trie stacks rather than a whole snapshot in memory. A terminal record and EOF are required. Existing export destinations are refused; an error can leave a private incomplete file, which must not be used and fails verification. Successful output is file-synced; keep a durable managed copy/backup before relying on it.

## Initialize a new PostgreSQL schema

Stop application owners, use a **new explicit non-public schema**, and inject `INDEXER_DB_POSTGRES_DSN` and `INDEXER_CHECKPOINT_RPC` privately into the process environment. Do not put credentials in command arguments, files committed to Git or logs. No extra write pool is created.

```sh
execution-checkpoint --action import --schema NEW_EXPLICIT_SCHEMA --input /PRIVATE/checkpoint.ndjson --expected /PRIVATE/expected.json
```

Import migrates the schema, then locks and checks that it contains no existing application data. Schema/migration bookkeeping alone is allowed. Existing execution, legacy, feature, capture or outbox data is refused, including a repeated import; this command is not normal resume. Values are bound parameters; catalog-derived identifiers are quoted.

One durable transaction stages the anchor header, all seven cursors, baseline state histories/current rows, mode, coverage and file digest. Any missing proof/root/terminal record, cancellation, DB error or fresh mismatch rolls it all back. After staged writes flush, the importer obtains an **uncached** anchor hash from the shared-rate-limited canonical RPC, checks context and generation/hash/root again, then commits. The CLI separately verifies RPC chain ID/genesis first. Cancellation or connection loss during COMMIT can leave the client uncertain whether the server committed. Inspect the new schema and its durable control/digest before deciding what to do; do not assume it is empty or blindly retry import.

These calls use the existing physical-attempt pool at rate5, including failover/retry behavior; no new unthrottled RPC client is introduced. Preparation requires the ingest owner stopped, so it is not a parallel extra RPC budget for a running indexer.

After successful preparation, set the normal indexer's `source.execution` original anchor to those exact values and use the matching executor config/spool identity. Starting either service remains a separate operational action. Subsequent restarts use normal E1 handshake/backlog/fresh gates; do not import again or move the original anchor to the tip.

## Read-only diagnosis and explicit recovery

```sh
execution-checkpoint --action inspect --schema EXISTING_SCHEMA
# An inspection-only copy of executor config can pin AnchorHeight/Hash/Root
# to the DB public parent, while spool metadata retains its original anchor.
stablenet-checkpoint --action inspect --config /PRIVATE/inspection-config.json --chaindata /MOUNT/existing/chaindata --ancients /MOUNT/existing/ancient > /PRIVATE/source-observation.json
execution-checkpoint --action inspect --schema EXISTING_SCHEMA --source /PRIVATE/source-observation.json
```

The DB inspection uses a read-only public snapshot and performs no RPC. Source inspection checks that the exact state root opens (it does not prove every descendant path or subsequent replay will succeed) and reads sealed spool metadata; it never opens the spool writer, creates its directory, completes a generation transition, ACKs or deletes a result. Two matching manifest observations do not prove concurrent files stable. Keep owners stopped for operational diagnosis. Source JSON and DB snapshots are separate observations; `compatible_observation` is not startup, deletion or ACK authorization. Runtime gates still decide acceptance.

| Finding | Required action |
| --- | --- |
| Import COMMIT outcome uncertain | Read-only inspect the target schema and compare its durable anchor/coverage/digest with the verified file. A repeated import never clears or replaces an existing result. |
| Incomplete DB rewind | Preserve its marker; normal fenced E1 recovery must finish before reads/import resume. |
| Source generation/ACK ahead of restored DB | Preserve both. Restore a mutually matched backup, or choose a new independently verified anchor and new DB schema **and new spool**. Never lower generation or infer ACK from Hello. |
| Exact public parent header/root/state missing | Restore matching node data/history. Latest state cannot replace it. If restoration is impossible, use a new verified checkpoint/schema/spool or genesis sync. |
| Missing export preimages | Supply a node backup containing preimages or rebuild from genesis with preservation enabled. Selected proofs remain partial; they cannot silently upgrade to full. |
| Quota/disk/retention problem | Preserve pending results/quarantine. E3 owns deletion policy; E2 deletes nothing to regain capacity. |

No tool applies a recovery plan destructively. Old schema/node/spool remain available for investigation and rollback; selecting a new boundary explicitly changes the supported historical range. APFS provisioning, peer access, backup/WAL restoration drills, automatic retention and performance comparisons remain deferred.

## Functional evidence

The [E2 plan/checklist](codemine/runs/20261024-e2-bootstrap/checklist.md) tracks RED/GREEN and release gates. Actual official WBFT state is exported into frozen full genesis/checkpoint fixtures and byte-compared on normal executor tests, without durable state changes. Root tests independently rebuild the same roots; real disposable fsync-on PostgreSQL tests cover atomic import, coverage, missing-zero-slot write/rewind, existing-data refusal and rollback. An operator-tool integration uses mock HTTP canonical responses, then existing seven writers and API parity. These are functional tests, not live synchronization or performance evidence.

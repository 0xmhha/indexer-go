# Arc native integrated acceptance

N6 connects the production `arc.native` App to an owned loopback ETH/68 peer,
actual pinned Arc/Reth importer and replay children, and disposable fsync-on
PostgreSQL. It is an offline functional qualification. It starts no execution
node, public peer, discovery crawl, operator App or public RPC workload.

The [plan/checklist](codemine/runs/20261031-arc-native-n6/plan.md) and
[qualification receipts](codemine/runs/20261031-arc-native-n6/validation.md)
distinguish local behavior, actual PR checks and operating HOLD conditions.

## Fixture and execution boundary

`executor/src/acceptance.rs` is compiled only into Rust tests. It uses the pinned
official Arc testnet5042002/genesis to export a full H0 checkpoint, expected
manifest,131 valid system/empty blocks, a short alternative and a deep branch.
Loopback enode and fork ID are synthetic transport inputs, not an official peer
contract. There is no production custom-genesis flag. Go independently verifies/imports
full checkpoint state into an empty SQL schema; the production native builder
prepares its separate native DB. Original identity/bootstrap/anchor remain fixed.

Expected inputs are constructed before the counted runtime. That construction
executes the SDK and precomputes one exact H3 payload for retransmission; it is
not a normal runtime pass. Official system/empty blocks exercise the real EVM,
but are not a funded user-transaction or live-chain fixture. Existing N0–N5/E1
unit and library evidence retains those separate scopes.

The opt-in Cargo feature `acceptance-trace` emits one bounded stderr line at the
actual importer/replay EVM entry. Default builds omit these calls entirely. The
feature adds no CLI/environment capability, protocol field, state mutation or
canonical authority. Test-owned `exec` wrappers keep child identity, inherited
leases and stdout framing, and retain only their own stderr logs. Normal blocks
have exactly one import and one replay; Full persistence receives the executed
outcome and invokes no executor. Restart and exact duplicate confirmation do
not run either EVM again. Failure retries/reorgs are separate from this healthy
path claim.

## Functional matrix

- Withhold a real HTTP canonical response and require public height to stay
  behind it; then publish/ACK. The App uses its shared physical HTTP5 pool.
- Require all seven SQL writer parts/digests, original header/body/receipt
  archive bytes, public GraphQL access through a reserved reader and outbox
  publication before exact source ACK.
- Stop/join and reopen the same identity/raw/source/schema; exact H3 payload
  retransmission must confirm its original publication without another outbox
  event or EVM execution.
- Reorg to an alternative H3 and back with receiver generations2/3. Keep raw,
  native/import/read epochs and SQL generations separate; continue to H131.
- Explicitly drain/E3 with KeepBlocks129. Prove a positive durable raw deletion
  ticket and absent original payload file, preserved original-generation SQL
  archive, native/source COW and successful restart.
- Fail one real maintenance `prune` child using a fixed test wrapper. Preserve
  SQL pending permit, raw completion and coordinator journal; deny resume and
  mismatched executable recovery. Read-only diagnostics must remain HOLD.
  Recover using the exact same executable/options/permit before resuming.
- A deep fork below the retained floor fences public/API reads and joins workers
  without moving the original anchor. A native pending intent and a wrong state
  root fail closed and preserve their owned evidence/raw data.

These checks do not repeat all N0–N5 crash injections or all four independent
SQL state-proof200 batches. Their prior release evidence remains valid; new
integration boundaries and new concurrent helpers have their own checks.

## Retention age without waiting or weakening policy

Window retention still has a24-hour minimum. A valid early E3 can perform
native/source COW with zero raw candidates; that is not positive deletion proof.
The acceptance case therefore passes a thread-safe Go embedding clock through
`window.Layout.Now` to the existing `Options.Now` and advances only the owned
raw window25 hours after publication/ACK. It does not alter RPC, native or SQL
clocks, the24-hour policy, KeepBlocks129, epochs, leases or fresh/ACK guards.

`Layout.Now` is excluded from YAML/JSON decoding/encoding and has no environment
or CLI binding. Parsing/validation does not call it. Nil preserves `time.Now`.
It is an embedding hook, not an operator-configurable time override. The owned
clock remains advanced across that fixture's reopen; no operator data is aged.

## Reproduce explicitly

Use the pinned toolchains and an owned target/output directory. Existing
operator config/DSNs/keys are not inputs. For example, prepare an empty private
parent and choose new child paths before running:

```sh
# Rust unit fixture defaults to four blocks. This explicit output selection
# adds the131-block export into a NEW directory.
INDEXER_ARC_INGRESS_FIXTURE_DIR=/PRIVATE/NEW/arc-fixture \
  cargo test --locked --features acceptance-trace \
  --manifest-path x/arc/executor/Cargo.toml --lib \
  acceptance::integrated_fixture_exports_official_checkpoint_and_two_branches
cargo build --locked --features acceptance-trace \
  --manifest-path x/arc/executor/Cargo.toml --bins
```

Copy the exact built bootstrap/import/maintenance/replay binaries into one
private directory and verify byte equality before deleting any owned target.
Then the explicit tagged test requires both inputs, never silently skips:

```sh
INDEXER_ARC_INGRESS_FIXTURE_DIR=/PRIVATE/NEW/arc-fixture \
INDEXER_ARC_ACCEPTANCE_BIN_DIR=/PRIVATE/arc-native-bin \
  env -u INDEXER_TEST_POSTGRES ./scripts/test-postgres.sh ./x/arc/acceptance \
  -tags arc_native -race -parallel=1 -count=1 -timeout=20m -v \
  -run '^TestArcNativeIntegrated'
```

The helper owns/cleans only its named/labeled PostgreSQL container and volume,
with fsync=on. Test roots/IPC/source/native/raw files are private and removed.
There is no source fallback to operator files, cached PASS or an external chain.

## CI and remaining HOLD

Default code CI remains [scoped short](CI.md). An Arc Rust change adds bounded
compilation/protocol and a four-block fixture check; it does not export131 blocks
or start this App/SQL matrix. The heavy matrix is an explicit full-workflow step
in the existing `arc` job, after exact binary preservation and owned target
cleanup. No additional required job or default stress200 is introduced.

Pool4/part2/API1/seven logical writers, two normal EVM passes, shared physical
HTTP5 including retries/failover/batch and NEW uncached canonical → post-response
generation fence → COMMIT → exact ACK remain unchanged. Passing owned fixtures
is not live performance, Arc independent consensus/finality, positive blob
supply or operating approval. Actual APFS identity/capacity, official enode and
fork/history contract, authenticated operating checkpoint and private image/
DSN/key/matched backup/restore evidence remain HOLD.

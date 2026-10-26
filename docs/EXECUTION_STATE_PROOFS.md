# Selected state inclusion proofs in execution ingestion

Retain selected standard-MPT account, code and storage evidence at an E1 block's exact **end-of-block state root**. This opt-in path adds no EVM execution, pool or writer. It uses the [capture proof core](../pkg/chainfeed/evmstate/proof/proof.go); the independent [capture state stream](STATE_PROOFS.md) keeps its own cursor, coverage and GC contracts.

## What the evidence establishes

[EIP-1186](https://eips.ethereum.org/EIPS/eip-1186) account paths bind nonce, consensus balance, storage root and code hash to `keccak(address)` under the header root. Storage paths bind each selected value or absence to `keccak(slot)` under that account's proven storage root. Requested code bytes must match the proven code hash. Missing paths or responses are not zero values.

This verifies selected block-end state. It does not establish all accounts or changes, transaction-level before/after state, a complete execution witness, independent consensus/finality, or native-asset accounting. In particular, `consensusBalance` is not an automatic mapping to Arc native USDC. A root/journal/call graph or blob KZG proof does not replace these paths. Four-field standard account encoding is supported; custom account fields require their own verifier. No selected proof expands E2 full-baseline coverage or authorizes E3 deletion.

The profile verifies the E1 header/body/receipts before the proof is admitted. The bundle binds to that result's exact root and whole input digest. The API trusts the existing E1 verified-ingest/storage boundary; it does not independently attest arbitrary database history or consensus.

## Retain and compose before spool admission

The isolated native-library API `checkpoint.ExportStateProofs(ctx, writer, retainedDB, root, targets)` in `x/stablenet/executor` reads a caller-selected retained root. It generates actual inclusion/absence paths without running EVM, starting services or mutating the source. Targets are `[]execwire.StateProofTarget` with exact addresses, code intent and slots. It sorts a copied selection, rejects duplicates, unsupported account fields and missing trie/code data, and buffers a bounded complete bundle before writing. Cancellation is checked before/between reads and after writing. The caller owns an atomic, private, fsynced output sink; an error never authorizes using or deleting an output.

`execution.AttachStateProofInput` validates an original complete EXR1 plus the retained `evm.state-proof.v1` JSON bundle and creates a **new** EXR1. `AttachStateProofFiles` uses the same private ownership/mode/inode/size checks and fsync/no-replace publication as [blob composition](EXECUTION_BLOBS.md). Parent directories are `0700`, files `0600`. Original files and generation are preserved. Existing output is refused; failure after publication may leave a complete output that must be inspected rather than overwritten.

```sh
go run ./cmd/execution-state-proofs \
  --chain-id YOUR_CHAIN_ID --genesis YOUR_EXACT_GENESIS_HASH \
  --result /YOUR_PRIVATE_DIR/original.exr \
  --proofs /YOUR_PRIVATE_DIR/proofs.json \
  --output /YOUR_PRIVATE_DIR/complete.exr --provider native-state
```

The command uses the generic `evm` binary profile. Chain-specific producers call the library with their actual profile. It accepts no RPC URL or node flags, starts no application and supplies no defaults for chain identity or selection. Timeout defaults to 15 seconds and is bounded to one hour. Output reports the new whole-payload digest, ref and byte count.

Submit the **new complete input** through the existing exporter only before any spool admission/digest/ACK. Enrichment cannot retrofit an already admitted bare result without conflicting with its immutable identity. These helpers request no rewind, ACK, GC or file deletion. Retained native state/proof files need an explicit capacity/window policy; the helper does not install one. Live Arc native proof acquisition/collector wiring is a separate integration.

## Borrow an existing RPC source when explicitly chosen

`execution.AcquireStateProofInput` takes an existing chain client implementing `StateProofCaller`; it never dials, closes, retries or selects an endpoint. The caller must pass the **same chain-wide client limited to 5 physical HTTP attempts/s**, including its existing retry/failover/batch-fallback policy.

It validates the original input, selection and provenance label before IO, then requests all selected `eth_getProof` and requested `eth_getCode` members in **one batch**. Every member uses [EIP-1898](https://eips.ethereum.org/EIPS/eip-1898) `{blockHash, requireCanonical: true}`. There is no height/latest fallback. Any batch/member error, missing/pruned response or proof mismatch fails the whole selection, with no partial result. Cancellation preserves its error. The final publication still performs a separate NEW uncached fresh canonical read; this acquisition does not replace it.

```go
complete, err := execution.AcquireStateProofInput(
    ctx, profile, chain, original, "shared-rpc", selection,
    existingChainClient.RPCClient(),
)
```

No live provider support or remote state retention was tested or enabled. Public RPC support for proof/hash selectors remains unconfirmed. Prefer retained native evidence when the producer integration supplies it.

## Require an exact selection

Inside an already configured execution source:

```yaml
source:
  execution:
    # existing socket/original anchor settings remain required
    required_state_proofs:
      targets:
        - address: "0x1111111111111111111111111111111111111111"
          code: true
          slots:
            - "0x0000000000000000000000000000000000000000000000000000000000000003"
```

This is a format example, not an operating watchlist. The default is absent for legacy compatibility. A configured selection must be nonempty and match the normalized exact address/code/slot digest before scheduling **and lost-ACK confirmation**. A missing bundle, different selection, unknown configuration key or invalid selection fails. Supplied bundles always verify even when the setting is absent. This is a source admission policy, not a durable historical coverage declaration or an automatic provider. Removing the setting permits future uncollected inputs; it does not change old rows or mark missing history complete. Changing the required selection can reject pending inputs from the old selection. Drain compatible inputs or retain their original policy; never rewrite an admitted input or its digest. Anchor proofs and historical backfill are not automatically published.

For libraries, `PrepareWithRequirements` adds this policy while `Prepare`/`PrepareContext` retain their existing signatures and defaults. With `code: false`, code hash still verifies but code bytes remain uncollected. No-slot accounts remain valid; empty-list/null representations round-trip without accepting omitted positive slot evidence.

## Durability and recovery

EXR1 appends optional version 1 `StateProofBundle` with fixed codec, profile, provenance label and bounded JSON evidence. Nil preserves **exact old wire bytes** and means not collected. Upgrade readers/schema before enriched producers. New readers still accept legacy inputs; old strict readers reject the added field.

Migration 0020 adds `exec_state_proofs` and the publication `state_proofs` marker. The existing **accounts part** stores the exact response, identity/root/selection metadata and checksums with parameter-bound values. Seven queues, one pool of four connections, part concurrency two and the existing API reservation remain. Durable private parts are not public completion.

Publication and lost-ACK recovery verify complete stored evidence, exact input digest/identity, metadata and all paths before the final NEW uncached fresh read. Only the existing generation fence and COMMIT follow fresh. A missing row, false marker, bad proof or damaged metadata cannot authorize ACK. Rewind removes abandoned published/unpublished proof rows. A surviving prefix keeps its original source generation/digest while queries pin the current public epoch. These rows survive raw-file removal and grant no new E2/E3 baseline, checkpoint, permit, lease or GC authority.

## Query from the database

```text
GET /execution/proofs/{height}/{address}
GET /execution/proofs/{height}/{address}?epoch={epoch}&hash={ref.Hash}
```

Use `/chains/{id}` in multichain mode. Height is explicit decimal; address is exact 20-byte `0x` hex, including standard checksum casing. Both optional pins must be supplied together and nonzero. Unknown, duplicate or malformed parameters are rejected; there is no latest selector or pagination fallback.

Reads use existing reserved public snapshots, visible height and maintenance/rewind gates. They revalidate the complete bounded retained bundle against the stored E1 header root and return the selected account's proof/values with **zero RPC/raw/native fallback**. The response contains current `epoch`, original wire `ref`, `root`, whole-input `digest`, normalized `selectionHash`, profile/provider/format, `verification: mpt_at_execution_header_root`, derived `account` and EIP-1186 `proof`.

`account` contains `address`, proven `exists`, hexadecimal quantity `nonce`, fixed 32-byte hex `consensusBalance`, `storageRoot`, `codeHash`, requested hex `code` and exact selected storage key/value words. Code is omitted when unrequested; requested empty code is `0x`. A proven absent account returns `exists: false`; unselected accounts are not reported as absent.

Malformed queries return 400; uncollected/unpublished/unselected data 404; stale pins 409; unavailable/uninitialized/maintenance/rewind 503; detected corruption/IO 500 with no private errors. Responses are compact and `no-store`. No GraphQL/JSON-RPC state or balance projection is implicitly replaced.

## Bounds and evidence

Reuse the existing proof limits: 8 accounts, 128 total slots, 65 nodes per path, 1024 bytes per node, 2 MiB per account proof, 256 KiB per code and 8 MiB per bundle. EXR1 retains 64 MiB whole-payload, 16 MiB metadata and token limits; combined blob/proof/journal input fails when any bound is reached. These are operating bounds, not chain consensus limits. Context is checked between bounded trie paths; in-progress library reads are not asynchronously interrupted.

Evidence includes independently generated native trie paths after actual first/second EVM executions, code/nonzero storage/absence, original DB immutability, private files, fsync-on PostgreSQL, seven queues, reserved API, exact ACK and read-only reopen after owned test-input removal. It is functional evidence, not live peer/provider, external-disk, GC or throughput validation. See [plan/checklist/evidence](codemine/runs/20261026-e5-execution-state-proofs/plan.md) and [all remaining work](analysis/open-items.md).

# E3 validation

All results are functional, not performance measurements. Local evidence uses owned `/private/tmp/indexer-e1-20261024/validation/E3-*` directories and disposable fsync-on PostgreSQL. Node services/peers/operational deletion remain held.

## U01

- Shared request RED: `TestRetentionWindowProtectsACKBacklogReorgAndAnchor` failed on a valid 86,400-block acknowledged window (`ErrUnsafe` stub). Same test GREEN after validating exact inclusive cutoff, original anchor, generation, backlog and minimum129; receipt binds the entire permit.
- PG RED: `E3-u01/red.log`, `TestExecutionRetentionDurablePermitRequiresFinalFresh` fails because an actual published seven-part prefix cannot acquire a permit.
- PG GREEN: `E3-u01/green.log`, race-enabled admission succeeds only after fresh. Expanded tests cover missing proof, writer ahead, bad ACK, rewind, cancellation, post-fresh generation change, durable read/part/publication/rewind exclusion and exact completion retry. Later public/ACK markers in this unit fixture are explicitly synthetic; the deleted prefix is an actual seven-part commit.

## U02

The requirement-failing `TestRetentionCollectPreservesWindowAndRequiresNodeCompletion` first returned `ErrUnsafe` on eligible old-generation data. GREEN covers node callback failure, protected window, exact file intent/receipt/release, malformed/corrupt/stale source, every injected file durability boundary, canceled reads/Rebind and Close joining the held collection gate. Fault cases use a sealed synthetic recovered checkpoint to avoid replaying 130 ACK fsyncs per fault; exact ACK durability remains independently tested. The producer integration fixture performs all 132 actual ACK operations.

## U03

`TestCompactPreservesGenesisHeadersPathStateAndProtectedBodies` failed on both real hot and frozen native storage before compaction. GREEN independently reads old/protected data and current state. All persisted phase faults resume only with the same permit; native ownership/capacity/link failures preserve the original. `TestCompactedNativeBlockChainImportsAndReorgsWithoutNodeStart` opens the real pinned core.BlockChain, imports the next generated EVM block and a short replacement branch. It uses a fake consensus engine, not WBFT peer/seal evidence. `TestStateHistoryUsesStateIDsAndPreservesUnchangedBlockParent` uses real path history where every second block changes state, retains the exact native boundary ID and recovers its account value. Genesis also consumes a state ID; the test expectation was corrected to the independently recorded native mapping, with no runtime relaxation.

## U04

Artifact I/O RED refused a valid private output; GREEN must preserve exclusive creation, strict single-document decode and bounds. Capacity RED allowed unobservable storage to continue; the checker now stops before import. Full-baseline admission RED (`E3-u01/full-red.log`) incorrectly authorized partial/missing-bootstrap-digest metadata; GREEN requires a verified full baseline and binds its digest in every permit/final fence. A frozen synthetic producer/storage vector crosses the incompatible geth module boundary; it is explicitly not consensus-valid peer evidence.

## Integration review corrections

- A completed permit file could otherwise be replayed after a DB restore. Collection now requires a **live pending DB lease**, launches the producer with a new challenge over inherited FD3 and accepts only that supervised reply. The producer refuses unsupervised apply; completion files alone cannot launch deletion. Completed retries return a release without launching a child.
- The actual subprocess test exposed a blocking inherited-pipe read: closing a plain `os.NewFile(3, ...)` did not interrupt it on macOS. The owned test child was terminated, its disposable DB cleaned up, and the pipe is now duplicated/nonblocking/reopened as a Go-pollable file. A dedicated test joins the reader while the parent keeps its writer open. The supervised real-PG tool roundtrip passed after this correction.
- Native future-freezer RED: `TestCompactedNativeBlockChainImportsAndReorgsWithoutNodeStart` reported `native freezer stalled at an expired hot-body gap 0 <nil>`. Merely deleting old hot bodies leaves the native freezer unable to advance later. The candidate now extends aligned freezer tables through the retired range, retaining headers/hashes/TD and empty payload positions. The same test must reopen, import/reorg **and advance the native freezer**. Only a standard prepared `ancient/chain` layout is supported; bare/legacy/external layouts are refused rather than rewritten implicitly.
- Full-baseline and supervisor PG tests passed under race. The 132-block vector is regenerated/byte-compared in the executor, consumed through seven actual PG writers in the root module, and crosses the actual inherited-pipe subprocess interface using an owned stub; native compaction is verified independently in the executor. It is not a live consensus/finality or performance claim.

## U05 final local verification

- Full normal root suite passed (`E3-final/functional-root.json`, 697.5s); full root/wire/executor race passed (`race-root.json`, 890.9s). Optional live Anvil/RPC/SLO opt-ins were disabled; no service/peer/performance evidence is claimed.
- Full disposable fsync-on PostgreSQL adapter/app/API/chain suite passed (`postgres-root.json`, 550.8s). The source/DB132-block supervisor roundtrip and restored-pool maintenance refusal were exercised. The script removed only its own container/volume.
- New concurrency: one `GOMAXPROCS=1 -race -count=200` invocation per wire-supervisor, spool read/Close/generation, native lease, capacity/start fence, exporter capacity and real-PG read/supervisor/completion group passed (`E3-concurrency/*.json`). Native lease took 396.6s locally; this is validation cost, not a performance/SLO result. No partial runs are summed. CI retains existing count200 gates and adds the new cases.
- Build/vet/lint across root/wire/executor/astgraph, gofmt, cache lifecycle, examples and offline operations/preflight/shell checks passed (`E3-final-static/other-root-checks.json`). 194 relative documentation links exist. Staged/history betterleaks 1.9.0 scans found no leaks.
- Latest-head required CI4 and squash/main synchronization are resolved from the source-branch PR; this pre-merge source record does not certify a future merge.

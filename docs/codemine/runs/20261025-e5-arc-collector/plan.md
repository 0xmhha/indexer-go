# E5 continuous Arc collector

Base: E5 #218, main `3481cdd`. Reuse the accepted two-pass design and unmodified official Arc/Reth sources. This release adds continuous **read-only native DB replay**, not a P2P client or a node service. Live native directories, peers, operational deletion, disk provisioning and performance measurements remain held.

## Units and acceptance

- U01: move chain-independent durable spool and exporter supervision into `execwire`, preserving StableNet import compatibility, persisted formats, crash recovery, quota, exact ACK and generation/reorg fences. First add a RED public common-module contract test; move existing fault/concurrency tests with the code.
- U02: add a bounded versioned private stdio service to the pinned Rust adapter. Fresh native providers serve identity/head/canonical/replay; exact retained parent and all replay checks remain mandatory. A replay response must still be canonical at output. No node startup, writes to native DB, or latest-state fallback. RED/GREEN offline protocol tests and real native-library fixtures.
- U03: Go Arc backend owns only its Rust child and joins cancellation/exit. Isolate upstream secondary LOG files in an inode-checked private workspace beside the source spool; remove that owned workspace after child exit, preserving native data/unknown siblings. Validate chain identity, request/response bounds and replay commitments before durable append. Expose a continuous collector command with an explicit prepared anchor, private spool/socket, quotas and authenticated exact-ACK supervision. Retain input on failed ACK; Hello alone never advances a cursor or authorizes deletion. Native reorg requests go through the existing indexer coordinator; generations are never invented locally. RED/GREEN fake-child/private-IPC and real disposable PG integration.
- U04: current docs/checklist/continuity, build/fmt/vet/lint/full functional/race/cache/PG/examples and Rust checks, single GOMAXPROCS1/race/count200 for new concurrency, secret scans; draft PR, exact-head CI4+Arc, ready/squash, synchronize clean main. Preserve remote branch.

Retain per-chain/process pool4, seven core queues/part2/API reservation, shared RPC5 physical attempts/s and final uncached fresh plus post-response generation fence. No RPC calls are added by native reads. No Arc native checkpoint export or retention adapter is implied by a common source spool. Unknown/quarantined files stay charged to quota. On full/corrupt/missing history, stop the owned adapter and preserve evidence; never stop an independently operated node.

Maintain current n/n-1/n-2 and the accepted E1 plan. At release report native checkpoint/retention, trace/blob/proofs, E6 and held operations; do not start the next phase automatically.

# E5 Arc execution adapter

Base: E4 #217, main `7ca9995`. Preserve the accepted E1 two-pass/upstream-unmodified design, E2 bootstrap, E3 retention and E4 feature/native balance authority. No node service, live peer, provisioning, operational deletion or performance measurement is authorized.

## Contract

Arc must replay with its pinned official `ArcEvmConfig`, not a generic Ethereum EVM. The already imported block is the first execution; the adapter performs the recording execution against the exact parent's retained state. Verify the native replay and independently reconstructed journal against the header state root and receipts. Missing parent state or incomplete verification fails closed. Pre-block system transitions, each transaction and post-block system accounting retain distinct ordered causes; none is silently attributed to a user transaction. Keep trace/blob supply separate from state coverage and execution validity. Ordinary RPC/native-feed behavior remains available without claiming journal support.

Use a separate library/adapter importing upstream public APIs; do not patch Arc source or start the upstream node. Frozen/offline native EVM and owned disposable PostgreSQL functional fixtures are permitted. The exact supported boundary and unfinished external inputs must be explicit; a mock alone cannot grant Arc execution capability.

The current release boundary is native Arc replay plus read-only one-block export and exact-ACK delivery of retained files. It does not copy the StableNet node assembly into Rust or patch the legacy Arc ExEx installer. A continuously supervised Arc collector/cursor/reorg loop, native full checkpoint export and native E3 retention need their own implementation/validation; keep these as E5 follow-ups, before claiming automated Arc parity. Runtime/API/schema opt-in is justified by native library and actual PostgreSQL evidence, not a mock alone.

## Units

- U01: ordered pre-block journal cause and strict validation/consumer coverage, RED then GREEN; retain all existing StableNet encodings.
- U02: pinned Arc native replay adapter, journal/root verification and bounds; offline RED/GREEN including Arc-specific system transitions and omission/corruption rejection.
- U03: adapter-to-indexer transport/publication, capability boundaries and current developer documentation; mock IPC and fsync-on PG functional integration without a node service.
- U04: required build/fmt/vet/lint/functional/race/cache/PG/examples and native adapter build/fmt/clippy/replay/fixture comparison; any new concurrency single GOMAXPROCS1/race/count200; secret scans; draft PR exact-head CI4 plus Arc job, ready/squash/ff main.

Preserve per-chain one pool4, seven core queues/part2/API reservation, shared RPC5 physical HTTP attempts/s, final uncached fresh and post-response generation fence, undo/outbox and ACK/reorg/lease/recovery/deletion fences. Stage explicit paths, use English Conventional Commits and preserve remote branches. Update the checklist after each unit; n/n-1/n-2 stay current. Report E6 and held operations after release; do not start them automatically.

# Arc native checkpoint export and retention

Base: main `27bb982` (#219). Keep the accepted E1 two-pass execution design and unmodified official Arc/Reth revisions. Native library fixtures use disposable owned directories; operator DBs, node services, peers, disk provisioning and performance measurements remain held.

## Implementation contracts

- U01: export exact native historical state as the existing full checkpoint stream. Recover raw keys from official genesis or a prior full checkpoint plus retained native changesets. Rebuild account/storage tries independently and compare the canonical header root before publishing an exclusive private fsynced file. Missing history, incomplete keys, code, canonicality, bounds or capacity fails closed. No latest-state or partial-to-full fallback. Test RED then GREEN, including independent Go verification.
- U02: integrate Arc native retention with E3's exact full-baseline permit, ACK/window, live DB supervision, durable source completion and DB-issued release. Preserve native headers/current state/code, original pre-anchor payload and protected window. Native-format segment boundaries may retain extra old data. Full cutoff state must remain recoverable and usable as the next export key seed. Never apply StableNet layout deletion to Reth.
- U03: enforce offline owner exclusion, inode-checked copy/swap/recovery and persistent pending fences. A native owner must use the managed guard; arbitrary independently launched clients are outside this maintenance contract. No operational GC activation in this work. Verify owned native fixtures and failure/recovery paths; update commands and operational docs.
- U04: build/fmt/vet/lint/functional/race/cache/fsync-on disposable PostgreSQL/examples/native checks; one GOMAXPROCS1/race/count200 for new concurrency; secret scan; draft PR and exact-head CI4+Arc, ready, squash and fast-forward clean main. Preserve branch.

Per chain/process DB pool4, seven queues/part2/API reservation, shared RPC5 physical attempts/s (including failover/retries), final uncached fresh and post-response generation fences are invariants. Native observations never grant consensus/finality or deletion authority. Unknown/corrupt/unacknowledged files remain protected and charged. Do not change other projects.

Current unit and n-1/n-2 are maintained in `continuity.json`; the accepted E1/E2/E3 plans remain authoritative for shared boundaries. Report remaining E5 traces/blob/proofs, E6 and held operations after release.

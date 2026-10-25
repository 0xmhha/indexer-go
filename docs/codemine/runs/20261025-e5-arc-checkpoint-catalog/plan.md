# Arc bounded-memory checkpoint catalogs

Base: main `03d0790` (#220). This is the first native-retention extension: remove the one-million candidate-key ceiling before attempting state-history compaction. The accepted E1/E2/E3 design, two executions and unmodified Arc/Reth revisions remain authoritative. No state history is deleted in this release.

- U01: RED → GREEN for catalogs above one million keys. Collect genesis/seed/history candidates into owned private disk runs, sort/deduplicate with bounded chunks and merge fan-in. Bound input, scratch bytes, descriptors and output; malformed/truncated runs or capacity errors fail closed. Scratch uses the chosen checkpoint output filesystem, or the owned native helper workspace for verification.
- U02: stream secure-key-ordered accounts/storage through incremental trie builders, including a disk spool for a single large account. Stream seed keys without reconstructing an in-memory map. Keep byte-identical NDJSON, exact header/root/coverage/code checks, no-clobber durable publication and fresh native canonicality. Verify temporary storage-spool digests before publication. Apply the same path to native export and retention cutoff verification.
- U03: owned native-library/CLI and independent Go verifier compatibility, fault/cleanup/bounds/large-catalog functional cases; current documentation and n/n-1/n-2. This is functional validation, not throughput measurement.
- U04: required local build/fmt/vet/lint/functional/full race/cache/PG/examples/native checks, staged/history secret scan, draft PR, exact-head CI4+Arc, ready/squash and clean fast-forward main. Preserve source branch and remove only task-owned disposable caches afterward.

Invariants: pool4/part2/API reservation, shared RPC5 physical attempts/s, final uncached fresh and post-response generation fences, exact ACK/window/full-baseline/live supervision/permit/receipt/release and unknown-data protection. Operator DB maintenance, actual node/peers, disk provisioning/formatting, operational GC and performance measurements remain held. Other projects/shared caches are untouched.

After release report state-history/index compaction, trace/blob/proofs and E6; do not automatically activate operations or begin another phase.

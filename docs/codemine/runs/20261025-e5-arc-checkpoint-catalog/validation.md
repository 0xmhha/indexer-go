# Arc checkpoint catalog validation

All required local checks are GREEN. The [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-checkpoint-catalog) resolves exact-head CI4+Arc and squash/main release status. Logs and disposable build caches are owned by `/private/tmp/indexer-arc-catalog-20261025`; shared caches and other projects are preserved.

## Required functional evidence

- RED: the old validator rejected 1,000,001 candidates with `checkpoint catalog exceeds one million address/slot keys`. GREEN: the requirement test now constructs and completely reads that many keys through the production disk catalog, without the fixture map helper.
- GREEN: the large catalog and tiny-chunk/multi-pass cases check complete iteration, secure-key order, exact deduplication and bounded buffer/fan-in instrumentation. This is a functional count check, not an RSS or throughput measurement.
- GREEN: private workspace ownership/replacement preservation, poisoned input/quota errors and truncated/reordered/changed/trailing run data. A same-length replacement with a valid key passes row/order checks but fails the final run digest. Creation explicitly uses 0700 because the generic tempfile directory mode was insufficient; checks were not relaxed.
- RED: a changed temporary storage row was accepted before spool checksum validation. GREEN: the checksum rejects it. Dense state with 1,024 added slots, seed roundtrip/re-export byte equality, zero/deleted candidates, spool quota/no publication and poisoned truncated seeds pass. Existing missing-key/code/root and no-clobber cases pass.
- GREEN: native export and retention cutoff verification share the streaming catalog. Native fmt/build/clippy/all-targets passed 20 library + 2 CLI tests, including owned native-v2 stores and the actual maintenance helper. Three regenerated Rust→Go fixtures are byte-identical. Full root Go functional passed, including independent checkpoint verification.

## Release gates

| Local gate | Result |
| --- | --- |
| Root build/vet/lint/gofmt, cache lifecycle, examples, offline operations/preflight/shell | GREEN |
| Wire, StableNet executor and AST module build/vet/lint | GREEN |
| Full root functional | GREEN |
| Full `make test-race` (root, wire, StableNet executor) | GREEN |
| Owned fsync-on PostgreSQL storage/app/API/StableNet bundle and container/volume cleanup | GREEN |
| Native fmt/build/clippy/all-targets, actual helper and three frozen fixture comparisons | GREEN |
| Current relative links and diff whitespace | GREEN |


Staged secrets are scanned before the feature commit and its history is scanned before push. Exact-head CI4+Arc is required before ready/squash/main sync; those mutable statuses are resolved through the source-branch PR. New Go concurrency requires a single GOMAXPROCS1/race/count200 invocation if introduced; catalog processing is sequential Rust.

The first local example run hit the sandbox’s `httptest` bind restriction. Only the affected receipts module was retried in the permitted context and passed; prior build/vet/lint/cache/chainprofile successes were kept. Offline operations, separate module build/vet/lint and full root functional passed. New Go concurrency was not introduced; unchanged local single200 selections were not restarted, and existing CI selections remain enabled.

No node/peer/operator DB operations, disk provisioning/formatting, operational GC activation or performance measurement is authorized by these functional fixtures. Pool4/RPC5/final fresh and all E3 maintenance boundaries remain unchanged.

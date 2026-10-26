# Arc native proof validation

Scope: pinned Arc/Reth libraries, owned retained native-v2 MDBX/static/RocksDB fixtures, synthetic first/second Arc EVM execution, private files/mock child/IPC and disposable fsync-on PostgreSQL. Actual nodes/peers/operator DB, disk formatting/provisioning, operational GC, performance and other projects remain held. Local logs are `/private/tmp/indexer-e5-arc-native-proofs-20261026/validation`; they are evidence, not repo dependencies.

## RED → GREEN

| Requirement | Runtime RED | GREEN |
| --- | --- | --- |
| Native block-end acquisition | `native-red.log`: retained-provider export returns not implemented | Native-v2 history at3/4/5/129/130, nonzero storage, deletion/absence/recreation, wrong-root rejection; same provider exact root proof/code and native canonical recheck |
| Source policy | `admission-red.log`: changed/disabled policy accepted on empty/pending and legacy source | Nonzero copied binding, mismatch before recovery and under lock; new policy cannot retrofit any existing spool; exact maintenance intent/permit preserves binding without active-write authority |
| Required producer evidence | `collector-red.log`: required proof omitted but replay succeeds | Operation5 exact selection, Go proof/root/profile/provider/ref validation before Append; bad/legacy proof joins child and preserves source |
| Private strict selection | `selection-red.log`: valid private selection refused | Bounded private regular file and parent, symlink/public-mode/duplicate/unknown/empty/oversized/trailing JSON/cancellation rejection before startup |

Initial setup-only failures (a wrong module-relative edit path, no matching test from that failed setup, an integration assertion using a nonexistent view field, and a capacity test that forgot quarantined records also count) are excluded from feature RED evidence. The quota test now independently proves the full limit before allowing explicit additional room for reorg quarantine. No partial count runs are combined.

## Functional evidence

- Native proof API uses pinned `StateProofProvider::proof` and exact historical block-end providers; no upstream change, RPC, third EVM, global pruning or node startup. Actual retained native-v2 provider tests cover historical changes/absence against header roots.
- `arc-state-proofs.json` uses an independent full-trie proof oracle over the FIRST Arc EVM output, then attaches proofs only after SECOND replay/root/journal verification. Existing five Arc JSON and checkpoint fixture bytes remain unchanged. Synthetic chain1337 is used only in libraries/layered fixtures; production Start still requires5042/5042002. This does not claim an end-to-end operator DB command or live import conformance.
- Collector child protocol tests cover exact enriched bytes, missing/invalid provenance/proof, normalized immutable scope, preflight, source quota, reorg reuse, restart, exact/lost ACK and cancellation with native owner lease release. Ownership is registered before handshake cancellation; failed identity joins/removes only owned scratch and releases the lease.
- Real fsync-on PG integration exercises complete enriched input through existing source spool/window/receiver/seven writers, final fresh failure/retry, exact lost ACK/no repeated outbox, reserved DB-only proof API, zero API RPC and read-only reopen after owned raw test-file removal. Pool4/part2/reservation, RPC5 physical-attempt budget and final uncached fresh/generation/COMMIT boundaries remain unchanged.
- Native build/fmt/clippy and all-target offline tests: GREEN (`native-results.json`). Targeted Go and PG: GREEN (`go-final-targeted.log`, `arc-proof-pg-results.json`).

## Release gates

All required local gates passed on the final implementation:

- Root functional: GREEN (`normal-results.json`,701.4s).
- Complete root/wire/StableNet `make test-race`: GREEN (`race-results.json`,1052.8s).
- Root/module build/vet/lint, fmt, examples/cache and offline operations/preflight/shell: GREEN (`static-results.json`, `modules-results.json`), zero lint issues.
- Full fsync-on PostgreSQL adapter/app/API/bundle: GREEN (`postgres-results.json`,1258.5s).
- Final changed Rust build/fmt/clippy/all-target offline tests: GREEN (`native-final/native-results.json`). All six Arc JSON and checkpoint generated fixtures match repository bytes; five old JSON/checkpoint fixtures remain unchanged.
- New single `GOMAXPROCS=1 -race -count=200`: collector/CLI493.5s, source admission/recovery159.1s, Arc proof PG/API/spool/fresh/lost-ACK/RO1068.5s (`arc-proof200-results.json`). Each is one complete invocation; no partial counts are added.
- Changed Markdown:104 relative links/anchors verified. Formatting/diff clean. Whole-history secret scan: no leaks.

Durations identify functional gates, not throughput measurements. CI preserves existing selectors and adds source admission/Arc PG/fixture comparisons.

Staged/history secret scans, exact-head lint/test/postgres/examples/arc, ready/squash and clean main sync complete release. Preserve `refactor/e5-arc-native-proofs`; delete only this task's caches afterwards and retain logs. The [source-branch PR](https://github.com/0xmhha/indexer-go/pulls?q=is%3Apr+head%3Arefactor%2Fe5-arc-native-proofs) is the release status source. [Remaining work](../../../analysis/open-items.md) separates blob transport, richer call queries, E6 and held operations.

# E5 execution state proof validation

Scope: owned private files, pinned EVM libraries and independently generated native trie paths, mock RPC/IPC and disposable fsync-on PostgreSQL. No actual node/peer/operator DB, disk provisioning/formatting, operational GC or performance measurements. Local evidence is under `/private/tmp/indexer-e5-state-proofs-20261026/validation`; these files are not repository dependencies. Arc Rust source is unchanged and required Arc CI remains mandatory.

## Requirement RED → GREEN

| Contract | Runtime RED | GREEN evidence |
| --- | --- | --- |
| Optional wire field | Correct canonical current test against previous-main wire integration rejects new JSON field (`wire-baseline-runtime-red.log`) | Bounded optional field, invalid version/labels/limits rejected, exact legacy bytes retained |
| Authenticate supplied proof | Prepare accepted `{}` without error (`consumer-red.log`) | Profile/header root, exact selection, account/slot/code inclusion or absence, incomplete/corrupt/wrong-path rejection |
| Required lost-ACK boundary | Missing selected evidence returned ACK (`strict-red.log`) | Required selection checked before scheduling/confirmation, no ACK/callback on missing data |
| Configuration | New required-selection YAML key rejected (`config-red.log`) | Round-trip and before-IO rejection of empty/duplicate/oversized/typo selections; existing DB4/RPC5 limits unchanged |
| Native exporter | Empty successful output instead of real paths (`native-functional-red.log`) | Actual retained root paths/code/nonzero storage/absence, deterministic fixture and unchanged original DB |
| Native cancellation | Cancel during write returned nil (`native-cancel-red.log`) | Context error after write, no success; pre-cancel/bad selection produces no output |
| Immutable attachment | Original exact input returned without evidence (`attach-red.log`) | New pre-spool digest, unchanged original/generation, existing bundle/output refusal, private fsynced file and cancellation checks |
| Borrowed supply | Shared caller never invoked (`supply-red.log`) | One exact blockHash/requireCanonical proof/code batch; member/pruned error fails atomically; no dial/retry/latest fallback |
| Supply preflight | Invalid provenance made one caller invocation (`supply-preflight-red.log`) | Invalid label rejected before IO |
| Storage | Same accounts-part writer/reader interfaces absent (`pg-red/proof-pg.log`) | Actual native input through seven parts; prepublication privacy/fresh rollback/marker/ACK/corruption/prefix/rewind/no-slot/RO cases |
| API | Route404, then checksum address400 (`api-red.log`, `api-checksum-red.log`) | Snapshot/reservation, lowercase/checksum addresses, strict pins/status/privacy/no-store and per-chain mounting |
| CLI | Valid request never delegated (`command-red.log`) | Offline composer invocation, exact identity/deadline/error/help behavior |
| Existing no-slot codec | Its own null storageProof serialization was unreadable (`empty-proof-red.log`) | No-slot round-trip fixed only at storage-proof decoding; missing positive selections and missing evidence still fail |

Setup-only failures are excluded: wrong working-directory-relative paths/no matching native tests, a temporary new test import requiring go.mod changes (removed; no dependency changes), initial sandbox socket denials, an initially noncanonical wire test construction (corrected; previous-main canonical RED reproduced in an owned copy), one edit overlapping compilation, a diagnostic slice bound, an invalid subtest regexp and an initially nonexistent scratch working directory. These are not feature RED/GREEN evidence. Final targeted logs contain actual test executions; no partial counts are summed.

## Targeted evidence

- `evm-state-proofs.json` bridges actual first/second EVM execution and native standard-MPT path generation. The existing seven fixture bytes remain unchanged. No node service starts and the original database snapshot is unchanged.
- The pure verifier retains capture aliases/API/serialization. Complete paths/values/code, malicious equal-checksum data, missing rows, wrong selection/profile and strict JSON unknown/duplicate keys fail. Consensus balance is kept separate from native currency accounting.
- Real fsync-on PostgreSQL stores the new bundle in the existing accounts part and proves privacy until publication, fresh rollback, exact ACK, same-prefix generation/digest retention, missing/corrupt data rejection before fresh and rewind cleanup. A no-slot/code-unrequested account remains readable without inventing code collection. Values are parameter-bound.
- Actual native evidence → private no-replace file → seven queues → reserved public proof API → exact ACK → owned input removal → read-only DB reopen passes (`app-final-targeted/proof-app.log`). The API adds zero RPC calls. This is not operating GC activation.
- Public API accepts checksum addresses while wire identity remains canonical lowercase. It distinguishes unselected/uncollected keys from proven absence, rejects malformed/duplicate/unknown pins, and keeps private errors out of output.

## Release gates

All required local gates passed on the final implementation:

- Root/module build/fmt/vet/lint, examples/cache and offline operations/preflight/shell: GREEN (`static-results.json`, `modules-results.json`), lint zero issues.
- Full root functional: GREEN (`normal-results.json`, 567.5 seconds).
- Complete root/wire/StableNet `make test-race`: GREEN (`race-results.json`, 787.7 seconds).
- Full fsync-on PostgreSQL adapter/app/API/bundle matrix: GREEN (`postgres-results.json`, 895.0 seconds).
- New single `GOMAXPROCS=1 -race -count=200` groups: native files/seven queues/reserved API/ACK/RO restart 408.6 seconds; receiver/API/CLI/shared codec 37.1 seconds; native export/cancellation 68.0 seconds (`proofs200-results.json`). Each group completed one invocation; partial counts are not combined.
- Changed documentation: 240 relative links/anchors verified, formatting/diff clean; previous seven fixture bytes and Arc Rust source unchanged.

Durations identify functional gates, not performance measurements. Existing CI selectors are preserved; new native export and actual PostgreSQL proof integration receive their own required concurrency coverage.

While the source-branch PR is open, staged/history secret scans, exact-head lint/test/postgres/examples/arc, ready/squash and clean main sync remain mandatory. MERGED with those gates completes this scope. Preserve `refactor/e5-execution-state-proofs`; clean only this task's caches after release and retain evidence. [All remaining work](../../../analysis/open-items.md) keeps Arc native acquisition/collector integration, real transport, E6 and held operations separate.

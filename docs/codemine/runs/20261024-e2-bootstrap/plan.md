# E2 checkpoint/bootstrap implementation

Base: E1 #214, main `3ae27d5`. Current scope implements offline preparation, not live operational approval. Two EVM executions, pool4/part2, shared RPC5 HTTP/s and publication fresh are unchanged.

## Decisions and acceptance

- Start modes: `genesis` (height0, full genesis allocation) and `checkpoint` (explicit trusted chain/height/hash/root, full or selected proven state). Normal restart preserves the original anchor. No implicit head selection.
- A streaming v1 NDJSON artifact carries a profile-decoded header, accounts including StableNet optional Extra, code, storage and optional MPT proofs. Full coverage requires rebuilding **every** storage root and the account root. Partial coverage verifies inclusion/absence for every supplied key, never infers full coverage. Bounded records and a required terminal marker prevent incomplete input being accepted.
- PostgreSQL bootstrap uses one exclusive execution transaction on the existing pool4: empty-schema admission, anchor, seven cursors, baseline histories, coverage and artifact digest become visible atomically. Verify trusted identity and roots, then uncached canonical fresh immediately before commit. Reject existing execution/legacy data; restart/recovery uses a new explicitly named schema, never clears the old one.
- Tools verify/export without starting node services. Genesis export uses official allocation; prepared-node export opens chaindata and path trie read-only, requires preimages and exact root availability. Missing parent/header/state reports recovery requirements and preserves files. No blind SetHead, node-state fabrication, peer/bootstrap trust claims or generation decrement.
- Recovery diagnosis compares DB/source identity, original anchor, generation/ACK and exact parent availability. A source ahead of a restored DB is a blocking mismatch; recover from a matched backup or bootstrap a new schema/spool at an explicitly verified anchor. No ACK inferred from Hello.
- Full DB coverage permits proven zero for an unlisted account/slot from the anchor onward, while partial coverage remains unknown. Existing E1 partial baselines stay partial on migration.

## Units and evidence

| Unit | Scope | Acceptance |
| --- | --- | --- |
| U01 | stream format and independent root/proof verifier | RED then GREEN for missing/truncated/tampered/full-vs-partial/Extra/code/absence and bounds/cancel |
| U02 | atomic PG bootstrap and coverage migration | RED then GREEN on disposable fsync-on PG: all-or-nothing, full-zero/partial-unknown, fresh rollback, duplicate/nonempty refusal, pool4 |
| U03 | offline genesis/prepared-node export, inspect/import entry points, recovery diagnosis | RED then GREEN using real trie fixtures and mock RPC; no node.Start; no deletion or implicit recovery |
| U04 | integration, developer guide and release | meaningful prior tests + build/fmt/vet/lint/race/cache/PG/examples, new concurrency200 only if added; secret checks; draft PR → four required latest-head checks → squash → main sync |

Each feature updates checklist and continuity (n/n-1/n-2). These tightly coupled units ship as one coherent PR so coverage format, migration and operator entry points cannot be released half-connected. No speculative performance or host-load loops. No other projects, formatting, actual node service/peer, GC activation or throughput measurements.

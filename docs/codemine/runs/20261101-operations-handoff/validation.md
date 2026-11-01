# Handoff validation

## Prior N6 release, not a new test run

| Identity | Executed value |
| --- | --- |
| PR | [258](https://github.com/0xmhha/indexer-go/pull/258), squash merged |
| Source head | 1817b6827ad7d6ce1cda578114db5081b8b8a006 |
| Reviewed tree | 364d19c004e8cb28d5806cc01e3fffb2aaa323cd |
| Main | a282758b92a40f8cd46fee34d3e648f7a6b855a7 |
| Parent | f4335d29c012f645c9c5d67914883ee73afe6442 |
| Exact-head short CI | [334](https://github.com/0xmhha/indexer-go/actions/runs/41162469157); scope/arc/examples/test/lint/postgres all success |

The source tree/main/parent and clean origin/main alignment are checked against
Git. Prior release and qualification receipts agree. Existing local actual
SDK/App/PG matrix and selected CPU1/race200 retain their own scope; this handoff
does not repeat them or label short CI as heavy native acceptance.

## Selected-path observations,2026-11-01 KST

At15:36:36, lstat found no `/Volumes/IndexerData` and no files at the two known
repository-relative candidate paths `deployments/testnets/host.json` and
`deployments/native/host.json`. The latter is only a previously explored path,
not a supported native host schema. No filesystem search or paths inside such
files were followed. Unknown manifests elsewhere remain unknown.

At15:37:09, read-only diskutil metadata identified `/Volumes/X31` as external
ExFAT with the previously recorded VolumeUUID; global permissions were disabled.
This is not an APFS candidate admitted for native DB/PGDATA, capacity evidence,
a data-content scan or authority to format. The sandboxed metadata call was
unavailable; the authorized host metadata retry succeeded. Existing old capacity
numbers remain explicitly dated2026-10-29 and were not remeasured.

Private config SHA256 and mode match the pre-existing baseline; values are not
printed. No private DSN/key, operator checkpoint, peer, image or backup was
opened, generated, supplied or treated as verified. The [readiness table](../../../OPERATIONS_READINESS.md)
records what remains HOLD and what evidence each next step needs.

## This documentation change

Local links/anchors passed:14 selected documents,364 relative links, zero errors.
All24 CI policy tests passed. The11 changed Markdown paths classify as
`docs_only=true`, `code=false`, `arc=false`, `full=false`; whitespace is clean.
Staged secret scan found zero. Reachable history had the same15 previously
reviewed public fixture findings, matched by commit/file/rule/line against the
hash-verified prior receipt; there were no unreviewed findings. Current-guide
assertions confirm the selected stale N6 statements are absent.

Private config hash/mode and executable/config/workflow/example bytes are
unchanged. Accepted E1/first-import plans are untouched. Build/SDK/DB/race/
performance/operating execution are outside this documentation-only scope.

New-head CI and the squash/main receipt are checked only after the corresponding
actions complete, without pre-certifying them in this document.

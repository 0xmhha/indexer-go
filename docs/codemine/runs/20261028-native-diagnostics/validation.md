# Native diagnostics validation — 2026-10-28

The authoritative implementation is the managed execution worktree, based on
main `8fdd0b6eaca27fb107e9b2129fd0112b296f2b92` (Native preflight #232).
The portable [source identity](../../../../verification/native-diagnostics/source.json)
and [gate receipts](../../../../verification/native-diagnostics/checks.jsonl)
record precise cwd, argv, source hashes, subprocess exit codes and complete local
log hashes. Full local logs are intentionally ignored, not committed. Receipts
are observations from this attempt, not previous-version or operating evidence.

The first test command `go test ./internal/nativediagnostics -run TestUnknownNeverReady -count=1`
compiled, then failed the requirement assertion: uninspected native/source/indexer
returned exit0 instead of HOLD/2. [RED output](../../../../verification/native-diagnostics/red.txt)
records that behavioral failure before implementation. Final source-bound checks
include the complete product suite and actual compiled CLI invocations, as well
as shared metadata/source/native format regressions. No new concurrent state was
introduced; no new count200 campaign is claimed.

The initial dependency-bearing test invocation found a pre-existing invalid
GVM module-cache location. Subsequent commands explicitly use the already
available Go module cache; no shared cache is removed or relocated. Initial
review lint found that shared native identity aliases require keyed literals;
that was fixed before final source freeze. Prior focused tests are development
evidence only; final contributor receipts identify the frozen sources.

Owned PostgreSQL uses the existing non-TCP fixture script and explicitly checked
`/Library/PostgreSQL/16/bin` binaries with fsync=on, serial package execution.
Heavy functional, full root/wire/executor race and PG gates run serially with
full20m package timeouts and package parallelism2 (PG1). No product assertion,
durability, freshness or RPC semantics is weakened. Failures/skips, if any, remain
in the actual receipt rather than being renamed successful checks.

Library/mock/native-format and owned PG regressions do not validate a running
node, native executable, P2P, operational DB, APFS/device, host load or performance.
No such resource is required or touched. Actual native DB head/state/indexes,
operating indexer DB, live canonical freshness, full reconstruction, external
manifest/APFS/enode and activation remain unperformed/HOLD.

The worker makes one local Conventional commit, returns its source/branch/tree
and tracked status, then stops. The parent reviews and owns publication, actual
exact-head lint/test/postgres/examples/arc CI5, ready+squash with SHA guard and
clean main synchronization. No worker push/PR/CI polling/merge/sync is claimed.

The first full functional gate failed `TestChainIdentifierLimits`: shared native
metadata selected chain layouts by product name. The fix moves layout selection
to each product adapter, without changing the recorded architecture limits.
A separate actual-CLI RED (`operator-red` in the receipts) exposed exact HOLD
path arguments being rejected. Empty/HOLD operator paths now stay unresolved;
missing flag arguments and malformed artifacts still FAIL. Source observations
with unread markers omit pending flags; artifact exports require nonoptional
fields so omitted zero/false values cannot fabricate genesis or pending absence.
These review changes are frozen under a new source hash before final gates;
first-version results are not attributed to that later source.

The intermediate `functional-final` invocation was deliberately interrupted
(exit1, complete log retained) before editing its frozen sources: review found
that a joined known-unsafe/changed/missing error could be classified HOLD.
`TestKnownCorruptionDominatesChangedAndMissing` then compiled and failed its
behavior assertion (got HOLD/2 instead of FAIL/1). Hard corruption/unsafe errors
now precede changed/missing HOLD in classification. A new freeze and the `-v2`
final contributor receipts cover this correction; interrupted/prior-source
checks are never counted as passing final gates.

Final review also checked the actual PostgreSQL bootstrap schema: supported
partial/prepared exports stay HOLD, while unsupported modes, a known zero digest,
full/prepared conflicts, genesis/nonzero-anchor conflicts and out-of-contract
partition bounds FAIL. Those cases have explicit functional regressions.

The first complete race target at source07b153e4 failed the existing
`TestEngineBlobOptInFilesAndJWT` with its unchanged context deadline; no race
warning appeared. The isolated unchanged test passed with GOMAXPROCS2. Final
heavy gates therefore pin CPU concurrency2 as well as package parallelism2;
product HTTP deadlines, RPC5 and test assertions are unchanged.

Before retry, the transition inspection was consolidated onto the existing
production pure journal/state check. A behavioral RED proved that committed-To
with a different retained Base had been reported as pending instead of corrupt.
The shared pure validator now rejects that conflict before any scan or write;
recovery itself is never invoked by diagnosis. The final `-v3` source freeze,
functional and `race-final` receipts include this correction. Earlier passing
functional checks and the failed race remain attached to their original hashes.

All required implementation gates passed at final frozen Go source
`e4ec28b6fcf8181f0fb79baeb953e9e4695efeaa251b3e31782a0330bb7cba38`:
pinned build/fmt/vet/lint, module build/vet/lint, full root functional, complete
root/wire/StableNet executor race target, examples, cache lifecycle, product
boundary/actual CLI regressions and the full owned PostgreSQL16 fsync-on suite.
The PostgreSQL fixture shut down and removed its owned directory normally.
Each final gate recorded exit0 and the same before/after source hash. Full raw
logs remain local and ignored; the linked structured receipts carry their exact
hashes. No operating/P2P/native-executable/performance validation is claimed.

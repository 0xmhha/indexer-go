# Validation

Base: main `1f311393b391e02d847f305c2fa5e9aa067c8f3d` (#237).
Whole Go/module source before work:
`b8da45385fbb6a218fb63f22b7d1517113acfbfa65f87c7323c5e98d08a98f92`.

Only deployment declarations, their offline Python checks and documentation are
in scope. Current ignored config, runtime Go/Rust, defaults, SQL, RPC and fresh
contracts remain unchanged. Checkpoint/source/native/config/secret host paths
in a declaration must never be opened by the checker.

Actual RED→GREEN evidence includes:6 DB/RPC/fresh conflicts initially masked by
HOLD; known GID20/21 conflict hidden by unknown UID; the run-lease layout
assertion; and real Arc file-preflight rejection of scratch containing spool.
The latter component review corrected the initial pure-Python GREEN: Arc's
existing guard needs a writable sibling-lock parent and production preflight
requires disjoint scratch. Final layout has writable parent/read-only DB-child
binds plus explicit `/scratch`. No runtime guard, source contract or acceptance
condition was relaxed. One earlier overlap fixture itself was corrected to
actually overlap the other declared directory; it was not a product defect.

Pinned PyYAML6.0.3 final local GREEN:13 runtime tests and13 existing diagnostic
tests, including both actual host-binary fixtures, all passed with no skips.
Four existing Go diagnostic binaries were built once into the owned task
directory and reused across the targeted RED/GREEN cases; they start no App,
native service, DB or RPC. The default Go module-cache path initially failed
before compilation; selecting the existing project cache fixed the environment
without global changes or deleting caches. CI runs the extended `test-cli.sh`
with its own temporary cleanup. State-proof batch contract1 and release
sequence4 tests passed. Checker CLI returned HOLD/2 with a valid declaration.

Coverage includes file-only access/no environment or network, hostile YAML,
symlink/FIFO/directory/size/depth/event bounds, redacted errors/short output,
limits, scoped mounts/secrets/partial UID, no extra node/stdio sidecar, exact
container-path acceptance and unchanged synthetic inputs/no locks. Syntax,
production import isolation, document links/anchors and continuity were checked.
Actual Docker overlay/flock/bind identity and operator files remain unverified.
Final document checks passed238 relative links/3 anchors. Staged secret scan
found0 new findings; history had15 reviewed public fixtures/0 unreviewed findings
(raw history scanner exit1 is preserved, not reported as a zero-findings scan).

[Frozen source](../../../../verification/native-runtime/source.json),
[checks](../../../../verification/native-runtime/checks.json) and
[RED receipts](../../../../verification/native-runtime/red.json) record exact
bytes and private-log hashes. The CI workflow adds only the new offline unit
invocation; its existing owned host fixture script also invokes the new suite
with already built binaries. Existing Go/race/200/postgres/Arc conditions remain
byte-identical.
Whole Go/module bytes and ignored config bytes/mode match the base. The prior
complete local race receipt on those exact Go bytes and its retained actual log
SHA were verified; no new complete local Go/race/native-EVM/PG/performance
campaign is claimed beyond the explicit four diagnostic builds/file fixtures.
There is no new concurrent runtime state requiring a new local200 campaign.

New-head CI5 remains required before squash/main sync, not substituted by
previous CI. Actual image/ABI, APFS/mapping, peer/native provider and operating
recovery/performance are held. No service, Docker command or host-data probe ran.

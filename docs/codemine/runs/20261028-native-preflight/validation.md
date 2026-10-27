# Validation and release handoff scope

This retry reconciled the existing isolated implementation and draft PR into the
AC workspace, inspected the production adapters, and executed validation again.
The current portable receipts are [checks.json](../../../../verification/native-preflight/checks.json)
and [compact gate output](../../../../verification/native-preflight/gates.log).
Commit/PR/head identity belongs to GitHub metadata and the parent handoff.

## Behavioral RED then GREEN

Two new tests compiled successfully and failed their behavioral assertions before
production fixes in this retry:

- `GOMODCACHE=/Users/kevin/Library/Caches/go/pkg-mod go test ./x/arc/preflight -run 'TestNativePreflightBehavior/shared_native_executable$' -count=1`
  returned FAIL instead of PASS: a shared collector/retention executable was
  compared against itself. It is now checked once against every storage path;
  a paired negative case keeps executables inside native data forbidden.
- `GOMODCACHE=/Users/kevin/Library/Caches/go/pkg-mod go test ./x/stablenet/preflight -run TestGenesisModeCannotHideKnownCheckpointConflict -count=1`
  returned HOLD instead of FAIL: an unresolved native height hid genesis mode's
  known height0 constraint when all other files agreed on a nonzero checkpoint.
  The fix retains the missing-input finding while comparing the mode constraint.

Both product suites then passed with `-race -count=1`. They also cover complete
synthetic declarations, unresolved APFS/enode/checkpoint inputs, known chain/
network/genesis/anchor/root/generation/start/floor conflicts, nested paths,
quotas, overflow-safe headroom, DB4/RPC5/head/part2/API1/writer7/final-fresh policy,
malformed/truncated/deep/duplicate/unknown input, unsupported/mixed profiles,
E6 policy/fork/anchor boundaries, secret redaction and source preservation.
The environment-isolation test uses the actual production PostgreSQL pool/DSN
variable names. The command creates no runtime concurrency; no new count200
experiment is needed.

## Gates and retry conditions

Required validation includes root build/vet/lint/functional tests, complete
root/wire/StableNet executor `make test-race`, execution module build/vet/race,
module/astgraph lint, gofmt, cache lifecycle, examples, owned PostgreSQL functional
regression, executable fixture checks, documentation links and staged/history
secret scans. Receipt commands identify successful invocations exactly.

The first full race invocation overlapped functional and PostgreSQL regressions.
It failed an existing blob fixture's context deadline and the App package's
10-minute timeout while `TestUnboundedQueriesUseTheIndexes` ran, without a race
report. The full functional suite passed. The final race invocation retains all
packages, race and count1, with `GOFLAGS=-p=2` and
`GOTEST='go test -timeout=20m'`. No product deadline or assertion was changed.

The first libpq16 fixture attempt could not load a missing host ICU73 library.
No host installation or library symlink was changed. The subsequent PostgreSQL15
fsync-on fixture passed adapter/App/bundle regressions but two API tests timed out
waiting for migration locks while packages ran in parallel. It shut down normally
and removed its owned directory. The final complete PostgreSQL gate serializes
packages (`GOFLAGS=-p=1`) using separately available PostgreSQL16 binaries and a
new owned fixture. Its normal shutdown/cleanup is part of the command result.
No operator cluster or Docker daemon/image/container/volume is used.

## Offline evidence limits and release owner

The executable check is
`python3 verification/native-preflight/verify.py build/arc-execution-preflight`;
links/anchors use `python3 verification/native-preflight/check-docs.py`.
The executable check verifies PASS/0, HOLD/2, FAIL/1, deterministic bounded JSON,
redaction and unchanged explicit synthetic source files. Inspection also confirms
unchanged storage/fetch/client/Arc upstream sources and separate product linking.

Synthetic PASS proves declared-file consistency. It does not authenticate an
anchor, rebuild full state, check genesis file contents, probe APFS/disk/peer,
prove actual process settings or verify live canonical freshness/finality.
Operational startup, GC, restore, provisioning and performance remain held.
No node/App/operating DB/public RPC/native executable is started by preflight.
Owned library/mock and disposable PG functional regressions are test evidence.

The parent owns exact-head lint/test/postgres/examples/arc CI5, tree review,
ready+squash merge and clean main synchronization. This implementation handoff
never polls CI, merges, deletes branches or resumes previous jobs/observers.

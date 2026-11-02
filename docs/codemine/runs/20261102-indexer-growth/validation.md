# Indexer growth qualification

## Released result

[PR260](https://github.com/0xmhha/indexer-go/pull/260) source head
`680d57c0b4b1ae8047424180d660ea130886a620` passed
[CI336](https://github.com/0xmhha/indexer-go/actions/runs/41386648007): all six jobs
succeeded. Root/execution short tests, build, static checks, examples, cache and
offline operations passed; durable PostgreSQL core11 passed with no fail/skip.
The Go-only change did not run native SDK smoke or manual full/stress suites.

Squash/main `030c1646b02f41dbad57022681d180c59e135458` has reviewed tree
`157ddb05974220efae05be51eaddcfa1ec3910bc` and one parent
`cc3e35c27d255e5efb0907b332b7ded0c711bb03`. Clean local main/origin sync and
unchanged private config were verified at release. This records completed
historical evidence; it does not claim another test run during the session
documentation update. [Current state](../../../SESSION_STATE.md) is portable;
machine-local logs are supplementary.

## Behavioral RED and GREEN

The first compiled command-level RED required a stop-horizon finding for two
owned snapshots: indexer use100→200 of1000 bytes in60 seconds leaves480 seconds,
inside the explicit600-second stop horizon even though usage is only20%.
The old command returned invalid arguments, without the required forecast.
The implemented command passes. These are synthetic functional inputs, not
measurements or operating thresholds.

Additional compiled REDs proved that unknown used/series values masked a known
zero/exceeded previous budget, and that an absent growth policy/partial history
masked a selected history version/current-series conflict. GREEN validates the
known conflicts independently. An initial sandbox cache-access setup failure is
not counted as a behavioral RED. A staticcheck De Morgan suggestion was fixed.

## Covered contracts

- Inclusive stop/warn boundaries, just-outside nanoseconds and full uint64
  multiplication without rounded forecasts.
- Identity/measurement-series/generation/budget matching; reset/rebind, flat/
  shrinking usage, missing observations and stale/interval/ordering/future-time
  behavior. The old sample's diagnostics are never made current.
- Unknown/duplicate/null/trailing/oversized input, private file permissions,
  links, missing history, duplicate flags, cancellation and output failures.
- Real three-file CLI path, borrowed-input bytes/inodes, fixed-code privacy,
  independent current stops and unchanged authority. Existing single-sample and
  actual Arc/StableNet producer/consumer compatibility tests remain applicable.

No new shared concurrent state, goroutine, timer, database/RPC client, poller,
notification sender or writable monitor state is added. A new concurrency200
campaign and native SDK/operating/performance reruns are not required.

## Final local and release gates

Final local checks passed:

- `go test -race ./internal/nativemonitor ./cmd/execution-monitor -count=1`:
  all monitor cases, including current Arc/StableNet diagnostic compatibility,
  pass; the thin command package has no separate tests.
- Actual command build, scoped Go vet, gofmt and scoped golangci-lint: zero issues.
- Built command help and private copies of unresolved policy/snapshot drafts:
  the new flag is documented and unresolved inputs remain HOLD/2.
- 12 selected Markdown documents,355 relative links, zero errors;24 CI policy
  cases, whitespace and code/non-native short scope pass. The workflow is unchanged.
- Staged secret findings zero; reachable history matches15 previously reviewed
  public fixtures, zero unreviewed. Private config hash/mode and source outside
  the monitor/docs/unresolved policy draft are unchanged.

Actual applicable exact-head short CI, squash and clean main identities belong
to the associated PR and post-merge receipt; preparation does not pre-certify
those actions. Local logs retain their source/check hashes outside the repository.

The operating inputs and held scope in [continuity](continuity.md) stay
unchanged. Current source and production config/pool/RPC/fresh/ACK boundaries
outside the monitor are unchanged. Existing guard and offline tools retain
their separate contracts.

# Offline Native monitoring

The [managed App runtime](../deployments/native/runtime/README.md) retains raw,
native, SQL public and source ACK boundaries separately. This monitor accepts
saved [managed diagnostic observations](EXECUTION_MANAGED_INSPECTION.md), including
received/native input refs and independent raw/native/read epochs. It does not
itself open raw/native/SQL or journals. Missing data stays unknown; metadata
compatibility grants no integrated acceptance, freshness or GC authority.

`cmd/execution-monitor` evaluates **saved, submitted observations** once. It runs
no chain. Functional fixtures consume the real Arc and StableNet
[diagnostic command](NATIVE_EXECUTION_DIAGNOSTICS.md) output against owned fake
source/export files, with synthetic capacity/pipeline numbers. They connect to
neither testnet, peer, public RPC nor operating PostgreSQL/native database.
The fixture chain IDs do not certify real genesis, forks or connectivity.

The evaluator has no poller, scheduler, listener, exporter subprocess, DB pool,
RPC client, notifications, latch, restart, GC or restore actuator. It writes only
the JSON diagnostic response. Operating deployment, actual probes, disk format
and additional performance measurements remain **HOLD**.

## Build and review unresolved inputs

Use the pinned Go toolchain from the repository root:

```sh
mkdir -p build
go build -o build/execution-monitor ./cmd/execution-monitor
build/execution-monitor --help
build/execution-monitor
# HOLD, exit2: no selected policy, snapshot or evaluation time.
```

The checked-in [policy](../deployments/native/monitoring/policy.draft.json) and
[snapshot](../deployments/native/monitoring/snapshot.draft.json) leave operator
values as null/empty unknowns. To examine these public drafts, copy them into
owned private fixture files; do not open an operating exporter to fill them:

```sh
monitor_demo_dir=$(mktemp -d)
trap 'rm -rf -- "$monitor_demo_dir"' EXIT
chmod 700 "$monitor_demo_dir"
install -m 600 deployments/native/monitoring/policy.draft.json "$monitor_demo_dir/policy.json"
install -m 600 deployments/native/monitoring/snapshot.draft.json "$monitor_demo_dir/snapshot.json"
build/execution-monitor --policy "$monitor_demo_dir/policy.json" \
  --snapshot "$monitor_demo_dir/snapshot.json" --at HOLD
# HOLD, exit2. No defaults or host measurements are invented.
```

Accepted flags are `--policy FILE`, `--snapshot FILE`, `--at RFC3339`, each once;
one-dash forms are equivalent. Missing/empty/`HOLD` arguments are unobserved.
Unknown, duplicated, dangling or positional arguments fail. No environment,
config/DSN, embedded pathname, URL, command or filesystem usage probe is followed.
`--at` is an explicit evaluation instant, never an implicit system clock.
Standalone `--help`/`-help`/`-h` exits0; evaluating a snapshot never authorizes
operations and currently returns HOLD/2 or FAIL/1.

## Policy and snapshot contract

The [consumer types](../internal/nativemonitor/types.go) define version1 JSON.
Both files require `version: 1`. Nullable numeric fields distinguish missing
observations from a measured zero. Negative/string numeric fields, unknown or
duplicate keys, trailing JSON and scalar nulls are rejected. Each input is at
most1MiB and uses the shared owned/private regular-file metadata reader with
ancestor/leaf-link and hard-link checks, bounded strict JSON and a final reread.
Input files/parents cannot be publicly writable; leaf group/other permissions
are rejected. The examples use0700 parents/0600 files. No directory enumeration,
LOCK, source/native mutation or recovery occurs. Matching rereads are neither
an atomic snapshot nor a maintenance lease.

| Policy field | Meaning |
| --- | --- |
| `expected` | Exact `profile`, `chain` (`ID`, `Genesis`) and `original_anchor` (`Generation`, `Height`, `Hash`). Null means unknown; genesis/anchor identity is never guessed. Original anchor generation is1; height0 must match genesis. |
| `max_age_seconds` | Positive claimed sample-age ceiling, at most86400 seconds. No implicit age default. |
| `capacity_warn_bps`, `capacity_stop_bps` | Positive integer basis points, warn < stop ≤10000. Compare used/limit with128-bit integer products, without floating-point rounding or uint64 overflow. |
| `min_free_bytes` | Positive shared free-space floor for the declared filesystem/Docker VM observations. It is not an automatically computed COW or operational storage budget. |
| `backlog_warn_blocks`, `backlog_stop_blocks` | Positive warn < stop block gaps, each at most signed64-bit maximum. |
| `lag_warn_seconds`, `lag_stop_seconds` | Positive warn < stop lag limits, each at most604800 seconds. |
| `required_storage` | Explicit unique subset of `raw`, `native`, `indexer`, `wal`, `filesystem`, `docker_vm`. Empty means unresolved. At most6 kinds. |

Missing thresholds stay HOLD; known zero/invalid limits or threshold-order
conflicts FAIL. The draft selects no operational capacity or lag limits.
Test values are synthetic, not recommended operator settings.

The snapshot contains `identity`, `diagnostics`, `metrics`. Identity must match
the policy when both are known. Each sample component has its own `observed_at`;
replacing a metrics timestamp does not freshen old diagnostics. The time is a
**submitter claim**, not authenticated measurement provenance. File mtime is not
used as sample time. Age equal to the limit is accepted; older samples HOLD;
future-dated samples FAIL. Freshness here never means a live canonical read.

`diagnostics.report` embeds the full version1 `execution-diagnose` JSON contract.
The consumer retains its authority, sample/atomicity and observation-basis limits.
Known diagnostic failures FAIL even when policy/time/other samples are missing.
Profile/chain/anchor conflicts and invalid report contracts FAIL. Producer
findings' opaque text is not copied to output. Actual product round-trip tests
detect consumer/producer schema drift without linking chain/DB code into the
production monitoring binary.

`metrics.pipeline` accepts one declared generation with nullable `head_height`,
`received_height`, `processed_height`, `ack_height`, `oldest_pending_seconds`,
`ack_lag_seconds`, `worker_lag_seconds`. Heights are signed64-bit compatible,
cannot precede the original anchor and, with a known valid generation, require
head ≥ received ≥ processed ≥ ACK even when an intermediate height is missing.
Derived gaps are head−received, received−processed and processed−ACK. Unknown
generation prevents gap calculation. Current source/public diagnostic generation
disagreement stays HOLD as an unverified rebind; archived permits and native
header parents are not treated as current source generation.

`metrics.storage` contains at most6 unique `{kind, used_bytes, limit_bytes,
free_bytes}` entries. Budget kinds (`raw`, `native`, `indexer`, `wal`) use
used/limit only; filesystem/Docker VM kinds use free only. A zero used value is
observed zero; null is unknown. Missing required kinds/values HOLD. Zero budget,
used greater than budget, invalid kinds or mixed field meanings FAIL. The tool
does not locate the measured disk or validate APFS identity itself.

## Results and limits

| Output | Meaning |
| --- | --- |
| `status: HOLD`, exit2 | Unperformed live probes and any missing/stale/rebind observations remain unresolved. It does not suppress independently known warnings. |
| `status: FAIL`, exit1 | Known invalid/contradictory input, threshold stop, failed I/O, cancellation or output failure. FAIL dominates HOLD. |
| `alert_level: warning` | A known warning threshold or pending native/source maintenance/transition was observed. It can coexist with overall HOLD. |
| `alert_level: critical` | A FAIL requires attention; this includes malformed evidence and does not imply authority to stop or delete anything. |
| `alert_level: unknown` | No known warning/critical was found, but overall safety/health is unverified. |

Threshold comparisons are inclusive. A free-space value at its floor is critical.
A known critical condition stays critical even in stale/partial input; stale
normal input never becomes current health. A pending fence, including a recorded
done phase awaiting release, is reported without clearing it or assuming TTL
expiry. Existing diagnostic hard conflicts (including checkpoint/ACK/floor
conflicts) propagate; exported data is not reclassified as authenticated/live.

One deterministic compact response contains at most128 fixed-code findings and
is bounded to64KiB. It echoes no paths, profile/chain identities, timestamps,
raw errors, credentials, opaque upstream findings or snapshot payloads. Fields
name only fixed checks and allowlisted storage kinds. Every result declares:
`operational_readiness: not_authorized`, `live_canonical_freshness: not_performed`,
`snapshot_authenticity: not_verified`, `actuation: none`.

This is a stateless single-sample evaluator, not the legacy
[opsguard](../internal/opsguard/monitor.go) live monitor. It does not replace that
monitor's hysteresis/latch/interlocks, calculate DB growth forecasts from one
sample, create a metric exporter, prove WAL recoverability, or install an
operational watcher/notification path. Those require explicitly scoped host and
operating observations after the current HOLD conditions are resolved.
The existing pool4/part2/API reservation, seven logical writers, RPC5 physical
attempts/s, NEW fresh canonical read and post-response generation fence before
commit, E1–E6 ACK/lease/window/full-baseline contracts and two EVM passes are
unchanged. Other projects and shared caches are untouched.

# MON-02-D: disabled App/config assembly

Base `7c4e9f62c5e0babe823b093c552a0e0e48985030`, branch
`refactor/observation-app-assembly`. Follow the [accepted D contract](../20261102-mon01-observation-design/implementation.md#mon-02-d--opt-in-app-assembly와-sink-경계)
and [C checkpoint boundary](../20261102-mon02c-alert-state/validation.md).

Implement strict `operations.observation`, nil/off by default; enabled v1 uses
declared whole-database/cluster resources and explicit budgets, an existing
private checkpoint directory and disabled operator sink. No endpoint/credential
or chain-notification setting is borrowed. Keep unsupported derived/native epoch
facts unknown. Real network transport/operating activation remain HOLD.

First compiled RED: enabled/disabled observation configuration is not recognized.
Then test disabled no-IO behavior and App assembly. Reuse the monitor's sampler
when capacity is enabled; otherwise own exactly one observation-only sampler.
Strip API reservation values via the existing B acquisition boundary.

Use one caller-independent alert worker, bounded producer inbox and explicit
cancel/join. Slow/failing fake persistence/sink cannot block producers or change
their publication/ACK results. Source attach/detach/rebind tokens fence queued
results; source failure, missing time or loss cannot clear incidents. Close all
owned work before Store closure, including direct Shutdown/never-started and
multi-chain shared-owner cases. Initialize alert metadata only when explicitly
declared and absent; never reconstruct or replace unsafe/existing history.

RED→GREEN, targeted final CPU1/race/count200 for new concurrency cases, owned
fsync-on PG functional integration, build/vet/lint and docs/policy/secrets precede
the applicable exact-head short CI, squash and clean main. No native SDK rebuild,
host/SLO/performance measurement, actual node/peer or other-project changes.
Preserve pool4/part2/API1, physical RPC5 and final NEW canonical/fence/COMMIT/exact ACK.

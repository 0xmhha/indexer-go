# MON-02-B: owner taps and one statistics sampler

Base `acb2f93e8958f15abac35c864246c6bbd16ff27f`, branch
`refactor/observation-owner-taps`. Follow the [accepted B contract](../20261102-mon01-observation-design/implementation.md#mon-02-b--기존-owner-결과-tap과-통계-sampler-소유권)
and [observation design](../../../NATIVE_MONITORING_INTEGRATION.md).

1. Compiled RED: detach returns while an owned SQL read is still active;
   a late result can still be journaled. Add cancel/join and source fencing.
2. Extract one shared sampler for legacy capacity and opt-in observation-only
   consumers. Preserve legacy query30s; observation caps query2s/round5s.
   Query contexts include acquisition time and never inherit API reservation
   or transaction values. Existing statistics SQL is reused without duplicate sampling, new pools,
   immediate retries or node RPC.
3. Add bounded nonblocking value-only taps with loss/retirement fencing, retaining
   original per-query and progress timestamps. Wire existing completed owner
   results through opt-in programmatic hooks, without configuration activation.
4. Verify overlap, latest-source/fallback, detach/rebind, cancel/join/close,
   direct App Shutdown and multi-chain ownership with controlled fake providers.
   Any changed DB lifecycle boundary also uses an owned fsync-on fixture.
5. One targeted `GOMAXPROCS=1 -race -count=200` invocation of the new concurrency
   cases; focused build/vet/lint and docs/policy/secrets. Existing exact-head
   short CI, squash and clean-main synchronization precede MON-02-C.

No actual node/peer/deployment, disk formatting, operating GC/restore, sender or
extra performance measurement. Pool4/part2/API1, physical RPC5, two EVM runs and
final NEW canonical/fence/COMMIT/exact ACK remain intact. MON-02-D owns actual
configuration/App activation; B only supplies disabled-by-default capabilities.

# E6 checklist

- [x] Native source audit; bounded explicit trust/fork contract.
- [x] Independent prepare/commit BLS quorum and previous seals.
- [x] Signed epoch transition, chain-bound RANDAO, exact ancestry.
- [x] Persisted immutable opt-in policy and monotonic finalized state.
- [x] Existing publication transaction verifies and stores consensus state.
- [x] Native-generated development fixtures; root good/bad checks.
- [x] fsync-on PG publication/fresh/generation/cancel/restart/ACK/finality tests.
- [x] Full root/wire/StableNet race and full fsync-on PG suite.
- [x] Build/fmt/vet/lint, nested static, examples/cache/offline checks.
- [x] One complete CPU1/race/count200 invocation for new app/PG integration.
- [x] Reviewed staged and whole-history secret scans.
- [ ] Draft PR, exact-head CI5, ready/squash without bypass.
- [ ] One-parent squash tree matches reviewed head; clean main/origin-main.
- [ ] Owned temporary resource cleanup and Korean stage report.

No actual node/peer/operator-anchor validation, deployment, operational GC,
performance loop, or priority 4 work is authorized by this stage's completion.

# MON-02-C checklist

- [x] Reconcile clean main, B/A contexts and selected C scope.
- [x] Compiled behavior RED for unknown/gap/restart preserving active critical.
- [x] Pure validated warning/critical/clear/reset/dedupe/cooldown reducer.
- [x] Bounded delivery queue, stable IDs, receipts/retries/dead letters/fake sink.
- [x] Private atomic checkpoint; missing/corrupt/link/permissions/size failures.
- [x] Restore keeps unresolved incidents/deliveries and resets temporal baselines.
- [x] Focused functionality/race/build/vet/lint and docs/policy/secrets.
- [x] Update state/backlog and preserve previous two accepted contexts.
- [x] Prepare normal PR and require actual exact-head short CI/squash/reviewed-tree/clean-main gates.

At this document snapshot, release gates are prepared; actual CI/merge/main are
verified in Git/PR metadata on resume. Preparation does not pre-certify a merge.

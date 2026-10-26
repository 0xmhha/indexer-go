# Arc native proofs checklist

- [x] U01 native provider/replay/proof protocol: runtime RED→GREEN, actual retained native-v2 and independent first/second EVM evidence, old fixture bytes preserved
- [x] U02 immutable source policy/private strict selection/Go collector/CLI and cancellation ownership
- [x] U03 native fixture→spool/window→PG/API, fresh failure/retry, lost ACK, read-only recovery
- [x] U04 full local gates, single200 and current docs/checklist/continuity; staged/history scans are mandatory before commit
- [ ] Release: exact-head CI5, ready, squash and clean main synchronization (source-branch PR status is authoritative)

Release requires exact-head lint/test/postgres/examples/arc success, ready, squash and clean main sync. Preserve comparison branch. Source-branch PR status is authoritative; do not mark live/operator/performance validation complete.

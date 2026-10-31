# Managed inspection checklist

- [x] Pure/read-only scope and n/n−1/n−2 plan recorded before implementation.
- [x] Managed App file preflight RED→GREEN, unknown/conflict/capacity/path cases.
- [x] Existing-pool read-only SQL export and strict artifact validation.
- [x] Fixed native/raw/journal metadata observation without recovery/locks.
- [x] Diagnostic CLI/Docker/consumer integration and no-mutation tests.
- [x] New reserved-reader regression: actual fsync-on PostgreSQL RED→GREEN, existing ReadSnapshot regression and one CPU1/race/single200 pass.
- [x] Affected functional/static/CLI/docs/policy checks and current evidence.
- [x] Staged/reachable-history redacted leak review: no new findings; existing15 public-fixture findings remain reviewed.

Release requires actual applicable exact-head short CI, squash and clean main
sync; retain associated PR/post-merge receipt. N6 combined acceptance and all
operational inputs/execution remain HOLD.

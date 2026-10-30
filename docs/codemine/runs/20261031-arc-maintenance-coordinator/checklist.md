# Coordinator checklist

- [x] Accepted boundaries and exact phase/recovery plan recorded.
- [x] Completed-E3 same-pool EX release scope: RED/GREEN and real DB cancellation.
- [x] Durable journal and raw-zero/uncertain-commit recovery.
- [x] Arc live/offline coordinator and optional App delegation.
- [x] Startup/resume gates and owned cancellation/join.
- [x] Affected functional/static and new targeted CPU1/race/200: live/App and actual PG scopes each pass in one invocation.
- [x] Current documentation, RED/GREEN receipts and final source/check evidence.

Release gate: associated PR must pass actual applicable scoped CI before squash;
retain its exact reviewed head/tree and clean main synchronization receipt after
merge. Those post-commit identities cannot be embedded in their own source tree.

N5 held Docker/capacity/inspection and N6 remain separate. Operations HOLD.

Behavioral RED receipts: [red.json](../../../../verification/native-maintenance-coordinator/red.json).
Prior n−1 release: [managed COW #254](../../../../verification/native-maintenance-coordinator/managed-release.json).
Local qualification and release boundaries are recorded in [validation](validation.md).
Final local [checks](../../../../verification/native-maintenance-coordinator/checks.json)
and [source hashes](../../../../verification/native-maintenance-coordinator/source.json)
retain the test/helper limits.

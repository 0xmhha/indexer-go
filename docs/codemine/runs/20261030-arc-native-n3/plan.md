# N3 embedded ingress plan

Implement the accepted [N3 milestone](../20261029-arc-first-import/plan.md) after
N2 [CI/squash/main](../../../../verification/native-import-n2/release.json).
The original [design](../../../ARC_NATIVE_IMPORT_DESIGN.md) remains authoritative.

1. Add an explicit managed raw-window kind and one bounded checksummed control.
   Refuse legacy adoption, unknown/partial ownership and GC without its current
   coordinator. Charge control bytes to the existing quota; preserve raw inputs.
2. Add a generic opt-in registry and prepared/runtime lifecycle inside App.
   Configuration validation is pure. Prepared checks precede API exposure;
   actual child/peer startup follows recovery and explicit socket readiness.
   Borrow the same Store/canonical reader, cancel and join every owned task.
3. Register Arc only through its bundle. Reuse ETH/68 batches and existing Arc
   commitment verification. Persist raw before dispatch; separate bounded active
   memory/reference notifications from the large durable disk backlog.
4. Own the bounded AIN1/AWH1/AWR1 child with filtered environment and exact
   identity/epoch/request/parent/root/digest checks. Reconcile retained raw on
   restart; advertise only native completion. No new RPC or SQL pool.

Initial admission may use one block (within the designed maximum8), with its
own fresh read through the existing shared5-attempt budget. E1's final NEW fresh
and generation fence remain separate. Native completion is no E1 ACK/public/GC.
N4 adds managed second replay/reorg; N5 adds permanent ingress bytes/authorized
GC; N6 supplies actual combined offline acceptance. No operational activation.

Each behavior uses requirement-failing RED before GREEN. Run new Go concurrency
with a single GOMAXPROCS=1/race/count200, required functional/static/race/example
checks, maintained docs and secret scans. Exact-head CI5 then guarded squash and
clean main synchronization precede N4. Preserve pool4/part2/API1/writers7, fixed
Arc/Reth pins, two normal EVM passes and all legacy paths.

APFS/enode/authenticated production checkpoint/private inputs remain HOLD.
Owned temporary windows/native libraries/mock App/disposable PostgreSQL fsync-on
are functional fixtures. No actual node/peer/user-config run, provisioning,
operational GC/restore or additional performance campaign.

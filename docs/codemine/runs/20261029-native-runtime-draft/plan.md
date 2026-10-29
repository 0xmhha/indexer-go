# Native runtime composition draft

The user selected the recommended Native App/collector/executor deployment
draft. Design and check the declaration offline. This does not authorize image
build/pull, Docker, App, node, DB, peer, formatting, GC/restore or performance
execution. Do not touch other projects or private operating inputs.

Keep the existing diagnostic Compose independent. Add a separate four-service
runtime *draft*: Arc App and collector (owned Rust stdio child in that container),
StableNet App and executor (owned import/replay). PostgreSQL16 is an explicitly
prepared external dependency, not a new initialized DB service. BSC/Sepolia
remain in the existing legacy composition; never overlap either native App with
the legacy writer of the same chain.

Arc collection requires an already imported native DB. No current Arc component
in this composition performs first import/P2P synchronization. Record its
provider as HOLD; an enode alone does not close this implementation/integration
gap. Do not substitute a full-node service or a StableNet executor for it.

Acceptance:

1. Pin declared image digests, same non-root UID:GID for authenticated IPC,
   private configs and per-chain mounted DSN_FILE via the existing wrapper.
   Producers receive no DB secrets or direct DSN environment.
2. Preserve pool4/part2/API1/writers7, RPC5 physical attempts/s including retry,
   and final NEW uncached canonical→post-response generation fence→COMMIT.
3. Declare separate native/source/scratch mounts. Arc's native parent is writable
   for the existing sibling run-lease lock; a nested read-only `/native/node`
   bind protects the selected DB. Explicit `/scratch` must be disjoint from
   `/source/spool` for the production preflight. Default parent scratch cannot
   substitute for that declared layout. StableNet owns writable native/source
   parents. IPC is off-data0700 with a0600 socket; Apps get no native/spool.
4. Explicit manual startup/readiness/shutdown/restart boundaries. Compose
   service_started is only ordering, never prepared-schema/socket readiness.
   No automatic restart, deletion, generation rewrite or unknown socket cleanup.
5. Pure bounded/redacted file checker; unknowns HOLD, known unsafe conflicts
   FAIL even with missing inputs. No environment expansion or declared-path,
   daemon, store, network, credential or actual config reads by the checker.
6. Write requirement-failing RED tests, implement, prove GREEN. Cover drift of
   limits/secrets/paths/stdio ownership, bounded hostile input/output and real
   product file-preflight acceptance of the proposed container paths. Owned
   temporary diagnostic binaries/fixtures start no App/native service/DB/RPC.
7. Update current docs/checklist and n−1/n−2 via [continuity](continuity.json).
   Reuse exact-byte prior Go race evidence only if all Go/modules are unchanged;
   new-head CI5 must pass before guarded squash/main synchronization.

The actual APFS identity/capacity, image contents/ABI, private DSNs, authenticated
full checkpoint, official peers, host/container ownership/socket mapping and
Arc native import provider remain HOLD. A valid draft is not a deployable or
verified operating stack. See [checklist](checklist.md) and
[validation](validation.md).

# N0: pinned SDK and bounded import contract

Implement N0 of the [approved plan](../20261029-arc-first-import/plan.md), before N1-N6.
This release qualifies the pinned native SDK and adds the bounded first-import
input/fence contract. It does not activate a native writer, collector, peer or
application. Operational readiness remains HOLD.

1. First write behavioral tests for bounded AIN1 input frames, exact identity,
   pins/request/epoch/height, payload digest, EOF, hostile sizes, unknown and
   duplicate fields. Run the tests against missing behavior to record RED.
2. Implement framing independently from EVM/SQL/ACK. Raw RLP is bounded opaque
   input at this layer; N2 must decode and validate block/receipt/root semantics
   before execution or persistence. A successfully parsed frame grants no ACK,
   publication or deletion authority.
3. First test a durable private no-overwrite pending fence, including restart,
   malformed paths and partial writes. Implement it after actual RED. The fence
   has no automatic clear, TTL, completion or ACK API.
4. Qualify actual official ArcEVM outcome -> pinned provider save_blocks Full,
   native-v2 changes/trie/history, reopen and a pre-existing RO secondary refresh.
   Persisting an already executed SDK outcome must not execute the EVM again.
   Verify the existing collector-style second replay at exact retained parent.
5. Qualify full state/preimage checkpoint export at a non-genesis imported height,
   healthy unwind/reimport, and failed-closed guards at interrupted save/commit/
   unwind boundaries in owned temporary native DBs. An SDK compile result alone
   is not this evidence. N1 initializes authenticated checkpoint state; N2 owns
   complete managed intent/receipt recovery and N4 managed collector admission.
6. Run appropriate Rust fmt/clippy/build/full functional regressions, frozen Go
   source/race evidence checks if exact-byte unchanged, staged/history leak scan,
   doc links and continuity. No new Go concurrency is introduced by N0.
7. Commit/PR under the repository rules, all five exact-head CI checks successful,
   expected-head squash and clean main FF sync before N1. Release evidence lives
   in actual logs/portable receipts and PR metadata, never fabricated future CI.

Use only task-owned library fixtures/builds. No node/services, real peers/RPC,
production DB, provisioning/format, snapshot download, operational GC/restore,
performance/SLO or host-load campaign. Fixed pins, pool4/part2/API1/writers7,
shared RPC5 and NEW uncached fresh -> generation fence -> COMMIT stay unchanged.

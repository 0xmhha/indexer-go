# N0 validation

Base main39a0082 (#240); N0 only. N1-N6 remain pending and operations HOLD.
[Source](../../../../verification/native-import-n0/source.json),
[actual RED](../../../../verification/native-import-n0/red.json) and
[checks](../../../../verification/native-import-n0/checks.json) record portable
source/log digests. Logs and disposable fixtures are retained/owned outside the
checkout; operator data and machine-home paths are not committed.

Behavioral RED preceded framing and pending-fence implementation. A compiler
borrow error, a Rustix flag spelling correction and a zero-test selector probe
were excluded from RED/GREEN proof. Final fmt, all-target build, clippy with
-D warnings and all-target Rust tests pass:51 library tests and2 command tests,
0 failures/ignored. Eight new tests include an owned reader-process harness;
the parent SDK tests actually exercise that child, not just its no-env entry.

Actual pinned native-v2 tests cover empty system state and a synthetic funded
transaction with code/storage/nonempty receipt; Full outcome persistence,
separate RO process opened before commit and catching up, exact second Arc EVM
replay, reopen, H1 full checkpoint/root/preimage export, unwind/reimport, and
pending fences before/after save/unwind commits with separate-process restart.
Same-PID dual MDBX open returned EAGAIN locally; it is not the designed topology.
The empty system block can have storage history without account-history rows;
the transaction case separately requires account history and changed storage.

Controlled transaction interruption is NOT a power-loss or per-store killpoint
repair proof. N2 implements managed writer/receipts/recovery; N4 wires managed
collector admission. Frame RLP is opaque at N0 and must be decoded/validated at
N2. Pending input never grants E1 ACK/publication/GC and has no automatic clear.
No new runtime command, native writer, peer or application startup is enabled.

Go/module bytes are unchanged at
b8da45385fbb6a218fb63f22b7d1517113acfbfa65f87c7323c5e98d08a98f92.
The prior complete successful local race receipt in
[checks](../../../../verification/offline-monitoring/checks.json) and its actual
retained log SHA256
19999d4fc634883bdecf2a8ac0e652371edb075f7a2abf445b9f06a15951bf89
were verified. This is reused exact-byte evidence, not a new Go/race campaign.
No new Go concurrency test requires another count200 invocation at N0. Rust
checks above are new actual runs; final exact-head CI5 remains required.

Document/continuity checks pass. Staged leak scan found0 findings; history had15
previously reviewed public fixtures and0 unreviewed findings (raw exit1, not a
zero-findings history claim). No new allowlist was added. Actual CI/merge/main status is owned by the release PR, not claimed
in this pre-CI source record. Fixed pins, pool4/part2/API1/writers7, shared RPC5
and final NEW uncached canonical -> response generation fence -> COMMIT remain
unchanged. Other projects/resources and shared caches are preserved. No real
node/peer/RPC/production DB, formatting/provisioning, snapshots, operational
GC/restore or new performance/SLO/host-load campaign ran.

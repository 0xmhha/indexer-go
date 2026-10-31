# Held runtime alignment validation

The [plan](plan.md) was recorded before implementation. Behavioral RED reproduced
the extra external collector, absent App native/source/raw/key mounts and absent
raw/journal phase paths. These are requirement assertions against the previous
declaration, not compile failures. [RED receipts](../../../../verification/native-runtime-alignment/red.json)
retain the logs' hashes.

GREEN checks the three-service topology, App DB/RPC/peer networks, RW native
parent/source/raw, RO peer key and absence of a nested native-child mount.
It rejects legacy rollback, implicit initialization, missing peer egress,
wrong scratch/journal paths, limit drift, direct/producer DSNs, unsafe image/
UID/mounts and raw/private-input overlap. Strict hostile-file, bounded output,
single selected file read, no process/network and synthetic resolved-HOLD checks
remain intact. No declared path is followed or provisioned.

The standalone Python suite has16 cases and one explicit host-binary-only skip.
The owned host CLI fixture builds four existing Go diagnostic commands and
passes both diagnostic13 and runtime16 cases without skips. The final raw/key
overlap assertions were added to the pure case afterward and pass; unchanged
host commands/integration were not rebuilt to repeat that fixture. CI policy23,
Python AST syntax and diff checks pass. No Go/Rust/runtime concurrency changed;
prior #255 runtime/race200 evidence is preserved rather than rerun.

Host CLI preflight covers the **legacy** separate collector/executor file
formats. Its standalone synthetic `/scratch` is not the managed App layout.
The actual managed collector derives scratch from the source parent `/source`.
Managed App configuration/raw/SQL/E3/coordinator-journal inspection is still
missing and must qualify before operating acceptance. No legacy PASS or valid
HOLD declaration is claimed as that proof. Combined mock ETH68/actual SDK/App/
SQL/GC acceptance remains N6; host/APFS/binds/peers/images are not tested here.

Local receipts are [checks](../../../../verification/native-runtime-alignment/checks.json)
and [source hashes](../../../../verification/native-runtime-alignment/source.json).
Final staged/history leak and changed-document checks gate commit. Exact-head
short CI and guarded squash/main synchronization gate release; post-merge
identities are retained by the associated PR and release receipt.

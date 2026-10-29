# Native diagnostic Docker draft

Offline wiring for the existing Arc and StableNet
[file preflight](../../docs/NATIVE_EXECUTION_PREFLIGHT.md) and
[metadata diagnostics](../../docs/NATIVE_EXECUTION_DIAGNOSTICS.md).
**Image build/pull, containers, operating nodes/DBs, disk provisioning and
performance measurements remain HOLD.** No APFS volume or enode is needed to
check this draft with synthetic files.

This is a diagnostic-only image/profile. Native indexer/collector/executor
deployment is not included. The separate four-testnet
[legacy operations composition](../testnets/README.md) retains its existing
host/APFS guard and private DSN injection; it is not merged with this profile.

[Offline monitoring](../../docs/NATIVE_OFFLINE_MONITORING.md) is a separate host
command consuming saved JSON; its [unresolved input drafts](monitoring/policy.draft.json)
add no Docker service, probe, DB/RPC connection or notification to this profile.

## Developer checks

Use Python3.9+ with the repository's pinned parser:
`python3 -m pip install -r deployments/testnets/requirements.txt` in your own
environment. From the repository root:

```sh
python3 -B deployments/native/check.py
# JSON HOLD, exit2: checked-in operator variables are deliberately unresolved.
python3 -B -m unittest discover -s deployments/native -p test_diagnostics.py -v
bash deployments/native/test-cli.sh
```

`test-cli.sh` builds four host Go1.26.8 binaries with CGO enabled, runs their real
commands against owned synthetic bindings, and removes its temporary binaries
on exit. It does not build an image or invoke Docker/native services/DB/RPC.
The default unit invocation skips this one host-binary fixture; the script and
CI run it. New concurrent runtime state is not introduced.

## Files and commands

| File / service | Contract |
| --- | --- |
| [Dockerfile.draft](Dockerfile.draft) | Four diagnostic binaries only; Go1.26.8/bookworm builder, CGO1 for existing BLS dependencies, bookworm-slim runtime; no extra packages, indexer or native executable |
| [Dockerfile.draft.dockerignore](Dockerfile.draft.dockerignore) | Only Go/module source inputs; operator bundles, root config/secrets, Git data and native build targets are outside the selected context |
| [compose.draft.yaml](compose.draft.yaml) | Separate project `indexer-native-diagnostics-draft`, profile `native-diagnostics-draft`; no default services or application/DB dependencies |
| `arc-config` / `stablenet-config` | Product-specific `execution-preflight`, fixed explicit App/native/checkpoint/declaration flags; Arc also uses retention.json |
| `arc-metadata` / `stablenet-metadata` | Product-specific `execution-diagnose`, fixed source/native/three export arguments |
| [check.py](check.py) | Bounded offline declaration check, with no environment expansion or host-source traversal |

CGO0 does not compile the current `blst` dependency. Both this recipe and the
legacy operations recipe copy the local `execwire` replace module **before**
`go mod download`. Crypto implementations and upstream dependencies are unchanged.
Host compilation is not proof of the eventual Linux image/ABI or Docker Desktop
mount behavior; no image is built as part of this task.

All services declare `network_mode: none`, a read-only root filesystem,
`cap_drop: [ALL]`, `no-new-privileges`, a non-root explicit UID:GID,
`restart: "no"`, `pull_policy: never` and read-only long-syntax bind mounts with
`create_host_path: false`. There is no shell entrypoint, environment/secret
injection, port, Docker socket, named volume, build instruction, health server
or database dependency in the Compose contract. The Debian base itself contains
ordinary system utilities; the profile runs only the fixed Go entrypoints.

## Declared bindings — all still HOLD

The image is a single reviewed `repository@sha256:<64 lowercase hex>` reference
shared by all four services. Tags, credentialed URLs and zero-digest placeholders
are not accepted as a reviewed digest. The UID/GID are positive explicit host
identifiers, not Docker secret-remapping assumptions.

| Variable suffix / input | Container location |
| --- | --- |
| `INDEXER_NATIVE_DIAGNOSTICS_IMAGE`, `INDEXER_RUN_UID`, `INDEXER_RUN_GID` | Shared image and user; unresolved today |
| `INDEXER_ARC_NATIVE_INPUTS` / `INDEXER_STABLENET_NATIVE_INPUTS` | `/inputs`, existing dedicated file-preflight bundle |
| `..._SPOOL` | `/source`, existing source spool |
| `..._PARENT` | `/native`, **dedicated parent** of the existing native directory |
| `..._NAME` | One directory basename; selected DB is `/native/<NAME>` |
| `..._EXPORTS` | `/observations`, existing supported source/checkpoint/retention export bundle |

`INPUTS` contains `app.yaml`, `native.json`, `checkpoint.json`, `declaration.json`
and, for Arc, `retention.json`. `EXPORTS` contains `source.json`,
`checkpoint.json`, `retention.json`, using the existing typed export contracts.
No exporter is run to obtain them. Do not invent empty/genesis exports or put
runtime credential files in these bundles. Diagnostic services receive no DSN
secret; legacy runtime DSN injection is unchanged.

The native **parent** mount keeps Arc's sibling
`.<NAME>.execution-retention` visible during directory swaps. StableNet's fence
and floor remain below the selected native root. Mounting only the DB directory
would hide Arc's sibling fence. The fixture checks exercise both products using
real binaries and preserve input bytes/inodes without creating LOCK/ACK/manifest
or maintenance records.

Use mutually disjoint, clean absolute directories for each product's inputs,
source, native parent and exports. Paths are declarations only: the checker
does not test existence, APFS identity, permissions, symlink resolution, available
space, Docker sharing or image authenticity/availability. The actual diagnostic
readers still enforce their own strict metadata/file ownership contracts.
Files and UID mapping inside Docker Desktop remain unverified. Config preflight
compares the declarations **inside** the selected files without rewriting host
paths into container paths; it does not certify a future runtime topology.

## Reports and limits

`check.py` accepts either no flags (the checked-in draft) or `--file FILE`
(one explicit complete private Compose declaration). It never reads `.env`,
ignored configs, secrets, mount source paths, Docker contexts or host devices.
Only the selected Compose file is opened, read-only, as a regular non-symlink
leaf, at most1MiB. YAML duplicates, aliases, merge keys, extra documents, nesting
over32 and more than8192 parser events fail. Unknown keys/services, unsafe
commands, mutable mounts, image/UID drift and overlapping selected directories
fail even when another input remains unknown. Output has at most128 findings,
fixed codes and no raw values/paths/parser errors; short/broken output returns1.

| Status / exit | Meaning |
| --- | --- |
| PASS / 0 | Only the declared diagnostic Compose contract is valid |
| HOLD / 2 | Empty/exact HOLD or the expected required-variable placeholders remain |
| FAIL / 1 | Known conflict, unsafe/malformed input, invalid CLI or I/O/output failure |

FAIL dominates HOLD. `operational_readiness: not_authorized`,
`docker_execution: not_performed`, `host_and_live_state: not_checked` and
`environment_expansion: not_performed` are not readiness grants. An explicit
service can activate a Compose profile; the profile is not an authorization
mechanism. A passing declaration never lifts the execution hold or bypasses
the host/APFS gate. No build/run command is prescribed during this hold.

These read-only observations are separate samples, not an atomic snapshot,
fresh canonical check or permission to start, ACK, delete, GC, recover or restore.
Pool4/part2/API reservation1/seven writers, two EVM executions, RPC5 physical
attempts/s and the final NEW uncached canonical → generation fence → COMMIT
remain in the existing runtime code. No application/RPC/SQL code changes here.

The backup adapter and offline monitoring are implemented. The separate
[runtime composition draft](runtime/README.md) describes Apps, producer ownership,
private sockets/DSNs and lifecycle with an offline contract checker. Runtime
deployment/activation still needs actual APFS/private inputs, authenticated
checkpoint, official peer and the Arc first-import provider. The collector does
not implement that provider; filling an enode does not close that dependency.

Docker semantics: [build-context ignore files](https://docs.docker.com/build/concepts/context/#dockerignore-files),
[service bind mounts](https://docs.docker.com/reference/compose-file/services/#volumes).

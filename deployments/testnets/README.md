# Four-testnet Docker operations draft

**Draft only. Disk formatting, initialization, image builds/pulls, service startup and performance measurements remain on hold.** The checked-in manifests contain unresolved inputs and are not a deployment approval.

This composition runs one indexer process per chain and a dedicated PostgreSQL 16 instance: `indexer-stablenet` for StableNet, `indexer-arc` for Arc, and the generic `indexer` for BSC and for Sepolia. Each chain has its own configuration, halt latch and API port. It includes no execution/consensus node, snapshot downloader, Docker socket mount, global container name, fixed network subnet or shared Docker volume. It does not use or modify the repository's legacy root Compose/Dockerfile.

| File | Purpose |
| --- | --- |
| [compose.draft.yaml](compose.draft.yaml) | PostgreSQL and one indexer service per chain (`indexer-<chain>`); private DB network; loopback-only API ports 8088 (StableNet), 8089 (BSC), 8090 (Sepolia), 8091 (Arc) |
| [Dockerfile.draft](Dockerfile.draft) | Separate Go1.26.8/CGO0 image recipe with the three binaries; each service selects its chain's with `INDEXER_BINARY`; not built or pulled |
| [host.draft.json](host.draft.json) | Proposed mount path and unresolved physical/volume UUIDs, private directory and image |
| [preflight.py](preflight.py) | Offline lint, read-only macOS inspection, and a guarded future startup entry point |
| [indexer-entrypoint.sh](indexer-entrypoint.sh) | Read a DSN secret as data and bridge it to the application's existing environment setting |
| [init-indexer.sql](init-indexer.sql) | Create the non-superuser application role only during a later, explicit new-DB initialization |
| [requirements.txt](requirements.txt) | Pinned YAML parser for offline checks and CI |

The [operating plan](../../docs/OPERATIONS_DRAFT.md), [per-chain application YAML drafts](../../docs/examples/testnets) (private copies `config-<chain>.yaml`) and [PostgreSQL settings](../../docs/examples/postgresql-testnets.draft.conf) define the remaining inputs. Arc enode, live peer fork ID, checkpoints, APFS preparation and the private files are still unavailable/unresolved. Genesis and network ID have [separate evidence](../../docs/analysis/arc-testnet-identity.md).

The [capacity, backlog and recovery policy draft](../../docs/OPERATIONS_CAPACITY_DRAFT.md) sets the stop/resume thresholds. The application's `operations` section implements them in-process (`internal/opsguard`): it samples APFS free space every30s, the PGDATA directory every5m and the Arc raw window with each usage report. A stop threshold, a forecast reaching the floor too soon, or a missing observation writes that chain's halt latch `state/<chain>/halt.json` in the private directory and ends that chain's process. Every chain process samples the same APFS volume and PGDATA, so a capacity stop reaches the others at their next samples. Nothing restarts a stopped chain (`restart: "no"`); the multi-chain manager's automatic restart of a failed chain is gone with one process per chain. Compose gives each indexer SIGTERM and 120s, and PostgreSQL SIGINT (fast shutdown) and 60s, instead of Docker's 10s before SIGKILL; `preflight.py` checks these values. Whether shutdown finishes within them has not been measured. The database size (`pg_database_size`) and retained WAL (`pg_ls_waldir`) are journaled every 60s for diagnosis and never stop indexing; `init-indexer.sql` grants EXECUTE on `pg_ls_waldir()` only, at new-PGDATA initialization, so an existing database needs the same GRANT by hand. Docker VM space is checked by the host checks before start and resume, not while running. `--watch <private-manifest>` checks every30s and, when the volume is gone or replaced, free space is at or below110GiB, or the halt latch carries `stopDatabase` (storage failures, PostgreSQL `disk_full`/`io_error`), runs one `compose stop` of the four indexer services and then `compose stop postgres` within their grace periods and exits with status3. If an indexer does not stop in time, PostgreSQL is left running for the operator. It never runs `down`, removes containers or volumes, or restarts anything; keeping it running on the host (launchd or similar) is not set up yet.

The [checkpoint, restart and GC runbook draft](../../docs/OPERATIONS_RUNBOOK_DRAFT.md) separates normal shutdown, incident recovery and deletion prerequisites. `collect: false` prevents new collection; opening the app/window can still complete an already approved durable deletion. Startup is not a read-only inspection. The current host guard refuses `collect: true`; GC activation requires a separate reviewed change.

The separate [checkpoint/GC metadata command](../../docs/OPERATIONS_INSPECTION.md) reads existing DB/window records without app startup or deletion recovery. Its mock-based checks do not validate actual PostgreSQL/APFS operation; physical inspection and service execution remain on hold.

## Fixed boundaries

- `max_conns: 4` is per chain process, shared by capture/writer/API; the four processes' pool budget is16. Server32 includes room for administration and the readiness probe, not additional application pools.
- The config and Compose environment fix the per-chain shared RPC HTTP limit at5. Capture and writer retain their existing pre-commit fresh/epoch fences; no Go code changes them.
- Arc remains execution-only, without balance/native, token metadata, orderbook or optional proof/trace/projection tasks; new GC collection stays disabled. The other three chains retain strict RPC state reads.
- Each indexer mounts its chain's private `state/<chain>` directory writable at `/var/lib/indexer-ops` for the halt latch and the bounded capacity journal. Only Arc mounts the APFS volume writable (raw window, peer key); the others mount it read-only for the capacity monitor. It is outside the monitored APFS volume, so a removed data disk cannot erase the latch. The application refuses to start while the latch exists.
- Both services use `restart: "no"`. Daemon restarts must not bypass a new host check. The `operations-draft` profile and required interpolation inputs prevent an unconfigured default startup.
- All host mounts use long syntax with `create_host_path: false`. Missing paths are errors. Nothing creates the APFS volume, PGDATA, raw window, keys or secrets during inspection.
- Each service declares Docker's `local` log driver with `max-size: "10m"` and `max-file: "3"`. This bounds rotated stdout/stderr logs for these containers; it is not a DB/WAL quota or a reservation. Docker Desktop stores those logs in its own storage, so external-APFS free space alone does not cover Docker VM capacity; the host checks below also read the VM disk. No daemon-wide setting or existing container is changed.

`max_unhealthy_duration` is omitted from the application draft because the manager currently does not use it as a stop timer. The proposed worker5m/30m alerts require the separate monitor. The Arc `queue_blocks: 256`/256MiB settings describe the writer's in-memory queue; raw notification references have a separate module default and durable backlog lives on disk.

The connection/RPC budgets belong to each live App's pool, not a host-global limiter. Do not overlap duplicate Apps for a chain or restart before previous resources finish closing. `ChainInstance.Stop` can defer Close after a timeout; the current controller has no hard global16-connection/restart interlock. This is a code-path limitation, not an observed duplicate-run incident.

## Host mount and identity checks

The completed private manifest pins the **APFS VolumeUUID** and the **physical-store partition DiskUUID**. These are distinct from the window library's persistent marker ID and from a transient `/dev/diskN` name. Device names are discovered from current `diskutil` output, not pinned across reboots.

Read-only host inspection requires all of the following:

1. macOS, an existing absolute mount root, no symlink substitution and an actual mount point.
2. APFS at that exact root, the pinned volume UUID, and enabled ownership enforcement.
3. Exactly one backing physical store with the pinned partition UUID, on an external physical disk. Internal disks and disk images are refused.
4. At least100GiB filesystem free space at startup. This is not a reservation or a160GiB PostgreSQL quota. The proposed operational resume threshold is **strictly above150GiB**, plus the other capacity conditions; the host check does not enforce that proposal.
5. Existing, owned PGDATA/raw/identity directories on the same filesystem; PGDATA mode0700 and a private regular peer-key file0600.
6. A complete runtime config, empty inline DSN, the fixed connection/RPC/state/GC policies, and Arc `start_height = anchor_height + 1`.
7. Private credential files and a local `desktop-linux` Docker context using a Unix socket. Docker endpoint or inherited `INDEXER_*` overrides are refused.

These host-policy checks do not replace the application's full Go schema, peer identity and checkpoint validation.

The volume identity is inspected again at the end of the checks. Only the explicit future `--start` path can then invoke Compose, for the fixed project/context, with image building and pulling disabled. Default lint never contacts Docker. `--check-host` (and so `--start` and `--resume`) runs exactly one throwaway container after the other checks pass: `df -P -k /` in the reviewed image with `--rm`, `--pull never`, no network, no capabilities and a read-only root, to read the Docker VM disk, which images, container layers and these logs use. It refuses unless more than 10GiB (`DOCKER_VM_MIN_FREE`, an initial review value) is free. This is a check at that instant, not continuous monitoring. Neither path calls `up`, `exec`, `build`, `pull`, format or initialization commands.

Direct `docker compose up` bypasses the macOS host guard and is not the supported entry point. A saved passing report is not reusable startup authorization. The host check is a point-in-time check: removal after handoff to Docker and physical power-loss durability are not proven. Existing raw-window runtime mount/identity checks remain active; the host watcher started by `--start` (below) also stops PostgreSQL after device removal. Docker Desktop must be allowed to share the selected host paths; actual mount/UID behavior remains unverified until the execution hold is lifted.

## Private credential injection

The completed host manifest itself must be an owned, regular, single-link file0600 when passed to `--check-host`/`--start`; keep that private copy outside the repository. Its `private_dir` must be outside both this repository and the mounted data volume, owned by the invoking non-root user, with mode0700. Existing regular, single-link files0600 are required:

| Private file | Consumer |
| --- | --- |
| `config.yaml` | Indexer, read-only; complete the YAML placeholders here, leave `database.postgres.dsn` empty |
| `postgres-dsn` | Indexer only, mounted at `/run/secrets/postgres_dsn` |
| `indexer-password` | PostgreSQL bootstrap only; must match the DSN password |
| `postgres-admin-password` | PostgreSQL image's `POSTGRES_PASSWORD_FILE`; distinct from the app password |

The private directory also needs an owned `state` subdirectory with mode0700, mounted writable into the indexer only. It holds no secrets: the halt latch records the reason, chain, a bounded error description and the time.

For this isolated, unexposed Docker DB service, the DSN targets `postgres:5432/indexer_testnets_v1`, role `indexer`, with `sslmode=disable`. This is a local bridge trust boundary, not an external/TLS-enabled DB recipe. Password files use distinct URL-safe32..128-character values; no real passwords are generated by this draft.

Compose declares secret **file paths**, never DSN/password values. The entrypoint reads the DSN with `cat`, not shell `source`/`eval`, disables tracing, rejects direct DSN overrides, and exports `INDEXER_DB_POSTGRES_DSN` only in the indexer process. The DSN is not added to command arguments or Compose's container environment definition. An administrator who can inspect the running process environment can still access it.

The application role is `NOSUPERUSER NOCREATEDB NOCREATEROLE`, with permission to connect/create its own chain schemas in the dedicated database. Bootstrap reads its password inside PostgreSQL and uses `%L` quoting; it does not put a password in `psql` arguments. Bootstrap runs only for an explicitly initialized empty PGDATA, not as a password-rotation mechanism for an existing database.

Both containers use the invoking host UID/GID so bind-mounted0600 secrets remain readable without making them public. This deliberately avoids assuming that Compose secret `uid`/`gid` remaps local file-backed secrets. PostgreSQL's Debian image supports arbitrary UIDs through `nss_wrapper`; actual initialization on the selected external APFS volume has not been run. The indexer receives the whole selected data volume for its window manager, so these two trusted services are not isolated from each other's files by separate filesystem users.

## Checks allowed during this hold

Host-side Python3.9 or newer and the pinned [requirements](requirements.txt) are needed. In your own virtual environment, install them with `python -m pip install -r deployments/testnets/requirements.txt`. These commands inspect draft files or synthetic fixtures only:

```bash
python3 deployments/testnets/preflight.py
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s deployments/testnets -p 'test_preflight.py'
sh -n deployments/testnets/indexer-entrypoint.sh
```

The offline tests replace `diskutil`, Docker context inspection, the VM disk report and mount/free-space readings with fixtures. The entrypoint test runs a temporary fake program, never indexer or a DB. The future startup dispatch is mocked, so no Docker service starts.

Default lint also cross-checks public Compose/application/PostgreSQL files: connection/RPC budgets, schema/window binding, effective strict-state overrides, API publication, secret/entrypoint paths, image reference, log limits, networks, mounts and durability settings. Completed private configs undergo the policy checks too. This is a selected operating-contract check, not the full Go schema/module validator or a check of live checkpoint/fork values. Unresolved host/application fields are listed by name; `draftArtifacts: consistent` does not mean runnable or approved.

The [offline review](../../docs/analysis/operations-offline-review.md) records the corrections and the final21 fixture checks. The separate metadata command has mock-based checks; live DB/window inspection, full GC eligibility validation, and measuring actual shutdown times remain operational work.

The required CI `test` job runs these fixture checks, draft lint and shell syntax with the pinned parser. It does not build the operations image, access real disks, contact peers/RPC or launch Docker/DB services for these checks.

After inputs are available, `--check-host <private-manifest>` performs real-host checks that change nothing but the throwaway `df` container above, and reports only nonsecret paths/UIDs. The separate `--start <private-manifest>` is implemented for later use **after the execution hold is explicitly lifted**, and is not invoked or prescribed as a current action. It always repeats the complete check before the fixed project's Compose startup, and refuses while a halt latch exists. It then does not exit: it watches the stack as `--watch <private-manifest>` does, every30s, and stops the indexer and then PostgreSQL on a storage emergency (exit 3; never down/rm, never restarts). It takes the stack's watcher lock (`watch.lock` in the private directory, holding the watcher's PID) before Compose starts, so it refuses to start a stack another watcher already watches, and a stack it starts always has a watcher. If Compose fails or times out (300s, PostgreSQL's healthcheck included), it reports that and does not watch. With `--background` (also accepted by `--watch`) the watcher detaches from the terminal into its own session, logs to `watch.log` in the private directory (0600) and the command returns its PID; stop it with `kill <PID>`. An interrupted or terminated watcher exits 130 and leaves the stack running unwatched. Without `--background` closing the terminal ends the watcher. Nothing restarts a watcher after a reboot; the services do not restart either (`restart: "no"`), so start again with `--start`. After a halt, resolve the cause, then run `--resume <private-manifest>`: it repeats the host checks and runs the `resume-check` command of every latched chain, each in a one-off container of its service without the database, and fails if any check refuses. The check samples free space three times,30s apart, each above150GiB, and the PGDATA directory below80% of160GiB, and only then removes the latch. A raw window halt or an integrity halt is never cleared by it: raw usage does not fall while collection is off, and those causes need the operator's own verification and manual removal of the latch. Images must already be reviewed and available locally. Initial raw-volume/window creation remains a separate deferred procedure; this draft requires initialization/GC flagsfalse and does not perform first-time provisioning.

## Verification limits and references

Offline/config success does not prove Docker Desktop file sharing, APFS fsync behavior, PG initialization/role grants, secret UID mapping, public peer connectivity, 24-hour storage capacity or450ms ingestion. Image tags in this draft are not digest pins; release image selection remains a later review step. No whole-repository/race200/performance reruns are required by these fixture checks alone.

- [Compose bind mounts](https://docs.docker.com/reference/compose-file/services/#volumes): `create_host_path: false`.
- [Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/): per-service file injection.
- [Official PostgreSQL image](https://hub.docker.com/_/postgres): password files, new-DB initialization and arbitrary-user behavior.
- [Docker local logging driver](https://docs.docker.com/engine/logging/drivers/local/): per-container rotation options.

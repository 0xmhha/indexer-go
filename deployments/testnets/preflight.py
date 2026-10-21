#!/usr/bin/env python3
"""Offline draft lint, read-only host checks, or explicit guarded startup after the hold is lifted."""

import argparse
import fcntl
import json
import os
from pathlib import Path
import plistlib
import re
import signal
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qs, unquote, urlsplit
import uuid

import yaml

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
# One indexer process per chain: Compose service indexer-<id>, running the
# binary built for the chain with config-<id>.yaml and state/<id>.
CHAINS = {
    "stablenet-testnet": {"chain_id": 8283, "binary": "indexer-stablenet", "port": 8088},
    "bsc-testnet": {"chain_id": 97, "binary": "indexer", "port": 8089},
    "sepolia": {"chain_id": 11155111, "binary": "indexer", "port": 8090},
    "arc-testnet": {"chain_id": 5042002, "binary": "indexer-arc", "port": 8091},
}
ARC = "arc-testnet"
SCHEMA_BASE = "indexer_testnets_v1"
DRAFTS = REPO / "docs/examples/testnets"
MANIFEST_KEYS = {"mount_root", "apfs_volume_uuid", "physical_store_uuid", "private_dir", "indexer_image", "docker_context"}
STATE_DIR = "/var/lib/indexer-ops"
# Docker images, container layers and the services' logs live on the Docker
# VM disk, not on the monitored APFS volume. Initial review value.
DOCKER_VM_MIN_FREE = 10 << 30
# --watch stops this project's PostgreSQL too at or below this APFS free
# space (the capacity draft's emergency line, below stop_free_bytes).
EMERGENCY_FREE = 110 << 30
WATCH_INTERVAL = 30
# Bounds for compose stop: each service's grace period plus a margin. The
# indexers stop together, in one command.
STOP_TIMEOUT = {"indexers": 180, "postgres": 120}
# The resume check samples three times, 30s apart.
RESUME_TIMEOUT = 300
# compose up waits for PostgreSQL's healthcheck before the indexer starts.
START_TIMEOUT = 300
# In the private directory, outside the state directory the indexer mounts:
# the lock that keeps one watcher per stack, holding its PID, and the
# background watcher's log.
WATCH_LOCK = "watch.lock"
WATCH_LOG = "watch.log"
COMPOSE = ["docker", "--context", "desktop-linux", "compose", "--project-name", "indexer-testnets-draft", "--file", str(HERE / "compose.draft.yaml"), "--profile", "operations-draft"]
OPERATIONS = {
    "halt_file": STATE_DIR + "/halt.json",
    "journal_file": STATE_DIR + "/capacity.jsonl",
    "capacity": {
        "filesystem_path": "/data/feed",
        "floor_free_bytes": 100 << 30,
        "stop_free_bytes": 120 << 30,
        "warn_free_bytes": 150 << 30,
        "resume_free_bytes": 150 << 30,
        "postgres_data_dir": "/data/feed/indexer/postgres/16",
        "postgres_budget_bytes": 160 << 30,
    },
}


class Refused(Exception):
    """Safe diagnostic: do not attach credentials or untrusted parser errors."""


def require(condition, message):
    if not condition:
        raise Refused(message)


def unique(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "Duplicate configuration key.")
        result[key] = value
    return result


class StrictYAML(yaml.SafeLoader):
    pass


def yaml_mapping(loader, node, deep=False):
    return unique((loader.construct_object(k, deep=deep), loader.construct_object(v, deep=deep)) for k, v in node.value)


StrictYAML.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, yaml_mapping)


def unresolved(value, prefix=""):
    if isinstance(value, dict):
        return [p for k, v in value.items() for p in unresolved(v, f"{prefix}.{k}".strip("."))]
    if isinstance(value, list):
        return [p for i, v in enumerate(value) for p in unresolved(v, f"{prefix}[{i}]")]
    return [prefix] if isinstance(value, str) and "UNCONFIRMED_" in value else []


def read_yaml(path):
    try:
        require(path.stat().st_size <= 1024 * 1024, "Configuration exceeds size limit.")
        result = yaml.load(path.read_text(), Loader=StrictYAML)
        require(isinstance(result, dict), "Configuration must be a mapping.")
        return result
    except (OSError, UnicodeError, yaml.YAMLError):
        raise Refused("Cannot read YAML configuration.") from None


def service(chain):
    """The Compose service of a chain."""
    return "indexer-" + chain


def chain_schema(chain):
    """The PostgreSQL schema of a chain: the one multi-chain mode used."""
    return re.sub(r"[^a-z0-9_]", "_", f"{SCHEMA_BASE}_{chain}".lower())


def draft_config(chain):
    return read_yaml(DRAFTS / f"{chain}.draft.yaml")


def check_policy(cfg, chain, draft=False):
    """Check one chain's configuration; returns its unresolved fields."""
    try:
        require(chain in CHAINS, "Unknown chain.")
        require(cfg["database"]["driver"] == "postgres" and cfg["database"]["postgres"]["max_conns"] == 4, "Require PostgreSQL and four connections per chain.")
        require(cfg["database"]["postgres"]["schema"] == chain_schema(chain), "Unexpected per-chain schema.")
        require(cfg["database"]["postgres"].get("dsn", "") == "", "Inline DSN is forbidden; use the secret file.")
        require(cfg["rpc"]["rate_limit"] == 5 and cfg["indexer"]["finality"] == "head", "Require shared RPC5 and the existing head/fresh policy.")
        require(cfg["node"]["role"] == "all" and not cfg.get("multichain", {}).get("enabled"), "Require one single-chain all-role process per chain.")
        require(cfg["api"]["enabled"] is True and cfg["api"]["host"] == "0.0.0.0" and cfg["api"]["port"] == CHAINS[chain]["port"], "App listener must match Compose publication/healthcheck.")
        require(cfg["operations"] == OPERATIONS, "Capacity halt latch/thresholds differ from the operating plan.")
        if chain == ARC:
            feed = cfg["source"]["feed"]
            require(feed["module"] == "arc.eth68" and feed["window"] == "arc-testnet" and str(feed["chain_id"]) == str(CHAINS[ARC]["chain_id"]), "Unexpected Arc feed module/window/chain.")
            require(feed["collect"] is False and feed["initialize_volume"] is False and feed["create_window"] is False, "GC and initialization remain deferred.")
            require(not feed.get("state_proofs") and not feed.get("call_traces") and not feed.get("projections"), "Extra feed tasks are outside this draft.")
            require(feed["key_file"] == "/data/feed/indexer/identity/arc-peer.key", "Unexpected peer identity path.")
            for name in ("balance.native", "token.metadata", "dex.orderbook"):
                require(cfg["features"][name]["enabled"] is False, "Arc execution-only state features must remain disabled.")
            require(set(cfg["window_storage"]["volumes"]) == {"external"} and set(cfg["window_storage"]["windows"]) == {"arc-testnet"}, "Only the planned external volume and Arc raw window are allowed.")
            volume = cfg["window_storage"]["volumes"]["external"]
            window = cfg["window_storage"]["windows"]["arc-testnet"]
            require(window["volume"] == "external" and max(window.get("min_free_bytes", 0), volume["min_free_bytes"]) == 100 << 30, "Window volume/floor differs from the operating plan.")
            require(volume["root"] == "/data/feed" and volume["id"] == "indexer-apfs-testnets-v1" and volume["allow_directory"] is False, "Unexpected raw volume binding.")
            require((volume["max_bytes"], volume["min_free_bytes"], window["max_bytes"], window["retention"]) == (160 << 30, 100 << 30, 128 << 30, "24h"), "Raw capacity/retention policy changed.")
            require(window["path"] == "/data/feed/indexer/window/arc", "Unexpected raw window path.")
        else:
            require("feed" not in cfg.get("source", {}) and not cfg.get("window_storage"), "Only Arc may select the feed and a raw window.")
            for name in ("balance.native", "token.metadata"):
                require(cfg["features"][name]["enabled"] is True and cfg["features"][name]["allow_pruned_state"] is False, "Strict RPC state features must remain enabled.")
        missing = unresolved(cfg)
        if not draft:
            require(not missing, "Runtime configuration has unresolved inputs: " + ", ".join(missing))
            if chain == ARC:
                require(type(cfg["source"]["feed"]["anchor_height"]) is int and cfg["indexer"]["start_height"] == cfg["source"]["feed"]["anchor_height"] + 1, "Arc start must equal anchor + 1.")
        return missing
    except (KeyError, TypeError):
        raise Refused("Required operating configuration field is missing or malformed.") from None


def check_artifacts(compose=None, postgres_conf=None):
    """Cross-check public draft files only; no environment expansion or host probes."""
    if compose is None:
        compose = read_yaml(HERE / "compose.draft.yaml")
    if postgres_conf is None:
        postgres_conf = (REPO / "docs/examples/postgresql-testnets.draft.conf").read_text()
    try:
        require(compose["name"] == "indexer-testnets-draft" and set(compose["services"]) == {"postgres"} | {service(c) for c in CHAINS}, "Unexpected Compose project/services.")
        services = compose["services"]
        db = services["postgres"]
        require(not compose.get("volumes") and not db.get("ports"), "Named storage or a published DB port is outside the draft.")
        prefix = "${INDEXER_PRIVATE_DIR:?preflight required}/"
        expected_secrets = {"postgres_admin_password": "postgres-admin-password", "indexer_password": "indexer-password", "postgres_dsn": "postgres-dsn"}
        require(compose["secrets"] == {key: {"file": prefix + name} for key, name in expected_secrets.items()}, "Secret file paths differ from host/entrypoint checks.")
        require(db["secrets"] == ["postgres_admin_password", "indexer_password"], "Unexpected secret consumer.")
        require(db["image"] == "postgres:16-bookworm" and db["environment"]["POSTGRES_DB"] == "indexer_testnets_v1" and db["environment"]["POSTGRES_PASSWORD_FILE"] == "/run/secrets/postgres_admin_password", "PostgreSQL image/database/password file drift.")
        raw = "${INDEXER_APFS_ROOT:?preflight required}"
        db_mounts = {v["target"]: v for v in db["volumes"]}
        require(db_mounts["/var/lib/postgresql/data"]["source"] == raw + "/indexer/postgres/16" and db_mounts["/etc/postgresql/indexer.conf"]["source"] == "../../docs/examples/postgresql-testnets.draft.conf", "PGDATA/settings mount drift.")
        require(db["environment"]["PGDATA"] == "/var/lib/postgresql/data" and db_mounts["/etc/postgresql/indexer.conf"]["read_only"] is True and db_mounts["/docker-entrypoint-initdb.d/010-indexer.sql"]["source"] == "./init-indexer.sql" and db_mounts["/docker-entrypoint-initdb.d/010-indexer.sql"]["read_only"] is True, "PostgreSQL init/config wiring drift.")
        require((db.get("stop_signal"), db.get("stop_grace_period")) == ("SIGINT", "60s") and db["networks"] == ["database"], "PostgreSQL shutdown/network drift.")
        require(compose["networks"] == {"database": {"internal": True}, "rpc": {}}, "Unexpected DB/RPC network boundary.")
        connections = 0
        for chain, spec in CHAINS.items():
            app = services[service(chain)]
            cfg = draft_config(chain)
            check_policy(cfg, chain, draft=True)
            port = spec["port"]
            connections += cfg["database"]["postgres"]["max_conns"]
            require(app["image"] == "${INDEXER_OPERATIONS_IMAGE:?reviewed operations image required}", "Compose must use the checked image reference.")
            require(app["ports"] == [f"127.0.0.1:{port}:{port}"] and app["read_only"] is True, "Unexpected API exposure or writable root filesystem.")
            require(app["healthcheck"]["test"] == ["CMD", "wget", "--quiet", "--tries=1", "--spider", f"http://127.0.0.1:{port}/health"], "Healthcheck differs from the chain's API port.")
            require(app["environment"] == {"INDEXER_BINARY": spec["binary"], "INDEXER_DB_POSTGRES_DSN_FILE": "/run/secrets/postgres_dsn", "INDEXER_DB_DRIVER": "postgres", "INDEXER_DB_POSTGRES_MAX_CONNS": str(cfg["database"]["postgres"]["max_conns"]), "INDEXER_RPC_RATE_LIMIT": str(cfg["rpc"]["rate_limit"])}, "Compose environment differs from the chain binary/app/secret policy.")
            require(app["secrets"] == ["postgres_dsn"], "Unexpected secret consumer.")
            mounts = {v["target"]: v for v in app["volumes"]}
            require(set(mounts) == {"/data/feed", "/etc/indexer/config.yaml", STATE_DIR, "/opt/indexer/entrypoint.sh"}, "Unexpected indexer mounts.")
            require(mounts["/data/feed"]["source"] == raw and bool(mounts["/data/feed"].get("read_only")) == (chain != ARC), "Only Arc may write the data volume.")
            require(mounts["/etc/indexer/config.yaml"]["source"] == prefix + f"config-{chain}.yaml" and mounts["/etc/indexer/config.yaml"]["read_only"] is True, "App config mount drift.")
            require(mounts[STATE_DIR]["source"] == prefix + f"state/{chain}" and not mounts[STATE_DIR].get("read_only"), "Halt latch state mount drift.")
            require(mounts["/opt/indexer/entrypoint.sh"]["source"] == "./indexer-entrypoint.sh" and mounts["/opt/indexer/entrypoint.sh"]["read_only"] is True and app["entrypoint"] == ["/bin/sh", "/opt/indexer/entrypoint.sh"] and app["command"] == [], "Secret entrypoint wiring drift.")
            require((app.get("stop_signal"), app.get("stop_grace_period")) == ("SIGTERM", "120s"), "Shutdown signal/grace period drift.")
            require(app["networks"] == ["database", "rpc"] and app["depends_on"] == {"postgres": {"condition": "service_healthy"}}, "Unexpected indexer network/dependency.")
        for name in services.values():
            require(name["profiles"] == ["operations-draft"] and name["restart"] == "no" and not name.get("container_name") and not name.get("build"), "Unexpected automatic startup/build behavior.")
            require(name["user"] == "${INDEXER_RUN_UID:?preflight required}:${INDEXER_RUN_GID:?preflight required}", "Host UID/GID mapping drift.")
            require(name["logging"] == {"driver": "local", "options": {"max-size": "10m", "max-file": "3"}}, "Require per-service bounded Docker logs.")
            for mount in name["volumes"]:
                require(mount["type"] == "bind" and mount["bind"]["create_host_path"] is False, "All binds must refuse missing paths.")
        pairs = []
        for line in postgres_conf.splitlines():
            line = line.split("#", 1)[0].strip()
            if line:
                match = re.fullmatch(r"([a-z_]+)\s*=\s*([a-z0-9]+)", line)
                require(match is not None, "Unexpected PostgreSQL settings syntax.")
                pairs.append(match.groups())
        settings = unique(pairs)
        require(settings == {"max_connections": "32", "fsync": "on", "synchronous_commit": "on", "full_page_writes": "on"}, "PostgreSQL connection/durability settings drift.")
        require(connections < int(settings["max_connections"]), "The indexers' pools exceed PostgreSQL's connections.")
    except (KeyError, TypeError):
        raise Refused("Required Compose/settings field is missing or malformed.") from None


def private_file(path, maximum=16384):
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, "rb") as stream:
            s = os.fstat(stream.fileno())
            require(stat.S_ISREG(s.st_mode) and s.st_uid == os.getuid() and stat.S_IMODE(s.st_mode) == 0o600 and s.st_nlink == 1, "Private file must be owned, regular, single-link and mode0600.")
            value = stream.read(maximum + 1)
            require(0 < len(value) <= maximum, "Private file is empty or oversized.")
            return value
    except OSError:
        raise Refused("Required private file is unavailable.") from None


def canonical_directory(path):
    require(path.is_absolute() and path.resolve(strict=True) == path and path.is_dir(), "Directory must exist at its absolute non-symlink path.")


def probe(*args):
    try:
        result = subprocess.run(args, check=True, capture_output=True, timeout=15)
        require(len(result.stdout) <= 4 * 1024 * 1024, "Inspection output exceeded limit.")
        return result.stdout
    except (OSError, subprocess.SubprocessError):
        raise Refused("Disk/context inspection failed.") from None


def inspect_apfs(manifest):
    require(sys.platform == "darwin", "Host inspection requires macOS.")
    root = Path(manifest["mount_root"])
    canonical_directory(root)
    require(os.path.ismount(root), "APFS root is not an actual mounted filesystem.")
    info = plistlib.loads(probe("/usr/sbin/diskutil", "info", "-plist", str(root)))
    require(info.get("FilesystemType", "").lower() == "apfs" and info.get("MountPoint") == str(root), "Require the exact APFS mount root.")
    require(str(info.get("VolumeUUID", "")).lower() == manifest["apfs_volume_uuid"].lower(), "APFS volume UUID mismatch.")
    require(info.get("GlobalPermissionsEnabled") is True, "APFS ownership must be enabled.")
    ref = info.get("APFSContainerReference", "")
    require(re.fullmatch(r"disk[0-9]+", ref), "APFS container reference is unavailable.")
    listing = plistlib.loads(probe("/usr/sbin/diskutil", "apfs", "list", "-plist", ref))
    containers = [c for c in listing.get("Containers", []) if c.get("ContainerReference") == ref]
    require(len(containers) == 1, "Cannot identify the APFS container.")
    stores = containers[0].get("PhysicalStores", [])
    require(len(stores) == 1, "Draft supports exactly one APFS physical store.")
    store = stores[0]
    require(str(store.get("DiskUUID", "")).lower() == manifest["physical_store_uuid"].lower(), "Physical-store partition UUID mismatch.")
    device = store.get("DeviceIdentifier", "")
    require(re.fullmatch(r"disk[0-9]+(?:s[0-9]+)*", device), "Physical-store device is unavailable.")
    physical = plistlib.loads(probe("/usr/sbin/diskutil", "info", "-plist", "/dev/" + device))
    require(physical.get("Internal") is False and physical.get("VirtualOrPhysical") == "Physical", "Require external physical storage, not an internal disk or disk image.")
    space = os.statvfs(root)
    require(space.f_bavail * space.f_frsize >= 100 << 30, "Filesystem free space is below the100GiB startup floor.")
    return root


def docker_vm_free(image):
    """Free bytes of the Docker VM disk, read by df in a throwaway container.

    The container uses the reviewed image without pulling, has no network,
    capabilities or writable root, and is removed when df exits.
    """
    out = probe("docker", "--context", "desktop-linux", "run", "--rm", "--pull", "never", "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--user", f"{os.getuid()}:{os.getgid()}", "--label", "com.0xmhha.indexer.owner=indexer-go", "--entrypoint", "df", image, "-P", "-k", "/")
    lines = out.decode("ascii", "replace").splitlines()
    fields = lines[1].split() if len(lines) == 2 else []
    require(len(fields) == 6 and fields[3].isdigit() and fields[5] == "/", "Unexpected Docker VM disk report.")
    return int(fields[3]) * 1024


def halt_latch(private, chain):
    """The latch a chain's indexer writes on a capacity or storage halt."""
    return private / "state" / chain / "halt.json"


def latched(private):
    """The chains whose halt latch exists, in CHAINS order."""
    return [c for c in CHAINS if halt_latch(private, c).exists()]


def compose_environment(manifest):
    """The nonsecret variables the Compose file interpolates."""
    return {"INDEXER_APFS_ROOT": manifest["mount_root"], "INDEXER_PRIVATE_DIR": manifest["private_dir"], "INDEXER_RUN_UID": str(os.getuid()), "INDEXER_RUN_GID": str(os.getgid()), "INDEXER_OPERATIONS_IMAGE": manifest["indexer_image"]}


def emergency(manifest):
    """Why this project's database must stop now, or None.

    The volume is gone, replaced or below its startup floor; free space is at
    or below the emergency line; or an indexer's latch asks for it.
    """
    try:
        root = inspect_apfs(manifest)
    except (Refused, OSError):
        return "data volume unavailable, replaced or below its floor"
    space = os.statvfs(root)
    if space.f_bavail * space.f_frsize <= EMERGENCY_FREE:
        return "free space at or below the110GiB emergency line"
    for chain in CHAINS:
        try:
            latch = json.loads(private_file(halt_latch(Path(manifest["private_dir"]), chain), 4096))
        except (Refused, ValueError):
            continue  # no latch, or one the indexer itself refuses to start with
        if isinstance(latch, dict) and latch.get("stopDatabase") is True:
            return f"{chain} halt latch asks to stop the database"
    return None


def stop_stack(manifest):
    """Stop every indexer together, then PostgreSQL, each within its grace period.

    Never down, rm or volume removal: data and containers stay for recovery.
    """
    environment = {**os.environ, **compose_environment(manifest)}
    indexers = [service(c) for c in CHAINS]
    stopped = []
    for group, names in (("indexers", indexers), ("postgres", ["postgres"])):
        try:
            subprocess.run([*COMPOSE, "stop", *names], check=True, capture_output=True, timeout=STOP_TIMEOUT[group], env=environment)
        except (OSError, subprocess.SubprocessError):
            raise Refused(f"Stopping {group} failed or timed out; check it by hand.") from None
        stopped.extend(names)
    return stopped


def check_watchable(manifest):
    require(set(manifest) == MANIFEST_KEYS and not unresolved(manifest), "Host manifest fields do not match the draft schema.")
    require(not any(os.environ.get(k) for k in ("DOCKER_HOST", "DOCKER_CONTEXT")), "Clear Docker endpoint overrides before watching.")


def watch_lock(private):
    """Hold the stack's watcher lock until this process (or its forked child) exits.

    Refuses while another watcher holds it. The file holds the holder's PID.
    """
    try:
        fd = os.open(private / WATCH_LOCK, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    except OSError:
        raise Refused("Watcher lock file is unavailable.") from None
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        os.close(fd)
        raise Refused("Another watcher already runs for this stack.") from None
    write_pid(fd)
    return fd


def write_pid(fd):
    os.ftruncate(fd, 0)
    os.pwrite(fd, f"{os.getpid()}\n".encode(), 0)


def detach(private):
    """Fork; the child leaves the terminal's session and logs to WATCH_LOG.

    Returns the child's PID in the parent and 0 in the child.
    """
    log = os.open(private / WATCH_LOG, os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
    # Buffered output would otherwise be written twice, once by each process.
    sys.stdout.flush()
    sys.stderr.flush()
    pid = os.fork()
    if pid:
        os.close(log)
        return pid
    os.setsid()
    null = os.open(os.devnull, os.O_RDONLY)
    os.dup2(null, 0)
    os.dup2(log, 1)
    os.dup2(log, 2)
    os.close(null)
    os.close(log)
    return 0


def watch(manifest, sleep=time.sleep, lock=None):
    """Check every WATCH_INTERVAL seconds; on an emergency stop the stack once and return.

    Takes the watcher lock unless the caller already holds it.
    """
    check_watchable(manifest)
    if lock is None:
        lock = watch_lock(Path(manifest["private_dir"]))
    while True:
        reason = emergency(manifest)
        if reason is not None:
            return {"emergencyStop": reason, "stopped": stop_stack(manifest)}
        sleep(WATCH_INTERVAL)


def start_stack(environment):
    """compose up the checked stack; returns once its services are up."""
    try:
        subprocess.run([*COMPOSE, "up", "--detach", "--no-build", "--pull", "never"], check=True, timeout=START_TIMEOUT, env={**os.environ, **environment})
    except (OSError, subprocess.SubprocessError):
        raise Refused("Compose startup failed or timed out; no watcher runs. Check the stack by hand.") from None


def stop_watching(signum, frame):
    raise KeyboardInterrupt


def run_watch(manifest, background, lock=None):
    """Watch in this process, or in a detached child when background is set.

    Returns the exit status: 3 after an emergency stop, 0 for the parent of a
    background watcher, 130 when the watcher is interrupted or terminated.
    """
    check_watchable(manifest)
    private = Path(manifest["private_dir"])
    if lock is None:
        lock = watch_lock(private)
    if background:
        pid = detach(private)
        if pid:
            os.close(lock)
            print(json.dumps({"watcher": pid, "log": str(private / WATCH_LOG), "lock": str(private / WATCH_LOCK)}))
            return 0
        write_pid(lock)
    signal.signal(signal.SIGTERM, stop_watching)
    try:
        result = watch(manifest, lock=lock)
    except KeyboardInterrupt:
        print("Watcher stopped; the stack keeps running unwatched.", file=sys.stderr, flush=True)
        return 130
    finally:
        os.close(lock)
    print(json.dumps(result), flush=True)
    return 3


def check_host(manifest, resume=False):
    require(set(manifest) == MANIFEST_KEYS, "Host manifest fields do not match the draft schema.")
    require(not unresolved(manifest), "Host manifest still has unresolved inputs.")
    for key in ("apfs_volume_uuid", "physical_store_uuid"):
        require(str(uuid.UUID(manifest[key])).lower() == manifest[key].lower(), "Invalid pinned disk UUID.")
    require(os.getuid() != 0, "Run as the intended non-root owner.")
    require(manifest["docker_context"] == "desktop-linux", "Only the local Docker Desktop context is supported.")
    require(not any(os.environ.get(k) for k in ("DOCKER_HOST", "DOCKER_CONTEXT")), "Clear Docker endpoint overrides before inspection.")
    require(not any(k.startswith("INDEXER_") for k in os.environ), "Clear inherited INDEXER overrides before inspection.")
    require(re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9./_:@-]+", manifest["indexer_image"]), "Invalid reviewed indexer image reference.")
    root = inspect_apfs(manifest)
    private = Path(manifest["private_dir"])
    canonical_directory(private)
    require(not private.is_relative_to(REPO) and not private.is_relative_to(root), "Private directory must be outside the repository and data volume.")
    s = private.stat()
    require(s.st_uid == os.getuid() and stat.S_IMODE(s.st_mode) == 0o700, "Private directory must be owner-only mode0700.")
    for path in [private / "state", *(private / "state" / c for c in CHAINS)]:
        canonical_directory(path)
        s = path.stat()
        require(s.st_uid == os.getuid() and stat.S_IMODE(s.st_mode) == 0o700, "Private state directories must be owner-only mode0700.")
    halted = latched(private)
    require(resume or not halted, "A halt latch is present: resolve its cause, then run --resume before --start.")
    require(not resume or halted, "No halt latch is present; nothing to resume.")
    for chain in CHAINS:
        try:
            cfg = yaml.load(private_file(private / f"config-{chain}.yaml", 1024 * 1024), Loader=StrictYAML)
        except yaml.YAMLError:
            raise Refused("Cannot parse private runtime configuration.") from None
        require(isinstance(cfg, dict), "Configuration must be a mapping.")
        check_policy(cfg, chain)
    for name in ("indexer/postgres/16", "indexer/window/arc", "indexer/identity"):
        path = root / name
        canonical_directory(path)
        require(path.stat().st_dev == root.stat().st_dev, "Data path crosses a different mounted filesystem.")
        require(path.stat().st_uid == os.getuid(), "Data directories must belong to the invoking UID.")
    require(stat.S_IMODE((root / "indexer/postgres/16").stat().st_mode) == 0o700, "PGDATA must be mode0700.")
    key = private_file(root / "indexer/identity/arc-peer.key", 128).strip()
    require(re.fullmatch(rb"[0-9a-fA-F]{64}", key) is not None, "Invalid peer key file format.")
    admin = private_file(private / "postgres-admin-password").rstrip(b"\r\n")
    password = private_file(private / "indexer-password").rstrip(b"\r\n")
    require(admin != password and all(re.fullmatch(rb"[A-Za-z0-9_-]{32,128}", x) for x in (admin, password)), "Require distinct URL-safe passwords of32..128 characters.")
    dsn = private_file(private / "postgres-dsn").decode("utf-8").rstrip("\r\n")
    parsed = urlsplit(dsn)
    require(parsed.scheme in ("postgres", "postgresql") and parsed.hostname == "postgres" and parsed.port == 5432 and parsed.username == "indexer" and parsed.path == "/indexer_testnets_v1", "DSN must target this draft's internal PostgreSQL service and app role.")
    require(unquote(parsed.password or "").encode() == password and parse_qs(parsed.query) == {"sslmode": ["disable"]} and not parsed.fragment, "DSN secret does not match the internal connection policy.")
    require(not any(c in dsn for c in "\r\n\0"), "DSN must be a single line.")
    contexts = json.loads(probe("docker", "context", "inspect", "desktop-linux"))
    require(len(contexts) == 1 and contexts[0].get("Name") == "desktop-linux" and contexts[0].get("Endpoints", {}).get("docker", {}).get("Host", "").startswith("unix://"), "Require the inspected local Docker Desktop context.")
    require(docker_vm_free(manifest["indexer_image"]) > DOCKER_VM_MIN_FREE, "Docker VM disk has too little free space for images, container layers and logs.")
    # Detect removal/replacement during the checks before returning to the caller.
    require(inspect_apfs(manifest) == root, "APFS identity changed during inspection.")
    return compose_environment(manifest)


def resume(manifest, environment):
    """Run the resume check of every latched chain, one container each.

    Each container has its service's mounts and no database. Returns 0 if
    every check cleared its latch, 1 otherwise; the others are still run.
    """
    status = 0
    for chain in latched(Path(manifest["private_dir"])):
        command = [*COMPOSE, "run", "--rm", "--no-deps", "--pull", "never", service(chain), "resume-check"]
        try:
            subprocess.run(command, check=True, timeout=RESUME_TIMEOUT, env={**os.environ, **environment})
            print(json.dumps({"chain": chain, "resumeCheck": "cleared"}), flush=True)
        except (OSError, subprocess.SubprocessError):
            print(json.dumps({"chain": chain, "resumeCheck": "refused"}), flush=True)
            status = 1
    return status


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check-host", type=Path, help="inspect a completed private host manifest; no launch")
    mode.add_argument("--start", type=Path, help="AFTER the execution hold is lifted: inspect, start only this stack, then watch it as --watch does")
    mode.add_argument("--resume", type=Path, help="inspect, then run the resume check of every latched chain, which removes its halt latch only if its conditions hold")
    mode.add_argument("--watch", type=Path, help="every30s, stop the indexers and then PostgreSQL on a storage emergency; never restarts anything")
    parser.add_argument("--background", action="store_true", help="with --start or --watch: watch in a detached process that logs to watch.log in the private directory")
    args = parser.parse_args()
    if args.background and not (args.start or args.watch):
        parser.error("--background needs --start or --watch")
    try:
        check_artifacts()
        if args.watch:
            manifest = json.loads(private_file(args.watch).decode(), object_pairs_hook=unique)
            return run_watch(manifest, args.background)
        if args.check_host or args.start or args.resume:
            manifest = json.loads(private_file(args.check_host or args.start or args.resume).decode(), object_pairs_hook=unique)
            environment = check_host(manifest, resume=bool(args.resume))
            if args.resume:
                return resume(manifest, environment)
            if args.start:
                # Never reached by lint/check-host. No daemon operation precedes
                # the complete mount, identity, secret and policy checks, and
                # the stack never starts without a watcher: the lock is taken
                # first.
                check_watchable(manifest)
                lock = watch_lock(Path(manifest["private_dir"]))
                try:
                    start_stack(environment)
                except BaseException:
                    os.close(lock)
                    raise
                return run_watch(manifest, args.background, lock=lock)
            print(json.dumps({"hostCheck": "passed-at-this-instant", "composeEnvironment": environment, "servicesStarted": 0}))
        else:
            manifest = json.loads((HERE / "host.draft.json").read_text(), object_pairs_hook=unique)
            pending = {chain: check_policy(draft_config(chain), chain, draft=True) for chain in CHAINS}
            print(json.dumps({"mode": "offline-draft-lint", "draftArtifacts": "consistent", "unconfirmedHostFields": unresolved(manifest), "unconfirmedConfigFields": pending, "servicesStarted": 0}))
        return 0
    except (Refused, OSError, ValueError, KeyError, TypeError, plistlib.InvalidFileException):
        # Parser/URL/JSON exceptions can contain credential-bearing input.
        # Only explicitly safe Refused messages may reach the terminal.
        error = sys.exc_info()[1]
        print(str(error) if isinstance(error, Refused) else "Operating draft inspection refused malformed or unavailable input.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

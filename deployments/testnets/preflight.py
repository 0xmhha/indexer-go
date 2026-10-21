#!/usr/bin/env python3
"""Offline draft lint, read-only host checks, or explicit guarded startup after the hold is lifted."""

import argparse
import json
import os
from pathlib import Path
import plistlib
import re
import stat
import subprocess
import sys
from urllib.parse import parse_qs, unquote, urlsplit
import uuid

import yaml

HERE = Path(__file__).resolve().parent
REPO = HERE.parents[1]
CHAINS = {"stablenet-testnet": 8283, "bsc-testnet": 97, "sepolia": 11155111, "arc-testnet": 5042002}
MANIFEST_KEYS = {"mount_root", "apfs_volume_uuid", "physical_store_uuid", "private_dir", "indexer_image", "docker_context"}
STATE_DIR = "/var/lib/indexer-ops"
# Docker images, container layers and the services' logs live on the Docker
# VM disk, not on the monitored APFS volume. Initial review value.
DOCKER_VM_MIN_FREE = 10 << 30
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


def check_policy(cfg, draft=False):
    try:
        require(cfg["database"]["driver"] == "postgres" and cfg["database"]["postgres"]["max_conns"] == 4, "Require PostgreSQL and four connections per chain.")
        require(cfg["database"]["postgres"]["schema"] == "indexer_testnets_v1", "Unexpected per-chain schema base.")
        require(cfg["database"]["postgres"].get("dsn", "") == "", "Inline DSN is forbidden; use the secret file.")
        require(cfg["rpc"]["rate_limit"] == 5 and cfg["indexer"]["finality"] == "head", "Require shared RPC5 and the existing head/fresh policy.")
        require(cfg["node"]["role"] == "all" and cfg["multichain"]["enabled"] is True, "Require one multi-chain all-role process.")
        require(cfg["api"]["enabled"] is True and cfg["api"]["host"] == "0.0.0.0" and cfg["api"]["port"] == 8088, "App listener must match Compose publication/healthcheck.")
        chains = cfg["multichain"]["chains"]
        require(len(chains) == 4 and {c["id"]: c["chain_id"] for c in chains} == CHAINS and all(c["enabled"] is True for c in chains), "Require exactly the four configured testnets.")
        arc = next(c for c in chains if c["id"] == "arc-testnet")
        feed = arc["feed"]
        require(all("feed" not in c for c in chains if c is not arc), "Only Arc may select the feed.")
        require(feed["module"] == "arc.eth68" and feed["window"] == "arc-testnet", "Unexpected Arc feed module/window.")
        require(feed["collect"] is False and feed["initialize_volume"] is False and feed["create_window"] is False, "GC and initialization remain deferred.")
        require(not feed.get("state_proofs") and not feed.get("call_traces") and not feed.get("projections"), "Extra feed tasks are outside this draft.")
        require(feed["key_file"] == "/data/feed/indexer/identity/arc-peer.key", "Unexpected peer identity path.")
        for name in ("balance.native", "token.metadata", "dex.orderbook"):
            require(arc["features"][name]["enabled"] is False, "Arc execution-only state features must remain disabled.")
        for name in ("balance.native", "token.metadata"):
            require(cfg["features"][name]["enabled"] is True and cfg["features"][name]["allow_pruned_state"] is False, "Strict RPC state features must remain enabled.")
            for chain in chains:
                if chain is not arc:
                    effective = {**cfg["features"][name], **chain.get("features", {}).get(name, {})}
                    require(effective["enabled"] is True and effective["allow_pruned_state"] is False, "Per-chain overrides must preserve strict RPC state reads.")
        require(set(cfg["window_storage"]["volumes"]) == {"external"} and set(cfg["window_storage"]["windows"]) == {"arc-testnet"}, "Only the planned external volume and Arc raw window are allowed.")
        volume = cfg["window_storage"]["volumes"]["external"]
        window = cfg["window_storage"]["windows"]["arc-testnet"]
        require(window["volume"] == "external" and max(window.get("min_free_bytes", 0), volume["min_free_bytes"]) == 100 << 30, "Window volume/floor differs from the operating plan.")
        require(volume["root"] == "/data/feed" and volume["id"] == "indexer-apfs-testnets-v1" and volume["allow_directory"] is False, "Unexpected raw volume binding.")
        require((volume["max_bytes"], volume["min_free_bytes"], window["max_bytes"], window["retention"]) == (160 << 30, 100 << 30, 128 << 30, "24h"), "Raw capacity/retention policy changed.")
        require(window["path"] == "/data/feed/indexer/window/arc", "Unexpected raw window path.")
        require(cfg["operations"] == OPERATIONS, "Capacity halt latch/thresholds differ from the operating plan.")
        missing = unresolved(cfg)
        if not draft:
            require(not missing, "Runtime configuration has unresolved inputs: " + ", ".join(missing))
            require(type(feed["anchor_height"]) is int and arc["start_height"] == feed["anchor_height"] + 1, "Arc start must equal anchor + 1.")
        return missing
    except (KeyError, TypeError, StopIteration):
        raise Refused("Required operating configuration field is missing or malformed.") from None


def check_artifacts(compose=None, postgres_conf=None):
    """Cross-check public draft files only; no environment expansion or host probes."""
    if compose is None:
        compose = read_yaml(HERE / "compose.draft.yaml")
    if postgres_conf is None:
        postgres_conf = (REPO / "docs/examples/postgresql-testnets.draft.conf").read_text()
    try:
        require(compose["name"] == "indexer-testnets-draft" and set(compose["services"]) == {"postgres", "indexer"}, "Unexpected Compose project/services.")
        services = compose["services"]
        app, db = services["indexer"], services["postgres"]
        require(app["image"] == "${INDEXER_OPERATIONS_IMAGE:?reviewed operations image required}", "Compose must use the checked image reference.")
        require(not compose.get("volumes") and not db.get("ports"), "Named storage or a published DB port is outside the draft.")
        require(app["ports"] == ["127.0.0.1:8088:8088"] and app["read_only"] is True, "Unexpected API exposure or writable root filesystem.")
        cfg = read_yaml(REPO / "docs/examples/config-testnets-windowed.draft.yaml")
        check_policy(cfg, draft=True)
        require(cfg["api"]["host"] == "0.0.0.0" and cfg["api"]["port"] == 8088, "App listener differs from Compose publication/healthcheck.")
        require(app["environment"] == {"INDEXER_DB_POSTGRES_DSN_FILE": "/run/secrets/postgres_dsn", "INDEXER_DB_DRIVER": "postgres", "INDEXER_DB_POSTGRES_MAX_CONNS": str(cfg["database"]["postgres"]["max_conns"]), "INDEXER_RPC_RATE_LIMIT": str(cfg["rpc"]["rate_limit"])}, "Compose environment differs from app/secret policy.")
        prefix = "${INDEXER_PRIVATE_DIR:?preflight required}/"
        expected_secrets = {"postgres_admin_password": "postgres-admin-password", "indexer_password": "indexer-password", "postgres_dsn": "postgres-dsn"}
        require(compose["secrets"] == {key: {"file": prefix + name} for key, name in expected_secrets.items()}, "Secret file paths differ from host/entrypoint checks.")
        require(app["secrets"] == ["postgres_dsn"] and db["secrets"] == ["postgres_admin_password", "indexer_password"], "Unexpected secret consumer.")
        require(db["image"] == "postgres:16-bookworm" and db["environment"]["POSTGRES_DB"] == "indexer_testnets_v1" and db["environment"]["POSTGRES_PASSWORD_FILE"] == "/run/secrets/postgres_admin_password", "PostgreSQL image/database/password file drift.")
        raw = "${INDEXER_APFS_ROOT:?preflight required}"
        app_mounts = {v["target"]: v for v in app["volumes"]}
        db_mounts = {v["target"]: v for v in db["volumes"]}
        require(app_mounts["/data/feed"]["source"] == raw and app_mounts["/etc/indexer/config.yaml"]["source"] == prefix + "config.yaml" and app_mounts["/etc/indexer/config.yaml"]["read_only"] is True, "App data/config mount drift.")
        state = app_mounts[STATE_DIR]
        require(state["source"] == prefix + "state" and not state.get("read_only"), "Halt latch state mount drift.")
        require(app_mounts["/opt/indexer/entrypoint.sh"]["source"] == "./indexer-entrypoint.sh" and app_mounts["/opt/indexer/entrypoint.sh"]["read_only"] is True and app["entrypoint"] == ["/bin/sh", "/opt/indexer/entrypoint.sh"] and app["command"] == [], "Secret entrypoint wiring drift.")
        require(db_mounts["/var/lib/postgresql/data"]["source"] == raw + "/indexer/postgres/16" and db_mounts["/etc/postgresql/indexer.conf"]["source"] == "../../docs/examples/postgresql-testnets.draft.conf", "PGDATA/settings mount drift.")
        require(db["environment"]["PGDATA"] == "/var/lib/postgresql/data" and db_mounts["/etc/postgresql/indexer.conf"]["read_only"] is True and db_mounts["/docker-entrypoint-initdb.d/010-indexer.sql"]["source"] == "./init-indexer.sql" and db_mounts["/docker-entrypoint-initdb.d/010-indexer.sql"]["read_only"] is True, "PostgreSQL init/config wiring drift.")
        require((app.get("stop_signal"), app.get("stop_grace_period"), db.get("stop_signal"), db.get("stop_grace_period")) == ("SIGTERM", "120s", "SIGINT", "60s"), "Shutdown signal/grace period drift.")
        require(compose["networks"] == {"database": {"internal": True}, "rpc": {}} and db["networks"] == ["database"] and app["networks"] == ["database", "rpc"], "Unexpected DB/RPC network boundary.")
        for service in services.values():
            require(service["profiles"] == ["operations-draft"] and service["restart"] == "no" and not service.get("container_name") and not service.get("build"), "Unexpected automatic startup/build behavior.")
            require(service["user"] == "${INDEXER_RUN_UID:?preflight required}:${INDEXER_RUN_GID:?preflight required}", "Host UID/GID mapping drift.")
            require(service["logging"] == {"driver": "local", "options": {"max-size": "10m", "max-file": "3"}}, "Require per-service bounded Docker logs.")
            for mount in service["volumes"]:
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


def halt_latch(private):
    """The latch the indexer writes on a capacity or storage halt."""
    return private / "state" / "halt.json"


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
    state = private / "state"
    canonical_directory(state)
    s = state.stat()
    require(s.st_uid == os.getuid() and stat.S_IMODE(s.st_mode) == 0o700, "Private state directory must be owner-only mode0700.")
    latch = halt_latch(private)
    require(resume or not latch.exists(), "A halt latch is present: resolve its cause, then run --resume before --start.")
    require(not resume or latch.exists(), "No halt latch is present; nothing to resume.")
    cfg_path = private / "config.yaml"
    try:
        cfg = yaml.load(private_file(cfg_path, 1024 * 1024), Loader=StrictYAML)
    except yaml.YAMLError:
        raise Refused("Cannot parse private runtime configuration.") from None
    check_policy(cfg)
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
    return {"INDEXER_APFS_ROOT": str(root), "INDEXER_PRIVATE_DIR": str(private), "INDEXER_RUN_UID": str(os.getuid()), "INDEXER_RUN_GID": str(os.getgid()), "INDEXER_OPERATIONS_IMAGE": manifest["indexer_image"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check-host", type=Path, help="inspect a completed private host manifest; no launch")
    mode.add_argument("--start", type=Path, help="AFTER the execution hold is lifted: inspect, then start only this stack")
    mode.add_argument("--resume", type=Path, help="inspect, then run the indexer's resume check, which removes the halt latch only if its conditions hold")
    args = parser.parse_args()
    try:
        check_artifacts()
        if args.check_host or args.start or args.resume:
            manifest = json.loads(private_file(args.check_host or args.start or args.resume).decode(), object_pairs_hook=unique)
            environment = check_host(manifest, resume=bool(args.resume))
            if args.resume:
                # One container with the service's mounts and no database.
                command = ["docker", "--context", "desktop-linux", "compose", "--project-name", "indexer-testnets-draft", "--file", str(HERE / "compose.draft.yaml"), "--profile", "operations-draft", "run", "--rm", "--no-deps", "--pull", "never", "indexer", "--resume-check"]
                os.execvpe("docker", command, {**os.environ, **environment})
                return 0
            if args.start:
                # Never reached by lint/check-host. No daemon operation precedes
                # the complete mount, identity, secret and policy checks.
                command = ["docker", "--context", "desktop-linux", "compose", "--project-name", "indexer-testnets-draft", "--file", str(HERE / "compose.draft.yaml"), "--profile", "operations-draft", "up", "--detach", "--no-build", "--pull", "never"]
                os.execvpe("docker", command, {**os.environ, **environment})
                return 0
            print(json.dumps({"hostCheck": "passed-at-this-instant", "composeEnvironment": environment, "servicesStarted": 0}))
        else:
            cfg = read_yaml(REPO / "docs/examples/config-testnets-windowed.draft.yaml")
            manifest = json.loads((HERE / "host.draft.json").read_text(), object_pairs_hook=unique)
            print(json.dumps({"mode": "offline-draft-lint", "draftArtifacts": "consistent", "unconfirmedHostFields": unresolved(manifest), "unconfirmedConfigFields": check_policy(cfg, draft=True), "servicesStarted": 0}))
        return 0
    except (Refused, OSError, ValueError, KeyError, TypeError, plistlib.InvalidFileException):
        # Parser/URL/JSON exceptions can contain credential-bearing input.
        # Only explicitly safe Refused messages may reach the terminal.
        error = sys.exc_info()[1]
        print(str(error) if isinstance(error, Refused) else "Operating draft inspection refused malformed or unavailable input.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

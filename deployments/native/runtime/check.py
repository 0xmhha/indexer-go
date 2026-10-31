"""Offline runtime-draft contract checker; no service or declared-path access."""
import importlib.util
import os
import re
import stat
import sys
from pathlib import Path, PurePosixPath

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("native_diagnostic_declaration", HERE.parent / "check.py")
shared = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shared)

COMMON = {
    "pull_policy": "never", "profiles": ["native-runtime-draft"],
    "restart": "no", "init": True, "read_only": True,
    "cap_drop": ["ALL"], "security_opt": ["no-new-privileges:true"],
    "stop_signal": "SIGTERM", "stop_grace_period": "120s",
    "tmpfs": ["/tmp:mode=1777"],
}
PLAN = {
    "version": 2, "deployment_state": "HOLD", "postgres": "external_prepared_16",
    "arc_executor": "app_owned_capture_import_and_replay",
    "stablenet_executor": "owned_import_and_replay",
    "socket": "/ipc/execution.sock", "native": "/native/node", "spool": "/source/spool",
    "arc_scratch": "/source", "arc_run_lease": "/native/.node.execution-retention-lock",
    "arc_raw_volume": "/raw", "arc_raw_window": "/raw/arc-raw",
    "arc_maintenance_journal": "/native/.node.arc-execution-maintenance",
    "arc_ingress": {"module": "arc.native", "native_executable": "/app/arc-native-import",
                    "collector_executable": "/app/indexer-arc-executor", "peer_key_file": "/run/private/arc-peer-key",
                    "initialize_volume": False, "create_window": False, "window": "arc-raw", "max_raw_blocks": 172800},
    "maintenance": "existing_app_e3_explicit_library",
    "contracts": {
        "max_conns": 4, "part_transactions": 2, "api_reservation": 1,
        "logical_writers": 7, "physical_http_attempts_per_second": 5,
        "fresh": "new_uncached_canonical_post_response_generation_fence_commit",
        "evm_executions": 2,
    },
    "external_requirements": {k: "HOLD" for k in (
        "apfs_identity_and_capacity", "private_image_and_configs",
        "authenticated_full_checkpoint", "official_peers", "arc_app_ingress_acceptance",
        "prepared_postgres_schema", "host_uid_and_socket_mapping",
    )},
}


def same(actual, expected):
    """Reject YAML bool/int equivalence as well as schema/content drift."""
    if type(actual) is not type(expected):
        return False
    if isinstance(expected, dict):
        return set(actual) == set(expected) and all(same(actual[k], v) for k, v in expected.items())
    if isinstance(expected, list):
        return len(actual) == len(expected) and all(same(a, b) for a, b in zip(actual, expected))
    return actual == expected


def check_document(document):
    report = {"version": 1, "status": "HOLD", "declared_contract": "valid",
              "scope": "explicit_runtime_declaration_only", "operational_readiness": "not_authorized",
              "host_image_config_and_peer_checks": "not_performed", "live_freshness": "not_checked",
              "actuation": "none", "findings": []}

    def add(field, status, code):
        if status == "FAIL":
            report["status"], report["declared_contract"] = "FAIL", "invalid"
        if len(report["findings"]) < 128:
            report["findings"].append({"field": field, "status": status, "code": code})
        else:
            report["status"], report["declared_contract"] = "FAIL", "invalid"
            report["findings"][-1] = {"field": "report", "status": "FAIL", "code": "finding_limit"}

    def require(ok, field, code="unsafe_or_drifted_contract"):
        if not ok:
            add(field, "FAIL", code)
        return ok

    def unresolved(value, variable, field):
        if shared.unknown(value, variable):
            add(field, "HOLD", "operator_input_unresolved")
            return True
        return False

    if not require(isinstance(document, dict) and set(document) == {"name", "x-indexer-native-plan", "services", "networks", "secrets"}, "compose"):
        return report
    require(document["name"] == "indexer-native-runtime-draft", "project")
    require(same(document["x-indexer-native-plan"], PLAN), "plan", "runtime_boundaries_must_remain_explicit")
    for field in PLAN["external_requirements"]:
        add("external." + field, "HOLD", "arc_app_ingress_unverified" if field == "arc_app_ingress_acceptance" else "external_evidence_not_checked")

    services = document["services"]
    if not require(isinstance(services, dict) and set(services) == {"arc-app", "stablenet-app", "stablenet-executor"}, "services", "no_extra_node_or_stdio_sidecar"):
        return report
    known_paths, bindings, user_parts, app_images = [], {}, (set(), set()), set()

    def path(value, variable, field, binding):
        if unresolved(value, variable, field):
            return
        good = isinstance(value, str) and len(value) <= 4096 and re.fullmatch(r"/[A-Za-z0-9_./ -]+", value) is not None
        if good:
            p = PurePosixPath(value)
            good = str(p) == value and value != "/" and not value.startswith("//") and ".." not in p.parts
            good = good and not any(p == PurePosixPath(base) or PurePosixPath(base) in p.parents for base in (
                "/dev", "/proc", "/sys", "/run", "/var/run", "/etc", "/private/var/run", "/private/dev", "/private/etc"))
            repo = PurePosixPath(str(HERE.parents[2]))
            good = good and p != repo and repo not in p.parents
        if require(good, field, "unsafe_declared_path"):
            if binding in bindings:
                require(bindings[binding] == p, field, "shared_binding_mismatch")
            bindings[binding] = p
            known_paths.append((binding, p))

    for product in ("arc", "stablenet"):
        prefix = "INDEXER_" + product.upper() + "_RUNTIME_"
        for app in (True, False):
            if product == "arc" and not app:
                continue
            name = product + ("-app" if app else "-executor")
            service = services[name]
            network = {"networks": ["database", "rpc", "peers"] if product == "arc" else ["database", "rpc"]} if app else {"networks": ["peers"]}
            extra = {"environment", "secrets"} if app else {"depends_on"}
            keys = set(COMMON) | {"image", "user", "entrypoint", "command", "volumes"} | set(network) | extra
            if not require(isinstance(service, dict) and set(service) == keys, name):
                continue
            for key, expected in {**COMMON, **network}.items():
                require(same(service[key], expected), name + "." + key)
            image_var = "INDEXER_NATIVE_APP_IMAGE" if app else "INDEXER_STABLENET_EXECUTOR_IMAGE"
            image = service["image"]
            if not unresolved(image, image_var, name + ".image"):
                if require(isinstance(image, str) and len(image) <= 512 and shared.IMAGE.fullmatch(image) is not None and not image.endswith("0" * 64), name + ".image", "reviewed_digest_required") and app:
                    app_images.add(image)
            tokens = [r"(?:[0-9]+|HOLD|\$\{INDEXER_RUN_" + label + r":\?HOLD [A-Za-z ]+required\})" for label in ("UID", "GID")]
            user = service["user"]
            match = re.fullmatch("(" + tokens[0] + "):(" + tokens[1] + ")", user) if isinstance(user, str) else None
            if user in ("", "HOLD"):
                add(name + ".user", "HOLD", "operator_input_unresolved")
            elif require(match is not None, name + ".user", "nonroot_explicit_uid_gid_required"):
                parts = match.groups()
                for index, value in enumerate(parts):
                    if value.isdecimal():
                        if require(len(value) <= 10 and 0 < int(value) < (1 << 31), name + ".user", "nonroot_explicit_uid_gid_required"):
                            user_parts[index].add(int(value))
                if not all(p.isdecimal() for p in parts):
                    add(name + ".user", "HOLD", "operator_input_unresolved")
            if app:
                require(same(service["entrypoint"], ["/bin/sh", "/opt/indexer/entrypoint.sh"]) and same(service["command"], []), name + ".command")
                require(same(service["environment"], {"INDEXER_BINARY": "indexer-" + product, "INDEXER_DB_POSTGRES_DSN_FILE": "/run/secrets/postgres_dsn", "INDEXER_DB_DRIVER": "postgres", "INDEXER_DB_POSTGRES_MAX_CONNS": "4", "INDEXER_RPC_RATE_LIMIT": "5"}), name + ".environment", "dsn_file_and_existing_limits_required")
                require(same(service["secrets"], [{"source": product + "-postgres-dsn", "target": "postgres_dsn"}]), name + ".secrets")
                targets = [("/etc/indexer/config.yaml", "APP_CONFIG", True), ("/ipc", "IPC", False)]
                if product == "arc":
                    targets += [("/native", "NATIVE_PARENT", False), ("/source", "SOURCE_PARENT", False),
                                ("/raw", "RAW_VOLUME", False), ("/run/private/arc-peer-key", "PEER_KEY_FILE", True)]
                targets.append(("/opt/indexer/entrypoint.sh", None, True))
            else:
                require(same(service["entrypoint"], ["/app/stablenet-executor"]) and same(service["command"], ["--config", "/etc/indexer/native.json"]), name + ".command", "production_config_entrypoint_required")
                require(same(service["depends_on"], {product + "-app": {"condition": "service_started"}}), name + ".depends_on")
                targets = [("/etc/indexer/native.json", "NATIVE_CONFIG", True), ("/native", "NATIVE_PARENT", False),
                           ("/source", "SOURCE_PARENT", False), ("/ipc", "IPC", False), ("/etc/indexer/genesis.json", "GENESIS_FILE", True)]
            mounts = service["volumes"]
            if not require(isinstance(mounts, list) and len(mounts) == len(targets), name + ".mounts"):
                continue
            for mount, (target, variable, ro) in zip(mounts, targets):
                field = name + "." + (variable.lower() if variable else "entrypoint")
                if not require(isinstance(mount, dict) and set(mount) == {"type", "source", "target", "read_only", "bind"}, field):
                    continue
                require(mount["type"] == "bind" and mount["target"] == target and same(mount["read_only"], ro) and same(mount["bind"], {"create_host_path": False}), field, "production_mount_scope_required")
                if variable:
                    path(mount["source"], prefix + variable, field, (product, variable))
                else:
                    require(mount["source"] == "../../testnets/indexer-entrypoint.sh", field, "existing_dsn_wrapper_required")

    require(all(len(values) <= 1 for values in user_parts), "user", "same_uid_gid_for_authenticated_ipc")
    require(len(app_images) <= 1, "image", "app_image_must_match")
    networks = document["networks"]
    if require(isinstance(networks, dict) and set(networks) == {"database", "rpc", "peers"}, "networks"):
        require(same(networks["rpc"], {"driver": "bridge"}) and same(networks["peers"], {"driver": "bridge"}), "networks.egress")
        db = networks["database"]
        if require(isinstance(db, dict) and set(db) == {"external", "name"} and db["external"] is True, "networks.database", "prepared_external_database_required"):
            value = db["name"]
            if not unresolved(value, "INDEXER_NATIVE_DATABASE_NETWORK", "networks.database.name"):
                require(isinstance(value, str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", value) is not None, "networks.database.name")
    secrets = document["secrets"]
    if require(isinstance(secrets, dict) and set(secrets) == {"arc-postgres-dsn", "stablenet-postgres-dsn"}, "secrets"):
        for product in ("arc", "stablenet"):
            secret = secrets[product + "-postgres-dsn"]
            if require(isinstance(secret, dict) and set(secret) == {"file"}, product + ".dsn", "private_file_secret_required"):
                path(secret["file"], "INDEXER_" + product.upper() + "_RUNTIME_DSN_FILE", product + ".dsn", (product, "DSN_FILE"))
    for i, (binding, p) in enumerate(known_paths):
        for other_binding, other in known_paths[:i]:
            if binding == other_binding:
                continue
            if p == other or p in other.parents or other in p.parents:
                add("paths", "FAIL", "native_source_ipc_and_private_inputs_must_be_disjoint")
    return report


def main(args, out):
    try:
        if not args:
            path = HERE / "compose.draft.yaml"
        elif len(args) == 2 and args[0] == "--file" and len(args[1]) <= 4096:
            path = Path(args[1])
        else:
            raise shared.Invalid()
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
        with os.fdopen(fd, "rb") as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size > shared.MAX_BYTES:
                raise shared.Invalid()
            document = shared.decode(stream.read(shared.MAX_BYTES + 1))
        return shared.emit(check_document(document), out)
    except (shared.Invalid, OSError, ValueError, TypeError, RecursionError, KeyboardInterrupt):
        return shared.emit({"version": 1, "status": "FAIL", "declared_contract": "invalid",
                            "operational_readiness": "not_authorized", "actuation": "none",
                            "findings": [{"field": "input", "status": "FAIL", "code": "invalid_explicit_runtime_declaration"}]}, out)


if __name__ == "__main__":
    code = main(sys.argv[1:], sys.stdout)
    try:
        sys.stdout.close()
    except OSError:
        code = 1
    sys.exit(code)

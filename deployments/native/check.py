"""Bounded declaration checks only. No environment expansion or Docker access."""
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import sys

import yaml

HERE = Path(__file__).resolve().parent
MAX_BYTES = 1 << 20
PROJECT = "indexer-native-diagnostics-draft"
PROFILE = "native-diagnostics-draft"
FIXED = {
    "pull_policy": "never", "profiles": [PROFILE], "network_mode": "none",
    "read_only": True, "restart": "no", "cap_drop": ["ALL"],
    "security_opt": ["no-new-privileges:true"],
}
# A deliberately narrow digest-pinned reference grammar, without schemes,
# credentials or tags. Resolved values are never included in diagnostics.
IMAGE = re.compile(r"(?:[a-z0-9]+(?:[.-][a-z0-9]+)*(?::[1-9][0-9]{0,4})?/)?[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*@sha256:[0-9a-f]{64}")


class Invalid(Exception):
    """A fixed diagnostic is emitted instead of parser/OS error text."""


def decode(raw):
    if len(raw) > MAX_BYTES:
        raise Invalid()
    try:
        depth = count = 0
        for event in yaml.parse(raw):
            count += 1
            if count > 8192 or isinstance(event, yaml.AliasEvent):
                raise Invalid()
            if isinstance(event, (yaml.MappingStartEvent, yaml.SequenceStartEvent)):
                depth += 1
                if depth > 32:
                    raise Invalid()
            if isinstance(event, (yaml.MappingEndEvent, yaml.SequenceEndEvent)):
                depth -= 1
        class Strict(yaml.SafeLoader):
            pass
        def mapping(loader, node):
            result = {}
            for key, value in node.value:
                name = loader.construct_object(key)
                if not isinstance(name, str) or name in result or name == "<<":
                    raise Invalid()
                result[name] = loader.construct_object(value)
            return result
        Strict.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, mapping)
        value = yaml.load(raw, Loader=Strict)
        if not isinstance(value, dict):
            raise Invalid()
        return value
    except (yaml.YAMLError, UnicodeError, ValueError, TypeError, RecursionError) as e:
        raise Invalid() from e


def placeholder(value, variable):
    return isinstance(value, str) and re.fullmatch(r"\$\{" + re.escape(variable) + r":\?HOLD [A-Za-z ]+required\}", value) is not None


def unknown(value, variable):
    return value in ("", "HOLD") or placeholder(value, variable)


def check_document(document):
    report = {
        "version": 1, "status": "PASS", "scope": "docker_diagnostic_declarations_only",
        "operational_readiness": "not_authorized", "docker_execution": "not_performed",
        "host_and_live_state": "not_checked", "environment_expansion": "not_performed",
        "findings": [],
    }
    def add(field, status, code):
        if len(report["findings"]) >= 128:
            report["findings"][-1] = {"field": "report", "status": "FAIL", "code": "finding_limit"}
            report["status"] = "FAIL"
            return
        report["findings"].append({"field": field, "status": status, "code": code})
        if status == "FAIL" or status == "HOLD" and report["status"] == "PASS":
            report["status"] = status
    def require(ok, field, code="unsafe_or_drifted_contract"):
        if not ok:
            add(field, "FAIL", code)
        return ok
    if not require(isinstance(document, dict) and set(document) == {"name", "services"} and document.get("name") == PROJECT, "compose"):
        return report
    services = document["services"]
    names = {p + "-" + mode for p in ("arc", "stablenet") for mode in ("config", "metadata")}
    if not require(isinstance(services, dict) and set(services) == names, "services"):
        return report
    images, users, paths = [], [], []
    def path(value, variable, field):
        if unknown(value, variable):
            add(field, "HOLD", "operator_input_unresolved")
            return
        good = isinstance(value, str) and len(value) <= 4096 and re.fullmatch(r"/[A-Za-z0-9_./ -]+", value) is not None
        if good:
            p = PurePosixPath(value)
            good = str(p) == value and value != "/" and not value.startswith("//") and ".." not in p.parts
            good = good and not any(p == PurePosixPath(root) or PurePosixPath(root) in p.parents for root in ("/dev", "/proc", "/sys", "/run", "/var/run", "/etc", "/private/var/run", "/private/dev", "/private/etc"))
        if require(good, field, "unsafe_declared_path"):
            paths.append((field, PurePosixPath(value)))
    for product in ("arc", "stablenet"):
        prefix = "INDEXER_" + product.upper() + "_NATIVE_"
        for mode in ("config", "metadata"):
            name = product + "-" + mode
            s = services[name]
            keys = set(FIXED) | {"image", "user", "entrypoint", "command", "volumes"}
            if not require(isinstance(s, dict) and set(s) == keys, name):
                continue
            for key, expected in FIXED.items():
                require(type(s[key]) is type(expected) and s[key] == expected, name + "." + key)
            image = s["image"]
            if unknown(image, "INDEXER_NATIVE_DIAGNOSTICS_IMAGE"):
                add(name + ".image", "HOLD", "operator_input_unresolved")
            elif require(isinstance(image, str) and len(image) <= 512 and IMAGE.fullmatch(image) is not None and not image.endswith("0" * 64), name + ".image", "reviewed_digest_required"):
                images.append(image)
            user = s["user"]
            # Required-variable tokens contain ':' themselves: match the complete
            # UID:GID grammar rather than splitting or expanding environment data.
            tokens = []
            for label in ("UID", "GID"):
                tokens.append(r"(?:[0-9]+|HOLD|\$\{INDEXER_RUN_" + label + r":\?HOLD [A-Za-z ]+required\})")
            match = re.fullmatch("(" + tokens[0] + "):(" + tokens[1] + ")", user) if isinstance(user, str) else None
            if user in ("", "HOLD"):
                add(name + ".user", "HOLD", "operator_input_unresolved")
            elif require(match is not None, name + ".user", "nonroot_explicit_uid_gid_required"):
                values = match.groups()
                for value in values:
                    if value.isdecimal():
                        require(len(value) <= 10 and 0 < int(value) < (1 << 31), name + ".user", "nonroot_explicit_uid_gid_required")
                if all(v.isdecimal() for v in values):
                    users.append(values)
                else:
                    add(name + ".user", "HOLD", "operator_input_unresolved")
            binary = product + "-execution-" + ("preflight" if mode == "config" else "diagnose")
            require(s["entrypoint"] == ["/app/" + binary], name + ".entrypoint")
            if mode == "config":
                command = ["--app", "/inputs/app.yaml", "--native", "/inputs/native.json"]
                if product == "arc":
                    command += ["--retention", "/inputs/retention.json"]
                command += ["--checkpoint", "/inputs/checkpoint.json", "--declaration", "/inputs/declaration.json"]
                require(s["command"] == command, name + ".command")
                targets = [("/inputs", "INPUTS")]
            else:
                command = ["--native", None, "--spool", "/source", "--source-artifact", "/observations/source.json", "--checkpoint-artifact", "/observations/checkpoint.json", "--retention-artifact", "/observations/retention.json"]
                actual = s["command"]
                if require(isinstance(actual, list) and len(actual) == len(command) and actual[:1] == command[:1] and actual[2:] == command[2:], name + ".command"):
                    native = actual[1]
                    suffix = native[len("/native/"):] if isinstance(native, str) and native.startswith("/native/") else None
                    if unknown(suffix, prefix + "NAME"):
                        add(name + ".native_name", "HOLD", "operator_input_unresolved")
                    else:
                        require(isinstance(suffix, str) and len(suffix) <= 255 and suffix not in (".", "..") and re.fullmatch(r"[A-Za-z0-9_.-]+", suffix) is not None, name + ".native_name", "single_directory_name_required")
                targets = [("/source", "SPOOL"), ("/native", "PARENT"), ("/observations", "EXPORTS")]
            mounts = s["volumes"]
            if not require(isinstance(mounts, list) and len(mounts) == len(targets), name + ".mounts"):
                continue
            for mount, (target, variable) in zip(mounts, targets):
                field = name + "." + variable.lower()
                if not require(isinstance(mount, dict) and set(mount) == {"type", "source", "target", "read_only", "bind"}, field):
                    continue
                bind = mount["bind"]
                safe_bind = isinstance(bind, dict) and bind == {"create_host_path": False} and bind.get("create_host_path") is False
                require(mount["type"] == "bind" and mount["target"] == target and mount["read_only"] is True and safe_bind, field)
                path(mount["source"], prefix + variable, field)
    require(len(set(images)) <= 1, "image", "products_must_share_reviewed_image")
    require(len(set(users)) <= 1, "user", "one_explicit_host_identity_required")
    for i, (field, p) in enumerate(paths):
        for _, other in paths[:i]:
            if p == other or p in other.parents or other in p.parents:
                add(field, "FAIL", "selected_directories_must_be_disjoint")
    if report["status"] == "PASS":
        add("compose", "PASS", "declared_diagnostic_contract_valid")
    return report


def emit(report, out):
    try:
        data = json.dumps(report, separators=(",", ":")) + "\n"
        if out.write(data) != len(data):
            return 1
        if hasattr(out, "flush"):
            out.flush()
    except (OSError, ValueError):
        return 1
    return {"PASS": 0, "HOLD": 2, "FAIL": 1}[report["status"]]


def main(args, out):
    try:
        if not args:
            path = HERE / "compose.draft.yaml"
        elif len(args) == 2 and args[0] == "--file" and len(args[1]) <= 4096:
            path = Path(args[1])
        else:
            raise Invalid()
        # Explicit bounded regular input only. Mount source paths are declarations
        # and are never followed; the environment and Docker are never consulted.
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
        with os.fdopen(fd, "rb") as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size > MAX_BYTES:
                raise Invalid()
            document = decode(stream.read(MAX_BYTES + 1))
        return emit(check_document(document), out)
    except (Invalid, OSError, ValueError, RecursionError, KeyboardInterrupt):
        return emit({"version": 1, "status": "FAIL", "operational_readiness": "not_authorized", "findings": [{"field": "input", "status": "FAIL", "code": "invalid_explicit_compose"}]}, out)


if __name__ == "__main__":
    code = main(sys.argv[1:], sys.stdout)
    # Own the CLI stream through exit: a failed buffered pipe must not trigger
    # Python's later exit120/traceback after a successful status was returned.
    try:
        sys.stdout.close()
    except OSError:
        code = 1
    sys.exit(code)

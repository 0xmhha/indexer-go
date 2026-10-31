"""Declaration/fixture checks only: no Docker, node, DB, disk probe or RPC."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import yaml

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
spec = importlib.util.spec_from_file_location("native_docker_check", HERE / "check.py")
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class DiagnosticDockerTests(unittest.TestCase):
    def draft(self):
        return yaml.safe_load((HERE / "compose.draft.yaml").read_text())

    def test_managed_arc_diagnostics_mounts_raw_readonly_without_credentials(self):
        service = self.draft()["services"]["arc-metadata"]
        command = service["command"]
        self.assertIn("--raw", command, "managed raw must be an explicit read-only selection")
        self.assertIn("--ingress-artifact", command)
        mounts = {m["target"]: m for m in service["volumes"]}
        self.assertIn("/raw", mounts)
        self.assertIs(True, mounts["/raw"]["read_only"])
        self.assertEqual("none", service["network_mode"])
        self.assertNotIn("environment", service)
        self.assertNotIn("secrets", service)

    def resolved(self):
        d = self.draft()
        for name, service in d["services"].items():
            product = "arc" if name.startswith("arc-") else "stablenet"
            service["image"] = "example.invalid/indexer-diagnostics@sha256:" + "1" * 64
            service["user"] = "501:20"
            for volume in service["volumes"]:
                role = {"/inputs": "inputs", "/source": "spool", "/native": "native-parent", "/observations": "exports", "/raw": "raw-volume"}[volume["target"]]
                volume["source"] = "/synthetic/" + product + "/" + role
            if name.endswith("metadata"):
                service["command"][1] = "/native/custom-db"
                if product == "arc":
                    service["command"][11] = "/raw/arc-raw"
        return d

    @unittest.skipUnless(os.environ.get("INDEXER_NATIVE_DIAGNOSTICS_TEST_BIN_DIR"), "owned host-binary fixture only; run test-cli.sh")
    def test_real_managed_app_preflight_is_hold_and_opens_no_declared_store(self):
        binary = Path(os.environ["INDEXER_NATIVE_DIAGNOSTICS_TEST_BIN_DIR"]) / "arc-execution-preflight"
        original = ROOT / "configs/execution/preflight/synthetic-managed-arc"
        with tempfile.TemporaryDirectory(prefix="managed-file-preflight-") as temp:
            temp = Path(temp).resolve()
            for source in original.iterdir():
                if source.is_file():
                    target = temp / source.name
                    target.write_bytes(source.read_bytes())
                    target.chmod(0o600)
            before = {p.name: (p.stat().st_ino, p.read_bytes()) for p in temp.iterdir()}
            args = []
            for flag, name in (("--app", "app.yaml"), ("--native", "native.json"), ("--checkpoint", "checkpoint.json"), ("--retention", "retention.json"), ("--declaration", "declaration.json")):
                args.extend((flag, str(temp / name)))
            result = subprocess.run([str(binary), *args], capture_output=True, timeout=30, check=False)
            report = json.loads(result.stdout)
            self.assertEqual(2, result.returncode, report)
            self.assertEqual("not_authorized", report["operational_readiness"])
            self.assertEqual("not_checked", report["live_freshness"])
            self.assertFalse(result.stderr)
            self.assertNotIn(str(temp).encode(), result.stdout)
            self.assertEqual(before, {p.name: (p.stat().st_ino, p.read_bytes()) for p in temp.iterdir()})

    def test_unknown_inputs_never_become_ready(self):
        result = check.check_document({"name": "indexer-native-diagnostics-draft", "services": {}})
        self.assertNotEqual(result["status"], "PASS", "missing Docker diagnostic inputs must not be ready")

    def test_checked_in_draft_is_hold_without_environment_or_host_access(self):
        with mock.patch.dict(os.environ, {"INDEXER_NATIVE_DIAGNOSTICS_IMAGE": "secret", "DOCKER_HOST": "tcp://private.invalid"}), mock.patch("subprocess.run", side_effect=AssertionError("Docker forbidden")):
            result = check.check_document(self.draft())
        self.assertEqual(result["status"], "HOLD")
        self.assertEqual(result["operational_readiness"], "not_authorized")
        self.assertEqual(result["host_and_live_state"], "not_checked")
        self.assertNotIn("secret", json.dumps(result))

    def test_resolved_declarations_are_only_a_static_contract(self):
        result = check.check_document(self.resolved())
        self.assertEqual(result["status"], "PASS")
        self.assertEqual(result["docker_execution"], "not_performed")
        self.assertEqual(result["operational_readiness"], "not_authorized")
        self.assertNotIn("/synthetic", json.dumps(result))

    def test_network_secret_startup_or_write_paths_fail_despite_unknowns(self):
        mutations = [
            lambda s: s.update(network_mode="bridge"),
            lambda s: s.update(environment={"INDEXER_DB_POSTGRES_DSN": "postgres://user:password@private/db"}),
            lambda s: s.update(secrets=["dsn"]),
            lambda s: s.update(depends_on=["postgres"]),
            lambda s: s.update(ports=["8080:8080"]),
            lambda s: s.update(build="."),
            lambda s: s.update(restart="always"),
            lambda s: s.update(read_only=False),
            lambda s: s.update(privileged=True),
            lambda s: s.update(pull_policy="always"),
            lambda s: s.update(entrypoint=["/app/indexer-arc"]),
            lambda s: s.update(command=["apply"]),
            lambda s: s["volumes"][0].update(read_only=False),
            lambda s: s["volumes"][0]["bind"].update(create_host_path=True),
            lambda s: s["volumes"][0].update(type="volume"),
            lambda s: s["volumes"][0].update(source="/var/run/docker.sock"),
            lambda s: s["volumes"].append({"type": "bind", "source": "/", "target": "/host"}),
        ]
        for mutate in mutations:
            with self.subTest(mutate=mutate):
                d = self.draft()
                mutate(d["services"]["arc-metadata"])
                result = check.check_document(d)
                self.assertEqual(result["status"], "FAIL")
                self.assertNotIn("password", json.dumps(result))
                self.assertNotIn("private.invalid", json.dumps(result))

    def test_profiles_products_uid_digest_and_storage_are_not_interchangeable(self):
        for mutate in [
            lambda d: d["services"]["arc-config"].update(profiles=[]),
            lambda d: d["services"]["arc-config"].update(image="example.invalid/diagnostics:latest"),
            lambda d: d["services"]["arc-config"].update(user="0:0"),
            lambda d: d["services"]["arc-config"].update(user="502:20"),
            lambda d: d["services"]["arc-config"].update(entrypoint=["/app/stablenet-execution-preflight"]),
            lambda d: d["services"]["arc-metadata"]["command"].__setitem__(1, "/native/../db"),
            lambda d: d["services"]["arc-metadata"]["volumes"][0].update(source="/synthetic/arc/native-parent/inside"),
            lambda d: d.update(volumes={"shared": {}}),
            lambda d: d["services"].update(postgres={"image": "postgres:16"}),
        ]:
            d = self.resolved()
            mutate(d)
            self.assertEqual(check.check_document(d)["status"], "FAIL")

    def test_credentialed_or_malformed_image_is_not_a_valid_digest_pin(self):
        for image in ("https://user:private-password@sha256:" + "1" * 64, "repo//name@sha256:" + "1" * 64, "repo/@sha256:" + "1" * 64):
            d = self.draft()
            d["services"]["arc-config"]["image"] = image
            result = check.check_document(d)
            self.assertEqual(result["status"], "FAIL")
            self.assertNotIn("private-password", json.dumps(result))

    def test_closed_cli_output_pipe_is_failure_without_traceback(self):
        reader, writer = os.pipe()
        os.close(reader)
        try:
            process = subprocess.Popen([sys.executable, "-B", str(HERE / "check.py")], stdout=writer, stderr=subprocess.PIPE)
        finally:
            os.close(writer)
        _, error = process.communicate(timeout=10)
        self.assertEqual(process.returncode, 1)
        self.assertEqual(error, b"")

    def test_read_only_native_parent_keeps_sibling_fence_visible(self):
        for name in ("arc-metadata", "stablenet-metadata"):
            s = self.resolved()["services"][name]
            parent = next(v for v in s["volumes"] if v["target"] == "/native")
            self.assertTrue(parent["read_only"])
            self.assertEqual(s["command"][:2], ["--native", "/native/custom-db"])
            self.assertFalse(parent["bind"]["create_host_path"])

    def test_known_system_mount_aliases_cannot_be_selected_as_data(self):
        for value in ("/private/var/run/docker.sock", "//var/run/docker.sock", "/private/dev/unsafe"):
            d = self.resolved()
            d["services"]["arc-metadata"]["volumes"][0]["source"] = value
            self.assertEqual(check.check_document(d)["status"], "FAIL")

    def test_syntax_bounds_duplicates_aliases_and_output_failure(self):
        for raw in ("services: {}\nservices: {}", "null", "[]", "a: &x 1\nb: *x", "x: " + "[" * 34 + "1" + "]" * 34, " " * ((1 << 20) + 1)):
            with self.subTest(raw=raw[:25]):
                with self.assertRaises(check.Invalid):
                    check.decode(raw.encode())
        class Broken:
            def write(self, _):
                raise OSError("private password")
        self.assertEqual(check.emit(check.check_document(self.draft()), Broken()), 1)

    def test_actual_cli_exit_redaction_no_input_rewrite_and_no_docker(self):
        with tempfile.TemporaryDirectory(prefix="native-docker-test-") as temp:
            p = Path(temp) / "private-compose.yaml"
            p.write_text(yaml.safe_dump(self.resolved()))
            before = p.read_bytes()
            result = subprocess.run([sys.executable, "-B", str(HERE / "check.py"), "--file", str(p)], capture_output=True, check=False)
            self.assertEqual(result.returncode, 0, result.stdout)
            report = json.loads(result.stdout)
            self.assertEqual(report["status"], "PASS")
            self.assertEqual(p.read_bytes(), before)
            self.assertNotIn(temp.encode(), result.stdout + result.stderr)
        result = subprocess.run([sys.executable, "-B", str(HERE / "check.py")], capture_output=True, check=False)
        self.assertEqual(result.returncode, 2)
        self.assertEqual(json.loads(result.stdout)["status"], "HOLD")
        result = subprocess.run([sys.executable, "-B", str(HERE / "check.py"), "--start"], capture_output=True, check=False)
        self.assertEqual(result.returncode, 1)
        self.assertNotIn(b"Traceback", result.stderr)

    def test_image_build_targets_and_local_module_inputs(self):
        recipe = (HERE / "Dockerfile.draft").read_text()
        for product in ("arc", "stablenet"):
            for cmd in ("execution-preflight", "execution-diagnose"):
                self.assertIn("./x/" + product + "/cmd/" + cmd, recipe)
        self.assertIn("FROM debian:bookworm-slim", recipe)
        self.assertIn("CGO_ENABLED=1", recipe)
        self.assertNotIn("apt-get", recipe)
        self.assertNotIn("/cmd/indexer", recipe)
        self.assertNotIn("/cmd/execution-collector", recipe)
        for path in (HERE / "Dockerfile.draft", ROOT / "deployments/testnets/Dockerfile.draft"):
            content = path.read_text()
            self.assertIn("COPY execwire/", content, "local replace module is missing from the build stage")
            self.assertLess(content.index("COPY execwire/"), content.index("RUN go mod download"), "local replace module must exist before module resolution")
        ignore = (HERE / "Dockerfile.draft.dockerignore").read_text().splitlines()
        self.assertIn("**", ignore)
        self.assertIn("!execwire/**", ignore)
        self.assertIn("**/target", ignore)
        self.assertNotIn("!config.yaml", ignore)
        self.assertNotIn("!deployments/**", ignore)

    @unittest.skipUnless(os.environ.get("INDEXER_NATIVE_DIAGNOSTICS_TEST_BIN_DIR"), "owned host-binary fixture only; run test-cli.sh")
    def test_real_product_commands_use_composed_readonly_bindings(self):
        binaries = Path(os.environ["INDEXER_NATIVE_DIAGNOSTICS_TEST_BIN_DIR"])
        original = ROOT / "configs/execution/preflight/synthetic-arc"
        with tempfile.TemporaryDirectory(prefix="native-docker-cli-") as temp:
            temp = Path(temp).resolve()
            doc = self.resolved()
            for product in ("arc", "stablenet"):
                bindings = {}
                for name in (product + "-config", product + "-metadata"):
                    for mount in doc["services"][name]["volumes"]:
                        path = temp / product / mount["target"].lstrip("/")
                        path.mkdir(parents=True, exist_ok=True, mode=0o700)
                        bindings[mount["target"]] = path
                files = {p.name: p.read_bytes() for p in original.iterdir() if p.is_file()}
                if product == "stablenet":
                    declaration = json.loads(files["declaration.json"])
                    declaration["profile"] = "stablenet-native"
                    declaration["chain"] = {"ID": "8283", "Genesis": "0x" + "3" * 64}
                    declaration["network_id"] = 8283
                    checkpoint = json.loads(files["checkpoint.json"])
                    checkpoint["Chain"] = declaration["chain"]
                    native = {"start_mode": "checkpoint", "data_dir": "/synthetic/native", "spool_dir": "/synthetic/spool", "socket": "/synthetic/ipc/execution.sock", "genesis_file": "/synthetic/genesis.json", "chain_id": "8283", "network_id": 8283, "genesis_hash": declaration["chain"]["Genesis"], "anchor_height": 100, "anchor_hash": declaration["anchor"]["Hash"], "anchor_root": declaration["root"], "peers": [declaration["enode"]], "spool_bytes": 4 << 30, "spool_records": 262144}
                    for key, value in (("native.json", native), ("declaration.json", declaration), ("checkpoint.json", checkpoint)):
                        files[key] = json.dumps(value).encode()
                for name, data in files.items():
                    path = bindings["/inputs"] / name
                    path.write_bytes(data)
                    path.chmod(0o600)
                native = bindings["/native"] / "custom-db"
                native.mkdir(mode=0o700)
                fence = bindings["/native"] / ".custom-db.execution-retention" if product == "arc" else native / ".execution-retention"
                fence.write_bytes(b'{"private":"not-a-valid-fence-secret"}')
                fence.chmod(0o600)
                before = {str(p.relative_to(temp)): (p.stat().st_ino, p.read_bytes() if p.is_file() else None) for p in temp.rglob("*")}
                for mode, expected in (("config", 0), ("metadata", 1)):
                    service = doc["services"][product + "-" + mode]
                    args = []
                    for arg in service["command"]:
                        for target, path in bindings.items():
                            if arg == target or arg.startswith(target + "/"):
                                arg = str(path) + arg[len(target):]
                                break
                        args.append(arg)
                    binary = binaries / Path(service["entrypoint"][0]).name
                    result = subprocess.run([str(binary), *args], capture_output=True, timeout=30, check=False)
                    self.assertEqual(result.returncode, expected, result.stdout)
                    report = json.loads(result.stdout)
                    self.assertFalse(result.stderr)
                    self.assertNotIn(str(temp).encode(), result.stdout)
                    self.assertNotIn(b"not-a-valid-fence-secret", result.stdout)
                    if mode == "metadata":
                        self.assertEqual(report["profile"], product + "-native")
                        self.assertTrue(any(f["field"] == "native.maintenance" and f["code"] == "unsafe_metadata_input" for f in report["findings"]))
                after = {str(p.relative_to(temp)): (p.stat().st_ino, p.read_bytes() if p.is_file() else None) for p in temp.rglob("*")}
                self.assertEqual(before, after, "diagnostic bindings may not create locks, manifests, ACKs or rewrite selected inputs")


if __name__ == "__main__":
    unittest.main()

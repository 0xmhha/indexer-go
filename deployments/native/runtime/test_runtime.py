"""Offline contract and hostile-file tests; no Docker, DB, peer or RPC."""
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
spec = importlib.util.spec_from_file_location("native_runtime_check", HERE / "check.py")
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class RuntimeDraftTests(unittest.TestCase):
    def draft(self):
        return check.shared.decode((HERE / "compose.draft.yaml").read_bytes())

    def resolved(self):
        d = self.draft()
        d["networks"]["database"]["name"] = "synthetic-db"
        for product in ("arc", "stablenet"):
            for name, service in d["services"].items():
                if not name.startswith(product + "-"):
                    continue
                service["image"] = "example.invalid/native@sha256:" + "1" * 64
                service["user"] = "501:20"
                for mount in service["volumes"]:
                    target = mount["target"]
                    if target == "/opt/indexer/entrypoint.sh":
                        continue
                    if target in ("/native", "/source", "/raw"):
                        mount["source"] = "/synthetic/data/" + product + target
                    elif target == "/ipc":
                        mount["source"] = "/synthetic/private/" + product + "-ipc"
                    else:
                        mount["source"] = "/synthetic/private/" + product + "-" + Path(target).name
            d["secrets"][product + "-postgres-dsn"]["file"] = "/synthetic/private/" + product + ".dsn"
        return d

    def assert_fails(self, document):
        report = check.check_document(document)
        self.assertEqual("FAIL", report["status"], report)
        self.assertEqual("not_authorized", report["operational_readiness"])
        self.assertLessEqual(len(report["findings"]), 128)
        self.assertLess(len(json.dumps(report)), 65536)

    def test_arc_app_owns_ingress_without_external_collector(self):
        d = self.draft()
        self.assertEqual({"arc-app", "stablenet-app", "stablenet-executor"}, set(d["services"]),
                         "App-owned ingress must not compete with an external Arc collector")
        self.assertEqual(["database", "rpc", "peers"], d["services"]["arc-app"]["networks"])
        self.assertEqual("app_owned_capture_import_and_replay", d["x-indexer-native-plan"]["arc_executor"])

    def test_arc_app_mounts_support_raw_and_cow_without_pinning_node(self):
        d = self.draft()
        mounts = {m["target"]: m for m in d["services"]["arc-app"]["volumes"]}
        self.assertTrue({"/native", "/source", "/raw", "/run/private/arc-peer-key"} <= set(mounts),
                        "App needs native/source/raw ownership and a private peer key")
        self.assertNotIn("/native/node", mounts, "nested DB bind would pin the old COW inode")
        for target in ("/native", "/source", "/raw"):
            self.assertIs(False, mounts[target]["read_only"])
        self.assertIs(True, mounts["/run/private/arc-peer-key"]["read_only"])
        self.assertEqual("/source", d["x-indexer-native-plan"]["arc_scratch"])

    def test_arc_paths_bind_raw_and_phase_journal_outside_cow(self):
        plan = self.draft()["x-indexer-native-plan"]
        self.assertEqual("/raw/arc-raw", plan.get("arc_raw_window"), "raw is a separate durable queue")
        self.assertEqual("/native/.node.arc-execution-maintenance", plan.get("arc_maintenance_journal"))
        self.assertEqual("existing_app_e3_explicit_library", plan["maintenance"])

    def test_limit_drift_fails_while_other_inputs_hold(self):
        for field, value in (("INDEXER_DB_POSTGRES_MAX_CONNS", "7"), ("INDEXER_RPC_RATE_LIMIT", "6")):
            with self.subTest(field=field):
                d = self.draft()
                d["services"]["arc-app"]["environment"][field] = value
                self.assert_fails(d)
        for field, value in (("part_transactions", 4), ("api_reservation", 0), ("logical_writers", 8), ("fresh", "cached")):
            with self.subTest(field=field):
                d = self.draft()
                d["x-indexer-native-plan"]["contracts"][field] = value
                self.assert_fails(d)

    def test_draft_and_resolved_bindings_never_authorize_operation(self):
        for d in (self.draft(), self.resolved()):
            before = copy.deepcopy(d)
            r = check.check_document(d)
            self.assertEqual("HOLD", r["status"])
            self.assertEqual("valid", r["declared_contract"])
            self.assertEqual("not_authorized", r["operational_readiness"])
            self.assertEqual("none", r["actuation"])
            self.assertTrue(any(x["code"] == "arc_app_ingress_unverified" for x in r["findings"]))
            self.assertEqual(before, d)

    def test_mounts_keep_app_raw_native_and_source_access_scoped(self):
        for name, targets in (("arc-app", ("/source", "/native", "/raw", "/ipc", "/run/private/arc-peer-key")),
                              ("stablenet-executor", ("/source", "/native", "/ipc"))):
            for target in targets:
                d = self.draft()
                m = next(x for x in d["services"][name]["volumes"] if x["target"] == target)
                m["read_only"] = not m["read_only"]
                with self.subTest(name=name, target=target): self.assert_fails(d)
        for target in ("/native/node", "/source/spool", "/scratch"):
            d = self.draft()
            d["services"]["arc-app"]["volumes"].append({"type": "bind", "source": "/synthetic/data", "target": target})
            self.assert_fails(d)
        d = self.draft()
        next(x for x in d["services"]["arc-app"]["volumes"] if x["target"] == "/source")["target"] = "/source/spool"
        self.assert_fails(d)

    def test_no_new_node_or_stdio_sidecar_or_automatic_recovery(self):
        for change in ("sidecar", "node", "restart", "command", "import", "authorization"):
            d = self.draft()
            if change == "sidecar": d["services"]["arc-executor"] = copy.deepcopy(d["services"]["arc-app"])
            elif change == "node": d["services"]["arc-node"] = {}
            elif change == "restart": d["services"]["arc-app"]["restart"] = "always"
            elif change == "command": d["services"]["arc-app"]["command"] += ["--generation", "99"]
            elif change == "import": d["x-indexer-native-plan"]["arc_executor"] = "imports_from_enode"
            else: d["x-indexer-native-plan"]["deployment_state"] = "READY"
            with self.subTest(change=change): self.assert_fails(d)

    def test_secrets_are_file_scoped_and_never_sent_to_producers(self):
        for change in ("direct", "producer", "args", "file", "extra"):
            d = self.draft()
            if change == "direct": d["services"]["arc-app"]["environment"]["INDEXER_DB_POSTGRES_DSN"] = "secret-not-to-emit"
            elif change == "producer": d["services"]["stablenet-executor"]["secrets"] = ["arc-postgres-dsn"]
            elif change == "args": d["services"]["arc-app"]["command"] = ["--dsn", "secret-not-to-emit"]
            elif change == "file": d["secrets"]["arc-postgres-dsn"] = {"environment": "DSN"}
            else: d["services"]["arc-app"]["env_file"] = "/private/secret"
            with self.subTest(change=change):
                self.assert_fails(d)
                self.assertNotIn("secret-not-to-emit", json.dumps(check.check_document(d)))

    def test_path_overlap_and_ipc_identity_drift(self):
        for target in ("/source", "/native", "/ipc", "/raw"):
            d = self.resolved()
            parent = "source" if target == "/native" else "native"
            next(x for x in d["services"]["arc-app"]["volumes"] if x["target"] == target)["source"] = "/synthetic/data/arc/" + parent + "/nested"
            self.assert_fails(d)
        d = self.resolved();d["services"]["arc-app"]["user"] = "502:20";self.assert_fails(d)
        d = self.resolved();d["secrets"]["arc-postgres-dsn"]["file"] = "/synthetic/data/arc/source/secret";self.assert_fails(d)
        d = self.resolved()
        next(x for x in d["services"]["arc-app"]["volumes"] if x["target"] == "/run/private/arc-peer-key")["source"] = "/synthetic/data/arc/raw/peer-key"
        self.assert_fails(d)
        d = self.resolved();d["services"]["arc-app"]["user"] = "0:20";self.assert_fails(d)
        for path in ("relative", "/tmp/../secret", "/dev/disk1", "/proc/self", "/", "/double//slash"):
            d = self.resolved();d["secrets"]["arc-postgres-dsn"]["file"] = path
            with self.subTest(path=path): self.assert_fails(d)

    def test_known_gid_conflict_is_not_masked_by_unknown_uid(self):
        d = self.draft()
        d["services"]["arc-app"]["user"] = "${INDEXER_RUN_UID:?HOLD host UID required}:20"
        d["services"]["stablenet-executor"]["user"] = "501:21"
        self.assert_fails(d)

    def test_managed_contract_drift_and_legacy_collector_rollback_fail(self):
        for change in ("legacy", "init", "window", "journal", "scratch", "rw_native", "peer_network"):
            d = self.draft()
            if change == "legacy": d["services"]["arc-collector"] = copy.deepcopy(d["services"]["arc-app"])
            elif change == "init": d["x-indexer-native-plan"]["arc_ingress"]["initialize_volume"] = True
            elif change == "window": d["x-indexer-native-plan"]["arc_raw_window"] = "/native/node/raw"
            elif change == "journal": d["x-indexer-native-plan"]["arc_maintenance_journal"] = "/native/node/phase"
            elif change == "scratch": d["x-indexer-native-plan"]["arc_scratch"] = "/scratch"
            elif change == "rw_native": next(x for x in d["services"]["arc-app"]["volumes"] if x["target"] == "/native")["read_only"] = True
            else: d["services"]["arc-app"]["networks"] = ["database", "rpc"]
            with self.subTest(change=change): self.assert_fails(d)

    @unittest.skipUnless(os.environ.get("INDEXER_NATIVE_DIAGNOSTICS_TEST_BIN_DIR"), "owned host binaries only; run deployments/native/test-cli.sh")
    def test_legacy_product_file_preflight_keeps_its_separate_scope(self):
        binaries = Path(os.environ["INDEXER_NATIVE_DIAGNOSTICS_TEST_BIN_DIR"])
        original = ROOT / "configs/execution/preflight/synthetic-arc"
        for product in ("arc", "stablenet"):
            with self.subTest(product=product), tempfile.TemporaryDirectory(prefix="runtime-file-preflight-") as temp:
                temp = Path(temp).resolve()
                files = {p.name: p.read_bytes() for p in original.iterdir() if p.is_file()}
                declaration = json.loads(files["declaration.json"])
                declaration["checkpoint_path"] = "/checkpoints/" + product
                declaration["pg_data"] = "/postgres/data"
                app = check.shared.yaml.safe_load(files["app.yaml"])
                app["source"]["execution"]["socket"] = "/ipc/execution.sock"
                app["source"]["execution"]["anchor_hash"] = declaration["anchor"]["Hash"]
                app["source"]["execution"]["anchor_root"] = declaration["root"]
                if product == "arc":
                    native = json.loads(files["native.json"])
                    native["Native"].update(Executable="/app/arc-executor", DataDir="/native/node", ScratchDir="/scratch")
                    native.update(SpoolDir="/source/spool", Socket="/ipc/execution.sock")
                    retention = json.loads(files["retention.json"])
                    retention.update(NativeExecutable="/app/arc-native-maintenance", NativeDataDir="/native/node", SpoolDir="/source/spool")
                    files["retention.json"] = json.dumps(retention).encode()
                else:
                    declaration["profile"] = "stablenet-native"
                    declaration["chain"] = {"ID": "8283", "Genesis": "0x" + "3" * 64}
                    declaration["network_id"] = 8283
                    checkpoint = json.loads(files["checkpoint.json"])
                    checkpoint["Chain"] = declaration["chain"]
                    files["checkpoint.json"] = json.dumps(checkpoint).encode()
                    native = {"start_mode": "checkpoint", "data_dir": "/native/node", "spool_dir": "/source/spool", "socket": "/ipc/execution.sock", "genesis_file": "/etc/indexer/genesis.json", "chain_id": "8283", "network_id": 8283, "genesis_hash": declaration["chain"]["Genesis"], "anchor_height": 100, "anchor_hash": declaration["anchor"]["Hash"], "anchor_root": declaration["root"], "peers": [declaration["enode"]], "spool_bytes": 4 << 30, "spool_records": 262144}
                files["app.yaml"] = check.shared.yaml.safe_dump(app).encode()
                files["native.json"] = json.dumps(native).encode()
                files["declaration.json"] = json.dumps(declaration).encode()
                for name, data in files.items():
                    p = temp / name;p.write_bytes(data);p.chmod(0o600)
                before = {p.name: (p.stat().st_ino, p.read_bytes()) for p in temp.iterdir()}
                args = [str(binaries / (product + "-execution-preflight")), "--app", str(temp / "app.yaml"), "--native", str(temp / "native.json"), "--checkpoint", str(temp / "checkpoint.json"), "--declaration", str(temp / "declaration.json")]
                if product == "arc": args += ["--retention", str(temp / "retention.json")]
                result = subprocess.run(args, capture_output=True, timeout=30, check=False)
                report = json.loads(result.stdout)
                self.assertEqual(0, result.returncode, report)
                self.assertEqual("not_authorized", report["operational_readiness"])
                self.assertEqual("not_checked", report["live_freshness"])
                self.assertFalse(result.stderr)
                self.assertNotIn(str(temp).encode(), result.stdout)
                after = {p.name: (p.stat().st_ino, p.read_bytes()) for p in temp.iterdir()}
                self.assertEqual(before, after, "file preflight must not create locks or open declared runtime stores")

    def test_closed_schema_network_images_and_boolean_types(self):
        for change in ("image", "zero", "port", "privileged", "bool", "network", "unknown", "missing"):
            d = self.resolved()
            if change == "image": d["services"]["arc-app"]["image"] = "image:latest"
            elif change == "zero": d["services"]["arc-app"]["image"] = "example.invalid/native@sha256:" + "0" * 64
            elif change == "port": d["services"]["arc-app"]["ports"] = ["0.0.0.0:8080:8080"]
            elif change == "privileged": d["services"]["stablenet-executor"]["privileged"] = True
            elif change == "bool": d["services"]["arc-app"]["read_only"] = 1
            elif change == "network": d["services"]["arc-app"]["networks"] = ["database"]
            elif change == "unknown": d["x-indexer-native-plan"]["contracts"]["ignored"] = 1
            else: d["services"]["arc-app"].pop("user")
            with self.subTest(change=change): self.assert_fails(d)

    def run_file(self, path):
        out = io.StringIO();code = check.main(["--file", str(path)], out)
        return code, json.loads(out.getvalue())

    def test_cli_only_reads_selected_file_not_declared_bindings_or_environment(self):
        with tempfile.TemporaryDirectory() as temp:
            p = Path(temp) / "compose.yaml"
            p.write_text(check.shared.yaml.safe_dump(self.resolved()))
            real_open = os.open
            with mock.patch.dict(os.environ, {"INDEXER_DB_POSTGRES_DSN": "secret-not-to-emit"}), mock.patch.object(check.os, "open", wraps=real_open) as opened, mock.patch.object(subprocess, "run", side_effect=AssertionError("process")), mock.patch.object(socket, "socket", side_effect=AssertionError("network")):
                code, report = self.run_file(p)
                self.assertEqual(2, code)
                self.assertEqual(1, opened.call_count)
                self.assertEqual(str(p), str(opened.call_args.args[0]))
                self.assertNotIn("synthetic", json.dumps(report))
                self.assertNotIn("secret-not-to-emit", json.dumps(report))

    def test_cli_refuses_alias_duplicate_large_deep_and_nonregular_input(self):
        with tempfile.TemporaryDirectory() as temp:
            p = Path(temp) / "input.yaml"
            for raw in (b"name: a\nname: b", b"a: &a {}\nb: *a", b"!!python/object/apply:os.system ['false']", b"a: 1\n---\na: 2", b"[" * 40 + b"]" * 40, b"x" * ((1 << 20) + 1), b"x: [" + b"0," * 9000 + b"0]"):
                p.write_bytes(raw);code,report = self.run_file(p)
                self.assertEqual(1, code);self.assertEqual("FAIL", report["status"])
            p.write_text("name: invalid")
            link = Path(temp) / "link";link.symlink_to(p)
            self.assertEqual(1, self.run_file(link)[0])
            fifo = Path(temp) / "fifo";os.mkfifo(fifo)
            self.assertEqual(1, self.run_file(fifo)[0])
            self.assertEqual(1, self.run_file(Path(temp))[0])

    def test_cli_bad_arguments_and_broken_output(self):
        for args in (["--file"], ["--file", "a", "--file", "b"], ["--start"], ["--file", "x" * 4097]):
            out = io.StringIO();self.assertEqual(1, check.main(args, out))
            self.assertNotIn("Traceback", out.getvalue())
        class Short:
            def write(self, value): return 0
        class Broken:
            def write(self, value): raise BrokenPipeError()
        for out in (Short(), Broken()):self.assertEqual(1, check.main([], out))


if __name__ == "__main__":
    unittest.main()

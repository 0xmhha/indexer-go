"""Offline fixtures only: no diskutil, Docker daemon, RPC, indexer or PostgreSQL."""

import contextlib
import copy
import io
import json
import os
from pathlib import Path
import plistlib
import subprocess
import tempfile
import types
import unittest
from unittest import mock

import yaml

import preflight as p


class PolicyTests(unittest.TestCase):
    def setUp(self):
        self.cfg = p.read_yaml(p.REPO / "docs/examples/config-testnets-windowed.draft.yaml")

    def test_draft_lists_pending_values_without_host_or_secret_access(self):
        with mock.patch.object(p, "probe", side_effect=AssertionError("inspection forbidden")), mock.patch.object(p, "private_file", side_effect=AssertionError("secret access forbidden")):
            self.assertEqual(len(p.check_policy(self.cfg, draft=True)), 9)

    def test_pool_rate_and_strict_state_cannot_drift(self):
        for section, key, value in (("rpc", "rate_limit", 6), ("indexer", "finality", "finalized")):
            cfg = copy.deepcopy(self.cfg)
            cfg[section][key] = value
            with self.assertRaises(p.Refused):
                p.check_policy(cfg, draft=True)
        self.cfg["database"]["postgres"]["max_conns"] = 8
        with self.assertRaises(p.Refused):
            p.check_policy(self.cfg, draft=True)

    def test_inline_dsn_and_gc_are_refused(self):
        for mutate in (lambda c: c["database"]["postgres"].update(dsn="private"), lambda c: c["multichain"]["chains"][-1]["feed"].update(collect=True)):
            cfg = copy.deepcopy(self.cfg)
            mutate(cfg)
            with self.assertRaises(p.Refused):
                p.check_policy(cfg, draft=True)

    def test_rpc_chain_feature_overrides_must_remain_strict(self):
        for chain_index in range(3):
            for name in ("balance.native", "token.metadata"):
                for change in ({"enabled": False}, {"allow_pruned_state": True}):
                    with self.subTest(chain=chain_index, feature=name, change=change):
                        cfg = copy.deepcopy(self.cfg)
                        cfg["multichain"]["chains"][chain_index]["features"] = {name: change}
                        with self.assertRaises(p.Refused):
                            p.check_policy(cfg, draft=True)

    def test_schema_and_window_binding_cannot_drift(self):
        mutations = (
            lambda c: c["database"]["postgres"].update(schema="different_schema"),
            lambda c: c["window_storage"]["windows"]["arc-testnet"].update(volume="wrong_volume"),
            lambda c: c["window_storage"]["windows"]["arc-testnet"].update(min_free_bytes=200 << 30),
            lambda c: c["window_storage"]["windows"].update(extra={}),
            lambda c: c["api"].update(port=9090),
            lambda c: c["operations"]["capacity"].update(stop_free_bytes=10 << 30),
            lambda c: c["operations"].update(halt_file="/tmp/halt.json"),
            lambda c: c.pop("operations"),
        )
        for mutate in mutations:
            cfg = copy.deepcopy(self.cfg)
            mutate(cfg)
            with self.assertRaises(p.Refused):
                p.check_policy(cfg, draft=True)

    def test_public_artifacts_are_checked_without_host_or_secret_access(self):
        with mock.patch.object(p, "probe", side_effect=AssertionError("inspection forbidden")), mock.patch.object(p, "private_file", side_effect=AssertionError("secret access forbidden")):
            p.check_artifacts()

    def test_compose_cross_file_drift_is_refused(self):
        original = p.read_yaml(p.HERE / "compose.draft.yaml")
        mutations = (
            lambda c: c["services"]["indexer"].update(ports=["0.0.0.0:9090:9090"]),
            lambda c: c["services"]["indexer"]["environment"].update(INDEXER_DB_POSTGRES_MAX_CONNS="8"),
            lambda c: c["secrets"]["postgres_dsn"].update(file="wrong-secret-file"),
            lambda c: c["services"]["indexer"]["volumes"][0].update(source="/wrong-volume"),
            lambda c: c["services"]["postgres"]["volumes"][0].update(source="/wrong-pgdata"),
            lambda c: c["services"]["postgres"]["logging"]["options"].update({"max-file": "50"}),
            lambda c: c["services"]["postgres"].update(networks=["rpc"]),
            lambda c: c["services"]["indexer"].update(image="unchecked-image:latest"),
            lambda c: c["services"]["indexer"].update(entrypoint=["/app/indexer-go"]),
            lambda c: c["services"]["indexer"].pop("stop_grace_period"),
            lambda c: c["services"]["indexer"].update(stop_grace_period="10s"),
            lambda c: c["services"]["postgres"].update(stop_signal="SIGTERM"),
            lambda c: c["services"]["postgres"].pop("stop_grace_period"),
        )
        for mutate in mutations:
            compose = copy.deepcopy(original)
            mutate(compose)
            with self.assertRaises(p.Refused):
                p.check_artifacts(compose=compose)

    def test_postgres_durability_connection_and_duplicate_settings_are_refused(self):
        original = (p.REPO / "docs/examples/postgresql-testnets.draft.conf").read_text()
        variants = (original.replace("fsync = on", "fsync = off"), original.replace("max_connections = 32", "max_connections = 8"), original + "\nfsync = on\n", original + "\nfsync = 'off'\n")
        for settings in variants:
            with self.assertRaises(p.Refused):
                p.check_artifacts(postgres_conf=settings)

    def test_matching_public_config_and_compose_cannot_raise_pool_or_rpc(self):
        original = p.read_yaml(p.HERE / "compose.draft.yaml")
        for key, value, env_name in (("max_conns", 8, "INDEXER_DB_POSTGRES_MAX_CONNS"), ("rate_limit", 6, "INDEXER_RPC_RATE_LIMIT")):
            cfg, compose = copy.deepcopy(self.cfg), copy.deepcopy(original)
            section = cfg["database"]["postgres"] if key == "max_conns" else cfg["rpc"]
            section[key] = value
            compose["services"]["indexer"]["environment"][env_name] = str(value)
            with mock.patch.object(p, "read_yaml", return_value=cfg), self.assertRaises(p.Refused):
                p.check_artifacts(compose=compose)

    def test_compose_has_no_automatic_storage_creation_or_extra_node(self):
        compose = p.read_yaml(p.HERE / "compose.draft.yaml")
        self.assertEqual(set(compose["services"]), {"indexer", "postgres"})
        self.assertNotIn("volumes", compose)
        for service in compose["services"].values():
            self.assertEqual(service["profiles"], ["operations-draft"])
            self.assertEqual(service["restart"], "no")
            self.assertNotIn("container_name", service)
            for volume in service["volumes"]:
                self.assertEqual(volume["type"], "bind")
                self.assertIs(volume["bind"]["create_host_path"], False)
        self.assertNotIn("ports", compose["services"]["postgres"])
        state = {v["target"]: v for v in compose["services"]["indexer"]["volumes"]}[p.STATE_DIR]
        self.assertEqual(state["source"], "${INDEXER_PRIVATE_DIR:?preflight required}/state")
        self.assertNotIn("read_only", state)
        self.assertEqual(compose["services"]["indexer"]["secrets"], ["postgres_dsn"])
        self.assertNotIn("INDEXER_DB_POSTGRES_DSN", compose["services"]["indexer"]["environment"])
        self.assertEqual(compose["services"]["indexer"]["environment"]["INDEXER_DB_POSTGRES_MAX_CONNS"], str(self.cfg["database"]["postgres"]["max_conns"]))
        self.assertEqual(compose["services"]["indexer"]["environment"]["INDEXER_RPC_RATE_LIMIT"], str(self.cfg["rpc"]["rate_limit"]))


@unittest.skipIf(os.getuid() == 0, "host operation deliberately requires a non-root owner")
class HostTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="indexer-preflight-")
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name).resolve()
        self.root = self.base / "external"
        self.root.mkdir()
        self.private = self.base / "private"
        self.private.mkdir(mode=0o700)
        (self.private / "state").mkdir(mode=0o700)
        for part in ("indexer/postgres/16", "indexer/window/arc", "indexer/identity"):
            (self.root / part).mkdir(parents=True, mode=0o700)
        self.manifest = {"mount_root": str(self.root), "apfs_volume_uuid": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "physical_store_uuid": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "private_dir": str(self.private), "indexer_image": "indexer-go:operations-reviewed", "docker_context": "desktop-linux"}
        cfg = p.read_yaml(p.REPO / "docs/examples/config-testnets-windowed.draft.yaml")
        for chain in cfg["multichain"]["chains"]:
            chain["start_height"] = 101
        public_key = "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8"
        cfg["multichain"]["chains"][-1]["feed"].update(anchor_height=100, anchor_hash="0x" + "2" * 64, fork_hash="910609e1", fork_next=0, peers=[f"enode://{public_key}@192.0.2.1:30303"])
        self.write(self.private / "config.yaml", yaml.safe_dump(cfg))
        self.write(self.root / "indexer/identity/arc-peer.key", "1" * 64)
        self.password = "app-" + "b" * 40
        self.write(self.private / "postgres-admin-password", "admin-" + "a" * 40)
        self.write(self.private / "indexer-password", self.password)
        self.write(self.private / "postgres-dsn", "postgresql://indexer:" + self.password + "@postgres:5432/indexer_testnets_v1?sslmode=disable")
        self.info = {"FilesystemType": "apfs", "MountPoint": str(self.root), "VolumeUUID": self.manifest["apfs_volume_uuid"], "GlobalPermissionsEnabled": True, "APFSContainerReference": "disk9"}
        self.store = {"DiskUUID": self.manifest["physical_store_uuid"], "DeviceIdentifier": "disk8s2"}
        self.physical = {"Internal": False, "VirtualOrPhysical": "Physical"}
        self.vm_runs = []
        self.vm_report = self.df_report(64 << 30)
        self.patches = [mock.patch.object(p.sys, "platform", "darwin"), mock.patch.object(p.os.path, "ismount", side_effect=lambda path: path == self.root), mock.patch.object(p.os, "statvfs", return_value=types.SimpleNamespace(f_bavail=500 << 30, f_frsize=1)), mock.patch.object(p, "probe", side_effect=self.probe), mock.patch.dict(os.environ, {}, clear=True)]
        for patch in self.patches:
            patch.start()
            self.addCleanup(patch.stop)

    def write(self, path, value):
        path.write_text(value)
        path.chmod(0o600)

    def probe(self, *args):
        if args == ("/usr/sbin/diskutil", "info", "-plist", str(self.root)):
            return plistlib.dumps(self.info)
        if args == ("/usr/sbin/diskutil", "apfs", "list", "-plist", "disk9"):
            return plistlib.dumps({"Containers": [{"ContainerReference": "disk9", "PhysicalStores": [self.store]}]})
        if args == ("/usr/sbin/diskutil", "info", "-plist", "/dev/disk8s2"):
            return plistlib.dumps(self.physical)
        if args == ("docker", "context", "inspect", "desktop-linux"):
            return json.dumps([{"Name": "desktop-linux", "Endpoints": {"docker": {"Host": "unix:///fixture.sock"}}}]).encode()
        if args[:4] == ("docker", "--context", "desktop-linux", "run"):
            self.vm_runs.append(args)
            return self.vm_report
        raise AssertionError("unexpected command")

    def df_report(self, available_bytes):
        return f"Filesystem 1024-blocks Used Available Capacity Mounted on\noverlay 102625208 32364980 {available_bytes // 1024} 34% /\n".encode()

    def test_docker_vm_space_is_read_by_a_sealed_throwaway_container(self):
        p.check_host(self.manifest)
        self.assertEqual(len(self.vm_runs), 1)
        command = self.vm_runs[0]
        for flags in (("--rm",), ("--pull", "never"), ("--network", "none"), ("--read-only",), ("--cap-drop", "ALL"), ("--entrypoint", "df")):
            self.assertTrue(any(command[i:i + len(flags)] == flags for i in range(len(command))), flags)
        self.assertEqual(command[-4:], (self.manifest["indexer_image"], "-P", "-k", "/"))
        for report in (self.df_report(p.DOCKER_VM_MIN_FREE), b"", b"Filesystem 1024-blocks Used Available Capacity Mounted on\noverlay 1 2 x 34% /\n", b"header\noverlay 1 2 3 4% /data\n"):
            self.vm_report = report
            with self.assertRaises(p.Refused):
                p.check_host(self.manifest)

    def test_completed_fixture_passes_without_creating_any_file(self):
        before = sorted(str(f.relative_to(self.base)) for f in self.base.rglob("*"))
        result = p.check_host(self.manifest)
        self.assertEqual(result["INDEXER_APFS_ROOT"], str(self.root))
        self.assertEqual(result["INDEXER_RUN_UID"], str(os.getuid()))
        self.assertFalse(any("DSN" in k or self.password in v for k, v in result.items()))
        self.assertEqual(before, sorted(str(f.relative_to(self.base)) for f in self.base.rglob("*")))

    def test_mount_filesystem_identity_ownership_and_physical_guards(self):
        for field, value in (("FilesystemType", "exfat"), ("MountPoint", str(self.root / "subdirectory")), ("VolumeUUID", "wrong"), ("GlobalPermissionsEnabled", False)):
            saved = self.info[field]
            self.info[field] = value
            with self.assertRaises(p.Refused):
                p.check_host(self.manifest)
            self.info[field] = saved
        self.store["DiskUUID"] = "wrong"
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)
        self.store["DiskUUID"] = self.manifest["physical_store_uuid"]
        for field, value in (("Internal", True), ("VirtualOrPhysical", "Virtual")):
            saved = self.physical[field]
            self.physical[field] = value
            with self.assertRaises(p.Refused):
                p.check_host(self.manifest)
            self.physical[field] = saved

    def test_missing_mount_space_or_data_path_is_not_recreated(self):
        with mock.patch.object(p.os.path, "ismount", return_value=False):
            with self.assertRaises(p.Refused):
                p.check_host(self.manifest)
        with mock.patch.object(p.os, "statvfs", return_value=types.SimpleNamespace(f_bavail=99 << 30, f_frsize=1)):
            with self.assertRaises(p.Refused):
                p.check_host(self.manifest)
        missing = self.root / "indexer/window/arc"
        missing.rmdir()
        with self.assertRaises(FileNotFoundError):
            p.check_host(self.manifest)
        self.assertFalse(missing.exists())

    def test_secret_permissions_symlink_and_dsn_mismatch_are_refused(self):
        path = self.private / "postgres-dsn"
        path.chmod(0o644)
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)
        path.chmod(0o600)
        self.write(path, "postgresql://indexer:wrong@postgres:5432/indexer_testnets_v1?sslmode=disable")
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)
        path.unlink()
        path.symlink_to(self.private / "indexer-password")
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)

    def test_remote_context_and_manifest_placeholders_are_refused(self):
        with mock.patch.dict(os.environ, {"DOCKER_HOST": "tcp://remote.invalid:2375"}):
            with self.assertRaises(p.Refused):
                p.check_host(self.manifest)
        self.manifest["apfs_volume_uuid"] = "UNCONFIRMED_APFS_VOLUME_UUID"
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)

    def test_volume_replacement_during_inspection_is_refused(self):
        reads = 0
        def changing_probe(*args):
            nonlocal reads
            if args == ("/usr/sbin/diskutil", "info", "-plist", str(self.root)):
                reads += 1
                if reads == 2:
                    return plistlib.dumps({**self.info, "VolumeUUID": "replaced"})
            return self.probe(*args)
        with mock.patch.object(p, "probe", side_effect=changing_probe):
            with self.assertRaises(p.Refused):
                p.check_host(self.manifest)
        self.assertEqual(reads, 2)

    def test_private_directory_access_and_pending_runtime_are_refused(self):
        self.private.chmod(0o750)
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)
        self.private.chmod(0o700)
        cfg_path = self.private / "config.yaml"
        cfg = p.read_yaml(cfg_path)
        cfg["multichain"]["chains"][-1]["feed"]["anchor_height"] = "UNCONFIRMED_HEIGHT"
        self.write(cfg_path, yaml.safe_dump(cfg))
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)

    def test_halt_latch_blocks_start_and_gates_resume(self):
        latch = self.private / "state" / "halt.json"
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest, resume=True)
        self.write(latch, '{"version":1,"reason":"capacity"}')
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)
        self.assertIn("INDEXER_PRIVATE_DIR", p.check_host(self.manifest, resume=True))
        self.assertTrue(latch.exists(), "inspection never removes the latch")

    def test_state_directory_must_be_private(self):
        state = self.private / "state"
        state.chmod(0o750)
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)
        state.chmod(0o700)
        state.rmdir()
        with self.assertRaises(FileNotFoundError):
            p.check_host(self.manifest)
        self.assertFalse(state.exists())

    def test_cli_never_prints_credential_bearing_parse_errors(self):
        path = self.private / "host.json"
        self.write(path, json.dumps(self.manifest))
        self.write(self.private / "postgres-dsn", "postgresql://indexer:" + self.password + "@[bad-uri")
        error = io.StringIO()
        with mock.patch.object(p.sys, "argv", ["preflight.py", "--check-host", str(path)]), contextlib.redirect_stderr(error):
            self.assertEqual(p.main(), 1)
        self.assertNotIn(self.password, error.getvalue())
        self.assertNotIn("postgresql://", error.getvalue())


class EntrypointTests(unittest.TestCase):
    def test_start_never_reaches_docker_if_host_check_fails(self):
        error = io.StringIO()
        with mock.patch.object(p.sys, "argv", ["preflight.py", "--start", "/not-a-real-manifest"]), mock.patch.object(p, "private_file", return_value=b"{}"), mock.patch.object(p, "check_host", side_effect=p.Refused("mount rejected")), mock.patch.object(p.os, "execvpe") as execute, contextlib.redirect_stderr(error):
            self.assertEqual(p.main(), 1)
            execute.assert_not_called()

    def test_start_uses_only_checked_nonsecret_environment_and_fixed_project(self):
        with mock.patch.object(p.sys, "argv", ["preflight.py", "--start", "/not-a-real-manifest"]), mock.patch.object(p, "private_file", return_value=b"{}"), mock.patch.object(p, "check_host", return_value={"INDEXER_APFS_ROOT": "/fixture"}), mock.patch.object(p.os, "execvpe") as execute:
            self.assertEqual(p.main(), 0)
            command = execute.call_args.args[1]
            self.assertEqual(command[:4], ["docker", "--context", "desktop-linux", "compose"])
            self.assertIn("indexer-testnets-draft", command)
            self.assertEqual(command[-5:], ["up", "--detach", "--no-build", "--pull", "never"])
            self.assertNotIn("INDEXER_DB_POSTGRES_DSN", execute.call_args.args[2])

    def test_resume_runs_only_the_resume_check_without_database(self):
        with mock.patch.object(p.sys, "argv", ["preflight.py", "--resume", "/not-a-real-manifest"]), mock.patch.object(p, "private_file", return_value=b"{}"), mock.patch.object(p, "check_host", return_value={"INDEXER_APFS_ROOT": "/fixture"}) as check, mock.patch.object(p.os, "execvpe") as execute:
            self.assertEqual(p.main(), 0)
            check.assert_called_once_with({}, resume=True)
            command = execute.call_args.args[1]
            self.assertEqual(command[-7:], ["run", "--rm", "--no-deps", "--pull", "never", "indexer", "--resume-check"])
            self.assertNotIn("up", command)

    def test_secret_is_read_as_data_and_never_appears_in_output_or_argv(self):
        with tempfile.TemporaryDirectory(prefix="indexer-secret-fixture-") as name:
            tmp = Path(name)
            secret = tmp / "secret"
            value = "postgresql://fixture:$(not-a-command)@fixture.invalid/db"
            secret.write_text(value)
            fake = tmp / "fake-indexer"
            fake.write_text("#!/bin/sh\n[ \"$INDEXER_DB_POSTGRES_DSN\" = '" + value + "' ] || exit 11\n[ \"$#\" = 2 ] && [ \"$1\" = --config ] && [ \"$2\" = /etc/indexer/config.yaml ] || exit 12\nprintf 'injection-ok\\n'\n")
            fake.chmod(0o700)
            script = (p.HERE / "indexer-entrypoint.sh").read_text().replace("/run/secrets/postgres_dsn", str(secret)).replace("/app/indexer-go", str(fake))
            wrapper = tmp / "entrypoint.sh"
            wrapper.write_text(script)
            env = {"PATH": os.environ["PATH"], "INDEXER_DB_POSTGRES_DSN_FILE": str(secret)}
            result = subprocess.run(["/bin/sh", str(wrapper)], env=env, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, "injection-ok\n")
            self.assertNotIn(value, result.stderr)
            env["INDEXER_DB_POSTGRES_DSN"] = value
            result = subprocess.run(["/bin/sh", str(wrapper)], env=env, capture_output=True, text=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn(value, result.stdout + result.stderr)
            del env["INDEXER_DB_POSTGRES_DSN"]
            secret.unlink()
            result = subprocess.run(["/bin/sh", str(wrapper)], env=env, capture_output=True, text=True, timeout=5)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("injection-ok", result.stdout)

    def test_entrypoint_accepts_only_the_resume_check(self):
        with tempfile.TemporaryDirectory(prefix="indexer-entrypoint-fixture-") as name:
            tmp = Path(name)
            secret = tmp / "secret"
            secret.write_text("postgresql://fixture@fixture.invalid/db")
            fake = tmp / "fake-indexer"
            fake.write_text("#!/bin/sh\nprintf '%s|' \"$@\"\n")
            fake.chmod(0o700)
            script = (p.HERE / "indexer-entrypoint.sh").read_text().replace("/run/secrets/postgres_dsn", str(secret)).replace("/app/indexer-go", str(fake))
            wrapper = tmp / "entrypoint.sh"
            wrapper.write_text(script)
            env = {"PATH": os.environ["PATH"], "INDEXER_DB_POSTGRES_DSN_FILE": str(secret)}
            run = lambda *args: subprocess.run(["/bin/sh", str(wrapper), *args], env=env, capture_output=True, text=True, timeout=5)
            self.assertEqual(run().stdout, "--config|/etc/indexer/config.yaml|")
            self.assertEqual(run("--resume-check").stdout, "--config|/etc/indexer/config.yaml|--resume-check|")
            for args in (("--clear-data",), ("--resume-check", "--reindex"), ("",)):
                result = run(*args)
                self.assertNotEqual(result.returncode, 0, args)
                self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()

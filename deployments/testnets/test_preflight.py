"""Offline fixtures only: no diskutil, Docker daemon, RPC, indexer or PostgreSQL."""

import contextlib
import copy
import io
import json
import os
from pathlib import Path
import plistlib
import stat
import subprocess
import sys
import tempfile
import types
import unittest
from unittest import mock

import yaml

import preflight as p


class PolicyTests(unittest.TestCase):
    def setUp(self):
        self.cfgs = {chain: p.draft_config(chain) for chain in p.CHAINS}
        self.arc = self.cfgs[p.ARC]

    def refused(self, chain, mutate):
        cfg = copy.deepcopy(self.cfgs[chain])
        mutate(cfg)
        with self.assertRaises(p.Refused):
            p.check_policy(cfg, chain, draft=True)

    def test_draft_lists_pending_values_without_host_or_secret_access(self):
        with mock.patch.object(p, "probe", side_effect=AssertionError("inspection forbidden")), mock.patch.object(p, "private_file", side_effect=AssertionError("secret access forbidden")):
            pending = {chain: len(p.check_policy(cfg, chain, draft=True)) for chain, cfg in self.cfgs.items()}
        self.assertEqual(pending, {"stablenet-testnet": 1, "bsc-testnet": 1, "sepolia": 1, "arc-testnet": 6})

    def test_each_draft_is_one_single_chain_process(self):
        for chain, cfg in self.cfgs.items():
            self.assertNotIn("multichain", cfg)
            self.assertEqual(cfg["database"]["postgres"]["schema"], p.chain_schema(chain))
            self.assertEqual(cfg["api"]["port"], p.CHAINS[chain]["port"])
        self.assertEqual(len({c["port"] for c in p.CHAINS.values()}), len(p.CHAINS), "every chain has its own API port")
        self.refused("sepolia", lambda c: c.update(multichain={"enabled": True}))

    def test_pool_rate_and_strict_state_cannot_drift(self):
        for chain in p.CHAINS:
            for section, key, value in (("rpc", "rate_limit", 6), ("indexer", "finality", "finalized")):
                self.refused(chain, lambda c: c[section].update({key: value}))
            self.refused(chain, lambda c: c["database"]["postgres"].update(max_conns=8))

    def test_inline_dsn_and_gc_are_refused(self):
        self.refused("bsc-testnet", lambda c: c["database"]["postgres"].update(dsn="private"))
        self.refused(p.ARC, lambda c: c["source"]["feed"].update(collect=True))

    def test_rpc_chain_features_must_remain_strict(self):
        for chain in p.CHAINS:
            if chain == p.ARC:
                continue
            for name in ("balance.native", "token.metadata"):
                for change in ({"enabled": False}, {"allow_pruned_state": True}):
                    with self.subTest(chain=chain, feature=name, change=change):
                        self.refused(chain, lambda c: c["features"][name].update(change))

    def test_only_arc_selects_the_feed_and_a_window(self):
        self.refused("sepolia", lambda c: c.update(source={"feed": copy.deepcopy(self.arc["source"]["feed"])}))
        self.refused("stablenet-testnet", lambda c: c.update(window_storage=copy.deepcopy(self.arc["window_storage"])))
        self.refused(p.ARC, lambda c: c["features"]["balance.native"].update(enabled=True))

    def test_schema_and_window_binding_cannot_drift(self):
        mutations = (
            ("sepolia", lambda c: c["database"]["postgres"].update(schema="different_schema")),
            ("bsc-testnet", lambda c: c["database"]["postgres"].update(schema=p.chain_schema("sepolia"))),
            (p.ARC, lambda c: c["window_storage"]["windows"]["arc-testnet"].update(volume="wrong_volume")),
            (p.ARC, lambda c: c["window_storage"]["windows"]["arc-testnet"].update(min_free_bytes=200 << 30)),
            (p.ARC, lambda c: c["window_storage"]["windows"].update(extra={})),
            ("stablenet-testnet", lambda c: c["api"].update(port=8089)),
            ("bsc-testnet", lambda c: c["operations"]["capacity"].update(stop_free_bytes=10 << 30)),
            ("sepolia", lambda c: c["operations"].update(halt_file="/tmp/halt.json")),
            (p.ARC, lambda c: c.pop("operations")),
        )
        for chain, mutate in mutations:
            self.refused(chain, mutate)

    def test_public_artifacts_are_checked_without_host_or_secret_access(self):
        with mock.patch.object(p, "probe", side_effect=AssertionError("inspection forbidden")), mock.patch.object(p, "private_file", side_effect=AssertionError("secret access forbidden")):
            p.check_artifacts()

    def test_compose_cross_file_drift_is_refused(self):
        original = p.read_yaml(p.HERE / "compose.draft.yaml")
        mutations = (
            lambda c: c["services"]["indexer-sepolia"].update(ports=["0.0.0.0:9090:9090"]),
            lambda c: c["services"]["indexer-bsc-testnet"].update(ports=["127.0.0.1:8088:8088"]),
            lambda c: c["services"]["indexer-arc-testnet"]["environment"].update(INDEXER_DB_POSTGRES_MAX_CONNS="8"),
            lambda c: c["services"]["indexer-arc-testnet"]["environment"].update(INDEXER_BINARY="indexer"),
            lambda c: c["services"]["indexer-stablenet-testnet"]["environment"].update(INDEXER_BINARY="indexer-arc"),
            lambda c: c["secrets"]["postgres_dsn"].update(file="wrong-secret-file"),
            lambda c: c["services"]["indexer-arc-testnet"]["volumes"][0].update(source="/wrong-volume"),
            lambda c: c["services"]["indexer-sepolia"]["volumes"][0].pop("read_only"),
            lambda c: c["services"]["indexer-arc-testnet"]["volumes"][0].update(read_only=True),
            lambda c: c["services"]["indexer-sepolia"]["volumes"][1].update(source="${INDEXER_PRIVATE_DIR:?preflight required}/config-bsc-testnet.yaml"),
            lambda c: c["services"]["indexer-sepolia"]["volumes"][2].update(source="${INDEXER_PRIVATE_DIR:?preflight required}/state/arc-testnet"),
            lambda c: c["services"]["indexer-sepolia"]["healthcheck"].update(test=["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://127.0.0.1:8088/health"]),
            lambda c: c["services"].pop("indexer-bsc-testnet"),
            lambda c: c["services"].update(indexer=copy.deepcopy(c["services"]["indexer-sepolia"])),
            lambda c: c["services"]["postgres"]["volumes"][0].update(source="/wrong-pgdata"),
            lambda c: c["services"]["postgres"]["logging"]["options"].update({"max-file": "50"}),
            lambda c: c["services"]["postgres"].update(networks=["rpc"]),
            lambda c: c["services"]["indexer-sepolia"].update(image="unchecked-image:latest"),
            lambda c: c["services"]["indexer-arc-testnet"].update(entrypoint=["/app/indexer-arc"]),
            lambda c: c["services"]["indexer-bsc-testnet"].pop("stop_grace_period"),
            lambda c: c["services"]["indexer-bsc-testnet"].update(stop_grace_period="10s"),
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
            cfgs, compose = copy.deepcopy(self.cfgs), copy.deepcopy(original)
            section = cfgs["sepolia"]["database"]["postgres"] if key == "max_conns" else cfgs["sepolia"]["rpc"]
            section[key] = value
            compose["services"]["indexer-sepolia"]["environment"][env_name] = str(value)
            with mock.patch.object(p, "draft_config", side_effect=lambda chain: cfgs[chain]), self.assertRaises(p.Refused):
                p.check_artifacts(compose=compose)

    def test_compose_has_no_automatic_storage_creation_or_extra_node(self):
        compose = p.read_yaml(p.HERE / "compose.draft.yaml")
        self.assertEqual(set(compose["services"]), {"postgres", "indexer-stablenet-testnet", "indexer-bsc-testnet", "indexer-sepolia", "indexer-arc-testnet"})
        self.assertNotIn("volumes", compose)
        for service in compose["services"].values():
            self.assertEqual(service["profiles"], ["operations-draft"])
            self.assertEqual(service["restart"], "no")
            self.assertNotIn("container_name", service)
            for volume in service["volumes"]:
                self.assertEqual(volume["type"], "bind")
                self.assertIs(volume["bind"]["create_host_path"], False)
        self.assertNotIn("ports", compose["services"]["postgres"])
        for chain, spec in p.CHAINS.items():
            app = compose["services"][p.service(chain)]
            state = {v["target"]: v for v in app["volumes"]}[p.STATE_DIR]
            self.assertEqual(state["source"], "${INDEXER_PRIVATE_DIR:?preflight required}/state/" + chain)
            self.assertNotIn("read_only", state)
            self.assertEqual(app["secrets"], ["postgres_dsn"])
            self.assertNotIn("INDEXER_DB_POSTGRES_DSN", app["environment"])
            self.assertEqual(app["environment"]["INDEXER_BINARY"], spec["binary"])
            self.assertEqual(app["environment"]["INDEXER_DB_POSTGRES_MAX_CONNS"], str(self.cfgs[chain]["database"]["postgres"]["max_conns"]))
            self.assertEqual(app["environment"]["INDEXER_RPC_RATE_LIMIT"], str(self.cfgs[chain]["rpc"]["rate_limit"]))


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
        for chain in p.CHAINS:
            (self.private / "state" / chain).mkdir(mode=0o700)
        for part in ("indexer/postgres/16", "indexer/window/arc", "indexer/identity"):
            (self.root / part).mkdir(parents=True, mode=0o700)
        self.manifest = {"mount_root": str(self.root), "apfs_volume_uuid": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "physical_store_uuid": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "private_dir": str(self.private), "indexer_image": "indexer-go:operations-reviewed", "docker_context": "desktop-linux"}
        public_key = "79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8"
        for chain in p.CHAINS:
            cfg = p.draft_config(chain)
            cfg["indexer"]["start_height"] = 101
            if chain == p.ARC:
                cfg["source"]["feed"].update(anchor_height=100, anchor_hash="0x" + "2" * 64, fork_hash="910609e1", fork_next=0, peers=[f"enode://{public_key}@192.0.2.1:30303"])
            self.write(self.private / f"config-{chain}.yaml", yaml.safe_dump(cfg))
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
        cfg_path = self.private / "config-arc-testnet.yaml"
        cfg = p.read_yaml(cfg_path)
        cfg["source"]["feed"]["anchor_height"] = "UNCONFIRMED_HEIGHT"
        self.write(cfg_path, yaml.safe_dump(cfg))
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)

    def test_every_chain_needs_its_private_configuration(self):
        (self.private / "config-sepolia.yaml").unlink()
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)

    def test_emergency_reasons(self):
        self.assertIsNone(p.emergency(self.manifest))
        latch = p.halt_latch(self.private, "sepolia")
        self.write(latch, json.dumps({"version": 1, "reason": "capacity", "detail": "x"}))
        self.assertIsNone(p.emergency(self.manifest), "a capacity halt keeps the database")
        self.write(latch, json.dumps({"version": 1, "reason": "storage", "detail": "x", "stopDatabase": True}))
        self.assertIn("sepolia halt latch", p.emergency(self.manifest), "any chain's latch can ask")
        latch.unlink()
        with mock.patch.object(p.os, "statvfs", return_value=types.SimpleNamespace(f_bavail=p.EMERGENCY_FREE, f_frsize=1)):
            self.assertIn("emergency line", p.emergency(self.manifest))
        with mock.patch.object(p.os, "statvfs", return_value=types.SimpleNamespace(f_bavail=p.EMERGENCY_FREE + 1, f_frsize=1)):
            self.assertIsNone(p.emergency(self.manifest))
        self.info["VolumeUUID"] = "replaced"
        self.assertIn("volume", p.emergency(self.manifest))
        self.info["VolumeUUID"] = self.manifest["apfs_volume_uuid"]
        with mock.patch.object(p.os.path, "ismount", return_value=False):
            self.assertIn("volume", p.emergency(self.manifest))

    def test_stop_stack_stops_indexers_before_postgres_and_removes_nothing(self):
        indexers = [p.service(c) for c in p.CHAINS]
        with mock.patch.object(p.subprocess, "run") as run:
            self.assertEqual(p.stop_stack(self.manifest), [*indexers, "postgres"])
        commands = [c.args[0] for c in run.call_args_list]
        self.assertEqual([c[len(p.COMPOSE):] for c in commands], [["stop", *indexers], ["stop", "postgres"]])
        for command in commands:
            self.assertEqual(command[:len(p.COMPOSE)], p.COMPOSE)
            self.assertFalse({"down", "rm", "-v", "--volumes", "kill"} & set(command))
        self.assertEqual([c.kwargs["timeout"] for c in run.call_args_list], [180, 120])
        self.assertEqual(run.call_args_list[0].kwargs["env"]["INDEXER_APFS_ROOT"], str(self.root))
        with mock.patch.object(p.subprocess, "run", side_effect=p.subprocess.TimeoutExpired("docker", 180)) as run:
            with self.assertRaises(p.Refused):
                p.stop_stack(self.manifest)
        self.assertEqual(run.call_count, 1, "PostgreSQL is not stopped while an indexer may still write")

    def test_watch_polls_until_an_emergency_and_stops_once(self):
        sleeps = []
        reasons = iter([None, None, "free space at or below the110GiB emergency line"])
        with mock.patch.object(p, "emergency", side_effect=lambda m: next(reasons)), mock.patch.object(p, "stop_stack", return_value=["indexer", "postgres"]) as stop:
            result = p.watch(self.manifest, sleep=sleeps.append)
        self.assertEqual(sleeps, [p.WATCH_INTERVAL, p.WATCH_INTERVAL])
        stop.assert_called_once()
        self.assertEqual(result["stopped"], ["indexer", "postgres"])
        self.manifest["indexer_image"] = "UNCONFIRMED_REVIEWED_OPERATIONS_IMAGE"
        with self.assertRaises(p.Refused):
            p.watch(self.manifest, sleep=sleeps.append)

    def test_watch_cli_reports_the_stop_and_exits_nonzero(self):
        out = io.StringIO()
        with mock.patch.object(p.sys, "argv", ["preflight.py", "--watch", "/manifest"]), mock.patch.object(p, "private_file", return_value=json.dumps(self.manifest).encode()), mock.patch.object(p, "watch", return_value={"emergencyStop": "x", "stopped": ["indexer", "postgres"]}), contextlib.redirect_stdout(out):
            self.assertEqual(p.main(), 3)
        self.assertEqual(json.loads(out.getvalue())["stopped"], ["indexer", "postgres"])

    def start(self, *extra, up=None, watch=None):
        """Run --start with the host check passed; returns status and calls in order."""
        calls = []
        up = up or (lambda command, **kwargs: calls.append(("up", command, kwargs["env"])))
        watch = watch or (lambda manifest, lock=None: calls.append(("watch", lock)) or {"emergencyStop": "x", "stopped": ["indexer", "postgres"]})
        out, error = io.StringIO(), io.StringIO()
        with mock.patch.object(p.sys, "argv", ["preflight.py", "--start", "/manifest", *extra]), mock.patch.object(p, "private_file", return_value=json.dumps(self.manifest).encode()), mock.patch.object(p, "check_host", return_value=p.compose_environment(self.manifest)), mock.patch.object(p.subprocess, "run", side_effect=up), mock.patch.object(p, "watch", side_effect=watch), mock.patch.object(p.signal, "signal"), contextlib.redirect_stdout(out), contextlib.redirect_stderr(error):
            status = p.main()
        return status, calls, out.getvalue(), error.getvalue()

    def test_start_brings_up_the_stack_then_watches_it(self):
        status, calls, out, _ = self.start()
        self.assertEqual(status, 3)
        self.assertEqual([c[0] for c in calls], ["up", "watch"])
        command, env = calls[0][1], calls[0][2]
        self.assertEqual(command[:len(p.COMPOSE)], p.COMPOSE)
        self.assertEqual(command[len(p.COMPOSE):], ["up", "--detach", "--no-build", "--pull", "never"])
        self.assertEqual(env["INDEXER_APFS_ROOT"], str(self.root))
        self.assertNotIn("INDEXER_DB_POSTGRES_DSN", env)
        self.assertIsNotNone(calls[1][1], "the watcher keeps the lock taken before startup")
        self.assertEqual(json.loads(out)["stopped"], ["indexer", "postgres"])

    def test_start_never_reaches_docker_if_host_check_fails(self):
        with mock.patch.object(p.sys, "argv", ["preflight.py", "--start", "/manifest"]), mock.patch.object(p, "private_file", return_value=b"{}"), mock.patch.object(p, "check_host", side_effect=p.Refused("mount rejected")), mock.patch.object(p.subprocess, "run") as run, contextlib.redirect_stderr(io.StringIO()):
            self.assertEqual(p.main(), 1)
        run.assert_not_called()

    def test_start_refuses_while_another_watcher_runs(self):
        held = p.watch_lock(self.private)
        self.addCleanup(os.close, held)
        status, calls, _, error = self.start()
        self.assertEqual((status, calls), (1, []), "no stack starts that this process could not watch")
        self.assertIn("Another watcher", error)
        self.assertEqual((self.private / p.WATCH_LOCK).read_text(), f"{os.getpid()}\n")
        self.assertEqual(stat.S_IMODE((self.private / p.WATCH_LOCK).stat().st_mode), 0o600)

    def test_start_reports_a_failed_startup_and_does_not_watch(self):
        status, calls, _, error = self.start(up=mock.Mock(side_effect=p.subprocess.TimeoutExpired("docker", p.START_TIMEOUT)))
        self.assertEqual((status, calls), (1, []))
        self.assertIn("no watcher runs", error)
        p.watch_lock(self.private)  # released: the next start may take it

    def test_start_in_background_returns_while_its_child_watches(self):
        with mock.patch.object(p, "detach", return_value=4321) as detach:
            status, calls, out, _ = self.start("--background")
        self.assertEqual(status, 0)
        detach.assert_called_once_with(self.private)
        self.assertEqual([c[0] for c in calls], ["up"], "the parent does not watch")
        self.assertEqual(json.loads(out), {"watcher": 4321, "log": str(self.private / p.WATCH_LOG), "lock": str(self.private / p.WATCH_LOCK)})
        with mock.patch.object(p, "detach", return_value=0):
            status, calls, out, _ = self.start("--background")
        self.assertEqual(status, 3)
        self.assertEqual([c[0] for c in calls], ["up", "watch"], "the child watches")

    def test_watcher_interrupt_leaves_the_stack_running(self):
        status, calls, _, error = self.start(watch=mock.Mock(side_effect=KeyboardInterrupt))
        self.assertEqual(status, 130)
        self.assertEqual([c[0] for c in calls], ["up"])
        self.assertIn("keeps running unwatched", error)

    def test_watch_takes_the_lock_unless_held(self):
        held = p.watch_lock(self.private)
        with self.assertRaises(p.Refused):
            p.watch(self.manifest, sleep=lambda s: None)
        with mock.patch.object(p, "emergency", return_value="x"), mock.patch.object(p, "stop_stack", return_value=[]):
            self.assertEqual(p.watch(self.manifest, lock=held)["emergencyStop"], "x")
        os.close(held)

    def test_background_needs_start_or_watch(self):
        with mock.patch.object(p.sys, "argv", ["preflight.py", "--check-host", "/manifest", "--background"]), contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as exit:
                p.main()
        self.assertEqual(exit.exception.code, 2)

    def test_halt_latch_blocks_start_and_gates_resume(self):
        latch = self.private / "state" / "arc-testnet" / "halt.json"
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest, resume=True)
        self.write(latch, '{"version":1,"reason":"capacity"}')
        with self.assertRaises(p.Refused):
            p.check_host(self.manifest)
        self.assertIn("INDEXER_PRIVATE_DIR", p.check_host(self.manifest, resume=True))
        self.assertTrue(latch.exists(), "inspection never removes the latch")

    def test_state_directory_must_be_private(self):
        for state in (self.private / "state", self.private / "state" / "bsc-testnet"):
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
    def test_detached_watcher_leaves_the_session_and_logs_privately(self):
        # A real fork, in a separate interpreter so the test runner is never forked.
        with tempfile.TemporaryDirectory(prefix="indexer-detach-fixture-") as name:
            private = Path(name)
            script = "import os, sys, preflight as p\npid = p.detach(__import__('pathlib').Path(sys.argv[1]))\nif pid == 0:\n    print('child', os.getsid(0) == os.getpid(), os.isatty(0), flush=True)\n    os._exit(0)\nos.waitpid(pid, 0)\nprint('parent')\n"
            result = subprocess.run([sys.executable, "-c", script, name], cwd=p.HERE, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, "parent\n", "the child writes nothing to the terminal")
            log = private / p.WATCH_LOG
            self.assertEqual(log.read_text(), "child True False\n")
            self.assertEqual(stat.S_IMODE(log.stat().st_mode), 0o600)

    def test_resume_runs_only_the_latched_chains_resume_checks_without_database(self):
        with tempfile.TemporaryDirectory(prefix="indexer-resume-fixture-") as name:
            private = Path(name)
            for chain in p.CHAINS:
                (private / "state" / chain).mkdir(parents=True)
            for chain in ("bsc-testnet", "arc-testnet"):
                (private / "state" / chain / "halt.json").write_text("{}")
            manifest = {"private_dir": name}
            refused = lambda command, **kwargs: (_ for _ in ()).throw(p.subprocess.CalledProcessError(1, command)) if "indexer-arc-testnet" in command else None
            out = io.StringIO()
            with mock.patch.object(p.sys, "argv", ["preflight.py", "--resume", "/not-a-real-manifest"]), mock.patch.object(p, "private_file", return_value=json.dumps(manifest).encode()), mock.patch.object(p, "check_host", return_value={"INDEXER_APFS_ROOT": "/fixture"}) as check, mock.patch.object(p.subprocess, "run", side_effect=refused) as run, contextlib.redirect_stdout(out):
                self.assertEqual(p.main(), 1, "a refused check fails the command")
            check.assert_called_once_with(manifest, resume=True)
            commands = [c.args[0] for c in run.call_args_list]
            self.assertEqual([c[len(p.COMPOSE):] for c in commands], [["run", "--rm", "--no-deps", "--pull", "never", "indexer-bsc-testnet", "resume-check"], ["run", "--rm", "--no-deps", "--pull", "never", "indexer-arc-testnet", "resume-check"]])
            self.assertTrue(all("up" not in c for c in commands))
            self.assertEqual([json.loads(line) for line in out.getvalue().splitlines()], [{"chain": "bsc-testnet", "resumeCheck": "cleared"}, {"chain": "arc-testnet", "resumeCheck": "refused"}])

    def test_secret_is_read_as_data_and_never_appears_in_output_or_argv(self):
        with tempfile.TemporaryDirectory(prefix="indexer-secret-fixture-") as name:
            tmp = Path(name)
            secret = tmp / "secret"
            value = "postgresql://fixture:$(not-a-command)@fixture.invalid/db"
            secret.write_text(value)
            fake = tmp / "fake-indexer"
            fake.write_text("#!/bin/sh\n[ \"$INDEXER_DB_POSTGRES_DSN\" = '" + value + "' ] || exit 11\n[ \"$#\" = 2 ] && [ \"$1\" = --config ] && [ \"$2\" = /etc/indexer/config.yaml ] || exit 12\nprintf 'injection-ok\\n'\n")
            fake.chmod(0o700)
            script = (p.HERE / "indexer-entrypoint.sh").read_text().replace("/run/secrets/postgres_dsn", str(secret)).replace('"/app/$INDEXER_BINARY"', str(fake))
            wrapper = tmp / "entrypoint.sh"
            wrapper.write_text(script)
            env = {"PATH": os.environ["PATH"], "INDEXER_DB_POSTGRES_DSN_FILE": str(secret), "INDEXER_BINARY": "indexer-arc"}
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
            script = (p.HERE / "indexer-entrypoint.sh").read_text().replace("/run/secrets/postgres_dsn", str(secret)).replace('"/app/$INDEXER_BINARY"', str(fake))
            wrapper = tmp / "entrypoint.sh"
            wrapper.write_text(script)
            env = {"PATH": os.environ["PATH"], "INDEXER_DB_POSTGRES_DSN_FILE": str(secret), "INDEXER_BINARY": "indexer"}
            run = lambda *args: subprocess.run(["/bin/sh", str(wrapper), *args], env=env, capture_output=True, text=True, timeout=5)
            self.assertEqual(run().stdout, "--config|/etc/indexer/config.yaml|")
            self.assertEqual(run("resume-check").stdout, "--config|/etc/indexer/config.yaml|resume-check|")
            for args in (("--clear-data",), ("--resume-check",), ("resume-check", "--reindex"), ("",)):
                result = run(*args)
                self.assertNotEqual(result.returncode, 0, args)
                self.assertEqual(result.stdout, "")
            for binary in ("", "indexer-go", "../indexer", "indexer-arc --clear-data"):
                env["INDEXER_BINARY"] = binary
                result = run()
                self.assertNotEqual(result.returncode, 0, binary)
                self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()

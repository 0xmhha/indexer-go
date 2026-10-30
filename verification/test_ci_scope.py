import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("ci_scope", ROOT / "scripts/ci_scope.py")
scope = importlib.util.module_from_spec(spec)
spec.loader.exec_module(scope)


class Scope(unittest.TestCase):
    def test_docs_only_never_needs_go_docker_or_native(self):
        for paths in [["README.md"], ["CLAUDE.md", "docs/README.md"], ["docs/images/architecture.svg"]]:
            with self.subTest(paths=paths):
                self.assertEqual(scope.classify(paths), {"code": False, "arc": False, "full": False, "docs_only": True})

    def test_code_and_unknown_paths_are_conservative_but_go_does_not_rebuild_rust(self):
        for path in ["pkg/app/app.go", "pkg/storage/postgres/migrations/0024.sql", "execwire/codec.go", ".github/workflows/ci.yml", "Makefile", "configs/example.yaml", "testdata/fixture.json", "unexpected-file"]:
            with self.subTest(path=path):
                result = scope.classify([path])
                self.assertTrue(result["code"])
                self.assertFalse(result["arc"])

    def test_native_and_mixed_changes_run_native_checks(self):
        for path in ["x/arc/executor/src/main.rs", "x/arc/executor/Cargo.lock", "x/arc/executor/rust-toolchain.toml"]:
            with self.subTest(path=path):
                self.assertTrue(scope.classify(["README.md", path])["arc"])

    def test_text_dependency_and_runtime_fixtures_are_code(self):
        for path in ["requirements.txt", "deployments/testnets/requirements.txt", "testdata/fixture.txt", "pkg/api/testdata/query.md", "x/arc/executor/testdata/expected.txt"]:
            with self.subTest(path=path):
                result = scope.classify([path])
                self.assertTrue(result["code"])
                self.assertEqual(result["arc"], path.startswith("x/arc/executor/"))

    def test_unknown_diff_and_explicit_full_do_not_skip(self):
        for paths in [None, []]:
            self.assertTrue(scope.classify(paths)["code"])
        result = scope.classify(["README.md"], full=True)
        self.assertEqual(result, {"code": True, "arc": True, "full": True, "docs_only": False})

    def test_malformed_paths_cannot_masquerade_as_docs(self):
        for path in ["../README.md", "/README.md", "docs/../secret.md", "docs/a.md\nscript", "docs/a.py"]:
            with self.subTest(path=path):
                self.assertTrue(scope.classify([path])["code"])

    def test_invalid_revisions_never_reach_git(self):
        with mock.patch.object(scope.subprocess, "run", side_effect=AssertionError("unvalidated revision")):
            self.assertIsNone(scope.changed("HEAD; command", "0" * 40))

    def test_pr_merge_depth_two_contains_exact_base_and_head_for_docs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo, checkout = root / "source", root / "checkout"
            git = ["git", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"]

            def run(*args, cwd=repo):
                return subprocess.check_output([*git, *args], cwd=cwd, text=True, stderr=subprocess.DEVNULL).strip()

            run("init", "-q", "--initial-branch=main", str(repo), cwd=root)
            (repo / "README.md").write_text("# base\n")
            (repo / "source.go").write_text("package fixture\n")
            run("add", "README.md", "source.go")
            run("commit", "-qm", "base")
            base = run("rev-parse", "HEAD")
            run("switch", "-qc", "docs")
            (repo / "README.md").write_text("# documentation\n")
            run("commit", "-qam", "docs")
            head = run("rev-parse", "HEAD")
            run("switch", "-q", "main")
            run("merge", "--no-ff", "-qm", "PR merge", "docs")
            run("clone", "-q", "--depth=2", repo.as_uri(), str(checkout), cwd=root)
            previous = Path.cwd()
            try:
                os.chdir(checkout)
                self.assertEqual(scope.changed(base, head), ["README.md"])
                self.assertTrue(scope.classify(scope.changed(base, head))["docs_only"])
            finally:
                os.chdir(previous)


if __name__ == "__main__":
    unittest.main()

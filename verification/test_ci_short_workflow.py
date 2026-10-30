"""Fast PR checks must not silently become full stress/native campaigns."""
import os
from pathlib import Path
import re
import shlex
import unittest
import yaml

ROOT = Path(os.environ.get("INDEXER_CI_REPOSITORY", Path(__file__).resolve().parents[1]))


class ShortWorkflow(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text())

    def test_pr_go_check_uses_short_once(self):
        commands = "\n".join(step.get("run", "") for step in self.workflow["jobs"]["test"]["steps"])
        self.assertIn("test-ci-short", commands)

    def test_stress_is_manual_full_only(self):
        seen = 0
        for job in self.workflow["jobs"].values():
            for step in job.get("steps", []):
                if "-count=200" in step.get("run", ""):
                    seen += 1
                    self.assertIn("full", str(step.get("if", "")))
        self.assertGreater(seen, 10, "full coverage must be preserved, not deleted")

    def test_docs_and_native_are_scoped(self):
        self.assertIn("scope", self.workflow["jobs"])
        arc = self.workflow["jobs"]["arc"]
        prerequisites = [s for s in arc["steps"] if "Arc native build prerequisites" == s.get("name")]
        self.assertEqual(len(prerequisites), 1)
        self.assertTrue(prerequisites[0].get("if"), "Go/docs changes must not rebuild native SDK")

    def test_full_requires_explicit_manual_dispatch(self):
        events = self.workflow.get("on", self.workflow.get(True, {}))
        self.assertIn("workflow_dispatch", events)
        full = events["workflow_dispatch"]["inputs"]["full"]
        self.assertEqual(full["type"], "boolean")
        self.assertIs(full["default"], False)

    def test_postgres_pr_keeps_durable_core_smoke(self):
        commands = "\n".join(step.get("run", "") for step in self.workflow["jobs"]["postgres"]["steps"])
        self.assertIn("test-ci-postgres-short", commands)

    def test_docs_do_not_start_compilers_or_services(self):
        for name, job in self.workflow["jobs"].items():
            if name == "scope":
                continue
            for step in job["steps"]:
                command = step.get("run", "")
                expensive = step.get("uses", "").startswith(("actions/setup-go", "actions/cache")) or any(
                    token in command for token in ["go test", "go build", "go vet", "gofmt", "make ", "cargo ", "rustup ", "apt-get", "test-postgres.sh"]
                )
                if expensive:
                    self.assertIn("needs.scope.outputs.", step.get("if", ""), f"{name}: {step}")

    def test_native_smoke_filters_still_select_real_cases(self):
        step = next(s for s in self.workflow["jobs"]["arc"]["steps"] if s.get("name") == "Arc compile and bounded protocol smoke")
        filters = []
        for line in step["run"].splitlines():
            argv = shlex.split(line)
            if argv[:2] == ["cargo", "test"]:
                module, _, prefix = argv[-1].split("::")
                source = (ROOT / "x/arc/executor/src" / (module + ".rs")).read_text()
                names = re.findall(r"\bfn (\w+)\(", source)
                self.assertTrue(any(name.startswith(prefix) for name in names), argv)
                filters.append(argv[-1])
        self.assertEqual(len(filters), 3)


if __name__ == "__main__":
    unittest.main()

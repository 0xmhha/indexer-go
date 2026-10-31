"""The heavy real SDK/App/PG boundary must remain explicit full-only."""
from pathlib import Path
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[1]

class NativeAcceptancePolicy(unittest.TestCase):
    def test_actual_app_acceptance_is_full_only_and_preserves_exact_binaries(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text())
        job = workflow["jobs"]["arc"]
        steps = job["steps"]
        matches = [s for s in steps if s.get("name") == "Real Arc App native ingress and retention acceptance"]
        self.assertEqual(1, len(matches), "N6 must be a visible explicit full acceptance step")
        step = matches[0]
        self.assertEqual("needs.scope.outputs.full == 'true'", step["if"])
        self.assertIn("scripts/test-postgres.sh", step["run"])
        self.assertIn("-tags arc_native", step["run"])
        self.assertIn("TestArcNativeIntegrated", step["run"])
        self.assertNotIn("-count=200", step["run"])
        self.assertNotIn("services", job, "no PostgreSQL service starts for default PRs")
        preserve = next(s for s in steps if s.get("name") == "Preserve tested native binaries and release owned build cache")
        for binary in ["arc-native-bootstrap", "arc-native-import", "arc-native-maintenance", "indexer-arc-executor"]:
            self.assertIn(binary, preserve["run"])
        self.assertIn('cmp "target/debug/$name"', preserve["run"])
        self.assertLess(steps.index(preserve), steps.index(step))
        build = next(s for s in steps if s.get("name") == "Arc native build, lint and offline replay")
        self.assertIn("INDEXER_ARC_INGRESS_FIXTURE_DIR", build["env"])
        self.assertIn("--features acceptance-trace", build["run"])
        short = next(s for s in steps if s.get("name") == "Arc compile and bounded protocol smoke")
        self.assertIn("acceptance::integrated_fixture_exports_official_checkpoint_and_two_branches", short["run"])
        self.assertNotIn("INDEXER_ARC_INGRESS_FIXTURE_DIR", short.get("env", {}))
        self.assertNotIn("acceptance-trace", short["run"])
        self.assertNotIn("test-postgres", short["run"])

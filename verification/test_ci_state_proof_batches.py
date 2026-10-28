"""Each durable state-proof case retains 200 race runs and an independent budget."""
import pathlib
import re
import shlex
import unittest

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
CASES = {
    "TestExecutionNativeStateProofPublicationAndRestart",
    "TestExecutionArcNativeStateProofPublicationAndRestart",
    "TestExecutionArcNativeProofSpoolPublicationAndLostACK",
    "TestExecutionArcNativeProofFreshFailureRetainsUnpublishedParts",
}


class StateProofBudgets(unittest.TestCase):
    def test_all_cases_keep_independent_budgets_and_required_conditions(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text())
        steps = workflow["jobs"]["postgres"]["steps"]
        invocations = []
        for step in steps:
            for command in step.get("run", "").splitlines():
                if any(case in command for case in CASES):
                    invocations.append((step["name"], shlex.split(command)))
        self.assertEqual(
            len(invocations), 4,
            "four acceptance cases need independent package timeout budgets",
        )
        selected = []
        for name, argv in invocations:
            with self.subTest(step=name):
                self.assertEqual(argv[:3], ["GOMAXPROCS=1", "./scripts/test-postgres.sh", "./pkg/app"])
                self.assertEqual(argv[3:7], ["-race", "-count=200", "-timeout=20m", "-run"])
                self.assertEqual(len(argv), 8)
                self.assertTrue(argv[7].startswith("^") and argv[7].endswith("$"))
                matches = [case for case in CASES if re.fullmatch(argv[7], case)]
                self.assertEqual(len(matches), 1, "one existing case per timeout budget")
                selected.extend(matches)
        self.assertEqual(sorted(selected), sorted(CASES), "no lost, duplicated or extra acceptance case")
        self.assertEqual(len({name for name, _ in invocations}), 4)
        sources = "\n".join(p.read_text() for p in (ROOT / "pkg/app").glob("*_test.go"))
        all_cases = re.findall(r"func (Test\w+)\(t \*testing\.T\)", sources)
        for _, argv in invocations:
            self.assertEqual(
                len([case for case in all_cases if re.fullmatch(argv[7], case)]), 1,
                "a batch must not silently select additional tests",
            )
        for case in CASES:
            self.assertRegex(sources, rf"func {case}\(t \*testing\.T\)")


if __name__ == "__main__":
    unittest.main()

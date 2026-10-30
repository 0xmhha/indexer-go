import importlib.util
import json
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("pg_smoke", ROOT / "scripts/ci_postgres_smoke.py")
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class CoreSmoke(unittest.TestCase):
    def test_each_selected_case_exists_and_essential_guards_remain(self):
        sources = "\n".join(p.read_text() for p in (ROOT / "pkg/storage/postgres").glob("*_test.go"))
        for case in smoke.CASES:
            self.assertEqual(len(re.findall(rf"func {case}\(t \*testing.T\)", sources)), 1)
        for fragment in ["SevenPartsFresh", "PostFreshGenerationFence", "LostACK", "ReservedFourthSlot", "CancellationJoins", "ReorgCheck"]:
            self.assertTrue(any(fragment in case for case in smoke.CASES))

    def test_missing_or_skipped_is_not_success(self):
        output = "\n".join(json.dumps({"Test": case, "Action": "pass"}) for case in smoke.CASES)
        smoke.validate(output)
        with self.assertRaises(ValueError):
            smoke.validate("")
        with self.assertRaises(ValueError):
            smoke.validate(output + "\n" + json.dumps({"Test": smoke.CASES[0], "Action": "skip"}))


if __name__ == "__main__":
    unittest.main()

import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location("observer", pathlib.Path(__file__).with_name("verify_release_sequence.py"))
observer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(observer)

class SyncReceiptTests(unittest.TestCase):
    def test_actual_three_predecessor_receipts_are_clean(self):
        for priority in (1, 2, 3):
            import json
            receipt = json.loads(pathlib.Path(__file__).with_name(f"priority{priority}-sync.json").read_text())
            self.assertEqual(observer.sync_status(receipt), "")

    def test_absence_dirty_and_conflicting_fields_fail_closed(self):
        for receipt in ({}, {"status": None}, {"primary_status": " M tracked"}, {"status": "", "primary_status": "?? file"}):
            with self.subTest(receipt=receipt), self.assertRaises(ValueError):
                observer.sync_status(receipt)

class OwnedRunTests(unittest.TestCase):
    def test_both_reviewed_generations_are_required(self):
        paths = ["/private/tmp/indexer-four.current", "/private/tmp/indexer-four.preserved"]
        release = {"owned_run_dir": paths[0], "additional_owned_run_dirs": paths[1:]}
        self.assertEqual([str(p) for p in observer.owned_run_paths(release, "\n".join(paths))], paths)
        with self.assertRaises(ValueError):
            observer.owned_run_paths(release, paths[0])

    def test_unscoped_duplicate_and_malformed_generations_fail(self):
        primary = "/private/tmp/indexer-four.current"
        for additional in ("bad", [primary], ["/tmp/indexer-four.other"], ["/private/tmp/indexer-four."],
                           ["/private/tmp/indexer-four.other/../foreign"], [None]):
            with self.subTest(additional=additional), self.assertRaises(ValueError):
                observer.owned_run_paths({"owned_run_dir": primary, "additional_owned_run_dirs": additional}, primary + str(additional))

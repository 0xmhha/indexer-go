"""Result/exit handling only; never builds, measures, or accesses Docker/DB/RPC."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('placement', Path(__file__).with_name('measure-api-placement.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class MeasurementResultTests(unittest.TestCase):
    def test_accepts_legacy_and_new_success(self):
        for extra in ({}, {'apiValidation': {'Passed': True}}):
            with self.subTest(extra=extra), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'run.log'
                row = dict(api={'drain/block': {'Errors': {}}}, **extra)
                log = 'MIXED_POOL_MEASURE ' + json.dumps(row) + '\nPASS\n'
                self.assertEqual(m.parse_measurement(log, path), row)
                self.assertFalse(path.with_suffix('.failed.json').exists())

    def test_rejects_failure_even_with_pass_and_retains_raw(self):
        cases = [
            ({'api': {'drain/block': {'Errors': {'timeout': 1}}}}, 'api_errors'),
            ({'api': {'drain/block': {'Errors': {'invalid_data': 1}}}}, 'api_errors'),
            ({'api': {'drain/block': {'Errors': {}}}, 'apiValidation': {'Passed': False}}, 'api_validation_failed'),
            ({'api': {'drain/block': {'Errors': {}, 'IncompleteDiagnostics': 1}}}, 'incomplete_api_diagnostics'),
            ({'api': {}}, 'missing_api_results'),
        ]
        for row, reason in cases:
            with self.subTest(reason=reason), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'run.log'
                log = 'MIXED_POOL_MEASURE ' + json.dumps(row) + '\nPASS\n'
                path.write_text(log)
                with self.assertRaises(m.MeasurementError) as failed:
                    m.parse_measurement(log, path)
                self.assertIn(reason, failed.exception.reasons)
                self.assertEqual(failed.exception.row, row)
                self.assertEqual(path.read_text(), log)
                saved = json.loads(path.with_suffix('.failed.json').read_text())
                self.assertFalse(saved['valid'])
                self.assertEqual(saved['row'], row)

    def test_nonzero_exit_preserves_emitted_result(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'run.log'
            row = {'api': {'drain/block': {'Errors': {'timeout': 1}}}}
            log = 'MIXED_POOL_MEASURE ' + json.dumps(row) + '\nFAIL\n'
            path.write_text(log)
            with patch.object(m, 'logged', side_effect=RuntimeError('failed child')):
                with self.assertRaises(m.MeasurementError) as failed:
                    m.measured([], {}, directory, path)
            self.assertEqual(failed.exception.row, row)
            self.assertIn('command_failed', failed.exception.reasons)
            self.assertIn('missing_PASS', failed.exception.reasons)

    def test_missing_malformed_and_duplicate_result(self):
        for log in ('PASS\n', 'MIXED_POOL_MEASURE {\nPASS\n',
                    'MIXED_POOL_MEASURE {}\nMIXED_POOL_MEASURE {}\nPASS\n'):
            with self.subTest(log=log), tempfile.TemporaryDirectory() as directory:
                with self.assertRaises(m.MeasurementError):
                    m.parse_measurement(log, Path(directory) / 'run.log')


if __name__ == '__main__':
    unittest.main()

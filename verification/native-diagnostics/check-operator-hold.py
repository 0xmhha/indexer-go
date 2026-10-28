#!/usr/bin/env python3
"""Actual CLI regression: deliberate missing operator paths stay HOLD, never malformed."""
import json
from pathlib import Path
import subprocess

binary = Path('verification/native-diagnostics/logs/operator-diagnose').resolve()
subprocess.run(['go', 'build', '-o', str(binary), './x/arc/cmd/execution-diagnose'], check=True)
for flag in ['--native', '--spool', '--source-artifact', '--checkpoint-artifact', '--retention-artifact']:
    for missing in ['HOLD', '']:
        result = subprocess.run([str(binary), flag, missing], text=True, capture_output=True, check=False)
        report = json.loads(result.stdout)
        assert result.returncode == 2 and report['status'] == 'HOLD', f'{flag}: deliberate operator placeholder must HOLD/2, got {result.returncode}/{report["status"]}'
print('PASS: every explicit operator placeholder remains HOLD/2 in the actual CLI')

#!/usr/bin/env python3
"""Exercise the shipped CLI against explicit synthetic inputs, without runtime stores."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

binary = str(Path(sys.argv[1]).resolve())
base = Path('configs/execution/preflight/synthetic-arc')
files = sorted(base.iterdir())
before = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in files}
args = [binary, '--app', str(base / 'app.yaml'), '--native', str(base / 'native.json'),
        '--retention', str(base / 'retention.json'), '--checkpoint', str(base / 'checkpoint.json')]
for declaration, status, code in [('declaration.json', 'PASS', 0), ('declaration.hold.json', 'HOLD', 2)]:
    result = subprocess.run(args + ['--declaration', str(base / declaration)], capture_output=True, check=False)
    report = json.loads(result.stdout)
    assert result.returncode == code, (result.returncode, result.stderr, report)
    assert report['status'] == status
    assert report['operational_readiness'] == 'not_authorized'
    assert report['live_freshness'] == 'not_checked'
    assert report['full_state_reconstruction'] == 'not_performed'
    assert not result.stderr and len(result.stdout) <= 64 * 1024
    assert all(set(item) == {'field', 'status', 'code'} for item in report['findings'])
    again = subprocess.run(args + ['--declaration', str(base / declaration)], capture_output=True, check=False)
    assert again.stdout == result.stdout
result = subprocess.run([binary, '--unsupported=secret-query-token'], capture_output=True, check=False)
assert result.returncode == 1 and json.loads(result.stdout)['status'] == 'FAIL'
assert b'secret-query-token' not in result.stdout + result.stderr
assert before == {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(base.iterdir())}
print('PASS: executable PASS/0 HOLD/2 FAIL/1, deterministic bounded redaction, unchanged explicit source fixtures')

#!/usr/bin/env python3
"""Scan staged task files and reachable history, retaining reviewed baseline distinctions."""
import hashlib
import json
from pathlib import Path
import subprocess

here = Path(__file__).resolve().parent
logs = here / 'logs'
logs.mkdir(exist_ok=True)
reviewed = {item['fingerprint'] for item in json.loads(Path('verification/secret-scan-reviewed.json').read_text())}
receipts = []
for scope, options in [('staged', ['--staged']), ('history', ['--log-opts=HEAD'])]:
    report = logs / (scope + '-secrets.json')
    command = ['gitleaks', 'git', '--redact', '--no-banner', '--report-format=json', '--report-path=' + str(report), *options, '.']
    result = subprocess.run(command, text=True, capture_output=True, check=False)
    assert result.returncode in [0, 1], 'secret scanner execution failed'
    findings = json.loads(report.read_text())
    unknown = [item for item in findings if item.get('Fingerprint') not in reviewed]
    # Do not emit matched text, secrets or opaque parser errors.
    print(json.dumps({'scope': scope, 'scanner_exit': result.returncode, 'findings': len(findings), 'reviewed_public_test_fixtures': len(findings) - len(unknown), 'unreviewed': [{'file': f['File'], 'line': f['StartLine'], 'rule': f['RuleID'], 'fingerprint': f.get('Fingerprint')} for f in unknown]}, sort_keys=True))
    assert not unknown, 'unreviewed secret findings require local inspection'
    receipts.append({'scope': scope, 'argv': command, 'scanner_exit': result.returncode, 'findings': len(findings), 'reviewed': len(findings), 'unreviewed': 0, 'report_sha256': hashlib.sha256(report.read_bytes()).hexdigest()})
(here / 'secrets.json').write_text(json.dumps(receipts, indent=2) + '\n')
print('PASS: staged and reachable-history secret checks; reviewed baseline fixtures remain distinct')

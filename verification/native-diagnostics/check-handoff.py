#!/usr/bin/env python3
"""Validate the one-commit local handoff and all final-source gate receipts."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
from gate import source_hash

root = Path(__file__).resolve().parents[2]
here = Path(__file__).resolve().parent
frozen = json.loads((here / 'source.json').read_text())
assert source_hash() == frozen['source_sha256'], 'source changed after final gates'
records = [json.loads(line) for line in (here / 'checks.jsonl').read_text().splitlines()]
required = ['build-final-v3', 'vet-final-v3', 'lint-final-v3', 'modules-final-v3', 'format-final-v3', 'functional-final-v3', 'race-final', 'cache', 'examples', 'postgres', 'pgtools-final-v3', 'docs-final', 'boundaries-final-v3', 'operator-green-v3', 'secrets-final']
receipts = []
for scope in required:
    matches = [record for record in records if record['scope'] == scope and record['source_sha256'] == frozen['source_sha256']]
    assert matches, 'missing final-source receipt: ' + scope
    record = matches[-1]
    assert record['exit'] == 0 and record['source_after_sha256'] == frozen['source_sha256'], scope
    log = root / record['log']
    assert log.is_file() and hashlib.sha256(log.read_bytes()).hexdigest() == record['log_sha256'], 'log mismatch: ' + scope
    receipts.append({'scope': scope, 'exit': record['exit'], 'log': record['log'], 'log_sha256': record['log_sha256']})
def git(*args):
    return subprocess.check_output(['git', *args], cwd=root, text=True).strip()
assert not git('status', '--porcelain', '--untracked-files=no'), 'tracked tree is not clean'
assert git('rev-parse', 'HEAD^') == frozen['base'] and git('rev-list', '--count', frozen['base'] + '..HEAD') == '1', 'handoff must contain exactly one local commit'
subject = git('log', '-1', '--format=%s')
assert re.match(r'^(feat|fix)(\([^)]*\))?: [A-Za-z]', subject), 'non-Conventional subject'
message = git('log', '-1', '--format=%B')
assert 'codex' not in message.lower() and 'co-authored-by' not in message.lower(), 'unexpected attribution'
files = git('diff-tree', '--no-commit-id', '--name-only', '-r', 'HEAD').splitlines()
assert not any(path.startswith(('.ouroboros/', 'build/')) or '/logs/' in path for path in files), 'machine-local artifacts included'
print(json.dumps({'cwd': '.', 'branch': git('branch', '--show-current'), 'commit': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'), 'subject': subject, 'tracked_status': [], 'untracked_status': [line for line in git('status', '--porcelain', '--untracked-files=normal').splitlines() if line.startswith('??')], 'changed_files': files, 'source_sha256': frozen['source_sha256'], 'receipts': receipts, 'worker_publication': 'not_performed; parent owns review, PR, exact-head CI5, squash and main sync'}, indent=2))

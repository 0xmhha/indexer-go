#!/usr/bin/env python3
"""Record exact argv, exit, full local log hash and frozen source identity."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent

def source_hash():
    files = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z', '*.go', 'go.mod', 'go.sum', '**/go.mod', '**/go.sum'], cwd=ROOT).split(b'\0')
    h = hashlib.sha256()
    for name in sorted(set(f for f in files if f and not f.startswith(b'.ouroboros/'))):
        path = ROOT / os.fsdecode(name)
        h.update(name + b'\0' + path.read_bytes() + b'\0')
    return h.hexdigest()

def main():
    os.chdir(ROOT)
    freeze = HERE / 'source.json'
    if sys.argv[1:] == ['freeze']:
        freeze.write_text(json.dumps({'base': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(), 'source_sha256': source_hash(), 'go_version': subprocess.check_output(['go', 'version'], text=True).strip()}, indent=2) + '\n')
        print(freeze.read_text(), end='')
        return 0
    if sys.argv[1:] == ['verify-source']:
        assert source_hash() == json.loads(freeze.read_text())['source_sha256'], 'sources changed after freeze'
        print('PASS frozen source identity')
        return 0
    label, *argv = sys.argv[1:]
    assert label.replace('-', '').isalnum() and argv
    expected = json.loads(freeze.read_text())['source_sha256']
    assert source_hash() == expected, 'sources changed before gate'
    # Complete output stays local and ignored; the portable receipt carries its hash.
    log = HERE / 'logs' / (label + '.log')
    log.parent.mkdir(exist_ok=True)
    with log.open('wb') as stream:
        result = subprocess.run(argv, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT, check=False)
    after = source_hash()
    receipt = {'cwd': str(ROOT), 'scope': label, 'argv': argv, 'source_sha256': expected, 'source_after_sha256': after, 'environment': {key: os.environ.get(key) for key in ['GOTOOLCHAIN','GOMODCACHE','GOFLAGS','PGBIN','GOMAXPROCS']}, 'exit': result.returncode, 'log': str(log.relative_to(ROOT)), 'log_sha256': hashlib.sha256(log.read_bytes()).hexdigest(), 'log_bytes': log.stat().st_size}
    record = HERE / 'checks.jsonl'
    with record.open('a') as stream:
        stream.write(json.dumps(receipt, sort_keys=True) + '\n')
    print(json.dumps(receipt, sort_keys=True))
    if result.returncode:
        print(log.read_text(errors='replace')[-10000:])
    assert after == expected, 'sources changed during gate'
    return result.returncode

if __name__ == '__main__':
    sys.exit(main())

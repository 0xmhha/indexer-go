#!/usr/bin/env python3
"""Check all repository Go sources, including newly added task files."""
from pathlib import Path
import subprocess
files = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z', '*.go']).split(b'\0')
files = sorted(set(f.decode() for f in files if f and not f.startswith(b'.ouroboros/')))
result = subprocess.run(['gofmt', '-l', *files], check=True, text=True, capture_output=True)
assert not result.stdout, result.stdout
print(f'PASS: {len(files)} Go files are formatted')

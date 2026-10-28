#!/usr/bin/env python3
"""Check existing PostgreSQL16 fixture executables; never install or repair host tools."""
from pathlib import Path
import os
import subprocess
root = Path('/Library/PostgreSQL/16/bin')
for name in ['postgres', 'initdb', 'pg_ctl', 'createdb', 'psql']:
    tool = root / name
    assert tool.is_file() and os.access(tool, os.X_OK), f'unavailable PostgreSQL fixture tool: {name}'
    result = subprocess.run([str(tool), '--version'], text=True, capture_output=True, check=True)
    assert '16.' in result.stdout, f'wrong PostgreSQL major: {name}'
    print(result.stdout.strip())
print('PASS: owned PostgreSQL16 fixture binaries available; no install/symlink changes')

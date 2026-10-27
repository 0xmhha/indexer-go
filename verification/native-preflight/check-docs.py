#!/usr/bin/env python3
"""Check relative Markdown links/anchors in this feature's maintained docs."""
from pathlib import Path
import re
import unicodedata

paths = [Path(p) for p in ['README.md', 'CLAUDE.md', 'docs/README.md', 'docs/NATIVE_EXECUTION_PREFLIGHT.md',
         'docs/OPERATIONS_READINESS.md', 'docs/analysis/open-items.md', 'x/arc/collector/README.md']]
paths += sorted(Path('docs/codemine/runs/20261028-native-preflight').glob('*.md'))
checked = 0

def anchor(text):
    text = re.sub(r'[`*_]', '', text.lower())
    text = ''.join(c for c in text if c in ' -_' or unicodedata.category(c)[0] in 'LN')
    return text.replace(' ', '-')

for source in paths:
    content = source.read_text()
    content = re.sub(r'```.*?```', '', content, flags=re.S)
    for link in re.findall(r'\[[^\]]*\]\(([^)]+)\)', content):
        if re.match(r'[a-z]+://', link) or link.startswith('mailto:'):
            continue
        target, _, section = link.partition('#')
        dest = (source.parent / target) if target else source
        assert dest.exists(), (source, link)
        if section:
            headings = re.findall(r'^#+\s+(.+)', dest.read_text(), flags=re.M)
            assert section in {anchor(h) for h in headings}, (source, link)
        checked += 1
print(f'PASS: {checked} relative documentation links/anchors')

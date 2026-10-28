#!/usr/bin/env python3
"""Validate maintained feature links, anchors, documented flags and continuity."""
from pathlib import Path
import json
import re
import unicodedata

paths = [Path(p) for p in ['README.md', 'CLAUDE.md', 'docs/README.md', 'docs/NATIVE_EXECUTION_DIAGNOSTICS.md', 'docs/NATIVE_EXECUTION_PREFLIGHT.md', 'docs/OPERATIONS_READINESS.md', 'docs/analysis/open-items.md']]
paths += sorted(Path('docs/codemine/runs/20261028-native-diagnostics').glob('*.md'))
def anchor(text):
    text = re.sub(r'[`*_]', '', text.lower())
    return ''.join(c for c in text if c in ' -_' or unicodedata.category(c)[0] in 'LN').replace(' ', '-')
checked = 0
for source in paths:
    content = re.sub(r'```.*?```', '', source.read_text(), flags=re.S)
    for link in re.findall(r'\[[^\]]*\]\(([^)]+)\)', content):
        if re.match(r'[a-z]+://', link) or link.startswith('mailto:'):
            continue
        target, _, section = link.partition('#')
        dest = source.parent / target if target else source
        assert dest.exists(), (source, link)
        if section:
            headings = re.findall(r'^#+\s+(.+)', dest.read_text(), flags=re.M)
            assert section in {anchor(h) for h in headings}, (source, link)
        checked += 1
    assert 'file://' not in content
continuity = Path('docs/codemine/runs/20261028-native-diagnostics/continuity.json')
data = json.loads(continuity.read_text())
assert data['n_minus_1']['pr'] == 232 and data['n_minus_2']['pr'] == 230
for key in ['accepted_design', 'accepted_plan', 'e6', 'calls']:
    assert (continuity.parent / data[key]).exists()
code = Path('internal/nativediagnostics/diagnose.go').read_text()
guide = Path('docs/NATIVE_EXECUTION_DIAGNOSTICS.md').read_text()
assert set(re.findall(r'case "(--[a-z-]+)":', code)) == set(re.findall(r'`(--[a-z-]+)`', guide))
assert all(value in guide for value in ['not_authorized', 'not_performed', 'separate_samples_not_atomic_not_a_lease'])
print(f'PASS: {checked} relative links/anchors, all CLI flags, report limits and continuity')

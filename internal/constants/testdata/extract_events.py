#!/usr/bin/env python3
"""Extract canonical event signatures from go-stablenet system contract sources.

usage: extract_events.py <go-stablenet>/systemcontracts/solidity
Prints "<signature> <source file>" for every event declared in v1, v2,
abstracts and interfaces (OpenZeppelin and tests excluded), sorted.
"""
import re, sys, glob, os
root = sys.argv[1]
canon = {'uint': 'uint256', 'int': 'int256'}
out = set()
for f in glob.glob(os.path.join(root, '**', '*.sol'), recursive=True):
    rel = os.path.relpath(f, root)
    if rel.startswith(('openzeppelin', 'test')):
        continue
    src = re.sub(r'//[^\n]*|/\*.*?\*/', '', open(f).read(), flags=re.S)
    for m in re.finditer(r'\bevent\s+(\w+)\s*\(([^)]*)\)', src):
        name, params = m.group(1), m.group(2)
        types = []
        for p in [x.strip() for x in params.split(',') if x.strip()]:
            t = p.split()[0]
            types.append(canon.get(t, t))
        out.add((f"{name}({','.join(types)})", rel))
for sig, rel in sorted(out):
    print(sig, rel)

#!/usr/bin/env python3
"""Check product module isolation and unchanged owned runtime contract sources."""
import subprocess

for product, foreign in [('arc', 'stablenet'), ('stablenet', 'arc')]:
    deps = subprocess.check_output(['go', 'list', '-deps', '-f', '{{.ImportPath}}', './x/' + product + '/cmd/execution-diagnose'], text=True).splitlines()
    assert not any('/x/' + foreign + '/' in dep for dep in deps), (product, 'foreign chain import')
    assert not any('/x/stablenet/executor' in dep for dep in deps), 'native EVM fork imported into root module'
    print('PASS:', product, 'product dependency isolation')
protected = ['pkg/app', 'pkg/fetch', 'pkg/storage', 'pkg/execution', 'pkg/capture', 'pkg/window', 'pkg/chainfeed', 'internal/client', 'internal/rpcbatch', 'internal/rpcproxy', 'x/arc/executor', 'x/stablenet/executor/nodebackend', 'x/stablenet/executor/backend', 'go.mod', 'go.sum']
changed = subprocess.check_output(['git', 'diff', '--name-only', 'HEAD', '--', *protected], text=True)
assert not changed, 'unexpected runtime source changes: ' + changed
print('PASS: DB4/part2/API1/writer7/RPC5/final NEW fresh/E1-E6/upstream runtime source boundaries unchanged')

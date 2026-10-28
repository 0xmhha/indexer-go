#!/usr/bin/env bash
# Owned host-binary fixture; no Docker/image/node/DB operation.
set -euo pipefail
cd "$(dirname "$0")/../.."
fixture=$(mktemp -d "${TMPDIR:-/tmp}/indexer-native-diagnostic-cli.XXXXXX")
trap 'rm -rf -- "$fixture"' EXIT
mkdir "$fixture/bin"
for product in arc stablenet; do
  for command in execution-preflight execution-diagnose; do
    CGO_ENABLED=1 go build -trimpath -o "$fixture/bin/$product-$command" "./x/$product/cmd/$command"
  done
done
INDEXER_NATIVE_DIAGNOSTICS_TEST_BIN_DIR="$fixture/bin" python3 -B -m unittest discover -s deployments/native -p test_diagnostics.py -v
INDEXER_NATIVE_DIAGNOSTICS_TEST_BIN_DIR="$fixture/bin" python3 -B -m unittest discover -s deployments/native/runtime -p test_runtime.py -v

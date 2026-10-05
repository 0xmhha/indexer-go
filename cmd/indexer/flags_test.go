package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const baseConfig = `rpc:
  endpoint: "http://127.0.0.1:8545"
database:
  path: "/tmp/indexer-flags-test"
indexer:
  workers: 7
  chunk_size: 5
api:
  enabled: true
`

func configFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

func TestFlagsOnlyOverrideWhenGiven(t *testing.T) {
	path := configFile(t, baseConfig)

	f, err := parseFlagsFrom([]string{"--config", path}, flag.ContinueOnError)
	require.NoError(t, err)
	cfg, err := loadAndValidateConfig(f)
	require.NoError(t, err)
	require.Equal(t, 7, cfg.Indexer.Workers, "the --workers default must not replace the config value")
	require.Equal(t, 5, cfg.Indexer.ChunkSize)
	require.True(t, cfg.API.Enabled)

	f, err = parseFlagsFrom([]string{"--config", path, "--workers", "3", "--api=false"}, flag.ContinueOnError)
	require.NoError(t, err)
	cfg, err = loadAndValidateConfig(f)
	require.NoError(t, err)
	require.Equal(t, 3, cfg.Indexer.Workers)
	require.False(t, cfg.API.Enabled, "boolean flags must be able to switch features off")
}

func TestFlagsCanSupplyRequiredValues(t *testing.T) {
	// The file lacks the RPC endpoint; --rpc provides it. Validation must
	// run after flags are applied.
	path := configFile(t, "database:\n  path: \"/tmp/indexer-flags-test\"\n")
	f, err := parseFlagsFrom([]string{"--config", path, "--rpc", "http://127.0.0.1:8545"}, flag.ContinueOnError)
	require.NoError(t, err)
	cfg, err := loadAndValidateConfig(f)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:8545", cfg.RPC.Endpoint)
}

func TestDefaultConfigFileIsOptional(t *testing.T) {
	// No --config and no ./config.yaml (tests run in cmd/indexer).
	f, err := parseFlagsFrom([]string{"--rpc", "http://127.0.0.1:8545", "--db", t.TempDir()}, flag.ContinueOnError)
	require.NoError(t, err)
	_, err = loadAndValidateConfig(f)
	require.NoError(t, err)

	// An explicitly given config file must exist.
	f, err = parseFlagsFrom([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}, flag.ContinueOnError)
	require.NoError(t, err)
	_, err = loadAndValidateConfig(f)
	require.Error(t, err)
}

func TestStartupRejectsUnsafeModes(t *testing.T) {
	for name, body := range map[string]string{
		"multichain": baseConfig + "multichain:\n  enabled: true\n  chains:\n    - id: a\n      name: a\n      rpc_endpoint: \"http://127.0.0.1:8545\"\n      chain_id: 1\n      enabled: true\n",
		"readonly":   "rpc:\n  endpoint: \"http://127.0.0.1:8545\"\ndatabase:\n  path: \"/tmp/indexer-flags-test\"\n  readonly: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := configFile(t, body)
			f, err := parseFlagsFrom([]string{"--config", path}, flag.ContinueOnError)
			require.NoError(t, err)
			_, err = loadAndValidateConfig(f)
			require.Error(t, err)
			require.Contains(t, err.Error(), name)
		})
	}
}

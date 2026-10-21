package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/config"
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

func loadConfigFile(t *testing.T, body string) (*config.Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	cfg, err := config.LoadUnvalidated(p)
	require.NoError(t, err)
	return cfg, ValidateConfig(cfg)
}

// TestMultiChainConfigLoads: multichain mode starts from a file without a
// root rpc.endpoint (refactoring plan R2-8).
func TestMultiChainConfigLoads(t *testing.T) {
	cfg, err := loadConfigFile(t, "database:\n  path: \""+t.TempDir()+"\"\nmultichain:\n  enabled: true\n  chains:\n    - id: a\n      name: a\n      rpc_endpoint: \"http://127.0.0.1:8545\"\n      chain_id: 1\n      enabled: true\n")
	require.NoError(t, err)
	require.True(t, cfg.MultiChainMode())
}

func TestStartupRejectsUnsafeModes(t *testing.T) {
	for name, body := range map[string]string{
		// A chain id that would put its database outside database.path.
		"multichain": baseConfig + "multichain:\n  enabled: true\n  chains:\n    - id: ../a\n      name: a\n      rpc_endpoint: \"http://127.0.0.1:8545\"\n      chain_id: 1\n      enabled: true\n",
		"readonly":   "rpc:\n  endpoint: \"http://127.0.0.1:8545\"\ndatabase:\n  path: \"/tmp/indexer-flags-test\"\n  readonly: true\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadConfigFile(t, body)
			require.Error(t, err)
			require.Contains(t, err.Error(), name)
		})
	}
}

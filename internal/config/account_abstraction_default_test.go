package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

// TestAccountAbstractionDefault checks that AA indexing is on when the key is
// omitted and off only when a config file disables it explicitly.
func TestAccountAbstractionDefault(t *testing.T) {
	base := "rpc:\n  endpoint: \"http://127.0.0.1:8545\"\ndatabase:\n  path: \"/tmp/indexer-test\"\n"

	cfg, err := Load(writeConfig(t, base))
	require.NoError(t, err)
	require.True(t, cfg.AccountAbstraction.Enabled, "omitted key must default to enabled")

	cfg, err = Load(writeConfig(t, base+"account_abstraction:\n  enabled: false\n"))
	require.NoError(t, err)
	require.False(t, cfg.AccountAbstraction.Enabled, "explicit false must disable it")
}

// TestRemovedIngestPathsRejected: the legacy write path and the legacy
// client path were removed after v0.1.0. Leaving the settings out (or true)
// is accepted; setting them to false stops loading with an explanation.
func TestRemovedIngestPathsRejected(t *testing.T) {
	base := "rpc:\n  endpoint: \"http://127.0.0.1:8545\"\ndatabase:\n  path: \"/tmp/indexer-test\"\n"

	_, err := Load(writeConfig(t, base))
	require.NoError(t, err)
	_, err = Load(writeConfig(t, base+"indexer:\n  atomic_block: true\n  profile_source: true\n"))
	require.NoError(t, err)

	for _, setting := range []string{"atomic_block", "profile_source"} {
		_, err = Load(writeConfig(t, base+"indexer:\n  "+setting+": false\n"))
		require.ErrorContains(t, err, "removed after v0.1.0", setting)
	}

	t.Setenv("INDEXER_ATOMIC_BLOCK", "false")
	_, err = Load(writeConfig(t, base))
	require.ErrorContains(t, err, "removed after v0.1.0", "environment")
}

func TestUnsupportedSettings(t *testing.T) {
	cfg := NewConfig()
	require.Empty(t, cfg.UnsupportedSettings(), "defaults must not warn")

	cfg.EventBus.Type = "kafka"
	cfg.Watchlist.Enabled = true
	cfg.Resilience.Enabled = true
	cfg.AccountAbstraction.EntryPointAddresses = []string{"0x01"}
	require.Len(t, cfg.UnsupportedSettings(), 4)
}

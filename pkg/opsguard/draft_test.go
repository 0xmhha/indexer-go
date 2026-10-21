package opsguard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestTestnetDraftOperationsSection keeps the operations section of every
// chain's testnet configuration draft decodable, valid and the same.
func TestTestnetDraftOperationsSection(t *testing.T) {
	for _, chain := range []string{"stablenet-testnet", "bsc-testnet", "sepolia", "arc-testnet"} {
		t.Run(chain, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "docs", "examples", "testnets", chain+".draft.yaml"))
			require.NoError(t, err)
			// Other sections hold deliberate placeholders; only this one is decoded.
			var root map[string]yaml.Node
			require.NoError(t, yaml.Unmarshal(data, &root))
			node := root["operations"]
			var ops Config
			require.NoError(t, node.Decode(&ops))
			cfg := ops.WithDefaults()
			require.NoError(t, cfg.Validate())
			require.True(t, cfg.Enabled())
			require.Equal(t, int64(120)<<30, cfg.Capacity.StopFreeBytes)
			require.Equal(t, int64(160)<<30, cfg.Capacity.PostgresBudgetBytes)
		})
	}
}

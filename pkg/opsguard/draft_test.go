package opsguard

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestTestnetDraftOperationsSection keeps the operations section of the
// testnet configuration draft decodable and valid.
func TestTestnetDraftOperationsSection(t *testing.T) {
	data, err := os.ReadFile("../../docs/examples/config-testnets-windowed.draft.yaml")
	require.NoError(t, err)
	var doc struct {
		Operations Config `yaml:"operations"`
	}
	// Other sections hold deliberate placeholders; only this one is decoded.
	var root map[string]yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &root))
	node := root["operations"]
	require.NoError(t, node.Decode(&doc.Operations))
	cfg := doc.Operations.WithDefaults()
	require.NoError(t, cfg.Validate())
	require.True(t, cfg.Enabled())
	require.Equal(t, int64(120)<<30, cfg.Capacity.StopFreeBytes)
	require.Equal(t, int64(160)<<30, cfg.Capacity.PostgresBudgetBytes)
}

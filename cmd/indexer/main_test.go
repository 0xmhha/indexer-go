package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/0xmhha/indexer-go/x/stablenet"
)

// TestOtherProductsMatchTheirProfiles: the indexer refuses exactly the
// nodes the chain binaries' profiles detect.
func TestOtherProductsMatchTheirProfiles(t *testing.T) {
	detect := map[string]func(chains.NodeInfo) bool{
		"indexer-stablenet": stablenet.Detect,
		"indexer-arc":       arc.New().Detect,
	}
	nodes := []chains.NodeInfo{
		{ClientVersion: "Gstable/v1.1.0-stable/linux-amd64/go1.24", ChainID: 8283},
		{ClientVersion: "go-stablenet/v0.9.0", ChainID: 1},
		{ClientVersion: "StableOne/v0.1.0", ChainID: 2},
		{ClientVersion: "reth/v1.0.0", ChainID: 5042},
		{ClientVersion: "reth/v1.0.0", ChainID: 5042002},
		{ClientVersion: "Geth/v1.16.5-stable", ChainID: 11155111},
		{ClientVersion: "Geth/v1.16.5-stable", ChainID: 97},
		{ClientVersion: "anvil/v1.0.0", ChainID: 31337},
	}
	require.Len(t, otherProducts, len(detect))
	for _, p := range otherProducts {
		d, ok := detect[p.Binary]
		require.True(t, ok, p.Binary)
		for _, n := range nodes {
			require.Equal(t, d(n), p.Matches(n), "%s on %+v", p.Binary, n)
		}
	}
}

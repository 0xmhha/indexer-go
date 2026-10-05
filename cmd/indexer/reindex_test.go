package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/testchain"
)

// TestReindexClearsAllChainData indexes both scenarios (including undo
// records and a rollback's orphans) and reindexes: no key may remain, since
// the scenarios store no user data (contract verification).
func TestReindexClearsAllChainData(t *testing.T) {
	for name, chain := range map[string]*testchain.Chain{
		"evm":       testchain.BuildDefault().Chain,
		"stablenet": testchain.BuildStableNet().Chain,
	} {
		t.Run(name, func(t *testing.T) {
			srv := testchain.NewServer(chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")
			runSession(t, srv, dir, 0, chain.Head())

			require.NoError(t, reindexData(dir, zap.NewNop()))
			left, err := testchain.DumpKeyspace(dir, nil)
			require.NoError(t, err)
			var keys []string
			for _, e := range left {
				keys = append(keys, string(e.Key))
			}
			require.Empty(t, keys, "keys left after reindex")
		})
	}
}

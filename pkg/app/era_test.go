package app

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// eraConfig is the test configuration reading history from eraDir and the
// rest from the node at endpoint.
func eraConfig(t testing.TB, endpoint, eraDir, dbDir string) *config.Config {
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Source.EraDir = eraDir
	setTestDatabase(t, cfg, dbDir)
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
	return cfg
}

// TestEraSourceMatchesGolden writes the first part of the reference scenario
// as era1 files and indexes the whole chain: blocks in the archive must come
// from the files, the rest from the node, and the database must equal the
// golden (which was indexed from the node only).
func TestEraSourceMatchesGolden(t *testing.T) {
	golden := dumpScenarioIndex(t)
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	split := head / 2

	eraDir := t.TempDir()
	// Two files, as exports split history into eras.
	require.NoError(t, sc.Chain.WriteEra1(filepath.Join(eraDir, "test-00000-00000000.era1"), 0, split/2))
	require.NoError(t, sc.Chain.WriteEra1(filepath.Join(eraDir, "test-00001-00000000.era1"), split/2+1, split))

	dbDir := filepath.Join(t.TempDir(), "db")
	app, err := NewApp(eraConfig(t, srv.URL(), eraDir, dbDir), zap.NewNop(), false, "")
	require.NoError(t, err)
	srv.ResetBlockLoads() // startup checks read single hashes
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	app.Shutdown()

	loads := srv.BlockLoads()
	for n := uint64(0); n <= head; n++ {
		if n <= split {
			require.Zero(t, loads[n], "block %d is in the archive but was read from the node", n)
		} else {
			require.NotZero(t, loads[n], "block %d is after the archive and must come from the node", n)
		}
	}
	diff := testchain.DiffKeyspace(golden, dumpDir(t, dbDir), 0)
	require.Empty(t, diff, "era1 index differs from the node index: %v", testchain.SummarizeDiff(diff))
}

// TestEraSourceRejectsOtherChain refuses to start when the archive holds a
// different chain than the node.
func TestEraSourceRejectsOtherChain(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	other := testchain.NewChain(testchain.DefaultChainID, map[common.Address]*big.Int{{1}: big.NewInt(1)})
	for i := 0; i < 3; i++ {
		other.AddBlock()
	}
	eraDir := t.TempDir()
	require.NoError(t, other.WriteEra1(filepath.Join(eraDir, "other-00000-00000000.era1"), 0, 2))

	_, err := NewApp(eraConfig(t, srv.URL(), eraDir, filepath.Join(t.TempDir(), "db")), zap.NewNop(), false, "")
	require.ErrorIs(t, err, source.ErrDifferentChain)
}

package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestChainCheckRefusesTheNode: a binary's chain check sees what the node
// reports and the selected profile, and its refusal stops the start and
// releases the database.
func TestChainCheckRefusesTheNode(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	dir := filepath.Join(t.TempDir(), "db")
	setTestDatabase(t, cfg, dir)
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)

	refused := errors.New("not this chain")
	var seen chains.NodeInfo
	var profile string
	_, err := newApp(cfg, zap.NewNop(), StartOptions{CheckChain: func(info chains.NodeInfo, p chains.Profile) error {
		seen, profile = info, p.ID()
		return refused
	}})
	require.ErrorIs(t, err, refused)
	require.Equal(t, testchain.DefaultClientVersion, seen.ClientVersion)
	require.NotZero(t, seen.ChainID)
	require.Equal(t, "evm", profile)

	// The refused start released the database: the next one opens it.
	a, err := newApp(cfg, zap.NewNop(), StartOptions{CheckChain: func(chains.NodeInfo, chains.Profile) error { return nil }})
	require.NoError(t, err)
	a.Shutdown()
}

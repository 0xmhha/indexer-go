package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestMultiChainAppliesTheChainCheck: in multi-chain mode every chain of
// the process goes through the binary's chain check, so a process of one
// binary indexes only the chains that binary is built for. The refused
// chain reports why; the others index.
func TestMultiChainAppliesTheChainCheck(t *testing.T) {
	evm := testchain.BuildDefault()
	evmSrv := testchain.NewServer(evm.Chain)
	t.Cleanup(evmSrv.Close)
	sn := testchain.BuildStableNet()
	snSrv := testchain.NewServer(sn.Chain)
	t.Cleanup(snSrv.Close)

	other := chainEntry("other", snSrv.URL())
	other.ChainID = sn.Chain.ChainID().Uint64()
	cfg := multiChainConfig(t, filepath.Join(t.TempDir(), "db"), chainEntry("generic", evmSrv.URL()), other)
	cfg.API.Enabled = false
	refused := errors.New("built for another chain")
	app, err := newApp(cfg, zap.NewNop(), StartOptions{CheckChain: func(info chains.NodeInfo, _ chains.Profile) error {
		if info.ClientVersion == testchain.StableNetClientVersion {
			return refused
		}
		return nil
	}})
	require.NoError(t, err)
	defer app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	go func() { _ = app.Run(ctx) }()

	require.Eventually(t, func() bool {
		ci, err := app.multichainManager.GetChain("generic")
		if err != nil {
			return false
		}
		h, ok := ci.IndexedHeight(ctx)
		return ok && h == evm.Chain.Head()
	}, time.Minute, 20*time.Millisecond)
	ci, err := app.multichainManager.GetChain("other")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return strings.Contains(ci.HealthCheck(ctx).LastError, refused.Error())
	}, 10*time.Second, 20*time.Millisecond, "the refused chain reports the check")
	_, _, ok := app.multichainManager.ChainStore("other")
	require.False(t, ok)
}

package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	featuretoken "github.com/0xmhha/indexer-go/pkg/features/token"
	"github.com/0xmhha/indexer-go/pkg/rpcproxy"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestTokenBalancesReadMissingMetadataFromTheNode: with token.metadata off
// the store has no metadata of the scenario's ERC-20; GetTokenBalances
// reads it from the node through the RPC proxy once and keeps it.
func TestTokenBalancesReadMissingMetadataFromTheNode(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()

	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
	cfg.API.Enabled = true
	cfg.API.Host = "127.0.0.1"
	cfg.API.Port = freeAPIPort(t)
	cfg.API.EnableGraphQL = true
	cfg.API.EnableJSONRPC = false
	cfg.API.EnableWebSocket = false
	enableTestChainFeatures(cfg)
	off := false
	cfg.Features[featuretoken.MetadataName] = config.FeatureConfig{Enabled: &off}
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	_, err = app.storage.GetTokenMetadata(ctx, sc.ERC20)
	require.ErrorIs(t, err, port.ErrNotFound, "token.metadata is off")

	holder := sc.Accounts[1].Address
	erc20 := func() port.TokenBalance {
		balances, err := app.storage.GetTokenBalances(ctx, holder, "")
		require.NoError(t, err)
		for _, b := range balances {
			if b.ContractAddress == sc.ERC20 {
				return b
			}
		}
		t.Fatalf("no balance of %s", sc.ERC20.Hex())
		return port.TokenBalance{}
	}
	got := erc20()
	assert.Equal(t, "Test Token", got.Name)
	assert.Equal(t, "TT", got.Symbol)
	require.NotNil(t, got.Decimals)
	assert.Equal(t, 18, *got.Decimals)

	stored, err := app.storage.GetTokenMetadata(ctx, sc.ERC20)
	require.NoError(t, err)
	assert.Equal(t, "Test Token", stored.Name)

	calls := srv.Calls()["eth_call"]
	assert.Equal(t, "TT", erc20().Symbol)
	assert.Equal(t, calls, srv.Calls()["eth_call"], "the second request reads the stored metadata")
}

// TestTokenMetadataWithUnansweredCallsIsNotKept: when the proxy refuses a
// node call (rate limit), the fetcher returns the error instead of partial
// metadata the store would keep for good.
func TestTokenMetadataWithUnansweredCallsIsNotKept(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	rc, err := rpc.Dial(srv.URL())
	require.NoError(t, err)
	defer rc.Close()
	ctx := context.Background()

	newFetcher := func(burst int) *proxyTokenMetadata {
		cfg := rpcproxy.DefaultConfig()
		cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.BurstSize = 0.001, burst
		p := rpcproxy.NewProxy(ethclient.NewClient(rc), rc, nil, cfg, zap.NewNop())
		t.Cleanup(func() { _ = p.Stop() })
		return &proxyTokenMetadata{proxy: p, logger: zap.NewNop()}
	}

	md, err := newFetcher(3).FetchTokenMetadata(ctx, sc.ERC20)
	assert.ErrorIs(t, err, rpcproxy.ErrRateLimited, "code and two calls fit; name, symbol, ... do not")
	assert.Nil(t, md)

	md, err = newFetcher(100).FetchTokenMetadata(ctx, sc.ERC20)
	require.NoError(t, err)
	assert.Equal(t, "Test Token", md.Name)
	assert.Equal(t, uint8(18), md.Decimals)
}

package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/stablenet/wbft"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// failingFeature fails on one block, to check that feature handlers run
// inside the block transaction.
type failingFeature struct{}

const failingFeatureName = "test.fail_at_block_3"

var errFeatureFailed = errors.New("feature failed")

func (failingFeature) Name() string       { return failingFeatureName }
func (failingFeature) Requires() []string { return nil }
func (failingFeature) Register(r feature.Registrar) error {
	r.OnBlock(feature.BlockHandlerFunc(func(_ context.Context, b *feature.Block) error {
		if b.Model.Number == 3 {
			return errFeatureFailed
		}
		return nil
	}))
	return nil
}

func init() { feature.Register(failingFeature{}) }

func startAppFeatures(t *testing.T, srv *testchain.Server, dir string, features map[string]bool) (*App, error) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = dir
	cfg.API.Enabled = false
	cfg.Features = map[string]config.FeatureConfig{}
	for name, on := range features {
		on := on
		cfg.Features[name] = config.FeatureConfig{Enabled: &on}
	}
	return NewApp(cfg, zap.NewNop(), false, "")
}

func TestUnknownFeatureStopsStartup(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	_, err := startAppFeatures(t, srv, filepath.Join(t.TempDir(), "db"), map[string]bool{"no.such_feature": true})
	require.ErrorContains(t, err, "not registered")
}

// TestFeatureFailureAbortsBlock requires a failing feature handler to leave
// no trace of its block: it runs in the block's storage transaction.
func TestFeatureFailureAbortsBlock(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	app, err := startAppFeatures(t, srv, filepath.Join(t.TempDir(), "db"), map[string]bool{failingFeatureName: true})
	require.NoError(t, err)
	defer app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	err = app.fetcher.FetchRange(ctx, 0, sc.Chain.Head())
	require.ErrorIs(t, err, errFeatureFailed)

	head, err := app.storage.GetLatestHeight(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), head)
	_, err = app.storage.GetBlock(ctx, 3)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

// TestWBFTFeatureOnNonWBFTChain enables stablenet.wbft on the test chain,
// whose headers are not WBFT: the feature skips them and the stored data is
// exactly the golden.
func TestWBFTFeatureOnNonWBFTChain(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	app, err := startAppFeatures(t, srv, dir, map[string]bool{wbft.Name: true})
	require.NoError(t, err)
	require.Equal(t, []string{wbft.Name}, app.fetcherFeatures())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	app.Shutdown()

	diff := testchain.DiffKeyspace(dumpScenarioIndex(t), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

func (a *App) fetcherFeatures() []string { return a.features.Features() }

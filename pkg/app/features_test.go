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

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/features/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/features/wbft"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/features/aa"
	"github.com/0xmhha/indexer-go/pkg/features/address"
	"github.com/0xmhha/indexer-go/pkg/features/balance"
	"github.com/0xmhha/indexer-go/pkg/features/token"
	"github.com/0xmhha/indexer-go/pkg/testchain"
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
	setTestDatabase(t, cfg, dir)
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
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
	require.ErrorIs(t, err, port.ErrNotFound)
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
	require.Contains(t, app.fetcherFeatures(), wbft.Name)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	app.Shutdown()

	// Apart from its own state key, the feature leaves no trace.
	diff := testchain.DiffKeyspace(dumpScenarioIndex(t), excludeEntries(dumpDir(t, dir), featureStateKey(wbft.Name)), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

func (a *App) fetcherFeatures() []string { return a.features.Features() }

// featureKeys lists, per feature, the key prefixes only that feature
// writes. Turning a feature off must remove exactly its keys and leave every
// other key and value of the reference scenario unchanged, so features can
// be enabled independently (and, later, backfilled).
var featureKeys = map[string][]string{
	address.Name:         {"/index/addr/", "/data/contract/", "/index/contract/"},
	balance.Name:         {"/index/balance/"},
	token.TransfersName:  {"/data/erc20/", "/index/erc20/", "/data/erc721/", "/index/erc721/"},
	aa.EIP7702:           {"/data/setcode/", "/index/setcode/"},
	aa.ERC4337:           {"/data/userop/", "/index/userop/", "/data/bundler/", "/data/smartaccount/"},
	aa.ERC7579:           {"/data/module/", "/index/module/"},
	systemcontracts.Name: {"/data/syscontracts/", "/index/syscontracts/"},
}

func TestFeatureOffRemovesOnlyItsKeys(t *testing.T) {
	with := dumpScenarioIndex(t)
	for name := range featureKeys {
		prefixes := featureKeyPrefixes(name)
		t.Run(name, func(t *testing.T) {
			sc := testchain.BuildDefault()
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")
			app, err := startAppFeatures(t, srv, dir, map[string]bool{name: false})
			require.NoError(t, err)
			require.NotContains(t, app.fetcherFeatures(), name)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
			app.Shutdown()

			n := 0
			for _, p := range prefixes {
				n += prefixCount(with, p)
			}
			require.Positive(t, n, "the scenario exercises %s", name)
			without := dumpDir(t, dir)
			// The feature's state key is absent too: it never ran.
			ignored := append([]string{featureStateKey(name)}, prefixes...)
			diff := testchain.DiffKeyspace(excludeEntries(with, ignored...), without, 0)
			require.Empty(t, diff, testchain.SummarizeDiff(diff))
		})
	}
}

func excludeEntries(es []testchain.Entry, prefixes ...string) []testchain.Entry {
	var out []testchain.Entry
next:
	for _, e := range es {
		for _, p := range prefixes {
			if strings.HasPrefix(string(e.Key), p) {
				continue next
			}
		}
		out = append(out, e)
	}
	return out
}

package main

import (
	"context"
	"errors"
	"math/big"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	fdmeta "github.com/0xmhha/indexer-go/pkg/chains/stablenet/feedelegation"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
)

// onlineFeature is an order-independent test feature: it records every
// transaction under its hash (as fee delegation metadata, the store at hand),
// so blocks can be processed in any order. failAt makes it fail on one block.
type onlineFeature struct{}

const onlineFeatureName = "test.online_marker"

var onlineFailAt atomic.Int64 // block height to fail at; -1 = never

func init() {
	onlineFailAt.Store(-1)
	feature.Register(onlineFeature{})
}

func (onlineFeature) Name() string           { return onlineFeatureName }
func (onlineFeature) Requires() []string     { return nil }
func (onlineFeature) OrderIndependent() bool { return true }
func (onlineFeature) Register(r feature.Registrar) error {
	w, err := fdmeta.OpenMetaStore(r.Deps().Storage)
	if err != nil {
		return err
	}
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		if int64(b.Model.Number) == onlineFailAt.Load() {
			return errors.New("injected failure")
		}
		for _, tx := range b.Model.Transactions {
			if err := w.SetTxMeta(ctx, &fdmeta.TxMeta{
				TxHash: tx.Hash, BlockNumber: b.Model.Number, OriginalType: tx.Type, FeePayer: tx.From,
				FeePayerV: big.NewInt(0), FeePayerR: big.NewInt(0), FeePayerS: big.NewInt(0),
			}); err != nil {
				return err
			}
		}
		return nil
	}))
	return nil
}

// extendChain adds n blocks with one transfer each.
func extendChain(sc *testchain.Scenario, n int) {
	a, b := sc.Accounts[1], sc.Accounts[2]
	for i := 0; i < n; i++ {
		sc.Chain.AddBlock(testchain.TxSpec{From: a, Tx: &types.LegacyTx{To: &b.Address, Value: big.NewInt(int64(7 + i)), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)}})
	}
}

func featureState(t *testing.T, app *App, name string) port.FeatureState {
	t.Helper()
	states, err := app.storage.(port.FeatureStateStore).FeatureStates(context.Background())
	require.NoError(t, err)
	return states[name]
}

// TestOnlineBackfill enables an order-independent feature on an indexed
// database: ingest continues while the feature fills the blocks it missed in
// the background, and the result equals a database that had the feature
// from the start.
func TestOnlineBackfill(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	dir := filepath.Join(t.TempDir(), "db")
	indexWithFeatures(t, srv, dir, 0, head, nil) // feature not enabled

	app, err := startAppFeatures(t, srv, dir, map[string]bool{onlineFeatureName: true})
	require.NoError(t, err)
	st := featureState(t, app, onlineFeatureName)
	require.True(t, st.Active, "processes new blocks at once")

	extendChain(sc, 4) // ingest goes on during the backfill
	newHead := sc.Chain.Head()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, head+1, newHead))
	app.fetcher.WaitBackground()
	require.Nil(t, featureState(t, app, onlineFeatureName).Gap, "gap filled")
	app.Shutdown()

	fresh := filepath.Join(t.TempDir(), "fresh")
	indexWithFeatures(t, srv, fresh, 0, newHead, map[string]bool{onlineFeatureName: true})
	diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

// TestOnlineBackfillResumes interrupts an online backfill at one block (it
// keeps retrying until shutdown) and restarts: the backfill resumes from that
// block and completes.
func TestOnlineBackfillResumes(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	dir := filepath.Join(t.TempDir(), "db")
	indexWithFeatures(t, srv, dir, 0, head, nil)

	onlineFailAt.Store(5)
	app, err := startAppFeatures(t, srv, dir, map[string]bool{onlineFeatureName: true})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		gap := featureState(t, app, onlineFeatureName).Gap
		return gap != nil && gap.From == 5
	}, 10*time.Second, 10*time.Millisecond, "stuck at the failing block")
	app.Shutdown() // stops the retrying backfill
	onlineFailAt.Store(-1)

	app, err = startAppFeatures(t, srv, dir, map[string]bool{onlineFeatureName: true})
	require.NoError(t, err)
	app.fetcher.WaitBackground()
	require.Nil(t, featureState(t, app, onlineFeatureName).Gap)
	app.Shutdown()

	fresh := filepath.Join(t.TempDir(), "fresh")
	indexWithFeatures(t, srv, fresh, 0, head, map[string]bool{onlineFeatureName: true})
	diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

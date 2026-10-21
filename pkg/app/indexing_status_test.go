package app

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	statusapi "github.com/0xmhha/indexer-go/pkg/api/indexing"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/progress"
	"github.com/0xmhha/indexer-go/pkg/projection"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestManagedFeedIndependentWorkerAndStatus(t *testing.T) {
	a1, a2 := testchain.NewAccount(1), testchain.NewAccount(2)
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{a1.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	for i := 0; i < 2; i++ {
		c.AddBlock(testchain.TxSpec{From: a1, Tx: &types.DynamicFeeTx{To: &a2.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(10)}, GasUsed: 21000, Failed: i == 1, Logs: []*types.Log{{Address: a2.Address, Topics: []common.Hash{{4}}, Data: []byte("fixture")}}})
	}
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.Projections = []string{projection.Summary}
	cfg.API.Enabled = true
	cfg.Indexer.PipelineMetrics = true
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	initial, e := indexingstatus.Read(t.Context(), a.storage, "", 100)
	require.NoError(t, e)
	require.Nil(t, initial.Source.Through)
	require.Nil(t, initial.Projection)
	require.Equal(t, "not_started", initial.Workers[0].Status)
	cancel, done := runFeedApp(t, a)
	var got indexingstatus.Status
	require.Eventually(t, func() bool {
		got, e = indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && len(got.Workers) == 1 && got.Workers[0].Complete != nil && got.Workers[0].Complete.Height == 2 && got.Projection != nil && got.Projection.Height == 2
	}, 25*time.Second, 20*time.Millisecond)
	require.True(t, got.ProjectionCanonical)
	require.Equal(t, "captured_facts", got.Mode)
	require.NotNil(t, got.Source.BlobCapability)
	require.Equal(t, "unsupported", got.Source.BlobCapability.Status)
	for _, u := range got.Units {
		require.Equal(t, "caught_up", u.Status, u.Name)
		require.EqualValues(t, 2, u.Complete.Height)
	}
	for _, d := range got.Source.Datasets {
		if d.Kind == "account_changes" || d.Kind == "storage_changes" || d.Kind == "traces" || d.Kind == "blobs" {
			require.Equal(t, "unavailable", d.Coverage)
		}
	}
	id, e := a.managedFeed.store.CanonicalAt(t.Context(), got.Source.Generation, 2)
	require.NoError(t, e)
	output, e := a.managedFeed.store.WorkerOutput(t.Context(), projection.Summary, got.Workers[0].Definition, id)
	require.NoError(t, e)
	var summary projection.SummaryOutput
	require.NoError(t, json.Unmarshal(output.Data, &summary))
	require.Equal(t, 1, summary.Transactions)
	require.Equal(t, 1, summary.Receipts)
	require.Equal(t, 1, summary.Failed)
	require.EqualValues(t, 21000, summary.GasUsed)
	// Stop peer/projection activity, then prove status itself sends no RPC.
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	before := rpc.Calls()
	require.Positive(t, a.fetcher.GetPipelineMetrics().FeatureProgress.Count)
	if cfg.Database.Driver == "postgres" {
		readonly, e := openStore(t.Context(), &cfg.Database, true, nil, zap.NewNop())
		require.NoError(t, e)
		view, e := indexingstatus.Read(t.Context(), readonly, "", 100)
		require.NoError(t, e)
		require.Equal(t, got.Source.Generation, view.Source.Generation)
		require.Equal(t, got.Source.BlobCapability, view.Source.BlobCapability)
		output, e := indexingstatus.ReadOutput(t.Context(), readonly, projection.Summary, 2)
		require.NoError(t, e)
		require.Equal(t, id, output.Output.Identity)
		require.NoError(t, readonly.Close())
	}

	handler := statusapi.Handler(a.storage, zap.NewNop())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", statusapi.Path+"?limit=2", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, before, rpc.Calls())
	mounted := httptest.NewRecorder()
	a.apiServer.Router().ServeHTTP(mounted, httptest.NewRequest("GET", "/indexing/workers/"+projection.Summary+"/blocks/2", nil))
	require.Equal(t, 200, mounted.Code)
	var published indexingstatus.WorkerResult
	require.NoError(t, json.Unmarshal(mounted.Body.Bytes(), &published))
	require.Equal(t, id, published.Output.Identity)
	for _, tc := range []struct {
		suffix string
		code   int
	}{{"/blocks/999", 404}, {"/blocks/bad", 400}} {
		mounted = httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(mounted, httptest.NewRequest("GET", "/indexing/workers/"+projection.Summary+tc.suffix, nil))
		require.Equal(t, tc.code, mounted.Code)
	}
	require.Equal(t, before, rpc.Calls(), "mounted output API is DB-only")

	for _, query := range []string{"?limit=101", "?after=%00"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", statusapi.Path+query, nil))
		require.Equal(t, 400, w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", statusapi.Path+"?after="+url.QueryEscape("a'; DROP TABLE kv; --"), nil))
	require.Equal(t, 200, w.Code)
	kv := a.storage.(port.KVStore)
	saved, e := kv.Get(t.Context(), []byte(progress.HeadKey))
	require.NoError(t, e)
	require.NoError(t, kv.Put(t.Context(), []byte(progress.HeadKey), []byte("broken")))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", statusapi.Path, nil))
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), "broken")
	require.NoError(t, kv.Put(t.Context(), []byte(progress.HeadKey), saved))
	a.Shutdown()
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done = runFeedApp(t, a)
	addFeedBlocks(c, 1)
	require.Eventually(t, func() bool {
		got, e = indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && got.Workers[0].Complete != nil && got.Workers[0].Complete.Height == 3 && got.Projection != nil && got.Projection.Height == 3
	}, 25*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
func TestManagedFeedFeatureProgressRollsBackOnFailure(t *testing.T) {
	cfg, _, _ := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	injected := errors.New("injected projection commit failure")
	var failed atomic.Int32
	a.fetcher.SetBeforeCommitHook(func(uint64) error { failed.Add(1); return injected })
	cancel, done := runFeedApp(t, a)
	require.Eventually(t, func() bool {
		st, e := a.managedFeed.store.State(context.Background())
		return e == nil && st.Through.Number == 2
	}, 15*time.Second, 20*time.Millisecond)
	// The hook executes after completion records were staged in the transaction.
	require.Eventually(t, func() bool { return failed.Load() > 0 }, 3*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	_, e = a.storage.(port.KVStore).Get(t.Context(), []byte(progress.HeadKey))
	require.ErrorIs(t, e, port.ErrNotFound)
	page, e := a.storage.(port.IndexingReader).ReadIndexingPage(t.Context(), "", 100)
	require.NoError(t, e)
	for _, u := range page.Units {
		require.Nil(t, u.Checkpoint)
	}
}

func TestManagedFeedIndependentWorkerReorg(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long: runs a managed feed through a reorg")
	}
	cfg, c, _ := feedAppConfig(t)
	cfg.Source.Feed.Projections = []string{projection.Summary}
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	var got indexingstatus.Status
	require.Eventually(t, func() bool {
		got, e = indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && got.Workers[0].Complete != nil && got.Workers[0].Complete.Height == 2 && got.Projection != nil && got.Projection.Height == 2
	}, 25*time.Second, 20*time.Millisecond)
	c.Reorg(0)
	c.AddBlockWithExtra([]byte("replacement"))
	c.AddBlock()
	c.SetHead(2)
	require.Eventually(t, func() bool {
		got, e = indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && got.Source.Generation == 2 && got.Workers[0].Epoch == 2 && got.Workers[0].Complete != nil && got.Workers[0].Complete.Hash == c.Block(2).Block.Hash() && got.Projection != nil && got.Projection.Hash == c.Block(2).Block.Hash()
	}, 25*time.Second, 20*time.Millisecond)
	for _, u := range got.Units {
		require.EqualValues(t, 2, u.Epoch)
		require.Equal(t, "caught_up", u.Status, u.Name)
	}
	output, e := indexingstatus.ReadOutput(t.Context(), a.storage, projection.Summary, 1)
	require.NoError(t, e)
	require.Equal(t, c.Block(1).Block.Hash(), output.Output.Identity.Block.Hash)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

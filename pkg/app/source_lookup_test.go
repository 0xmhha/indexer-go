package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	statusapi "github.com/0xmhha/indexer-go/pkg/api/indexing"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestManagedSourceLookupRestartReorgAndReadonlyAPI(t *testing.T) {
	from, to := testchain.NewAccount(1), testchain.NewAccount(2)
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{from.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	for i := 0; i < 2; i++ {
		c.AddBlock(testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(10)}, GasUsed: 21000, Logs: []*types.Log{{Address: to.Address, Topics: []common.Hash{{4}}, Data: []byte("first")}, {Address: to.Address, Data: []byte("second")}}}, testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(20)}, GasUsed: 21000, Logs: []*types.Log{{Address: to.Address, Data: []byte("other")}}})
	}
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.Projections = []string{model.SourceLookupWorker}
	cfg.Source.Feed.Telemetry = &feed.TelemetryConfig{ReceiveIntervalTarget: 450 * time.Millisecond}
	cfg.API.Enabled = true
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	waitIndexed := func(epoch, height uint64) {
		t.Helper()
		var appErr error
		var lastStatus indexingstatus.Status
		var lastReadErr error
		ok := assert.Eventually(t, func() bool {
			select {
			case appErr = <-done:
			default:
			}
			st, err := indexingstatus.Read(context.Background(), a.storage, "", 100)
			lastStatus, lastReadErr = st, err
			return appErr == nil && err == nil && st.Source.Generation == epoch && len(st.Workers) == 1 && st.Workers[0].Epoch == epoch && st.Workers[0].Complete != nil && st.Workers[0].Complete.Height == height
		}, 25*time.Second, 20*time.Millisecond)
		if !ok {
			t.Logf("last source status: %+v / read error: %v", lastStatus, lastReadErr)
			require.NoError(t, appErr, "feed exited before requested indexed prefix")
			t.FailNow()
		}
	}
	waitIndexed(1, 2)
	require.Eventually(t, func() bool {
		st, err := indexingstatus.Read(context.Background(), a.storage, "", 100)
		return err == nil && st.Pipeline != nil && st.Pipeline.Runtime.Metrics["lookupVerify"].Count == 2 && st.Pipeline.Runtime.Metrics["lookupCommit"].Count == 2
	}, time.Second, time.Millisecond)
	oldBlock := c.Block(2).Block.Hash()
	txHash := c.Block(1).Block.Transactions()[0].Hash()
	oldTx := c.Block(2).Block.Transactions()[0].Hash()
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	// Source APIs must succeed when the peer/worker loops are stopped and the
	// public RPC fails. No query may attempt a fallback request or lazy write.
	rawPath := cfg.WindowStorage.Windows["arc"].Path
	offline := rawPath + ".offline"
	require.NoError(t, os.Rename(rawPath, offline))
	t.Cleanup(func() {
		if _, err := os.Stat(offline); err == nil {
			_ = os.Rename(offline, rawPath)
		}
	})
	rpc.FailMethod("eth_getBlockByNumber", 10000)
	before := rpc.Calls()
	read := func(path string) indexingstatus.SourcePage {
		t.Helper()
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var out indexingstatus.SourcePage
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	first := read("/indexing/source/transactions/" + txHash.Hex() + "/log?limit=1")
	require.True(t, first.Found)
	require.False(t, first.Done)
	require.Len(t, first.Rows, 1)
	pin := fmt.Sprintf("&epoch=%d&hash=%s&definition=%s", first.Scope.Epoch, first.Scope.Hash.Hex(), url.QueryEscape(first.Scope.Definition))
	second := read("/indexing/source/transactions/" + txHash.Hex() + "/log?limit=1&after=1" + pin)
	require.True(t, second.Done)
	require.EqualValues(t, 1, second.Rows[0].Index)
	other := read("/indexing/source/transactions/" + c.Block(1).Block.Transactions()[1].Hash().Hex() + "/log")
	require.Len(t, other.Rows, 1)
	require.EqualValues(t, 2, other.Rows[0].Index)
	byHeight := read("/indexing/source/blocks/1/transaction")
	byHash := read("/indexing/source/blocks/by-hash/" + c.Block(1).Block.Hash().Hex() + "/transaction")
	require.Equal(t, byHeight.Rows, byHash.Rows)
	receipt := read("/indexing/source/transactions/" + txHash.Hex() + "/receipt")
	require.Equal(t, capture.RowCodec(capture.RowReceipt), receipt.Codec)
	require.Len(t, receipt.Rows, 1)
	decoded, e := model.DecodeReceipt(receipt.Rows[0].Value)
	require.NoError(t, e)
	require.Equal(t, txHash, decoded.TxHash)
	missing := read("/indexing/source/transactions/" + common.Hash{99}.Hex() + "/transaction")
	require.False(t, missing.Found)
	require.EqualValues(t, 2, missing.Through.Number)
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/indexing/source/blocks/999/header", 404},
		{"/indexing/source/blocks/1/balance", 400},
		{"/indexing/source/blocks/1/log?after=1", 400},
		{"/indexing/source/blocks/1/header?limit=257", 400},
		{"/indexing/source/transactions/bad/transaction", 400},
		{"/indexing/source/blocks/1/header?epoch=9&hash=" + first.Scope.Hash.Hex() + "&definition=" + url.QueryEscape(first.Scope.Definition), 409},
		{"/indexing/source/blocks/1/header?epoch=1&hash=" + first.Scope.Hash.Hex() + "&definition=" + url.QueryEscape("x'; DROP TABLE capture_records; --"), 409},
	} {
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		require.Equal(t, tc.status, w.Code, w.Body.String())
	}
	if cfg.Database.Driver == "postgres" {
		ro, e := openStore(t.Context(), &cfg.Database, true, nil, zap.NewNop())
		require.NoError(t, e)
		q := indexingstatus.SourceRequest{Hash: txHash, Transaction: true, Kind: capture.RowTransaction, Limit: 1}
		out, e := indexingstatus.ReadSource(t.Context(), ro, q)
		require.NoError(t, e)
		require.True(t, out.Found)
		require.NoError(t, ro.Close())
	}
	require.Equal(t, before, rpc.Calls(), "all source reads are DB-only")
	rpc.FailMethod("eth_getBlockByNumber", 0)
	require.NoError(t, os.Rename(offline, rawPath))
	a.Shutdown()
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done = runFeedApp(t, a)
	waitIndexed(1, 2)
	addFeedBlocks(c, 1)
	waitIndexed(1, 3)
	waitFeedProjection(t, a, c, 3)
	_, e = indexingstatus.ReadSource(t.Context(), a.storage, indexingstatus.SourceRequest{Hash: txHash, Transaction: true, Kind: capture.RowLog, After: 1, Limit: 1, Expected: first.Scope})
	require.NoError(t, e, "same-epoch root advancement must not invalidate a block page")
	c.Reorg(1)
	c.AddBlockWithExtra([]byte("replacement"))
	c.SetHead(2)
	waitIndexed(2, 2)
	waitFeedProjection(t, a, c, 2)
	current, e := indexingstatus.ReadSource(t.Context(), a.storage, indexingstatus.SourceRequest{Hash: oldBlock, Kind: capture.RowHeader, Limit: 1})
	require.NoError(t, e)
	require.False(t, current.Found)
	current, e = indexingstatus.ReadSource(t.Context(), a.storage, indexingstatus.SourceRequest{Hash: oldTx, Transaction: true, Kind: capture.RowTransaction, Limit: 1})
	require.NoError(t, e)
	require.False(t, current.Found)
	current, e = indexingstatus.ReadSource(t.Context(), a.storage, indexingstatus.SourceRequest{Hash: txHash, Transaction: true, Kind: capture.RowTransaction, Limit: 1})
	require.NoError(t, e)
	require.True(t, current.Found, "retained ancestor remains searchable")
	_, e = indexingstatus.ReadSource(t.Context(), a.storage, indexingstatus.SourceRequest{Hash: txHash, Transaction: true, Kind: capture.RowLog, After: 1, Limit: 1, Expected: first.Scope})
	require.ErrorIs(t, e, capture.ErrStale)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	// Simulate a process stopping after a durable reset but before projection
	// reconciliation. The missing old tip must also recover during NewApp.
	ancestor, e := a.managedFeed.store.CanonicalAt(t.Context(), 2, 1)
	require.NoError(t, e)
	c.Reorg(1)
	require.NoError(t, a.managedFeed.store.Reset(t.Context(), 2, ancestor.Block))
	a.Shutdown()
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done = runFeedApp(t, a)
	waitIndexed(3, 1)
	waitFeedProjection(t, a, c, 1)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestManagedSourceLookupOptIn(t *testing.T) {
	cfg, _, _ := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	t.Cleanup(a.Shutdown)
	w := httptest.NewRecorder()
	statusapi.SourceHandler(a.storage, zap.NewNop()).ServeHTTP(w, httptest.NewRequest("GET", "/indexing/source/blocks/1/header", nil))
	require.Equal(t, 503, w.Code)
}

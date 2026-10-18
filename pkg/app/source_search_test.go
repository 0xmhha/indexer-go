package app

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/0xmhha/indexer-go/pkg/indexingstatus"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestManagedSourceSearchRestartReorgAndReadonlyAPI(t *testing.T) {
	from, to := testchain.NewAccount(1), testchain.NewAccount(2)
	other := testchain.NewAccount(3)
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{from.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	topic := common.Hash{4}
	second := common.Hash{5}
	zero := common.Hash{}
	for i := 0; i < 2; i++ {
		c.AddBlock(testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(10)}, GasUsed: 21000, Logs: []*types.Log{{Address: to.Address, Topics: []common.Hash{topic, zero}, Data: []byte("first")}, {Address: other.Address, Topics: []common.Hash{topic, second}, Data: []byte("other")}, {Address: to.Address, Topics: []common.Hash{topic, second}, Data: []byte("last")}}})
	}
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.Projections = []string{model.SourceSearchWorker}
	cfg.API.Enabled = true
	cfg.Source.Feed.Telemetry = &feed.TelemetryConfig{ReceiveIntervalTarget: 450 * time.Millisecond}
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	wait := func(epoch, height uint64) {
		t.Helper()
		require.Eventually(t, func() bool {
			s, e := indexingstatus.Read(context.Background(), a.storage, "", 100)
			return e == nil && s.Source.Generation == epoch && len(s.Workers) == 1 && s.Workers[0].Epoch == epoch && s.Workers[0].Complete != nil && s.Workers[0].Complete.Height == height
		}, 25*time.Second, 20*time.Millisecond)
	}
	wait(1, 2)
	waitFeedProjection(t, a, c, 2)
	require.Eventually(t, func() bool {
		s, e := indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && s.Source.SourceSearch != nil && s.Pipeline.Runtime.Metrics["searchCommit"].Count == 2 && s.Pipeline.Runtime.Metrics["searchVerify"].Count == 2
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	raw := cfg.WindowStorage.Windows["arc"].Path
	offline := raw + ".offline"
	require.NoError(t, os.Rename(raw, offline))
	t.Cleanup(func() {
		if _, e := os.Stat(offline); e == nil {
			_ = os.Rename(offline, raw)
		}
	})
	rpc.FailMethod("eth_getBlockByNumber", 10000)
	before := rpc.Calls()
	read := func(path string) indexingstatus.SourceSearchPage {
		t.Helper()
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var out indexingstatus.SourceSearchPage
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	path := "/indexing/source/logs?from=1&to=2&address=" + to.Address.Hex() + "&topic0=" + topic.Hex() + "&topic1=" + second.Hex() + "&limit=1"
	first := read(path)
	require.Len(t, first.Rows, 1)
	require.EqualValues(t, 1, first.Rows[0].Identity.Block.Number)
	require.EqualValues(t, 2, first.Rows[0].Row.Index)
	require.NotEmpty(t, first.Next)
	next := read(path + "&after=" + url.QueryEscape(first.Next))
	require.Len(t, next.Rows, 1)
	require.EqualValues(t, 2, next.Rows[0].Identity.Block.Number)
	require.True(t, next.Done)
	require.Empty(t, next.Next)
	zeroPage := read("/indexing/source/logs?from=1&to=2&topic1=" + zero.Hex())
	require.Len(t, zeroPage.Rows, 2)
	emitted := read("/indexing/source/logs?from=1&to=2&address=" + other.Address.Hex())
	require.Len(t, emitted.Rows, 2)
	txs := read("/indexing/source/transactions?from=1&to=2&address=" + to.Address.Hex() + "&role=to")
	require.Len(t, txs.Rows, 2)
	tx, e := model.DecodeTransaction(txs.Rows[0].Row.Value)
	require.NoError(t, e)
	require.Equal(t, to.Address, *tx.To)
	outgoing := read("/indexing/source/transactions?from=1&to=2&address=" + to.Address.Hex() + "&role=from")
	require.Empty(t, outgoing.Rows)
	require.True(t, outgoing.Done)
	missing := read("/indexing/source/logs?from=1&to=2&topic0=" + common.Hash{99}.Hex())
	require.Empty(t, missing.Rows)
	require.True(t, missing.Done)
	q := capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 2, Address: &to.Address, Topics: [4]*common.Hash{&topic, &second}}
	if cfg.Database.Driver == "postgres" {
		ro, e := openStore(t.Context(), &cfg.Database, true, nil, zap.NewNop())
		require.NoError(t, e)
		got, e := indexingstatus.ReadSearch(t.Context(), ro, q, "", 1)
		require.NoError(t, e)
		require.Len(t, got.Rows, 1)
		at := uint64(1700000004)
		got, e = indexingstatus.ReadSearch(t.Context(), ro, capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 2, Addresses: []common.Address{to.Address, other.Address}, TopicAny: [][]common.Hash{nil, {second, zero}, nil, nil}, FromTime: &at, ToTime: &at}, "", 256)
		require.NoError(t, e)
		require.Len(t, got.Rows, 3, "OR/time reads also work on a read-only pool-one handle")
		require.NoError(t, ro.Close())
	}
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/indexing/source/logs?from=1&to=99", 404},
		{"/indexing/source/logs?from=2&to=1", 400},
		{"/indexing/source/logs?from=0&to=2", 400},
		{"/indexing/source/logs?from=1&to=2&address=bad", 400},
		{"/indexing/source/logs?from=1&to=2&address=" + url.QueryEscape("x'; DROP TABLE capture_records; --"), 400},
		{"/indexing/source/transactions?from=1&to=2&topic0=" + topic.Hex(), 400},
		{"/indexing/source/logs?from=1&to=2&after=bad", 400},
		{"/indexing/source/logs?from=1&to=2&topic0=" + topic.Hex() + "&after=" + url.QueryEscape(first.Next), 409},
	} {
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		require.Equal(t, tc.code, w.Code, w.Body.String())
	}

	// ORs within each dimension, ANDs across dimensions. Reordering and
	// duplicate alternatives preserve the cursor; time is inclusive uint64.
	unionPath := "/indexing/source/logs?from=1&to=2&address=" + other.Address.Hex() + "," + to.Address.Hex() + "&topic0=" + topic.Hex() + "," + common.Hash{99}.Hex() + "&topic1=" + second.Hex() + "," + zero.Hex() + "&limit=1"
	unionFirst := read(unionPath)
	require.Len(t, unionFirst.Rows, 1)
	require.EqualValues(t, 0, unionFirst.Rows[0].Row.Index)
	reversed := "/indexing/source/logs?from=1&to=2&address=" + to.Address.Hex() + "," + other.Address.Hex() + "," + to.Address.Hex() + "&topic0=" + common.Hash{99}.Hex() + "," + topic.Hex() + "&topic1=" + zero.Hex() + "," + second.Hex() + "&limit=256"
	unionNext := read(reversed + "&after=" + url.QueryEscape(unionFirst.Next))
	require.Len(t, unionNext.Rows, 5)
	require.True(t, unionNext.Done)
	require.Equal(t, unionFirst.Scope, unionNext.Scope)
	// Topic-only OR uses the positional postings, with no address driver.
	topicOR := read("/indexing/source/logs?from=1&to=2&topic1=" + zero.Hex() + "," + second.Hex())
	require.Len(t, topicOR.Rows, 6)
	timePage := read(reversed + "&from_time=1700000004&to_time=1700000004")
	require.Len(t, timePage.Rows, 3)
	for _, row := range timePage.Rows {
		require.EqualValues(t, 2, row.Identity.Block.Number)
	}
	timePage = read("/indexing/source/logs?from=1&to=2&from_time=0&to_time=1700000002")
	require.Len(t, timePage.Rows, 3)
	timePage = read("/indexing/source/logs?from=1&to=2&from_time=18446744073709551615")
	require.Empty(t, timePage.Rows)
	require.True(t, timePage.Done)
	participants := "/indexing/source/transactions?from=1&to=2&address=" + from.Address.Hex() + "," + to.Address.Hex()
	uniqueTxs := read(participants)
	require.Len(t, uniqueTxs.Rows, 2, "sender/recipient OR must deduplicate each transaction")
	incoming := read(participants + "&role=to&from_time=1700000004")
	require.Len(t, incoming.Rows, 1)
	require.EqualValues(t, 2, incoming.Rows[0].Identity.Block.Number)
	for _, extra := range []string{"&from_time=1&to_time=0", "&from_time=-1", "&to_time=18446744073709551616", "&address=", "&topic0=" + topic.Hex() + ",", "&from_time=1&after=" + url.QueryEscape(unionFirst.Next)} {
		w := httptest.NewRecorder()
		a.apiServer.Router().ServeHTTP(w, httptest.NewRequest("GET", reversed+extra, nil))
		expected := 400
		// repeated address/topic parameters are rejected before any DB read.
		if extra == "&from_time=1&after="+url.QueryEscape(unionFirst.Next) {
			expected = 409
		}
		require.Equal(t, expected, w.Code, w.Body.String())
	}
	require.Equal(t, before, rpc.Calls(), "source search must be DB-only even without the raw window")
	require.NoError(t, os.Rename(offline, raw))
	rpc.FailMethod("eth_getBlockByNumber", 0)
	a.Shutdown()
	a, e = NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done = runFeedApp(t, a)
	wait(1, 2)
	addFeedBlocks(c, 1)
	wait(1, 3)
	waitFeedProjection(t, a, c, 3)
	continued, e := indexingstatus.ReadSearch(t.Context(), a.storage, q, first.Next, 1)
	require.NoError(t, e)
	require.Len(t, continued.Rows, 1)
	require.Equal(t, first.Scope, continued.Scope, "later blocks cannot alter a fixed range page")
	c.Reorg(1)
	c.AddBlockWithExtra([]byte("replacement"))
	c.SetHead(2)
	wait(2, 2)
	waitFeedProjection(t, a, c, 2)
	_, e = indexingstatus.ReadSearch(t.Context(), a.storage, q, first.Next, 1)
	require.ErrorIs(t, e, capture.ErrStale)
	unionFilter := capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 2, Addresses: []common.Address{other.Address, to.Address}, TopicAny: [][]common.Hash{{topic, {99}}, {second, zero}, nil, nil}}
	_, e = indexingstatus.ReadSearch(t.Context(), a.storage, unionFilter, unionFirst.Next, 1)
	require.ErrorIs(t, e, capture.ErrStale)
	current, e := indexingstatus.ReadSearch(t.Context(), a.storage, q, "", 10)
	require.NoError(t, e)
	require.Len(t, current.Rows, 1, "only retained ancestor logs remain in the current range")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}
func TestManagedSourceSearchOptIn(t *testing.T) {
	cfg, _, _ := feedAppConfig(t)
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	t.Cleanup(a.Shutdown)
	_, e = indexingstatus.ReadSearch(t.Context(), a.storage, capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 2}, "", 1)
	require.ErrorIs(t, e, indexingstatus.ErrUnavailable)
	// Invalid requests do not gain access to the DB regardless of configuration.
	_, e = indexingstatus.ReadSearch(t.Context(), nil, capture.SearchFilter{Kind: capture.RowBody, From: 1, To: 2}, "", 1)
	require.ErrorIs(t, e, port.ErrInvalidData)
}

func TestManagedSourceSearchCandidateAndBlockBudgets(t *testing.T) {
	from, to := testchain.NewAccount(4), testchain.NewAccount(5)
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{from.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	primary, other, wanted := common.Hash{1}, common.Hash{2}, common.Hash{3}
	for n := 0; n < 18; n++ {
		logs := []*types.Log{{Address: to.Address, Topics: []common.Hash{primary, wanted}}}
		if n == 0 {
			logs = nil
			for i := 0; i < 270; i++ {
				topic := other
				if i == 269 {
					topic = wanted
				}
				logs = append(logs, &types.Log{Address: to.Address, Topics: []common.Hash{primary, topic}})
			}
		}
		c.AddBlock(testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000)}, GasUsed: 21000, Logs: logs})
	}
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.Projections = []string{model.SourceSearchWorker}
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	require.Eventually(t, func() bool {
		s, e := indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && len(s.Workers) == 1 && s.Workers[0].Complete != nil && s.Workers[0].Complete.Height == 18
	}, 45*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	before := rpc.Calls()
	q := capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 1, Address: &to.Address, Topics: [4]*common.Hash{&primary, &wanted}}
	first, e := indexingstatus.ReadSearch(t.Context(), a.storage, q, "", 256)
	require.NoError(t, e)
	require.Empty(t, first.Rows)
	require.Equal(t, 256, first.ScannedCandidates)
	require.NotEmpty(t, first.Next)
	require.False(t, first.Done)
	next, e := indexingstatus.ReadSearch(t.Context(), a.storage, q, first.Next, 256)
	require.NoError(t, e)
	require.Len(t, next.Rows, 1)
	require.True(t, next.Done)
	require.EqualValues(t, 269, next.Rows[0].Row.Index)
	q = capture.SearchFilter{Kind: capture.RowTransaction, From: 1, To: 18, Addresses: []common.Address{from.Address, to.Address}}
	first, e = indexingstatus.ReadSearch(t.Context(), a.storage, q, "", 256)
	require.NoError(t, e)
	require.Equal(t, 16, first.DecodedBlocks)
	require.Len(t, first.Rows, 16)
	require.False(t, first.Done)
	require.NotEmpty(t, first.Next)
	next, e = indexingstatus.ReadSearch(t.Context(), a.storage, q, first.Next, 256)
	require.NoError(t, e)
	require.Len(t, next.Rows, 2)
	require.True(t, next.Done)
	require.Equal(t, 2, next.DecodedBlocks)
	require.Equal(t, before, rpc.Calls())
}

func TestManagedSourceSearchByteBudgetRetainsNextCandidate(t *testing.T) {
	from, to := testchain.NewAccount(6), testchain.NewAccount(7)
	c := testchain.NewChain(5042002, map[common.Address]*big.Int{from.Address: new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)})
	data := bytes.Repeat([]byte{42}, 3<<20)
	for i := 0; i < 2; i++ {
		c.AddBlock(testchain.TxSpec{From: from, Tx: &types.DynamicFeeTx{To: &to.Address, Gas: 50000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000)}, GasUsed: 21000, Logs: []*types.Log{{Address: to.Address, Data: data}}})
	}
	cfg, _, rpc := feedAppConfig(t, c)
	cfg.Source.Feed.Projections = []string{model.SourceSearchWorker}
	volume := cfg.WindowStorage.Volumes["external"]
	volume.MaxBytes = 48 << 20
	cfg.WindowStorage.Volumes["external"] = volume
	raw := cfg.WindowStorage.Windows["arc"]
	raw.MaxBytes = 32 << 20
	raw.SegmentBytes = 8 << 20
	raw.MaxPayloadBytes = 4 << 20
	cfg.WindowStorage.Windows["arc"] = raw
	a, e := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, e)
	cancel, done := runFeedApp(t, a)
	require.Eventually(t, func() bool {
		s, e := indexingstatus.Read(context.Background(), a.storage, "", 100)
		return e == nil && len(s.Workers) == 1 && s.Workers[0].Complete != nil && s.Workers[0].Complete.Height == 2
	}, 35*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	before := rpc.Calls()
	q := capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 2}
	first, e := indexingstatus.ReadSearch(t.Context(), a.storage, q, "", 256)
	require.NoError(t, e)
	require.Len(t, first.Rows, 1)
	require.False(t, first.Done)
	require.NotEmpty(t, first.Next)
	require.LessOrEqual(t, len(first.Rows[0].Row.Value), 8<<20)
	require.Equal(t, 1, first.ScannedCandidates)
	second, e := indexingstatus.ReadSearch(t.Context(), a.storage, q, first.Next, 256)
	require.NoError(t, e)
	require.Len(t, second.Rows, 1)
	require.True(t, second.Done)
	require.EqualValues(t, 2, second.Rows[0].Identity.Block.Number)
	require.Equal(t, before, rpc.Calls())
}

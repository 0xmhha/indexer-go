package capture_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func searchFixture() capture.SearchData {
	to := common.Address{2}
	return capture.SearchData{Transactions: []capture.SearchTransaction{{Hash: common.Hash{10}, From: common.Address{1}, To: &to}, {Hash: common.Hash{11}, From: to, To: &to}}, Logs: []capture.SearchLog{{Address: to, Topics: []common.Hash{{3}, {}}, Transaction: 0}, {Address: common.Address{4}, Topics: []common.Hash{{5}, {3}}, Transaction: 1}, {Address: to, Topics: []common.Hash{{3}}, Transaction: 1}}}
}
func TestSourceSearchTransactionsAndReorg(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := openFactory(t, backend)
			db := open()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			d := model.SourceSearchDescriptor{Version: 1, Profile: "source'; DROP TABLE capture_records; --"}
			data := searchFixture()
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			b := input(2, 1, common.Hash{3}, a.Manifest.Identity.Block.Hash)
			require.NoError(t, s.Commit(ctx, a))
			require.NoError(t, s.Commit(ctx, b))
			_, e := s.InitializeWorker(ctx, model.SourceSearchWorker, d.Definition())
			require.NoError(t, e)
			require.ErrorIs(t, s.CommitWorker(ctx, model.SourceSearchWorker, d.Definition(), 1, a.Manifest.Identity, []byte(`{}`)), port.ErrInvalidData)
			require.ErrorIs(t, s.CommitSearch(ctx, d, 1, b.Manifest.Identity, data), capture.ErrGap)
			writes := 0
			require.ErrorIs(t, service(t, countLookupDB{db, &writes}, o).CommitSearch(ctx, d, 1, a.Manifest.Identity, data), errInjected)
			require.Greater(t, writes, 3)
			t.Logf("atomic search writes tested: %d", writes)
			for i := 1; i <= writes; i++ {
				require.ErrorIs(t, service(t, failDB{CaptureDatabase: db, at: i}, o).CommitSearch(ctx, d, 1, a.Manifest.Identity, data), errInjected)
				snap, e := s.SearchSnapshot(ctx, d, 1)
				require.NoError(t, e)
				require.Equal(t, snap.Anchor, snap.Through)
			}
			o.fork(1, common.Hash{99})
			require.ErrorIs(t, s.CommitSearch(ctx, d, 1, a.Manifest.Identity, data), capture.ErrCanonical)
			o.fork(1, a.Manifest.Identity.Block.Hash)
			before := o.calls
			require.ErrorIs(t, service(t, uncertainDB{db}, o).CommitSearch(ctx, d, 1, a.Manifest.Identity, data), errInjected)
			require.NoError(t, s.CommitSearch(ctx, d, 1, a.Manifest.Identity, data))
			require.Equal(t, before+2, o.calls)
			bad := searchFixture()
			bad.Logs[0].Topics[0] = common.Hash{100}
			require.ErrorIs(t, s.CommitSearch(ctx, d, 1, a.Manifest.Identity, bad), capture.ErrConflict)
			q := capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 1, Address: &data.Logs[0].Address}
			first, e := s.ScanSearch(ctx, d, 1, q, nil, 1)
			require.NoError(t, e)
			require.Len(t, first.Candidates, 1)
			require.EqualValues(t, 0, first.Candidates[0].Position.Index)
			require.False(t, first.Done)
			second, e := s.ScanSearch(ctx, d, 1, q, &first.Candidates[0].Position, 1)
			require.NoError(t, e)
			require.Len(t, second.Candidates, 1)
			require.EqualValues(t, 2, second.Candidates[0].Position.Index)
			edge, e := s.ScanSearch(ctx, d, 1, q, &capture.SearchPosition{Height: 1, Index: 32767}, 20)
			require.NoError(t, e)
			require.True(t, edge.Done)
			require.Empty(t, edge.Candidates)
			last, e := s.ScanSearch(ctx, d, 1, q, &second.Candidates[0].Position, 1)
			require.NoError(t, e)
			require.Empty(t, last.Candidates)
			require.True(t, last.Done)
			topic := common.Hash{3}
			q.Address = nil
			q.Topics[1] = &topic
			got, e := s.ScanSearch(ctx, d, 1, q, nil, 20)
			require.NoError(t, e)
			require.Len(t, got.Candidates, 1)
			require.EqualValues(t, 1, got.Candidates[0].Position.Index)
			zero := common.Hash{}
			q.Topics[1] = &zero
			got, e = s.ScanSearch(ctx, d, 1, q, nil, 20)
			require.NoError(t, e)
			require.Len(t, got.Candidates, 1)
			require.Zero(t, got.Candidates[0].Position.Index)
			q = capture.SearchFilter{Kind: capture.RowTransaction, From: 1, To: 1, Address: &data.Transactions[1].From}
			got, e = s.ScanSearch(ctx, d, 1, q, nil, 20)
			require.NoError(t, e)
			require.Len(t, got.Candidates, 2, "self transfers emit one participant posting")
			// Both participants of the first transaction share one result and
			// the common keyset cursor excludes every posting for that row.
			union := capture.SearchFilter{Kind: capture.RowTransaction, From: 1, To: 1, Addresses: []common.Address{data.Transactions[1].From, data.Transactions[0].From, data.Transactions[1].From}}
			merged, e := s.ScanSearch(ctx, d, 1, union, nil, 1)
			require.NoError(t, e)
			require.EqualValues(t, 0, merged.Candidates[0].Position.Index)
			merged, e = s.ScanSearch(ctx, d, 1, union, &merged.Candidates[0].Position, 1)
			require.NoError(t, e)
			require.Len(t, merged.Candidates, 1)
			require.EqualValues(t, 1, merged.Candidates[0].Position.Index)
			merged, e = s.ScanSearch(ctx, d, 1, union, &merged.Candidates[0].Position, 1)
			require.NoError(t, e)
			require.Empty(t, merged.Candidates)
			require.True(t, merged.Done)
			union = capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 1, TopicAny: [][]common.Hash{nil, {{3}, {}}, nil, nil}}
			merged, e = s.ScanSearch(ctx, d, 1, union, nil, 20)
			require.NoError(t, e)
			require.Len(t, merged.Candidates, 2, "a missing topic position is not a zero topic")
			require.EqualValues(t, 0, merged.Candidates[0].Position.Index)
			require.EqualValues(t, 1, merged.Candidates[1].Position.Index)
			union = capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 1}
			for i := 0; i < capture.MaxSearchAlternatives; i++ {
				union.Addresses = append(union.Addresses, common.Address{byte(i)})
			}
			merged, e = s.ScanSearch(ctx, d, 1, union, nil, 20)
			require.NoError(t, e)
			require.Len(t, merged.Candidates, 3)

			// An empty block still has a root marker, and old range roots stay stable.
			require.NoError(t, s.CommitSearch(ctx, d, 1, b.Manifest.Identity, capture.SearchData{}))
			stable, e := s.ScanSearch(ctx, d, 1, q, nil, 20)
			require.NoError(t, e)
			require.Equal(t, got.Scope, stable.Scope)
			empty, e := s.ScanSearch(ctx, d, 1, capture.SearchFilter{Kind: capture.RowLog, From: 2, To: 2}, nil, 20)
			require.NoError(t, e)
			require.True(t, empty.Done)
			require.Empty(t, empty.Candidates)
			require.NoError(t, db.Close())
			db = open()
			defer func() { require.NoError(t, db.Close()) }()
			s = service(t, db, o)
			_, e = s.InitializeWorker(ctx, model.SourceSearchWorker, d.Definition())
			require.NoError(t, e)
			require.NoError(t, s.Reset(ctx, 1, a.Manifest.Identity.Block))
			_, e = s.ScanSearch(ctx, d, 1, q, nil, 20)
			require.ErrorIs(t, e, capture.ErrStale)
			_, e = s.InitializeWorker(ctx, model.SourceSearchWorker, d.Definition())
			require.NoError(t, e)
			retained, e := s.ScanSearch(ctx, d, 2, q, nil, 20)
			require.NoError(t, e)
			require.Len(t, retained.Candidates, 2)
			require.Equal(t, got.Scope.Root, retained.Scope.Root)
			replacement := input(2, 2, common.Hash{88}, a.Manifest.Identity.Block.Hash)
			o.fork(2, replacement.Manifest.Identity.Block.Hash)
			require.NoError(t, s.Commit(ctx, replacement))
			require.NoError(t, s.CommitSearch(ctx, d, 2, replacement.Manifest.Identity, data))
			q.From, q.To = 2, 2
			fresh, e := s.ScanSearch(ctx, d, 2, q, nil, 20)
			require.NoError(t, e)
			require.Len(t, fresh.Candidates, 2)
			require.Equal(t, replacement.Manifest.Identity.Block.Hash, fresh.Candidates[0].Location.BlockHash)
		})
	}
}

type searchFaultDB struct {
	port.CaptureDatabase
	corrupt bool
}
type searchFaultReader struct {
	port.CaptureReader
	corrupt bool
}

func (r searchFaultReader) Get(k []byte) ([]byte, error) {
	if bytes.HasPrefix(k, []byte("search/v1/nodes/")) {
		if r.corrupt {
			return []byte{0xc0}, nil
		}
		return nil, port.ErrNotFound
	}
	return r.CaptureReader.Get(k)
}
func (d searchFaultDB) CaptureView(ctx context.Context, ch model.FeedChain, fn func(port.CaptureReader) error) error {
	return d.CaptureDatabase.CaptureView(ctx, ch, func(r port.CaptureReader) error { return fn(searchFaultReader{r, d.corrupt}) })
}
func TestSourceSearchMissingAndTamperedNodes(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	d := model.SourceSearchDescriptor{Version: 1, Profile: "fixture"}
	_, e := s.InitializeWorker(ctx, model.SourceSearchWorker, d.Definition())
	require.NoError(t, e)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	require.NoError(t, s.CommitSearch(ctx, d, 1, a.Manifest.Identity, searchFixture()))
	for _, corrupt := range []bool{true, false} {
		r, e := capture.NewReader(searchFaultDB{db, corrupt}, chain, nil)
		require.NoError(t, e)
		_, e = r.ScanSearch(ctx, d, 1, capture.SearchFilter{Kind: capture.RowLog, From: 1, To: 1}, nil, 10)
		require.ErrorIs(t, e, capture.ErrConflict)
	}
}
func TestSourceSearchEpochRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	d := model.SourceSearchDescriptor{Version: 1, Profile: "fixture"}
	_, e := s.InitializeWorker(ctx, model.SourceSearchWorker, d.Definition())
	require.NoError(t, e)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	done := make(chan error, 1)
	go func() { done <- s.CommitSearch(ctx, d, 1, a.Manifest.Identity, searchFixture()) }()
	require.NoError(t, s.Reset(ctx, 1, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}))
	if e = <-done; e != nil {
		require.ErrorIs(t, e, capture.ErrStale)
	}
	c, e := s.InitializeWorker(ctx, model.SourceSearchWorker, d.Definition())
	require.NoError(t, e)
	require.Equal(t, c.Anchor, c.Through)
}

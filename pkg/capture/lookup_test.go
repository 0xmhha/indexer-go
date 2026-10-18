package capture_test

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestSourceLookupTransactionsAndReorg(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := openFactory(t, backend)
			db := open()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			d := model.SourceLookupDescriptor{Version: 1, Profile: "fixture'; DROP TABLE capture_records; --"}
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			b := input(2, 1, common.Hash{3}, a.Manifest.Identity.Block.Hash)
			require.NoError(t, s.Commit(ctx, a))
			require.NoError(t, s.Commit(ctx, b))
			_, e := s.InitializeWorker(ctx, model.SourceLookupWorker, d.Definition())
			require.NoError(t, e)
			require.ErrorIs(t, s.CommitWorker(ctx, model.SourceLookupWorker, d.Definition(), 1, a.Manifest.Identity, []byte(`{}`)), port.ErrInvalidData, "generic writer cannot bypass lookup node transaction")
			h1, h2 := common.Hash{41}, common.Hash{42}
			require.ErrorIs(t, s.CommitLookup(ctx, d, 1, b.Manifest.Identity, []common.Hash{h2}), capture.ErrGap)
			require.ErrorIs(t, s.CommitLookup(ctx, d, 1, a.Manifest.Identity, []common.Hash{h1, h1}), port.ErrInvalidData)
			// Nodes and worker metadata must roll back at every write boundary.
			writes := 0
			require.ErrorIs(t, service(t, countLookupDB{db, &writes}, o).CommitLookup(ctx, d, 1, a.Manifest.Identity, []common.Hash{h1}), errInjected)
			require.Greater(t, writes, 3)
			t.Logf("atomic lookup writes tested: %d", writes)
			for at := 1; at <= writes; at++ {
				err := service(t, failDB{CaptureDatabase: db, at: at}, o).CommitLookup(ctx, d, 1, a.Manifest.Identity, []common.Hash{h1})
				require.ErrorIs(t, err, errInjected)
				snap, e := s.LookupSnapshot(ctx, d, 1)
				require.NoError(t, e)
				require.Equal(t, snap.Anchor, snap.Through)
				events, e := s.WorkerEvents(ctx, model.SourceLookupWorker, 0, 10)
				require.NoError(t, e)
				require.Empty(t, events)
			}
			o.fork(1, common.Hash{99})
			require.ErrorIs(t, s.CommitLookup(ctx, d, 1, a.Manifest.Identity, []common.Hash{h1}), capture.ErrCanonical)
			o.fork(1, a.Manifest.Identity.Block.Hash)
			calls := o.calls
			require.ErrorIs(t, service(t, uncertainDB{db}, o).CommitLookup(ctx, d, 1, a.Manifest.Identity, []common.Hash{h1}), errInjected)
			require.NoError(t, s.CommitLookup(ctx, d, 1, a.Manifest.Identity, []common.Hash{h1}))
			require.Equal(t, calls+2, o.calls, "uncertain retry must perform another fresh check")
			require.ErrorIs(t, s.CommitLookup(ctx, d, 1, a.Manifest.Identity, nil), capture.ErrConflict)
			require.ErrorIs(t, s.CommitLookup(ctx, d, 1, b.Manifest.Identity, []common.Hash{h1}), capture.ErrConflict, "duplicate canonical tx cannot overwrite location")
			pending, e := s.FindLookup(ctx, d, 1, capture.LookupTransaction, h2)
			require.NoError(t, e)
			require.False(t, pending.Found)
			require.EqualValues(t, 1, pending.Through.Number)
			require.EqualValues(t, 2, pending.CapturedThrough.Number)
			require.NoError(t, s.CommitLookup(ctx, d, 1, b.Manifest.Identity, []common.Hash{h2}))
			result, e := s.FindLookup(ctx, d, 1, capture.LookupTransaction, h1)
			require.NoError(t, e)
			require.True(t, result.Found)
			require.EqualValues(t, 1, result.Location.Height)
			result, e = s.FindLookup(ctx, d, 1, capture.LookupTransaction, common.Hash{88})
			require.NoError(t, e)
			require.False(t, result.Found)
			require.EqualValues(t, 2, result.Through.Number)
			require.NoError(t, db.Close())
			db = open()
			defer func() { require.NoError(t, db.Close()) }()
			s = service(t, db, o)
			c, e := s.InitializeWorker(ctx, model.SourceLookupWorker, d.Definition())
			require.NoError(t, e)
			require.EqualValues(t, 2, c.Through.Number)
			require.NoError(t, s.Reset(ctx, 1, a.Manifest.Identity.Block))
			_, e = s.FindLookup(ctx, d, 1, capture.LookupTransaction, h2)
			require.ErrorIs(t, e, capture.ErrStale)
			_, e = s.FindLookup(ctx, d, 2, capture.LookupTransaction, h2)
			require.ErrorIs(t, e, capture.ErrStale)
			c, e = s.InitializeWorker(ctx, model.SourceLookupWorker, d.Definition())
			require.NoError(t, e)
			require.Equal(t, a.Manifest.Identity.Block, c.Through)
			result, e = s.FindLookup(ctx, d, 2, capture.LookupTransaction, h1)
			require.NoError(t, e)
			require.True(t, result.Found)
			result, e = s.FindLookup(ctx, d, 2, capture.LookupTransaction, h2)
			require.NoError(t, e)
			require.False(t, result.Found)
			replacement := input(2, 2, common.Hash{77}, a.Manifest.Identity.Block.Hash)
			o.fork(2, replacement.Manifest.Identity.Block.Hash)
			require.NoError(t, s.Commit(ctx, replacement))
			require.NoError(t, s.CommitLookup(ctx, d, 2, replacement.Manifest.Identity, []common.Hash{h2}))
			result, e = s.FindLookup(ctx, d, 2, capture.LookupTransaction, h2)
			require.NoError(t, e)
			require.Equal(t, replacement.Manifest.Identity.Block.Hash, result.Location.BlockHash)
			result, e = s.FindLookup(ctx, d, 2, capture.LookupBlock, b.Manifest.Identity.Block.Hash)
			require.NoError(t, e)
			require.False(t, result.Found)
			state, e := s.State(ctx)
			require.NoError(t, e)
			require.Equal(t, replacement.Manifest.Identity.Block, state.Through)
			require.Empty(t, state.RawConsumers, "optional index grants no raw GC ACK")
		})
	}
}

type lookupFaultDB struct {
	port.CaptureDatabase
	corrupt bool
}
type lookupFaultReader struct {
	port.CaptureReader
	corrupt bool
}

func (r lookupFaultReader) Get(k []byte) ([]byte, error) {
	if bytes.HasPrefix(k, []byte("lookup/v1/nodes/")) {
		if r.corrupt {
			return []byte{0xc0}, nil
		}
		return nil, port.ErrNotFound
	}
	return r.CaptureReader.Get(k)
}
func (d lookupFaultDB) CaptureView(ctx context.Context, ch model.FeedChain, fn func(port.CaptureReader) error) error {
	return d.CaptureDatabase.CaptureView(ctx, ch, func(r port.CaptureReader) error { return fn(lookupFaultReader{r, d.corrupt}) })
}
func TestSourceLookupMissingAndTamperedNodes(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s)
	d := model.SourceLookupDescriptor{Version: 1, Profile: "fixture"}
	_, e := s.InitializeWorker(ctx, model.SourceLookupWorker, d.Definition())
	require.NoError(t, e)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	require.NoError(t, s.CommitLookup(ctx, d, 1, a.Manifest.Identity, []common.Hash{{41}}))
	for _, corrupt := range []bool{false, true} {
		r, e := capture.NewReader(lookupFaultDB{db, corrupt}, chain, nil)
		require.NoError(t, e)
		_, e = r.FindLookup(ctx, d, 1, capture.LookupTransaction, common.Hash{41})
		require.ErrorIs(t, e, capture.ErrConflict)
		_, e = r.FindLookup(ctx, d, 1, capture.LookupTransaction, common.Hash{99})
		require.ErrorIs(t, e, capture.ErrConflict, "missing nodes are not authenticated absence")
	}
}
func TestSourceLookupEpochRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	d := model.SourceLookupDescriptor{Version: 1, Profile: "fixture"}
	_, e := s.InitializeWorker(ctx, model.SourceLookupWorker, d.Definition())
	require.NoError(t, e)
	start := make(chan struct{})
	result := make(chan error, 1)
	go func() { <-start; result <- s.CommitLookup(ctx, d, 1, a.Manifest.Identity, []common.Hash{{41}}) }()
	close(start)
	require.NoError(t, s.Reset(ctx, 1, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}))
	e = <-result
	if e != nil {
		require.True(t, errors.Is(e, capture.ErrStale), e)
	}
	_, e = s.InitializeWorker(ctx, model.SourceLookupWorker, d.Definition())
	require.NoError(t, e)
	found, e := s.FindLookup(ctx, d, 2, capture.LookupTransaction, common.Hash{41})
	require.NoError(t, e)
	require.False(t, found.Found)
}

// Probe every actual write, then force a rollback before commit.
type countLookupDB struct {
	port.CaptureDatabase
	writes *int
}
type countLookupWriter struct {
	port.CaptureWriter
	writes *int
}

func (w countLookupWriter) Put(k, v []byte) error { (*w.writes)++; return w.CaptureWriter.Put(k, v) }
func (d countLookupDB) CaptureUpdate(ctx context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	return d.CaptureDatabase.CaptureUpdate(ctx, ch, func(w port.CaptureWriter) error {
		if e := fn(countLookupWriter{w, d.writes}); e != nil {
			return e
		}
		return errInjected
	})
}
func TestSourceLookupTrieManyEntries(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s)
	d := model.SourceLookupDescriptor{Version: 1, Profile: "fixture"}
	_, e := s.InitializeWorker(ctx, model.SourceLookupWorker, d.Definition())
	require.NoError(t, e)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	hashes := make([]common.Hash, 512)
	for i := range hashes {
		hashes[i] = common.BigToHash(big.NewInt(int64(i + 1)))
	}
	require.NoError(t, s.CommitLookup(ctx, d, 1, a.Manifest.Identity, hashes))
	for _, i := range []int{0, 1, 127, 255, 256, 511} {
		out, e := s.FindLookup(ctx, d, 1, capture.LookupTransaction, hashes[i])
		require.NoError(t, e)
		require.True(t, out.Found)
		require.EqualValues(t, i, out.Location.Index)
	}
}

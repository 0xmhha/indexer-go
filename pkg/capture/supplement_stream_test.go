package capture_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func streamDescriptor(from uint64) model.SupplementStreamDescriptor {
	raw := json.RawMessage(`{"tracked":["';DROP TABLE capture_records;--"]}`)
	return model.SupplementStreamDescriptor{Version: 1, Name: "tracked-v1", Codec: "fixture.v1", SelectionHash: sha256.Sum256(append([]byte("fixture.v1\x00"), raw...)), Selection: raw, FromHeight: from}
}
func streamSupplement(d model.SupplementStreamDescriptor, id model.CaptureIdentity) capture.Supplement {
	b := []byte(`{"proof":"codec-owned fixture"}`)
	return capture.Supplement{Identity: id, Codec: d.Codec, Selection: d.SelectionHash, Digest: sha256.Sum256(b), Data: b}
}
func TestSupplementStreamAtomicScopeAndReset(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := openFactory(t, backend)
			db := open()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			a, b, c := input(1, 1, common.Hash{2}, chain.GenesisHash), input(2, 1, common.Hash{3}, common.Hash{2}), input(3, 1, common.Hash{4}, common.Hash{3})
			require.NoError(t, s.Commit(ctx, a))
			require.NoError(t, s.Commit(ctx, b))
			require.NoError(t, s.Commit(ctx, c))
			d := streamDescriptor(2)
			cursor, e := s.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			require.Nil(t, cursor.Complete)
			status, e := s.SupplementStreamStatus(ctx, d)
			require.NoError(t, e)
			require.EqualValues(t, 2, status.Backlog)
			require.ErrorIs(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, a.Manifest.Identity)), capture.ErrGap)
			require.ErrorIs(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, c.Manifest.Identity)), capture.ErrGap)
			for at := 1; at <= 4; at++ {
				broken := service(t, failDB{CaptureDatabase: db, at: at}, o)
				require.ErrorIs(t, broken.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, b.Manifest.Identity)), errInjected)
				status, e := s.SupplementStreamStatus(ctx, d)
				require.NoError(t, e)
				require.Nil(t, status.Complete)
				require.Zero(t, status.Sequence)
				_, e = s.ReadSupplement(ctx, 1, b.Manifest.Identity, d.Codec, d.SelectionHash)
				require.ErrorIs(t, e, port.ErrNotFound)
				events, e := s.SupplementStreamEvents(ctx, d, 0, 10)
				require.NoError(t, e)
				require.Empty(t, events)
			}
			o.fork(2, common.Hash{77})
			require.ErrorIs(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, b.Manifest.Identity)), capture.ErrCanonical)
			o.fork(2, common.Hash{3})
			require.ErrorIs(t, service(t, uncertainDB{db}, o).CommitStreamSupplement(ctx, 1, d, streamSupplement(d, b.Manifest.Identity)), errInjected)
			require.NoError(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, b.Manifest.Identity)))
			events, e := s.SupplementStreamEvents(ctx, d, 0, 10)
			require.NoError(t, e)
			require.Len(t, events, 1)
			require.NoError(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, c.Manifest.Identity)))
			st, e := s.State(ctx)
			require.NoError(t, e)
			require.EqualValues(t, 3, st.Sequence, "state stream does not publish base facts events or ACKs")
			require.NoError(t, db.Close())
			db = open()
			defer func() { require.NoError(t, db.Close()) }()
			s = service(t, db, o)
			cursor, e = s.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			require.Equal(t, c.Manifest.Identity.Block, *cursor.Complete)
			changed := d
			changed.FromHeight = 3
			_, e = s.InitializeSupplementStream(ctx, changed)
			require.ErrorIs(t, e, capture.ErrConflict)
			require.NoError(t, s.Reset(ctx, 1, b.Manifest.Identity.Block))
			status, e = s.SupplementStreamStatus(ctx, d)
			require.NoError(t, e)
			require.Equal(t, "stale", status.Status)
			require.Nil(t, status.Complete)
			cursor, e = s.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			require.EqualValues(t, 2, cursor.Epoch)
			require.Equal(t, b.Manifest.Identity.Block, *cursor.Complete)
			status, e = s.SupplementStreamStatus(ctx, d)
			require.NoError(t, e)
			require.Equal(t, "caught_up", status.Status)
			require.NoError(t, s.Reset(ctx, 2, a.Manifest.Identity.Block))
			cursor, e = s.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			require.Nil(t, cursor.Complete)
			require.ErrorIs(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, b.Manifest.Identity)), capture.ErrStale)
			o.fork(2, common.Hash{33})
			branch := input(2, 3, common.Hash{33}, a.Manifest.Identity.Block.Hash)
			require.NoError(t, s.Commit(ctx, branch))
			require.NoError(t, s.CommitStreamSupplement(ctx, 3, d, streamSupplement(d, branch.Manifest.Identity)))
			status, e = s.SupplementStreamStatus(ctx, d)
			require.NoError(t, e)
			require.Equal(t, branch.Manifest.Identity.Block.Hash, status.Complete.Hash)
			// Metadata status must not load up to 8MiB of proof on every poll.
			view := service(t, streamReadFaultDB{CaptureDatabase: db, payload: true}, o)
			_, e = view.SupplementStreamStatus(ctx, d)
			require.NoError(t, e)
			_, e = view.ReadSupplement(ctx, 3, branch.Manifest.Identity, d.Codec, d.SelectionHash)
			require.ErrorIs(t, e, errInjected)
			view = service(t, streamReadFaultDB{CaptureDatabase: db, receipt: true}, o)
			_, e = view.SupplementStreamStatus(ctx, d)
			require.ErrorIs(t, e, errInjected)
			events, e = s.SupplementStreamEvents(ctx, d, 1, 1)
			require.NoError(t, e)
			require.Len(t, events, 1)
			require.EqualValues(t, 2, events[0].Sequence)
		})
	}
}

type streamReadFaultDB struct {
	port.CaptureDatabase
	payload, receipt bool
}
type streamReadFault struct {
	port.CaptureReader
	payload, receipt bool
}

func (r streamReadFault) Get(k []byte) ([]byte, error) {
	if r.payload && bytes.HasPrefix(k, []byte("supplement/")) || r.receipt && bytes.Contains(k, []byte("/input/")) {
		return nil, errors.Join(port.ErrNotFound, errInjected)
	}
	return r.CaptureReader.Get(k)
}
func (d streamReadFaultDB) CaptureView(ctx context.Context, ch model.FeedChain, fn func(port.CaptureReader) error) error {
	return d.CaptureDatabase.CaptureView(ctx, ch, func(r port.CaptureReader) error { return fn(streamReadFault{r, d.payload, d.receipt}) })
}
func TestSupplementStreamEpochRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	d := streamDescriptor(1)
	_, e := s.InitializeSupplementStream(ctx, d)
	require.NoError(t, e)
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() { <-start; done <- s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, a.Manifest.Identity)) }()
	close(start)
	require.NoError(t, s.Reset(ctx, 1, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}))
	if e := <-done; e != nil {
		require.ErrorIs(t, e, capture.ErrStale)
	}
	cursor, e := s.InitializeSupplementStream(ctx, d)
	require.NoError(t, e)
	require.Nil(t, cursor.Complete)
	require.EqualValues(t, 2, cursor.Epoch)
}

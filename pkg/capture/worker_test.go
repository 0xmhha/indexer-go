package capture_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestIndependentWorkerTransactions(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := openFactory(t, backend)
			db := open()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			b := input(2, 1, common.Hash{3}, a.Manifest.Identity.Block.Hash)
			require.NoError(t, s.Commit(ctx, a))
			require.NoError(t, s.Commit(ctx, b))
			_, e := s.InitializeWorker(ctx, "summary", "v1")
			require.NoError(t, e)
			require.ErrorIs(t, s.CommitWorker(ctx, "summary", "v1", 1, b.Manifest.Identity, []byte(`{"height":2}`)), capture.ErrGap)
			// Neither a failed transaction nor a fresh mismatch may publish output/cursor/event.
			broken := service(t, failDB{CaptureDatabase: db, at: 2}, o)
			require.ErrorIs(t, broken.CommitWorker(ctx, "summary", "v1", 1, a.Manifest.Identity, []byte(`{"height":1}`)), errInjected)
			_, e = s.WorkerOutput(ctx, "summary", "v1", a.Manifest.Identity)
			require.ErrorIs(t, e, port.ErrNotFound)
			events, e := s.WorkerEvents(ctx, "summary", 0, 10)
			require.NoError(t, e)
			require.Empty(t, events)
			o.fork(1, common.Hash{99})
			require.ErrorIs(t, s.CommitWorker(ctx, "summary", "v1", 1, a.Manifest.Identity, []byte(`{"height":1}`)), capture.ErrCanonical)
			o.fork(1, common.Hash{2})
			require.ErrorIs(t, service(t, uncertainDB{db}, o).CommitWorker(ctx, "summary", "v1", 1, a.Manifest.Identity, []byte(`{"height":1}`)), errInjected)
			require.NoError(t, s.CommitWorker(ctx, "summary", "v1", 1, a.Manifest.Identity, []byte(`{"height":1}`)))
			require.ErrorIs(t, s.CommitWorker(ctx, "summary", "v1", 1, a.Manifest.Identity, []byte(`{"height":999}`)), capture.ErrConflict)
			require.NoError(t, s.CommitWorker(ctx, "summary", "v1", 1, b.Manifest.Identity, []byte(`{"height":2}`)))
			cursor, e := s.InitializeWorker(ctx, "other", "v1")
			require.NoError(t, e)
			require.Equal(t, cursor.Anchor, cursor.Through)
			require.NoError(t, db.Close())
			db = open()
			defer func() { require.NoError(t, db.Close()) }()
			s = service(t, db, o)
			cursor, e = s.InitializeWorker(ctx, "summary", "v1")
			require.NoError(t, e)
			require.EqualValues(t, 2, cursor.Through.Number)
			require.NoError(t, s.Reset(ctx, 1, a.Manifest.Identity.Block))
			d := model.ProjectionDescriptor{Version: 1, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}, Plan: plan, Workers: []string{"summary"}}
			_, _, e = capture.ReadWorkerOutput(ctx, db, d, "summary", 1)
			require.ErrorIs(t, e, capture.ErrStale)

			require.ErrorIs(t, s.CommitWorker(ctx, "summary", "v1", 1, b.Manifest.Identity, []byte(`{"height":2}`)), capture.ErrStale)
			cursor, e = s.InitializeWorker(ctx, "summary", "v1")
			require.NoError(t, e)
			require.EqualValues(t, 2, cursor.Epoch)
			require.Equal(t, a.Manifest.Identity.Block, cursor.Through)
			require.NoError(t, s.CommitWorker(ctx, "summary", "v1", 2, a.Manifest.Identity, []byte(`{"height":1}`)))
			old, e := s.WorkerOutput(ctx, "summary", "v1", b.Manifest.Identity)
			require.NoError(t, e)
			require.JSONEq(t, `{"height":2}`, string(old.Data))
			events, e = s.WorkerEvents(ctx, "summary", 0, 10)
			require.NoError(t, e)
			require.Len(t, events, 3)
			require.Equal(t, "reset", events[2].Kind)
			_, _, e = capture.ReadWorkerOutput(ctx, db, d, "summary", 2)
			require.ErrorIs(t, e, port.ErrNotFound, "old fork output is not a current API result")

			// A current source endpoint never reports historical old-fork completion.
			snapshot, e := capture.ReadStatus(ctx, db, model.ProjectionDescriptor{Version: 1, Anchor: cursor.Anchor, Plan: plan, Workers: []string{"summary", "other"}}, port.IndexingPage{})
			require.NoError(t, e)
			require.EqualValues(t, 1, snapshot.Workers[0].Complete.Height)
			require.Equal(t, "stale", snapshot.Workers[1].Status)
		})
	}
}

func TestCaptureStatusGapsAndStaleEpoch(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s)
	for n := uint64(1); n <= 3; n++ {
		h := o.hashes[n]
		parent := o.hashes[n-1]
		require.NoError(t, s.Commit(ctx, input(n, 1, h, parent)))
	}
	digest, _ := plan.Digest()
	anchor := model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}
	cp := &model.UnitCheckpoint{Version: 1, Epoch: 1, Chain: chain, PlanDigest: digest, Start: 1, Ranges: []model.CompletedRange{{From: 2, To: 3, EndHash: o.hashes[3]}}}
	page := port.IndexingPage{Units: []port.IndexingUnit{{Name: "u", State: port.FeatureState{Active: true}, Checkpoint: cp}}}
	d := model.ProjectionDescriptor{Version: 1, Anchor: anchor, Plan: plan}
	got, e := capture.ReadStatus(ctx, db, d, page)
	require.NoError(t, e)
	require.Nil(t, got.Units["u"].Complete)
	require.EqualValues(t, 3, got.Units["u"].LastApplied.Height)
	cp.Ranges[0].From = 1
	page.Units[0].State.Gap = &port.BlockRange{From: 2, To: 2}
	got, e = capture.ReadStatus(ctx, db, d, page)
	require.NoError(t, e)
	require.EqualValues(t, 1, got.Units["u"].Complete.Height)
	require.NoError(t, s.Reset(ctx, 1, model.FeedBlock{Chain: chain, Number: 1, Hash: o.hashes[1]}))
	got, e = capture.ReadStatus(ctx, db, d, page)
	require.NoError(t, e)
	require.Equal(t, "stale", got.Units["u"].Status)
	require.Nil(t, got.Units["u"].Complete)
}

func TestIndependentWorkerEpochRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	o := newOracle()
	s := service(t, db, o)
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	_, e := s.InitializeWorker(ctx, "race", "v1")
	require.NoError(t, e)
	start := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		<-start
		result <- s.CommitWorker(ctx, "race", "v1", 1, a.Manifest.Identity, []byte(`{"ok":true}`))
	}()
	close(start)
	require.NoError(t, s.Reset(ctx, 1, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}))
	e = <-result
	if e != nil {
		require.ErrorIs(t, e, capture.ErrStale)
	}
	cursor, e := s.InitializeWorker(ctx, "race", "v1")
	require.NoError(t, e)
	require.EqualValues(t, 2, cursor.Epoch)
	require.Equal(t, cursor.Anchor, cursor.Through)
}

type statusFaultDB struct{ port.CaptureDatabase }
type statusFaultReader struct{ port.CaptureReader }

func (r statusFaultReader) Get(k []byte) ([]byte, error) {
	if bytes.HasPrefix(k, []byte("canonical/")) {
		return nil, errors.Join(capture.ErrConflict, errInjected)
	}
	return r.CaptureReader.Get(k)
}
func (d statusFaultDB) CaptureView(ctx context.Context, ch model.FeedChain, fn func(port.CaptureReader) error) error {
	return d.CaptureDatabase.CaptureView(ctx, ch, func(r port.CaptureReader) error { return fn(statusFaultReader{r}) })
}
func TestCaptureStatusDoesNotHideReadFailure(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	_, e := capture.ReadStatus(ctx, statusFaultDB{db}, model.ProjectionDescriptor{Version: 1, Anchor: model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}, Plan: plan}, port.IndexingPage{Head: &model.BlockPoint{Height: 1, Hash: a.Manifest.Identity.Block.Hash}})
	require.ErrorIs(t, e, errInjected)
}

package capture_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func requiredDescriptor() model.SupplementStreamDescriptor {
	d := streamDescriptor(2)
	d.Required, d.ParentCheckpoint = true, true
	return d
}
func fixtureVerifier(d model.SupplementStreamDescriptor, b model.FeedBlock, raw []byte) error {
	if d.Codec != "fixture.v1" || b.Chain != chain || string(raw) != `{"proof":"codec-owned fixture"}` {
		return errInjected
	}
	return nil
}
func checkpointFixture(b model.FeedBlock) capture.SupplementCheckpoint {
	raw := []byte(`{"proof":"codec-owned fixture"}`)
	return capture.SupplementCheckpoint{Block: b, Data: raw, Digest: sha256.Sum256(raw)}
}
func rawRef(in capture.Input) window.Reference {
	p := in.Payloads[0]
	return window.Reference{StoreID: common.Hash{8}, Segment: 1, Block: in.Manifest.Identity.Block, Kind: p.Kind, Codec: "fixture.v1", Digest: sha256.Sum256(p.Bytes), PayloadBytes: int64(len(p.Bytes))}
}
func TestRequiredSupplementDeletionContract(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := openFactory(t, backend)
			db := open()
			defer func() { require.NoError(t, db.Close()) }()
			o := newOracle()
			s := service(t, db, o).WithSupplementVerifier(fixtureVerifier)
			initialize(t, s, "raw")
			d := requiredDescriptor()
			require.NoError(t, s.BindRequiredSupplement(ctx, &d))
			_, e := s.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			a, b := input(1, 1, common.Hash{2}, chain.GenesisHash), input(2, 1, common.Hash{3}, common.Hash{2})
			require.NoError(t, s.Commit(ctx, a))
			require.NoError(t, s.Commit(ctx, b))
			require.NoError(t, s.Finalize(ctx, 1, b.Manifest.Identity.Block))
			require.NoError(t, s.AckRaw(ctx, a.Manifest.Identity, "raw"))
			require.NoError(t, s.AckRaw(ctx, b.Manifest.Identity, "raw"))
			calls := 0
			remove := func() error { calls++; return nil }
			require.Error(t, s.WithDeletePermit(ctx, []window.Reference{rawRef(b)}, remove))
			require.Zero(t, calls)
			require.Error(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, b.Manifest.Identity)))
			for n := 1; n <= 3; n++ {
				broken := service(t, failDB{CaptureDatabase: db, at: n}, o).WithSupplementVerifier(fixtureVerifier)
				require.ErrorIs(t, broken.CommitSupplementCheckpoint(ctx, 1, d, checkpointFixture(a.Manifest.Identity.Block)), errInjected)
				status, e := s.SupplementStreamStatus(ctx, d)
				require.NoError(t, e)
				require.Nil(t, status.Parent)
				require.Nil(t, status.Complete)
			}
			cp := checkpointFixture(a.Manifest.Identity.Block)
			o.fork(1, common.Hash{99})
			require.ErrorIs(t, s.CommitSupplementCheckpoint(ctx, 1, d, cp), capture.ErrCanonical)
			o.fork(1, common.Hash{2})
			require.ErrorIs(t, service(t, uncertainDB{db}, o).WithSupplementVerifier(fixtureVerifier).CommitSupplementCheckpoint(ctx, 1, d, cp), errInjected)
			require.NoError(t, s.CommitSupplementCheckpoint(ctx, 1, d, cp))
			events, e := s.SupplementStreamEvents(ctx, d, 0, 10)
			require.NoError(t, e)
			require.Len(t, events, 1)
			require.Equal(t, "checkpoint", events[0].Kind)
			require.ErrorIs(t, s.RequiredSupplementReady(ctx, 1, b.Manifest.Identity.Block), capture.ErrSupplementIncomplete)
			require.Error(t, s.WithDeletePermit(ctx, []window.Reference{rawRef(b)}, remove))
			require.Zero(t, calls)
			require.NoError(t, s.WithDeletePermit(ctx, []window.Reference{rawRef(a)}, remove))
			require.Equal(t, 1, calls)
			require.NoError(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, b.Manifest.Identity)))
			require.NoError(t, s.RequiredSupplementReady(ctx, 1, b.Manifest.Identity.Block))
			require.ErrorIs(t, service(t, db, o).WithDeletePermit(ctx, []window.Reference{rawRef(b)}, remove), capture.ErrSupplementIncomplete)
			rejecting := s.WithSupplementVerifier(func(model.SupplementStreamDescriptor, model.FeedBlock, []byte) error { return errInjected })
			require.ErrorIs(t, rejecting.WithDeletePermit(ctx, []window.Reference{rawRef(b)}, remove), errInjected)
			require.Equal(t, 1, calls)
			require.NoError(t, s.WithDeletePermit(ctx, []window.Reference{rawRef(b)}, remove))
			require.Equal(t, 2, calls)
			require.ErrorIs(t, s.BindRequiredSupplement(ctx, nil), capture.ErrConflict)
			changed := d
			changed.Name = "renamed"
			require.ErrorIs(t, s.BindRequiredSupplement(ctx, &changed), capture.ErrConflict)
			changed = d
			changed.Required = false
			require.ErrorIs(t, s.BindRequiredSupplement(ctx, &changed), port.ErrInvalidData)
			// Missing proof bytes or receipt I/O must never hide behind metadata completion.
			broken := service(t, requiredReadFaultDB{db}, o).WithSupplementVerifier(fixtureVerifier)
			require.ErrorIs(t, broken.WithDeletePermit(ctx, []window.Reference{rawRef(b)}, remove), errInjected)
			// A pending durable deletion is not a time-based unlock or policy migration.
			require.ErrorIs(t, s.WithDeletePermit(ctx, []window.Reference{rawRef(b)}, func() error { return errInjected }), errInjected)
			require.ErrorIs(t, s.BindRequiredSupplement(ctx, nil), capture.ErrConflict)
			intent, e := s.PendingDeletion(ctx)
			require.NoError(t, e)
			require.NotZero(t, intent.Token)
		})
	}
}
func TestRequiredSupplementReorgAndLateBinding(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			o := newOracle()
			s := service(t, db, o).WithSupplementVerifier(fixtureVerifier)
			initialize(t, s)
			d := requiredDescriptor()
			require.NoError(t, s.BindRequiredSupplement(ctx, &d))
			_, e := s.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			a, b := input(1, 1, common.Hash{2}, chain.GenesisHash), input(2, 1, common.Hash{3}, common.Hash{2})
			require.NoError(t, s.Commit(ctx, a))
			require.NoError(t, s.Commit(ctx, b))
			require.NoError(t, s.CommitSupplementCheckpoint(ctx, 1, d, checkpointFixture(a.Manifest.Identity.Block)))
			require.NoError(t, s.CommitStreamSupplement(ctx, 1, d, streamSupplement(d, b.Manifest.Identity)))
			st, e := s.State(ctx)
			require.NoError(t, e)
			require.NoError(t, s.Reset(ctx, 1, st.Anchor))
			status, e := s.SupplementStreamStatus(ctx, d)
			require.NoError(t, e)
			require.Equal(t, "stale", status.Status)
			require.Nil(t, status.Complete)
			_, e = s.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			o.fork(1, common.Hash{44})
			a = input(1, 2, common.Hash{44}, chain.GenesisHash)
			o.fork(2, common.Hash{55})
			b = input(2, 2, common.Hash{55}, common.Hash{44})
			require.NoError(t, s.Commit(ctx, a))
			require.NoError(t, s.Commit(ctx, b))
			status, e = s.SupplementStreamStatus(ctx, d)
			require.NoError(t, e)
			require.Nil(t, status.Parent)
			require.Nil(t, status.Complete)
			require.Error(t, s.CommitStreamSupplement(ctx, 2, d, streamSupplement(d, b.Manifest.Identity)))
			require.NoError(t, s.CommitSupplementCheckpoint(ctx, 2, d, checkpointFixture(a.Manifest.Identity.Block)))
			require.NoError(t, s.CommitStreamSupplement(ctx, 2, d, streamSupplement(d, b.Manifest.Identity)))
			require.ErrorIs(t, s.RequiredSupplementReady(ctx, 1, b.Manifest.Identity.Block), capture.ErrStale)
			require.NoError(t, s.RequiredSupplementReady(ctx, 2, b.Manifest.Identity.Block))
			// A different pre-existing source cannot acquire a retrospective required contract.
			other := openFactory(t, backend)()
			defer func() { require.NoError(t, other.Close()) }()
			x := service(t, other, newOracle())
			initialize(t, x)
			require.NoError(t, x.Commit(ctx, input(1, 1, common.Hash{2}, chain.GenesisHash)))
			require.ErrorIs(t, x.BindRequiredSupplement(ctx, &d), capture.ErrConflict)
		})
	}
}
func TestCheckpointResetRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle()).WithSupplementVerifier(fixtureVerifier)
	initialize(t, s)
	d := requiredDescriptor()
	require.NoError(t, s.BindRequiredSupplement(ctx, &d))
	_, e := s.InitializeSupplementStream(ctx, d)
	require.NoError(t, e)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	st, e := s.State(ctx)
	require.NoError(t, e)
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		done <- s.CommitSupplementCheckpoint(context.Background(), 1, d, checkpointFixture(a.Manifest.Identity.Block))
	}()
	close(start)
	require.NoError(t, s.Reset(ctx, 1, st.Anchor))
	e = <-done
	if e != nil {
		require.True(t, errors.Is(e, capture.ErrStale))
	}
	_, e = s.ReadSupplementCheckpoint(ctx, 1, d)
	require.ErrorIs(t, e, capture.ErrStale)
}

type requiredReadFaultDB struct{ port.CaptureDatabase }
type requiredReadFaultWriter struct{ port.CaptureWriter }

func (w requiredReadFaultWriter) Get(k []byte) ([]byte, error) {
	return (streamReadFault{CaptureReader: w.CaptureWriter, payload: true}).Get(k)
}
func (d requiredReadFaultDB) CaptureUpdate(ctx context.Context, ch model.FeedChain, fn func(port.CaptureWriter) error) error {
	return d.CaptureDatabase.CaptureUpdate(ctx, ch, func(w port.CaptureWriter) error { return fn(requiredReadFaultWriter{w}) })
}

func TestRequiredCheckpointWindowRecovery(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := openFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			o := newOracle()
			s := service(t, db, o).WithSupplementVerifier(fixtureVerifier)
			initialize(t, s)
			d := requiredDescriptor()
			require.NoError(t, s.BindRequiredSupplement(ctx, &d))
			_, e := s.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			require.NoError(t, s.Commit(ctx, a))
			raw, opts, refs := recoveryRaw(t, t.TempDir())
			// Ordinary checkpoint lag retains expired raw data, without stopping capture.
			status, changed, e := capture.ControlStep(ctx, raw, s, finalityFixture{block: a.Manifest.Identity.Block}, true, 128)
			require.NoError(t, e)
			require.False(t, changed)
			require.NotNil(t, status.Blocked)
			require.Zero(t, status.Collected)
			require.NoError(t, s.CommitSupplementCheckpoint(ctx, 1, d, checkpointFixture(a.Manifest.Identity.Block)))
			require.ErrorIs(t, s.WithDeletePermit(ctx, refs, func() error { return errInjected }), errInjected)
			require.NoError(t, raw.Close())
			// A second handle cannot complete the old permit without the codec verifier.
			_, e = service(t, db, o).OpenWindow(ctx, opts)
			require.ErrorIs(t, e, capture.ErrSupplementIncomplete)
			_, e = s.PendingDeletion(ctx)
			require.NoError(t, e)
			raw, e = s.OpenWindow(ctx, opts)
			require.NoError(t, e)
			_, e = s.PendingDeletion(ctx)
			require.ErrorIs(t, e, port.ErrNotFound)
			n, e := raw.Collect(ctx, s)
			require.NoError(t, e)
			require.Zero(t, n, "Open already completed the exact pending deletion")
			_, e = raw.Lookup(ctx, refs[0].Block, refs[0].Kind)
			require.ErrorIs(t, e, window.ErrNotFound)
			require.NoError(t, raw.Close())
		})
	}
}

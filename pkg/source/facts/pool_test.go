package facts_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type heldFresh struct {
	source.CanonicalReader
	number  uint64 // zero retains the original height-one fixture
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *heldFresh) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	want := f.number
	if want == 0 {
		want = 1
	}
	if n == want {
		f.once.Do(func() { close(f.entered) })
		select {
		case <-f.release:
		case <-ctx.Done():
			return common.Hash{}, ctx.Err()
		}
	}
	return f.CanonicalReader.HashAt(ctx, n)
}

// Exercise the real writer and real capture Commit concurrently at pool4.
// Both remain inside their independent fresh checks while a prior durable
// ACK and DB-only reads progress. Reset still cannot cross either fence.
func TestFactsPublicationDuringFresh(t *testing.T) {
	for _, mode := range []string{"success", "fresh-reject", "cancel-append"} {
		t.Run(mode, func(t *testing.T) {
			db := dbFactoryWithConns(t, "postgres", 4)()
			defer func() { require.NoError(t, db.Close()) }()
			f := build(t, db, false)
			seed(t, db, f)
			live := &heldFresh{CanonicalReader: f.fresh, number: 2, entered: make(chan struct{}), release: make(chan struct{})}
			src, err := facts.New(ctx, facts.Config{Store: f.store, Profile: f.profile, Canonical: live, Consumer: "publication"})
			require.NoError(t, err)
			writer := makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
			defer writer.Close()
			require.NoError(t, writer.FetchBlock(ctx, 1))
			f.chain.AddBlockWithExtra([]byte("publication-four"))
			f.chain.SetHead(4)
			in := assembleFact(t, f, 4)
			appendFresh := &heldFresh{CanonicalReader: f.fresh, number: 4, entered: make(chan struct{}), release: make(chan struct{})}
			appender, err := capture.New(db, f.feed, appendFresh)
			require.NoError(t, err)
			workCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			appendCtx, cancelAppend := context.WithCancel(workCtx)
			defer cancelAppend()
			writeDone, appendDone := make(chan struct{}), make(chan struct{})
			var writeErr, appendErr error
			var writerOnce, appendOnce sync.Once
			releaseWriter := func() { writerOnce.Do(func() { close(live.release) }) }
			releaseAppend := func() { appendOnce.Do(func() { close(appendFresh.release) }) }
			go func() { writeErr = writer.FetchBlock(workCtx, 2); close(writeDone) }()
			defer func() { releaseWriter(); cancel(); <-writeDone }()
			select {
			case <-live.entered:
			case <-workCtx.Done():
				t.Fatal("writer did not reach fresh", workCtx.Err())
			}
			go func() { appendErr = appender.Commit(appendCtx, in); close(appendDone) }()
			defer func() { releaseAppend(); cancelAppend(); <-appendDone }()
			select {
			case <-appendFresh.entered:
			case <-workCtx.Done():
				t.Fatal("capture must reach fresh while the writer holds its generation fence", workCtx.Err())
			}
			probe, stop := context.WithTimeout(workCtx, time.Second)
			defer stop()
			require.NoError(t, f.store.AckProjection(probe, "publication", 1, f.inputs[0].Manifest.Identity), "durable ACK retry must not wait behind capture fresh")
			_, _, err = src.BlockWithReceipts(probe, 3)
			require.NoError(t, err)
			_, err = db.GetBlock(probe, 1)
			require.NoError(t, err)
			_, err = db.GetBlock(probe, 2)
			require.ErrorIs(t, err, port.ErrNotFound, "staged projection remains invisible")
			state, err := f.store.State(probe)
			require.NoError(t, err)
			require.EqualValues(t, 3, state.Through.Number, "uncommitted capture remains invisible")
			fenced, stopFence := context.WithTimeout(workCtx, 30*time.Millisecond)
			err = f.store.Reset(fenced, 1, f.inputs[0].Manifest.Identity.Block)
			stopFence()
			require.Error(t, err, "reset must wait for both independent fresh fences")
			if mode == "fresh-reject" {
				f.fresh.setWrong(true)
			}
			if mode == "cancel-append" {
				cancelAppend()
			}
			releaseAppend()
			<-appendDone
			switch mode {
			case "success":
				require.NoError(t, appendErr)
			case "fresh-reject":
				require.ErrorIs(t, appendErr, capture.ErrCanonical)
			case "cancel-append":
				require.ErrorIs(t, appendErr, context.Canceled)
			}
			f.fresh.setWrong(false)
			state, err = f.store.State(workCtx)
			require.NoError(t, err)
			want := uint64(3)
			if mode == "success" {
				want = 4
			}
			require.Equal(t, want, state.Through.Number)
			_, err = f.store.Read(workCtx, in.Manifest.Identity)
			if mode == "success" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, port.ErrNotFound)
			}
			releaseWriter()
			<-writeDone
			require.NoError(t, writeErr)
			_, err = f.store.ProjectionAck(workCtx, "publication", 1, f.inputs[1].Manifest.Identity)
			require.NoError(t, err)
			events, err := f.store.Events(workCtx, 0, 10)
			require.NoError(t, err)
			require.Len(t, events, int(want), "failed/cancelled appends publish no event")
			require.NoError(t, f.store.Reset(workCtx, 1, f.inputs[0].Manifest.Identity.Block))
			require.ErrorIs(t, appender.Commit(workCtx, in), capture.ErrStale)
			require.ErrorIs(t, f.store.AckProjection(workCtx, "publication", 1, f.inputs[1].Manifest.Identity), capture.ErrStale)
			_, err = db.GetBlock(workCtx, 2)
			require.NoError(t, err, "source reset does not erase durable projection; undo owns reconciliation")
		})
	}
}

// Hold the REAL writer inside its live canonical check. Spare pool capacity
// must let acquisition read immutable facts without exposing staged projection
// rows or allowing a reset through the writer's capture generation fence.
func TestFactsReadsDuringFresh(t *testing.T) {
	for _, trial := range []struct {
		budget int32
		reject bool
	}{{1, false}, {2, false}, {4, false}, {4, true}} {
		name := fmt.Sprint(trial.budget)
		if trial.reject {
			name += "-reject"
		}
		t.Run(name, func(t *testing.T) {
			budget := trial.budget
			db := dbFactoryWithConns(t, "postgres", budget)()
			defer func() { require.NoError(t, db.Close()) }()
			f := build(t, db, false)
			seed(t, db, f)
			live := &heldFresh{CanonicalReader: f.fresh, entered: make(chan struct{}), release: make(chan struct{})}
			src, e := facts.New(ctx, facts.Config{Store: f.store, Profile: f.profile, Canonical: live, Consumer: "pool-fence"})
			require.NoError(t, e)
			writer := makeFetcher(t, db, src, src, src.BindFeatures(feature.Deps{Storage: db, Logger: zap.NewNop()}))
			workCtx, stop := context.WithCancel(ctx)
			done := make(chan struct{})
			var writeErr error
			var release sync.Once
			unblock := func() { release.Do(func() { close(live.release) }) }
			go func() { writeErr = writer.FetchBlock(workCtx, 1); close(done) }()
			// Join before the deferred database Close, even if an assertion fails.
			defer func() { unblock(); stop(); <-done; writer.Close() }()
			select {
			case <-live.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not reach its fresh check")
			}
			readTimeout := 2 * time.Second
			if budget == 1 {
				readTimeout = 100 * time.Millisecond
			}
			readCtx, cancel := context.WithTimeout(ctx, readTimeout)
			b, rs, e := src.BlockWithReceipts(readCtx, 2)
			cancel()
			if budget == 1 {
				require.ErrorIs(t, e, context.DeadlineExceeded)
				require.Nil(t, b)
				require.Nil(t, rs)
			} else {
				require.NoError(t, e)
				require.Equal(t, f.chain.Block(2).Block.Hash(), b.Hash)
				_, e = db.GetBlock(ctx, 1)
				require.ErrorIs(t, e, port.ErrNotFound, "uncommitted writer rows must remain invisible")
			}
			resetCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			e = f.store.Reset(resetCtx, src.Epoch(), f.inputs[0].Manifest.Identity.Block)
			cancel()
			require.Error(t, e, "spare connections must not bypass the writer's generation fence")
			if trial.reject {
				f.fresh.setWrong(true)
			}
			unblock()
			<-done
			if trial.reject {
				require.ErrorIs(t, writeErr, capture.ErrCanonical)
				_, e = db.GetBlock(ctx, 1)
				require.ErrorIs(t, e, port.ErrNotFound)
				seq, e := db.(port.Outbox).LastOutboxSeq(ctx)
				require.NoError(t, e)
				require.Zero(t, seq)
			} else {
				require.NoError(t, writeErr)
			}
			b, _, e = src.BlockWithReceipts(ctx, 2)
			require.NoError(t, e)
			require.Equal(t, f.chain.Block(2).Block.Hash(), b.Hash)
			_, e = f.store.ProjectionAck(ctx, "pool-fence", src.Epoch(), f.inputs[0].Manifest.Identity)
			if trial.reject {
				require.ErrorIs(t, e, port.ErrNotFound)
			} else {
				require.NoError(t, e)
			}
			st, e := f.store.State(ctx)
			require.NoError(t, e)
			require.Equal(t, src.Epoch(), st.Generation)
		})
	}
}

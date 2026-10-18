package evmblob

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

func TestBlobWorkerCompletesThenJoins(t *testing.T) {
	db := database(t, "pebble")()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	f := blockFixture(t, []Entry{vector(false)})
	o := &oracle{block: f.id.Block}
	s, d := initialize(t, db, f, o)
	p := &provider{raw: f.raw}
	supplier, e := New(s, evm.New("evm"), p)
	require.NoError(t, e)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	var runErr error
	go func() { runErr = supplier.Run(ctx, d, 20*time.Millisecond); close(done) }()
	// Always join before a failed assertion closes the borrowed database.
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("worker did not join")
		}
	})
	require.Eventually(t, func() bool {
		status, e := s.SupplementStreamStatus(t.Context(), d)
		return e == nil && status.Complete != nil
	}, 5*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 1, p.calls.Load())
	cancel()
	select {
	case <-done:
		require.ErrorIs(t, runErr, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("worker did not join")
	}
}
func TestBlobWorkerNeverSkipsMissingData(t *testing.T) {
	db := database(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	f := blockFixture(t, []Entry{vector(false)})
	o := &oracle{block: f.id.Block}
	s, d := initialize(t, db, f, o)
	p := &provider{err: errIO}
	supplier, e := New(s, evm.New("evm"), p)
	require.NoError(t, e)
	e = supplier.Run(t.Context(), d, 20*time.Millisecond)
	require.ErrorIs(t, e, ErrUnavailable)
	require.ErrorIs(t, e, errIO)
	status, e := s.SupplementStreamStatus(t.Context(), d)
	require.NoError(t, e)
	require.Nil(t, status.Complete)
	require.EqualValues(t, 1, status.Backlog)
}

type canceledDB struct{ cancel context.CancelFunc }

func (d canceledDB) CaptureView(context.Context, model.FeedChain, func(port.CaptureReader) error) error {
	return errIO
}
func (d canceledDB) CaptureUpdate(context.Context, model.FeedChain, func(port.CaptureWriter) error) error {
	d.cancel()
	return errIO
}
func TestBlobWorkerCancellationPreservesIO(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := blockFixture(t, nil)
	o := &oracle{block: f.id.Block}
	s, e := capture.New(canceledDB{cancel: cancel}, f.id.Block.Chain, o)
	require.NoError(t, e)
	d, e := Descriptor("blobs-v1", 1, Definition{Provider: "relay", Profile: "evm", Format: Format})
	require.NoError(t, e)
	supplier, e := New(s, evm.New("evm"), &provider{err: errIO})
	require.NoError(t, e)
	e = supplier.Run(ctx, d, 20*time.Millisecond)
	require.True(t, errors.Is(e, errIO))
	require.ErrorIs(t, e, context.Canceled)
}

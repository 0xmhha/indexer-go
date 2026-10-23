package execution

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type controlledBackend struct {
	active, max atomic.Int32
	mu          sync.Mutex
	seen        map[port.ExecutionWriter][]uint64
	published   []uint64
	block       chan struct{}
	entered     chan port.ExecutionWriter
	fail        error
	pubBlock    chan struct{}
	pubEntered  chan struct{}
}

func (b *controlledBackend) Part(ctx context.Context, w port.ExecutionWriter, p *Prepared) error {
	n := b.active.Add(1)
	defer b.active.Add(-1)
	for {
		m := b.max.Load()
		if n <= m || b.max.CompareAndSwap(m, n) {
			break
		}
	}
	if b.entered != nil {
		select {
		case b.entered <- w:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if b.block != nil {
		select {
		case <-b.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if b.fail != nil {
		return b.fail
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen == nil {
		b.seen = map[port.ExecutionWriter][]uint64{}
	}
	b.seen[w] = append(b.seen[w], p.result.Ref.Height)
	return nil
}
func (b *controlledBackend) Publish(ctx context.Context, p *Prepared) error {
	if b.pubEntered != nil {
		select {
		case b.pubEntered <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if b.pubBlock != nil {
		select {
		case <-b.pubBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, w := range port.ExecutionWriters() {
		found := false
		for _, h := range b.seen[w] {
			found = found || h == p.result.Ref.Height
		}
		if !found {
			return errors.New("publish before all seven parts")
		}
	}
	b.published = append(b.published, p.result.Ref.Height)
	return nil
}
func schedulerWork(n uint64) *Prepared {
	return &Prepared{result: execwire.Result{Ref: execwire.Ref{Generation: 1, Height: n, Hash: execwire.Hash{byte(n + 1)}}, Parent: execwire.Hash{byte(n)}}, bytes: 100, digest: execwire.Hash{byte(n + 1)}}
}
func schedulerCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func TestSchedulerSevenOrderedQueuesAndTwoPermits(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := schedulerCtx(t)
	b := &controlledBackend{block: make(chan struct{}), entered: make(chan port.ExecutionWriter, 32)}
	s, err := New(ctx, Config{Anchor: schedulerWork(0).result.Ref, MaxBlocks: 3, MaxBytes: 300}, b)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	var fs []*Future
	for n := uint64(1); n <= 3; n++ {
		f, err := s.Submit(ctx, schedulerWork(n))
		require.NoError(t, err)
		fs = append(fs, f)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-b.entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Equal(t, int32(2), b.active.Load())
	require.Equal(t, int32(2), b.max.Load())
	close(b.block)
	for _, f := range fs {
		require.NoError(t, f.Wait(ctx))
	}
	require.NoError(t, s.Close())
	for _, w := range port.ExecutionWriters() {
		require.Equal(t, []uint64{1, 2, 3}, b.seen[w])
	}
	require.Equal(t, []uint64{1, 2, 3}, b.published)
	require.Zero(t, b.active.Load())
}
func TestSchedulerActiveBudgetWaitCancelAndPublicationNoPermit(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := schedulerCtx(t)
	b := &controlledBackend{pubBlock: make(chan struct{}), pubEntered: make(chan struct{}, 2)}
	s, err := New(ctx, Config{Anchor: schedulerWork(0).result.Ref, MaxBlocks: 2, MaxBytes: 200}, b)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	f, err := s.Submit(ctx, schedulerWork(1))
	require.NoError(t, err)
	select {
	case <-b.pubEntered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Zero(t, b.active.Load())
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, f.Wait(cancelled), context.Canceled)
	f2, err := s.Submit(ctx, schedulerWork(2))
	require.NoError(t, err)
	_, err = s.Submit(cancelled, schedulerWork(3))
	require.ErrorIs(t, err, context.Canceled)
	b.mu.Lock()
	require.NotEmpty(t, b.seen)
	b.mu.Unlock()
	close(b.pubBlock)
	require.NoError(t, f.Wait(ctx))
	require.NoError(t, f2.Wait(ctx))
	f3, err := s.Submit(ctx, schedulerWork(3))
	require.NoError(t, err)
	require.NoError(t, f3.Wait(ctx))
}
func TestSchedulerFailureAndCloseJoinAllOwnedWork(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := schedulerCtx(t)
	failure := errors.New("part failed")
	b := &controlledBackend{fail: failure}
	s, err := New(ctx, Config{Anchor: schedulerWork(0).result.Ref, MaxBlocks: 1, MaxBytes: 100}, b)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	f, err := s.Submit(ctx, schedulerWork(1))
	require.NoError(t, err)
	require.ErrorIs(t, f.Wait(ctx), failure)
	require.ErrorIs(t, s.Close(), failure)
	require.Zero(t, b.active.Load())
}
func TestSchedulerRejectsGapGenerationAndOversize(t *testing.T) {
	ctx := schedulerCtx(t)
	s, err := New(ctx, Config{Anchor: schedulerWork(0).result.Ref, MaxBlocks: 1, MaxBytes: 99}, &controlledBackend{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	_, err = s.Submit(ctx, schedulerWork(1))
	require.Error(t, err)
	s2, err := New(ctx, Config{Anchor: schedulerWork(0).result.Ref, MaxBlocks: 1, MaxBytes: 100}, &controlledBackend{})
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	_, err = s2.Submit(ctx, schedulerWork(2))
	require.ErrorIs(t, err, port.ErrExecutionSequence)
	bad := schedulerWork(1)
	bad.result.Ref.Generation = 2
	_, err = s2.Submit(ctx, bad)
	require.ErrorIs(t, err, port.ErrExecutionStale)
}

func TestSchedulerCloseCancelsBlockedPartsAndPendingSubmit(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	ctx := schedulerCtx(t)
	b := &controlledBackend{block: make(chan struct{}), entered: make(chan port.ExecutionWriter, 32)}
	s, err := New(ctx, Config{Anchor: schedulerWork(0).result.Ref, MaxBlocks: 1, MaxBytes: 100}, b)
	require.NoError(t, err)
	f, err := s.Submit(ctx, schedulerWork(1))
	require.NoError(t, err)
	select {
	case <-b.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	submitted := make(chan error, 1)
	go func() { _, err := s.Submit(ctx, schedulerWork(2)); submitted <- err }()
	require.NoError(t, s.Close())
	require.Error(t, f.Wait(ctx))
	select {
	case err := <-submitted:
		require.Error(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Zero(t, b.active.Load())
}

func TestSchedulerAcceptedDuplicateSharesFutureWithoutNewBudget(t *testing.T) {
	ctx := schedulerCtx(t)
	b := &controlledBackend{block: make(chan struct{}), entered: make(chan port.ExecutionWriter, 32)}
	s, err := New(ctx, Config{Anchor: schedulerWork(0).result.Ref, MaxBlocks: 1, MaxBytes: 100}, b)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	f, err := s.Submit(ctx, schedulerWork(1))
	require.NoError(t, err)
	duplicate, err := s.Submit(ctx, schedulerWork(1))
	require.NoError(t, err)
	require.Same(t, f, duplicate)
	bad := schedulerWork(1)
	bad.digest[0] ^= 1
	_, err = s.Submit(ctx, bad)
	require.ErrorIs(t, err, port.ErrExecutionConflict)
	close(b.block)
	require.NoError(t, f.Wait(ctx))
	require.NoError(t, duplicate.Wait(ctx))
	require.NoError(t, s.Close())
	require.Equal(t, []uint64{1}, b.published)
}

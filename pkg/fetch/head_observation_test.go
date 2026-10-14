package fetch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHeadObservationSharesConcurrentReads(t *testing.T) {
	var observer heightObserver
	var calls atomic.Int64
	started, release := make(chan struct{}), make(chan struct{})
	query := func(context.Context) (uint64, bool, error) {
		calls.Add(1)
		close(started)
		<-release
		return 120, true, nil
	}
	var wg sync.WaitGroup
	results := make(chan heightObservation, 32)
	for range 32 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- observer.read(t.Context(), time.Hour, query) }()
	}
	<-started
	close(release)
	wg.Wait()
	close(results)
	var latency time.Duration
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, uint64(120), result.height)
		if latency == 0 {
			latency = result.latency
		}
		require.Equal(t, latency, result.latency, "cached callers report original RPC latency")
	}
	require.Equal(t, int64(1), calls.Load())
}

func TestHeadObservationFreshnessFailureAndLowerHead(t *testing.T) {
	now := time.Now()
	observer := heightObserver{now: func() time.Time { return now }}
	calls := 0
	head := uint64(120)
	var failure error
	query := func(context.Context) (uint64, bool, error) { calls++; return head, true, failure }
	require.Equal(t, head, observer.read(t.Context(), time.Second, query).height)
	head = 125
	require.Equal(t, uint64(120), observer.read(t.Context(), time.Second, query).height)
	require.Equal(t, 1, calls)
	require.Equal(t, head, observer.read(t.Context(), 0, query).height, "live polls bypass recent values")
	head = 110
	now = now.Add(time.Second)
	require.Equal(t, head, observer.read(t.Context(), time.Second, query).height, "reorg can lower the head")
	failure = errors.New("node offline")
	require.ErrorIs(t, observer.read(t.Context(), 0, query).err, failure)
	require.ErrorIs(t, observer.read(t.Context(), time.Second, query).err, failure, "failure invalidates previous success")
	failure = nil
	head = 130
	require.Equal(t, head, observer.read(t.Context(), time.Second, query).height)
	require.Equal(t, 6, calls)
}

func TestHeadObservationWaiterCancellation(t *testing.T) {
	var observer heightObserver
	started, release := make(chan struct{}), make(chan struct{})
	owner := make(chan heightObservation, 1)
	go func() {
		owner <- observer.read(t.Context(), 0, func(context.Context) (uint64, bool, error) {
			close(started)
			<-release
			return 120, true, nil
		})
	}()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	waiter := make(chan heightObservation, 1)
	go func() {
		waiter <- observer.read(ctx, time.Hour, func(context.Context) (uint64, bool, error) { panic("duplicate read") })
	}()
	cancel()
	require.ErrorIs(t, (<-waiter).err, context.Canceled)
	close(release)
	require.NoError(t, (<-owner).err)
}

func TestHeadObservationCancelledOwnerDoesNotCancelOtherCaller(t *testing.T) {
	var observer heightObserver
	var calls atomic.Int64
	started := make(chan struct{})
	query := func(ctx context.Context) (uint64, bool, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return 0, false, ctx.Err()
		}
		return 120, true, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	owner := make(chan heightObservation, 1)
	go func() { owner <- observer.read(ctx, 0, query) }()
	<-started
	waiter := make(chan heightObservation, 1)
	go func() { waiter <- observer.read(t.Context(), time.Hour, query) }()
	cancel()
	require.ErrorIs(t, (<-owner).err, context.Canceled)
	result := <-waiter
	require.NoError(t, result.err)
	require.Equal(t, uint64(120), result.height)
	require.Equal(t, int64(2), calls.Load())
}

func TestHeadObservationFreshPollDoesNotJoinOlderRead(t *testing.T) {
	var observer heightObserver
	var calls atomic.Int64
	started, release := make(chan struct{}), make(chan struct{})
	query := func(context.Context) (uint64, bool, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return 120, true, nil
		}
		return 125, true, nil
	}
	old := make(chan heightObservation, 1)
	go func() { old <- observer.read(t.Context(), time.Hour, query) }()
	<-started
	var released sync.Once
	defer released.Do(func() { close(release) })
	fresh := make(chan heightObservation, 1)
	go func() { fresh <- observer.read(t.Context(), 0, query) }()
	require.Eventually(t, func() bool { return calls.Load() == 2 }, time.Second, time.Millisecond,
		"a new head notification or explicit health probe starts its own RPC")
	require.Equal(t, uint64(125), (<-fresh).height)
	released.Do(func() { close(release) })
	result := <-old
	require.NoError(t, result.err)
	require.Equal(t, uint64(125), result.height, "older completion must not replace a newer observation")
	require.Equal(t, uint64(125), observer.read(t.Context(), time.Hour, query).height)
	require.Equal(t, int64(2), calls.Load())
}

type observedHeadClient struct {
	*movingHeadClient
	calls atomic.Int64
}

func (c *observedHeadClient) GetLatestBlockNumber(ctx context.Context) (uint64, error) {
	c.calls.Add(1)
	return c.movingHeadClient.GetLatestBlockNumber(ctx)
}

func TestHeadObservationMonitoringDoesNotReplaceLivePoll(t *testing.T) {
	client := &observedHeadClient{movingHeadClient: &movingHeadClient{mockClient: newMockClient()}}
	client.head.Store(120)
	f := newLifecycleFetcher(t, client, &Config{Finality: FinalityConfirmations, Confirmations: 3})
	n, ok, err := f.targetHead(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(117), n)
	client.head.Store(125)
	head, _, err := f.ObserveNodeHeight(t.Context(), time.Hour)
	require.NoError(t, err)
	require.Equal(t, uint64(120), head, "health observation is raw head, not confirmations target")
	n, ok, err = f.readTargetHead(t.Context(), false, progressPoll)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(117), n)
	require.Equal(t, int64(1), client.calls.Load())
	n, ok, err = f.targetHead(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(122), n, "new head notifications still cause a fresh live poll")
	require.Equal(t, int64(2), client.calls.Load())
}

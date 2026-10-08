package fetch

import (
	"context"
	"errors"
	"math/big"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

func newLifecycleFetcher(t *testing.T, client Client, cfg *Config) *Fetcher {
	t.Helper()
	store, err := storagepkg.NewPebbleStorage(storagepkg.DefaultConfig(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return NewFetcher(client, store, cfg, zap.NewNop(), nil)
}

// waitGoroutines waits until the goroutine count drops to at most want.
func waitGoroutines(want int, within time.Duration) int {
	deadline := time.Now().Add(within)
	for {
		n := runtime.NumGoroutine()
		if n <= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestFetchRangeConcurrentDoesNotLeakOnError covers C1: an early error used
// to leave workers blocked on the results channel forever.
func TestFetchRangeConcurrentDoesNotLeakOnError(t *testing.T) {
	client := newMockClient() // has no blocks: every fetch fails
	f := newLifecycleFetcher(t, client, &Config{BatchSize: 1, MaxRetries: 1, RetryDelay: time.Millisecond, NumWorkers: 32})

	before := runtime.NumGoroutine()
	err := f.FetchRangeConcurrent(context.Background(), 0, 500)
	require.Error(t, err)

	after := waitGoroutines(before, 2*time.Second)
	require.LessOrEqual(t, after, before, "goroutines leaked: before=%d after=%d", before, after)
}

func TestSleepCtxStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	start := time.Now()
	err := sleepCtx(ctx, time.Hour)
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), time.Second)
	require.NoError(t, sleepCtx(context.Background(), time.Millisecond))
}

// TestRunStopsPromptlyWhileWaiting covers C4: the live loop slept without
// checking the context, so shutdown waited for the full retry delay.
func TestRunStopsPromptlyWhileWaiting(t *testing.T) {
	client := newMockClient() // latest block 0, nothing to fetch beyond
	f := newLifecycleFetcher(t, client, &Config{StartHeight: 10, BatchSize: 1, MaxRetries: 1, RetryDelay: time.Hour, NumWorkers: 1})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

// hangingClient never answers until its context ends.
type hangingClient struct{ *mockClient }

func (hangingClient) GetBlockByNumber(ctx context.Context, _ uint64) (*types.Block, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (hangingClient) BalanceAt(ctx context.Context, _ common.Address, _ *big.Int) (*big.Int, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestRPCTimeoutBoundsCalls covers the missing per-call timeout: a stalled
// node used to block indexing indefinitely.
func TestRPCTimeoutBoundsCalls(t *testing.T) {
	f := newLifecycleFetcher(t, hangingClient{newMockClient()}, &Config{BatchSize: 1, MaxRetries: 1, NumWorkers: 1, RPCTimeout: 50 * time.Millisecond})

	start := time.Now()
	_, err := f.balanceAt(context.Background(), common.Address{}, nil)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "got %v", err)
	require.Less(t, time.Since(start), time.Second)
}

// movingHeadClient is a node whose head the test moves while Run reads it.
type movingHeadClient struct {
	*mockClient
	head atomic.Uint64
}

func (c *movingHeadClient) GetLatestBlockNumber(context.Context) (uint64, error) {
	return c.head.Load(), nil
}

// TestProgressFollowsTheNodeWhileRetrying: while a batch keeps failing (no
// block source here, and an hour between retries) the live loop does not
// poll the node, yet Progress follows the node's head, so an API can tell
// that indexing is behind.
func TestProgressFollowsTheNodeWhileRetrying(t *testing.T) {
	saved := progressPoll
	progressPoll = 10 * time.Millisecond
	defer func() { progressPoll = saved }()

	client := &movingHeadClient{mockClient: newMockClient()}
	client.head.Store(10)
	f := newLifecycleFetcher(t, client, &Config{BatchSize: 100, MaxRetries: 1, RetryDelay: time.Hour, NumWorkers: 1})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()
	require.Eventually(t, func() bool { target, ok := f.Progress(); return ok && target == 10 }, 5*time.Second, 5*time.Millisecond)

	client.head.Store(500) // the loop is waiting an hour to retry
	require.Eventually(t, func() bool { target, _ := f.Progress(); return target == 500 }, 5*time.Second, 5*time.Millisecond,
		"the progress follows the node while the batch retries")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

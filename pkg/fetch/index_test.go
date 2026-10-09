package fetch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/events"
)

func TestFetchBlock(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	require.NoError(t, h.f.FetchBlock(context.Background(), 0))
	require.NoError(t, h.f.FetchBlock(context.Background(), 1))
	h.requireIndexed(t, 0, 1)
}

func TestFetchBlockWithoutSourceFails(t *testing.T) {
	h := newChainHarness(t, &Config{MaxRetries: 1}, nil)
	h.f.SetSource(nil)
	require.ErrorIs(t, h.f.FetchBlock(context.Background(), 0), errNoSource)
}

func TestFetchBlockWithRetry(t *testing.T) {
	h := newChainHarness(t, &Config{MaxRetries: 3}, nil)
	h.src.failNext(2)
	require.NoError(t, h.f.FetchBlock(context.Background(), 0))
	h.requireIndexed(t, 0, 0)
}

// TestExponentialBackoff: retry n waits RetryDelay * 2^(n-1).
func TestExponentialBackoff(t *testing.T) {
	h := newChainHarness(t, &Config{MaxRetries: 3, RetryDelay: 20 * time.Millisecond}, nil)
	h.src.failNext(2)
	start := time.Now()
	require.NoError(t, h.f.FetchBlock(context.Background(), 0))
	require.GreaterOrEqual(t, time.Since(start), 60*time.Millisecond, "20ms then 40ms")
}

func TestFetchRange(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	head := h.chain.Head()
	require.NoError(t, h.f.FetchRange(context.Background(), 0, head))
	h.requireIndexed(t, 0, head)
}

// TestFetchRangeConcurrent fetches with several workers and indexes in
// height order: the stored chain and the published block events follow the
// chain, also when reads fail and are retried.
func TestFetchRangeConcurrent(t *testing.T) {
	bus := events.NewEventBus(1000, 1000)
	go bus.Run()
	t.Cleanup(bus.Stop)
	sub := bus.Subscribe("blocks", []events.EventType{events.EventTypeBlock}, nil, 1000)

	h := newChainHarness(t, &Config{NumWorkers: 4, MaxRetries: 3}, bus)
	h.src.failNext(3)
	head := h.chain.Head()
	require.NoError(t, h.f.FetchRangeConcurrent(context.Background(), 0, head))
	h.requireIndexed(t, 0, head)

	var got []uint64
	require.Eventually(t, func() bool {
		for {
			select {
			case ev := <-sub.Channel:
				got = append(got, ev.(*events.BlockEvent).Number)
			default:
				return uint64(len(got)) == head+1
			}
		}
	}, 5*time.Second, 10*time.Millisecond)
	for i, n := range got {
		require.Equal(t, uint64(i), n, "block events in height order")
	}
}

func TestFillGaps(t *testing.T) {
	h := newChainHarness(t, &Config{NumWorkers: 4}, nil)
	ctx := context.Background()
	head := h.chain.Head()
	require.NoError(t, h.f.FetchRange(ctx, 0, 3))
	require.NoError(t, h.f.FetchRange(ctx, 8, head))

	gaps, err := h.f.DetectGaps(ctx, 0, head)
	require.NoError(t, err)
	require.Equal(t, []GapRange{{Start: 4, End: 7}}, gaps)
	// No features are enabled, so filling below indexed blocks is allowed.
	require.NoError(t, h.f.FillGaps(ctx, gaps))

	gaps, err = h.f.DetectGaps(ctx, 0, head)
	require.NoError(t, err)
	require.Empty(t, gaps)
	h.requireIndexed(t, 0, head)
}

func TestFillGapsEmptyIsNoop(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	require.NoError(t, h.f.FillGaps(context.Background(), nil))
}

// TestRun indexes up to the node's head and returns when the context ends.
func TestRun(t *testing.T) {
	h := newChainHarness(t, &Config{PollInterval: 5 * time.Millisecond}, nil)
	head := h.chain.Head()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.f.Run(ctx) }()
	require.Eventually(t, func() bool {
		latest, err := h.db.GetLatestHeight(context.Background())
		return err == nil && latest == head
	}, 10*time.Second, 10*time.Millisecond)
	cancel()
	require.True(t, errors.Is(<-done, context.Canceled))
	h.requireIndexed(t, 0, head)
}

// TestRunWithGapRecovery fills missing blocks below the cursor before
// following the node.
func TestRunWithGapRecovery(t *testing.T) {
	h := newChainHarness(t, &Config{PollInterval: 5 * time.Millisecond}, nil)
	head := h.chain.Head()
	require.NoError(t, h.f.FetchRange(context.Background(), 0, 3))
	require.NoError(t, h.f.FetchRange(context.Background(), 8, head))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.f.RunWithGapRecovery(ctx) }()
	require.Eventually(t, func() bool {
		gaps, err := h.f.DetectGaps(context.Background(), 0, head)
		return err == nil && len(gaps) == 0
	}, 10*time.Second, 10*time.Millisecond)
	cancel()
	<-done
	h.requireIndexed(t, 0, head)
}

// TestGapRecoveryRetriesFailedRounds: a gap the first round cannot fill
// (the node fails every attempt at a block) is filled by a later round
// instead of being left behind while the live loop follows the node.
func TestGapRecoveryRetriesFailedRounds(t *testing.T) {
	// One worker: the three failures are the three attempts (MaxRetries+1)
	// at the gap's first block, so the first round fails.
	h := newChainHarness(t, &Config{PollInterval: 5 * time.Millisecond, MaxRetries: 2, NumWorkers: 1}, nil)
	head := h.chain.Head()
	require.NoError(t, h.f.FetchRange(context.Background(), 0, 3))
	require.NoError(t, h.f.FetchRange(context.Background(), 8, head))
	h.src.failNext(3)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.f.RunWithGapRecovery(ctx) }()
	require.Eventually(t, func() bool {
		gaps, err := h.f.DetectGaps(context.Background(), 0, head)
		return err == nil && len(gaps) == 0
	}, 10*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled, "recovery succeeded and the live loop ran")
	h.requireIndexed(t, 0, head)
}

// TestGapRecoveryGivesUpVisibly: when every round fails, startup stops with
// the error instead of following the node with the gap left behind.
func TestGapRecoveryGivesUpVisibly(t *testing.T) {
	h := newChainHarness(t, &Config{PollInterval: 5 * time.Millisecond, MaxRetries: 2}, nil)
	head := h.chain.Head()
	require.NoError(t, h.f.FetchRange(context.Background(), 0, 3))
	require.NoError(t, h.f.FetchRange(context.Background(), 8, head))
	h.src.failNext(1 << 20)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := h.f.RunWithGapRecovery(ctx)
	require.ErrorIs(t, err, errFlaky)
	assert.Contains(t, err.Error(), "gap recovery between blocks")
	require.NoError(t, ctx.Err(), "it gave up on its own, not at the deadline")
}

// TestRecoverRetriesRelayJoin: a relay that could not join the change
// stream when the fetcher was built joins in Recover; when it still cannot,
// Recover returns the error instead of letting indexing run with a relay
// that delivers nothing.
func TestRecoverRetriesRelayJoin(t *testing.T) {
	ctx := context.Background()
	h := newChainHarness(t, &Config{}, events.NewEventBus(16, 16))
	require.NotNil(t, h.f.outbox)
	require.NotNil(t, h.f.outbox.relay)

	h.f.outbox.joinErr = errors.New("storage busy")
	require.NoError(t, h.f.joinRelay(ctx), "the retry joins")
	require.NoError(t, h.f.outbox.joinErr)

	h.f.outbox.joinErr = errors.New("storage busy")
	require.NoError(t, h.db.Close())
	err := h.f.joinRelay(ctx)
	require.ErrorContains(t, err, "cannot join the change stream")
}

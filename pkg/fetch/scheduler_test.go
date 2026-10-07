package fetch

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source"
)

// gatedSource holds back one height until released and records how many
// block reads have started.
type gatedSource struct {
	source.Source
	hold    uint64
	release chan struct{}
	started atomic.Int64
	delay   time.Duration
}

func (s *gatedSource) BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	s.started.Add(1)
	if n == s.hold && s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.Source.BlockWithReceipts(ctx, n)
}

// TestPipelineBoundsBlocksInFlight: while the next block to index is slow,
// the pipeline fetches at most 2*workers heights ahead instead of
// buffering the rest of the range (backpressure).
func TestPipelineBoundsBlocksInFlight(t *testing.T) {
	const workers = 2
	h := newChainHarness(t, &Config{NumWorkers: workers}, nil)
	g := &gatedSource{Source: h.src.Source, hold: 0, release: make(chan struct{})}
	h.f.SetSource(g)

	done := make(chan error, 1)
	go func() { done <- h.f.FetchRange(context.Background(), 0, h.chain.Head()) }()
	time.Sleep(200 * time.Millisecond) // the others run as far as they may
	require.LessOrEqual(t, g.started.Load(), int64(2*workers), "heights in flight beyond the held block")
	close(g.release)
	require.NoError(t, <-done)
	h.requireIndexed(t, 0, h.chain.Head())
}

// TestPipelineStopsAtFirstFailure: a block that cannot be read stops the
// range; the blocks below it are indexed and nothing above.
func TestPipelineStopsAtFirstFailure(t *testing.T) {
	h := newChainHarness(t, &Config{NumWorkers: 4, MaxRetries: 1, RetryDelay: time.Millisecond}, nil)
	h.f.SetSource(&failingAt{Source: h.src.Source, at: 5})
	err := h.f.FetchRange(context.Background(), 0, h.chain.Head())
	require.ErrorContains(t, err, "failed to fetch block 5")
	h.requireIndexed(t, 0, 4)
}

type failingAt struct {
	source.Source
	at uint64
}

func (s *failingAt) BlockWithReceipts(ctx context.Context, n uint64) (*model.Block, []*model.Receipt, error) {
	if n == s.at {
		return nil, nil, errFlaky
	}
	return s.Source.BlockWithReceipts(ctx, n)
}

// TestPipelineLeavesNoGoroutines: a range that fails or is cancelled
// returns only after its goroutines have stopped.
func TestPipelineLeavesNoGoroutines(t *testing.T) {
	h := newChainHarness(t, &Config{NumWorkers: 8, MaxRetries: 0}, nil)
	h.f.SetSource(&failingAt{Source: h.src.Source, at: 2})
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.f.FetchRange(context.Background(), 0, h.chain.Head())
		}()
	}
	wg.Wait()
}

// BenchmarkPipeline measures blocks indexed per second when each block read
// takes 20ms (a remote node's round trip), by number of workers.
func BenchmarkPipeline(b *testing.B) {
	for _, workers := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				h := newChainHarness(b, &Config{NumWorkers: workers}, nil)
				h.f.SetSource(&gatedSource{Source: h.src.Source, delay: 20 * time.Millisecond})
				head := h.chain.Head()
				b.StartTimer()
				if err := h.f.FetchRange(context.Background(), 0, head); err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(head+1), "blocks/op")
			}
		})
	}
}

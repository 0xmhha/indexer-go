package fetch

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

var prepareTestID atomic.Uint64

type preparingFeature struct {
	name    string
	prepare func(context.Context, *feature.Block) (feature.PreparedHandler, error)
}

func (f preparingFeature) Name() string     { return f.name }
func (preparingFeature) Requires() []string { return nil }
func (f preparingFeature) Register(r feature.Registrar) error {
	r.OnBlock(preparingHandler{f.prepare})
	return nil
}

type preparingHandler struct {
	prepare func(context.Context, *feature.Block) (feature.PreparedHandler, error)
}

func (h preparingHandler) PrepareBlock(ctx context.Context, b *feature.Block) (feature.PreparedHandler, error) {
	return h.prepare(ctx, b)
}
func (preparingHandler) HandleBlock(context.Context, *feature.Block) error { return nil }
func setPreparingFeature(t *testing.T, h *chainHarness, prepare func(context.Context, *feature.Block) (feature.PreparedHandler, error)) {
	t.Helper()
	name := fmt.Sprintf("prepare-test-%d", prepareTestID.Add(1))
	feature.Register(preparingFeature{name, prepare})
	p, err := feature.Build([]string{name}, feature.Deps{})
	require.NoError(t, err)
	h.f.SetFeatures(p)
}
func TestPreparedRangeBoundsAndCancellation(t *testing.T) {
	h := newChainHarness(t, &Config{NumWorkers: 2, StatePrefetch: true, StateMaxBytes: 1024}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan uint64, 10)
	setPreparingFeature(t, h, func(ctx context.Context, b *feature.Block) (feature.PreparedHandler, error) {
		started <- b.Model.Number
		if b.Model.Number == 0 {
			<-ctx.Done()
			return feature.PreparedHandler{}, ctx.Err()
		}
		return feature.PreparedHandler{Bytes: 128}, nil
	})
	done := make(chan error, 1)
	go func() { done <- h.f.FetchRange(ctx, 0, 8) }()
	for range 4 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("bounded window did not start")
		}
	}
	// Height 0 still holds the window: no fifth preparation can start.
	select {
	case <-started:
		t.Fatal("preparation exceeded 2*workers")
	default:
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	stats := h.f.GetPipelineMetrics()
	require.Zero(t, stats.InFlight)
	require.Zero(t, stats.StateBytes)
	require.LessOrEqual(t, stats.PeakInFlight, int64(4))
	require.LessOrEqual(t, stats.PeakStateBytes, int64(4*1024))
	_, err := h.db.GetLatestHeight(context.Background())
	require.ErrorIs(t, err, port.ErrNotFound)
}

type changedHashSource struct {
	source.Source
	calls    atomic.Int64
	changeAt int64
}

func (s *changedHashSource) HashAt(ctx context.Context, n uint64) (common.Hash, error) {
	if s.calls.Add(1) >= s.changeAt {
		return common.HexToHash("0xdead"), nil
	}
	return s.Source.HashAt(ctx, n)
}
func TestPreparedStateRejectsForkOnWriter(t *testing.T) {
	for _, changeAt := range []int64{1} {
		t.Run(fmt.Sprint(changeAt), func(t *testing.T) {
			h := newChainHarness(t, &Config{NumWorkers: 2, StatePrefetch: true}, nil)
			setPreparingFeature(t, h, func(context.Context, *feature.Block) (feature.PreparedHandler, error) {
				return feature.PreparedHandler{ReadState: true, Bytes: 128}, nil
			})
			h.f.SetStateSource(&changedHashSource{Source: h.src, changeAt: changeAt})
			require.ErrorIs(t, h.f.FetchBlock(context.Background(), 0), ErrReorg)
			_, err := h.db.GetBlock(context.Background(), 0)
			require.ErrorIs(t, err, port.ErrNotFound)
			require.Zero(t, h.f.GetPipelineMetrics().StateBytes)
			h.f.SetStateSource(h.src)
			require.NoError(t, h.f.FetchBlock(context.Background(), 0), "retry prepares the current fork again")
		})
	}
}
func TestPreparedPayloadLimitAbortsWithoutCommit(t *testing.T) {
	h := newChainHarness(t, &Config{NumWorkers: 1, StatePrefetch: true, StateMaxBytes: 127}, nil)
	setPreparingFeature(t, h, func(context.Context, *feature.Block) (feature.PreparedHandler, error) {
		return feature.PreparedHandler{Bytes: 128}, nil
	})
	require.ErrorContains(t, h.f.FetchBlock(context.Background(), 0), "exceeds state_max_bytes")
	_, err := h.db.GetBlock(context.Background(), 0)
	require.ErrorIs(t, err, port.ErrNotFound)
	require.Zero(t, h.f.GetPipelineMetrics().StateBytes)
}

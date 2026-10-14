package feature

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type prepareProbe struct{ prepared, written *atomic.Int64 }

func (p prepareProbe) HandleBlock(context.Context, *Block) error { p.written.Add(100); return nil }
func (p prepareProbe) PrepareBlock(context.Context, *Block) (PreparedHandler, error) {
	p.prepared.Add(1)
	return PreparedHandler{Bytes: 128, ReadState: true, Handler: BlockHandlerFunc(func(context.Context, *Block) error { p.written.Add(1); return nil })}, nil
}
func TestPreparedBlockIdentityAndWriterOwnership(t *testing.T) {
	var prepared, written atomic.Int64
	pipeline := &Pipeline{handlers: []namedHandler{{feature: "probe", h: prepareProbe{&prepared, &written}}}}
	block := &Block{Model: &model.Block{Number: 42, Hash: common.HexToHash("0x42")}}
	snapshot, err := pipeline.PrepareBlock(context.Background(), block)
	require.NoError(t, err)
	require.Equal(t, int64(1), prepared.Load())
	require.Zero(t, written.Load(), "preparation must not run the writer")
	fork := &Block{Model: &model.Block{Number: 42, Hash: common.HexToHash("0x43")}}
	require.ErrorContains(t, snapshot.HandleBlock(context.Background(), fork), "do not belong")
	require.Zero(t, written.Load())
	require.NoError(t, snapshot.HandleBlock(context.Background(), block))
	require.Equal(t, int64(1), written.Load(), "only the prepared handler applies")
}

func TestPreparationReadLimitAndCancellation(t *testing.T) {
	deps := Deps{PrepareConcurrency: 2, prepareSlots: make(chan struct{}, 2)}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	started := make(chan struct{}, 20)
	var active, peak atomic.Int64
	run := func() error {
		return deps.PrepareReads(ctx, 10, func(ctx context.Context, _ int) error {
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
	}
	done := make(chan error, 2)
	go func() { done <- run() }()
	go func() { done <- run() }()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("read workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("exceeded the shared pipeline limit")
	default:
	}
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, int64(2), peak.Load())
	require.Zero(t, active.Load(), "no read job outlives preparation")
}

func TestPreparationFailureKeepsWriterUnused(t *testing.T) {
	var written atomic.Int64
	failure := errors.New("state reader down")
	pipeline := &Pipeline{handlers: []namedHandler{{feature: "failing", h: badPreparer{failure, &written}}}}
	_, err := pipeline.PrepareBlock(context.Background(), &Block{Model: &model.Block{}})
	require.ErrorIs(t, err, failure)
	require.Zero(t, written.Load())
}

type badPreparer struct {
	err     error
	written *atomic.Int64
}

func (p badPreparer) HandleBlock(context.Context, *Block) error { p.written.Add(1); return nil }
func (p badPreparer) PrepareBlock(context.Context, *Block) (PreparedHandler, error) {
	return PreparedHandler{}, p.err
}

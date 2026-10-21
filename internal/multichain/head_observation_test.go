package multichain

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type observedIndexer struct {
	*fakeIndexer
	err        error
	latency    time.Duration
	age        time.Duration
	reads      atomic.Int64
	legacyRead atomic.Int64
}

func (f *observedIndexer) NodeHeight(context.Context) (uint64, error) {
	f.legacyRead.Add(1)
	return f.node, f.err
}
func (f *observedIndexer) ObserveNodeHeight(_ context.Context, age time.Duration) (uint64, time.Duration, error) {
	f.age = age
	f.reads.Add(1)
	return f.node, f.latency, f.err
}

func TestHeadObservationHealthProbeIsFresh(t *testing.T) {
	idx := &observedIndexer{fakeIndexer: &fakeIndexer{node: 120, indexed: 118}, latency: 12 * time.Second}
	ci := NewChainInstance(testChainConfig("a"), nil, zap.NewNop())
	ci.Indexer = idx
	health := ci.HealthCheck(t.Context())
	require.Zero(t, idx.age, "explicit health does not reuse a recent success")
	require.Equal(t, idx.latency, health.RPCLatency)
	require.False(t, health.IsHealthy, "original slow RPC latency is preserved")
	idx.err = errors.New("node offline")
	health = ci.HealthCheck(t.Context())
	require.False(t, health.IsHealthy)
	require.Equal(t, idx.err.Error(), health.LastError)
	require.Zero(t, idx.legacyRead.Load())
}

func TestHeadObservationSyncMetricsDoNotPromoteOnFailure(t *testing.T) {
	idx := &observedIndexer{fakeIndexer: &fakeIndexer{node: 120, indexed: 118}, err: errors.New("node offline")}
	ci := NewChainInstance(testChainConfig("a"), nil, zap.NewNop())
	ci.Indexer = idx
	ci.setStatus(StatusSyncing)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := make(chan events.Event, 2)
	ch <- &events.BlockEvent{}
	ch <- &events.TransactionEvent{}
	close(ch)
	ci.trackMetrics(ctx, &events.Subscription{Channel: ch})
	require.Equal(t, StatusSyncing, ci.Status())
	require.Equal(t, time.Second, idx.age)
	require.Equal(t, uint64(1), ci.transactionsIndexed.Load())
	idx.err = nil
	ch = make(chan events.Event, 1)
	ch <- &events.BlockEvent{}
	close(ch)
	ci.trackMetrics(ctx, &events.Subscription{Channel: ch})
	require.Equal(t, StatusActive, ci.Status())
}

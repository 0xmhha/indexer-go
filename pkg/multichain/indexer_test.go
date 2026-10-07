package multichain

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/events"
)

// fakeIndexer runs until its context ends and reports fixed heights.
type fakeIndexer struct {
	cfg     *ChainConfig
	running atomic.Bool
	closed  atomic.Bool
	bus     *events.EventBus
	indexed uint64
	node    uint64
}

func (f *fakeIndexer) Run(ctx context.Context) error {
	f.running.Store(true)
	defer f.running.Store(false)
	<-ctx.Done()
	return ctx.Err()
}
func (f *fakeIndexer) Close() { f.closed.Store(true); f.bus.Stop() }
func (f *fakeIndexer) IndexedHeight(context.Context) (uint64, error) {
	return f.indexed, nil
}
func (f *fakeIndexer) NodeHeight(context.Context) (uint64, error) { return f.node, nil }
func (f *fakeIndexer) Store() port.QueryStore                     { return nil }
func (f *fakeIndexer) EventBus() *events.EventBus                 { return f.bus }

// fakeFactory records the indexers it builds.
type fakeFactory struct {
	built []*fakeIndexer
	err   error
}

func (ff *fakeFactory) build(_ context.Context, cfg *ChainConfig) (Indexer, error) {
	if ff.err != nil {
		return nil, ff.err
	}
	bus := events.NewEventBus(16, 16)
	go bus.Run()
	f := &fakeIndexer{cfg: cfg, bus: bus, indexed: 7, node: 9}
	ff.built = append(ff.built, f)
	return f, nil
}

func testChainConfig(id string) *ChainConfig {
	return &ChainConfig{ID: id, Name: id, RPCEndpoint: "http://127.0.0.1:1", ChainID: 1, Enabled: true}
}

// TestChainInstanceRunsFactoryIndexer: Start builds the chain's indexer
// with the factory and runs it; Stop ends the run and closes it.
func TestChainInstanceRunsFactoryIndexer(t *testing.T) {
	ff := &fakeFactory{}
	ci := NewChainInstance(testChainConfig("a"), ff.build, zap.NewNop())
	require.NoError(t, ci.Start(context.Background()))
	require.Len(t, ff.built, 1)
	idx := ff.built[0]
	require.Equal(t, "a", idx.cfg.ID)
	require.Eventually(t, idx.running.Load, time.Second, 5*time.Millisecond)

	h, ok := ci.IndexedHeight(context.Background())
	require.True(t, ok)
	require.Equal(t, uint64(7), h)
	_, bus, ok := ci.Store()
	require.True(t, ok)
	require.Same(t, idx.bus, bus)
	health := ci.HealthCheck(context.Background())
	require.Equal(t, uint64(9), health.LatestHeight)
	require.Equal(t, uint64(7), health.IndexedHeight)
	require.Equal(t, uint64(2), health.SyncLag)

	require.NoError(t, ci.Stop(context.Background()))
	require.False(t, idx.running.Load())
	require.True(t, idx.closed.Load())
	require.Equal(t, StatusStopped, ci.Status())
	_, _, ok = ci.Store()
	require.False(t, ok, "a stopped chain has no store")
	_, ok = ci.IndexedHeight(context.Background())
	require.False(t, ok)

	// A restart builds a new indexer.
	require.NoError(t, ci.Start(context.Background()))
	require.Len(t, ff.built, 2)
	require.NoError(t, ci.Stop(context.Background()))
}

func TestChainInstanceFactoryError(t *testing.T) {
	boom := errors.New("node unreachable")
	ci := NewChainInstance(testChainConfig("a"), (&fakeFactory{err: boom}).build, zap.NewNop())
	err := ci.Start(context.Background())
	require.ErrorIs(t, err, boom)
	require.Equal(t, StatusError, ci.Status())
	_, _, ok := ci.Store()
	require.False(t, ok)
}

// TestManagerChainStore: the manager gives each running chain's store to
// the API, and none for unknown chains.
func TestManagerChainStore(t *testing.T) {
	ff := &fakeFactory{}
	m, err := NewManager(&ManagerConfig{
		Enabled: true,
		Chains:  []ChainConfig{*testChainConfig("a"), *testChainConfig("b")},
	}, ff.build, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, m.Start(context.Background()))
	defer func() { require.NoError(t, m.Stop(context.Background())) }()

	require.Len(t, ff.built, 2)
	_, busA, ok := m.ChainStore("a")
	require.True(t, ok)
	_, busB, ok := m.ChainStore("b")
	require.True(t, ok)
	require.NotSame(t, busA, busB, "every chain has its own pipeline")
	_, _, ok = m.ChainStore("c")
	require.False(t, ok)
}

func TestChainIDValidation(t *testing.T) {
	for _, id := range []string{"stablenet", "eth-mainnet", "chain_1", "a.b", "1"} {
		require.True(t, ValidChainID(id), id)
	}
	for _, id := range []string{"", ".", "..", "../x", "a/b", `a\b`, ".hidden", "a b", "-x"} {
		require.False(t, ValidChainID(id), id)
		cfg := testChainConfig(id)
		require.Error(t, cfg.Validate(), id)
	}
}

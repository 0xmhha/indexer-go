package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/0xmhha/indexer-go/internal/opsguard"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type shutdownStatsStore struct {
	storage.Storage
	started, cancelled, release chan struct{}
	joined, early, closed       atomic.Bool
}

func (s *shutdownStatsStore) DatabaseBytes(ctx context.Context) (int64, error) {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	<-s.release
	s.joined.Store(true)
	return 123, nil
}
func (s *shutdownStatsStore) WALBytes(context.Context) (int64, error) {
	s.early.Store(true)
	return 0, nil
}
func (s *shutdownStatsStore) GetLatestHeight(context.Context) (uint64, error) {
	if !s.joined.Load() {
		s.early.Store(true)
	}
	return 0, nil
}
func (s *shutdownStatsStore) Close() error {
	if !s.joined.Load() {
		s.early.Store(true)
	}
	s.closed.Store(true)
	return nil
}

func TestOperationsShutdownJoinsStatsBeforeStoreClose(t *testing.T) {
	for _, root := range []bool{true, false} {
		cfg := operationsConfig(t.TempDir()).WithDefaults()
		m, e := opsguard.NewMonitor(cfg, nil)
		require.NoError(t, e)
		defer func() { require.NoError(t, m.Close()) }()
		s := &shutdownStatsStore{started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
		var once sync.Once
		release := func() { once.Do(func() { close(s.release) }) }
		defer release()
		a := &App{logger: zap.NewNop(), storage: s, statsDetach: m.AttachPostgresStats(s)}
		if root {
			a.opsMonitor = m
		}
		sampled := make(chan bool, 1)
		go func() { sampled <- m.StatsSampler().Sample(context.Background()) }()
		<-s.started
		closed := make(chan struct{})
		go func() { a.Shutdown(); close(closed) }()
		<-s.cancelled
		require.False(t, s.closed.Load())
		release()
		<-closed
		require.True(t, <-sampled)
		require.True(t, s.closed.Load())
		require.False(t, s.early.Load())
		// Root owns shutdown; a child detached only its own source and leaves
		// the shared sampler available to surviving chain Apps.
		_, e = m.StatsSampler().Attach(simpleStats{}, nil)
		if root {
			require.Error(t, e)
		} else {
			require.NoError(t, e)
			require.True(t, m.StatsSampler().Sample(context.Background()))
		}
	}
}

type simpleStats struct{}

func (simpleStats) DatabaseBytes(context.Context) (int64, error) { return 1, nil }
func (simpleStats) WALBytes(context.Context) (int64, error)      { return 1, nil }

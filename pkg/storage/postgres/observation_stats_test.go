package postgres

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/opsguard"
	"github.com/stretchr/testify/require"
)

type observationStatsProvider struct {
	store   *Store
	started chan struct{}
}

func (p observationStatsProvider) DatabaseBytes(ctx context.Context) (int64, error) {
	close(p.started)
	return p.store.DatabaseBytes(ctx)
}
func (p observationStatsProvider) WALBytes(ctx context.Context) (int64, error) {
	return p.store.WALBytes(ctx)
}

func TestObservationStatsUseGeneralPoolAndCancel(t *testing.T) {
	s := newTestStore(t)
	lease, e := s.ReserveQueryReader(t.Context())
	require.NoError(t, e)
	require.NotNil(t, lease)
	defer lease.Close()
	a, e := s.pool.Acquire(t.Context())
	require.NoError(t, e)
	defer a.Release()
	b, e := s.pool.Acquire(t.Context())
	require.NoError(t, e)
	defer b.Release()
	c, e := s.pool.Acquire(t.Context())
	require.NoError(t, e)
	defer c.Release()
	require.EqualValues(t, 4, s.pool.Config().MaxConns)
	sampler := opsguard.NewObservationStatsSampler()
	defer sampler.Close()
	started := make(chan struct{})
	detach, e := sampler.Attach(observationStatsProvider{s, started}, nil)
	require.NoError(t, e)
	defer detach()
	ctx, cancel := context.WithCancel(lease.Context(t.Context()))
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- sampler.Sample(ctx) }()
	<-started
	api, stop := context.WithTimeout(lease.Context(t.Context()), time.Second)
	defer stop()
	_, e = s.GetLatestHeight(api)
	require.True(t, e == nil || errors.Is(e, port.ErrNotFound), "reserved API slot must remain usable while stats waits for a general connection: %v", e)
	cancel()
	require.True(t, <-done)
	require.EqualValues(t, 4, s.pool.Stat().AcquiredConns(), "cancelled acquisition did not steal/return another owner's connection")
	c.Release()
	// Reattach a fresh wrapper: its one-use started channel identifies this read.
	detach()
	detach, e = sampler.Attach(observationStatsProvider{s, make(chan struct{})}, nil)
	require.NoError(t, e)
	require.True(t, sampler.Sample(t.Context()))
	detach()
}

package opsguard

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"github.com/stretchr/testify/require"
)

type statsFuncs struct {
	db, wal func(context.Context) (int64, error)
}

func (s statsFuncs) DatabaseBytes(c context.Context) (int64, error) { return s.db(c) }
func (s statsFuncs) WALBytes(c context.Context) (int64, error)      { return s.wal(c) }
func statsTap(t *testing.T) (*nativeobserve.Inbox, *nativeobserve.Tap) {
	t.Helper()
	i, e := nativeobserve.NewInbox(4)
	require.NoError(t, e)
	var bindings []nativeobserve.Binding
	for n, m := range []nativeobserve.Metric{nativeobserve.DatabaseBytes, nativeobserve.WALBytes} {
		k, d := nativeobserve.Database, nativeobserve.DatabaseLogical
		if n == 1 {
			k, d = nativeobserve.Cluster, nativeobserve.ClusterWALLogical
		}
		scope := nativeobserve.Scope{Kind: k, ID: nativeobserve.ResourceID{byte(n + 1)}}
		bindings = append(bindings, nativeobserve.Binding{Scope: scope, Metric: m, Unit: nativeobserve.Bytes, Definition: d, Source: nativeobserve.Source{Token: nativeobserve.SourceToken{1}, Incarnation: 1, Run: nativeobserve.RunID{1}}, Series: nativeobserve.Digest{1}, Policy: nativeobserve.Digest{1}, Budget: nativeobserve.Budget{Scope: scope, Definition: d, ID: nativeobserve.Digest{1}, Limit: nativeobserve.Number{Present: true, Value: 1000}}})
	}
	tap, e := i.Bind(bindings)
	require.NoError(t, e)
	return i, tap
}

func TestStatsSharedOwnerRejectsOverlapAndJoinsClose(t *testing.T) {
	s := NewObservationStatsSampler()
	provider := &joiningStats{started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(provider.release) }) }
	defer release()
	i, tap := statsTap(t)
	detach, e := s.Attach(provider, tap)
	require.NoError(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, time.Hour) }()
	<-provider.started
	require.False(t, s.Sample(ctx), "second consumer added a query")
	require.ErrorIs(t, s.Run(ctx, time.Hour), ErrStatsRunning)
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	<-provider.cancelled
	select {
	case <-closed:
		t.Fatal("closed before the query joined")
	default:
	}
	release()
	<-closed
	require.NoError(t, <-done)
	detach()
	detach()
	_, ok := i.Poll()
	require.False(t, ok)
	require.Zero(t, provider.wal.Load())
	require.False(t, s.Sample(ctx))
	require.NoError(t, s.Run(ctx, time.Hour))
}

func TestStatsTimesBoundsContextAndUnknownErrors(t *testing.T) {
	type apiKey struct{}
	for _, observed := range []bool{false, true} {
		m, _, _, _ := newTestMonitor(t)
		s := m.StatsSampler()
		if observed {
			s = NewObservationStatsSampler()
			defer s.Close()
		}
		expected := statsTimeout
		if observed {
			expected = ObservationQueryLimit
			require.Equal(t, ObservationRoundLimit, s.roundLimit)
		}
		i, tap := statsTap(t)
		count := 0
		s.now = func() time.Time { count++; return time.Date(2026, 11, 2, 0, 0, count, 0, time.UTC) }
		provider := statsFuncs{db: func(c context.Context) (int64, error) {
			require.Nil(t, c.Value(apiKey{}))
			d, ok := c.Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(d), expected)
			return 0, nil
		}, wal: func(context.Context) (int64, error) { return 99, errors.New("private provider error not emitted") }}
		detach, e := s.Attach(provider, tap)
		require.NoError(t, e)
		require.True(t, s.Sample(context.WithValue(context.Background(), apiKey{}, "reserved API reader")))
		db, ok := i.Poll()
		require.True(t, ok)
		wal, ok := i.Poll()
		require.True(t, ok)
		require.True(t, db.Number.Present)
		require.Zero(t, db.Number.Value)
		require.True(t, db.AcquiredAt.Before(wal.StartedAt))
		require.False(t, wal.Number.Present)
		require.Equal(t, nativeobserve.Failed, wal.Quality)
		require.Zero(t, wal.EventAt)
		require.Zero(t, db.EventAt)
		detach()
	}
}

func TestStatsRebindCancelsOldSourceBeforeNewRead(t *testing.T) {
	s := NewObservationStatsSampler()
	defer s.Close()
	old := &joiningStats{started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	detach, e := s.Attach(old, nil)
	require.NoError(t, e)
	done := make(chan bool, 1)
	go func() { done <- s.Sample(context.Background()) }()
	<-old.started
	var reads atomic.Int32
	read := func(context.Context) (int64, error) { reads.Add(1); return 1, nil }
	detachNew, e := s.Attach(statsFuncs{read, read}, nil)
	require.NoError(t, e)
	defer detachNew()
	<-old.cancelled
	require.False(t, s.Sample(context.Background()))
	require.Zero(t, reads.Load())
	close(old.release)
	require.True(t, <-done)
	detach()
	require.True(t, s.Sample(context.Background()))
	require.EqualValues(t, 2, reads.Load())
}

func TestStatsCloseWakesAndJoinsIdleRunner(t *testing.T) {
	s := NewObservationStatsSampler()
	entered, release := make(chan struct{}), make(chan struct{})
	provider := statsFuncs{db: func(context.Context) (int64, error) { close(entered); <-release; return 1, nil }, wal: func(context.Context) (int64, error) { return 1, nil }}
	detach, e := s.Attach(provider, nil)
	require.NoError(t, e)
	defer detach()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- s.Run(ctx, time.Hour) }()
	<-entered
	s.mu.Lock()
	roundDone := s.active.done
	s.mu.Unlock()
	close(release)
	<-roundDone
	s.Close()
	require.NoError(t, <-finished)
}

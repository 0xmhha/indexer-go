package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/nativealerts"
	"github.com/0xmhha/indexer-go/internal/observation"
	"github.com/0xmhha/indexer-go/internal/observeconfig"
	"github.com/0xmhha/indexer-go/internal/opsguard"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
)

func TestObservationRuntimeUsesPoolFourAndAPIReservation(t *testing.T) {
	s := newTestStore(t)
	lease, e := s.ReserveQueryReader(t.Context())
	require.NoError(t, e)
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
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	cfg := observeconfig.Config{Enabled: true, Initialize: true, CheckpointDir: dir, Database: observeconfig.Resource{ID: strings.Repeat("11", 32), Series: strings.Repeat("22", 32), BudgetBytes: 1}, WAL: observeconfig.Resource{ID: strings.Repeat("33", 32), Series: strings.Repeat("44", 32), BudgetBytes: 1}, Warn: 8000, Stop: 9000, Clear: 7000}
	changed := make(chan struct{}, 1)
	sampler := opsguard.NewObservationStatsSampler()
	defer sampler.Close()
	r, e := observation.New(cfg, sampler, false, observation.Options{Changed: changed})
	require.NoError(t, e)
	defer r.Stop()
	started := make(chan struct{})
	detach, e := r.AttachStats(observationStatsProvider{s, started})
	require.NoError(t, e)
	defer detach()
	done := make(chan error, 1)
	go func() { done <- r.Run(t.Context()) }()
	wait := func(predicate func(observation.Status) bool) {
		t.Helper()
		timeout := time.NewTimer(3 * time.Second)
		defer timeout.Stop()
		for {
			if predicate(r.Status()) {
				return
			}
			select {
			case <-changed:
			case <-timeout.C:
				t.Fatalf("runtime status not reached: %+v", r.Status())
			}
		}
	}
	wait(func(s observation.Status) bool { return s.Running })
	ctx, cancel := context.WithCancel(lease.Context(t.Context()))
	sampled := make(chan bool, 1)
	go func() { sampled <- sampler.Sample(ctx) }()
	<-started
	api, stop := context.WithTimeout(lease.Context(t.Context()), time.Second)
	_, e = s.GetLatestHeight(api)
	stop()
	require.True(t, e == nil || errors.Is(e, port.ErrNotFound))
	cancel()
	require.True(t, <-sampled)
	require.EqualValues(t, 4, s.pool.Stat().AcquiredConns())
	require.EqualValues(t, 4, s.pool.Config().MaxConns)
	c.Release()
	detach()
	detach, e = r.AttachStats(s)
	require.NoError(t, e)
	defer detach()
	require.True(t, sampler.Sample(t.Context()))
	wait(func(s observation.Status) bool {
		return len(s.Alerts) == 2 && s.Alerts[0].Active == nativealerts.Critical && s.Alerts[1].Active == nativealerts.Critical && !s.CheckpointUnknown
	})
	r.Stop()
	require.NoError(t, <-done)
	require.True(t, r.Status().Stopped)
	// Stop detached/joined observation, not the Store or API lease.
	_, e = s.GetLatestHeight(lease.Context(t.Context()))
	require.True(t, e == nil || errors.Is(e, port.ErrNotFound))
}

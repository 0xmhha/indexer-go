package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/multichain"
	"github.com/0xmhha/indexer-go/internal/observeconfig"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func appObservationConfig() *config.Config {
	c := config.NewConfig()
	c.Database.Driver = config.DriverPostgres
	c.Database.Postgres.MaxConns = 4
	c.Operations.Observation = &observeconfig.Config{Enabled: true, Initialize: true, CheckpointDir: "/private/unopened-observer", Sink: "disabled", Database: observeconfig.Resource{ID: strings.Repeat("11", 32), Series: strings.Repeat("22", 32), BudgetBytes: 1000}, WAL: observeconfig.Resource{ID: strings.Repeat("33", 32), Series: strings.Repeat("44", 32), BudgetBytes: 1000}, Warn: 8000, Stop: 9000, Clear: 7000}
	return c
}

type appStatsProvider struct{ calls atomic.Int32 }

func (p *appStatsProvider) DatabaseBytes(context.Context) (int64, error) {
	p.calls.Add(1)
	return 950, nil
}
func (p *appStatsProvider) WALBytes(context.Context) (int64, error) { return 950, nil }
func TestObservationAppSharesCapacitySamplerAndChildClose(t *testing.T) {
	c := appObservationConfig()
	observer := c.Operations.Observation
	c.Operations = operationsConfig(t.TempDir())
	c.Operations.Observation = observer
	a := &App{config: c, logger: zap.NewNop()}
	require.NoError(t, a.initOperations())
	t.Cleanup(a.closeOperations)
	// Exactly this monitor sampler supplies both capacity journal and alert inbox.
	provider := &appStatsProvider{}
	detach, e := a.observation.AttachStats(provider)
	require.NoError(t, e)
	child := &App{logger: zap.NewNop(), observation: a.observation, statsDetach: detach}
	require.True(t, a.opsMonitor.StatsSampler().Sample(t.Context()))
	require.EqualValues(t, 1, provider.calls.Load())
	child.closeOperations()
	require.False(t, a.observation.Status().Stopped)
	require.False(t, a.opsMonitor.StatsSampler().Sample(t.Context()))
	_, e = a.observation.AttachStats(provider)
	require.NoError(t, e)
	require.True(t, a.opsMonitor.StatsSampler().Sample(t.Context()))
	a.closeOperations()
	require.True(t, a.observation.Status().Stopped)
}
func TestObservationAppMultiChainOwnedPostgresShutdown(t *testing.T) {
	if !testOnPostgres() {
		t.Skip("owned PostgreSQL functional fixture only")
	}
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	c := appObservationConfig()
	setTestDatabase(t, c, filepath.Join(t.TempDir(), "db"))
	c.API.Enabled = false
	enableTestChainFeatures(c)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	c.Operations.Observation.CheckpointDir = dir
	a := &App{config: c, logger: zap.NewNop()}
	require.NoError(t, a.initOperations())
	t.Cleanup(a.closeOperations)
	first, e := a.newChainIndexer(t.Context(), &multichain.ChainConfig{ID: "one", Name: "one", RPCEndpoint: srv.URL()})
	require.NoError(t, e)
	one := first.(*chainIndexer).app
	t.Cleanup(one.Shutdown)
	second, e := a.newChainIndexer(t.Context(), &multichain.ChainConfig{ID: "two", Name: "two", RPCEndpoint: srv.URL()})
	require.NoError(t, e)
	two := second.(*chainIndexer).app
	t.Cleanup(two.Shutdown)
	require.Same(t, a.observation, one.observation)
	require.Same(t, a.observation, two.observation)
	require.False(t, one.ownObservation)
	require.False(t, two.ownObservation)
	require.NotNil(t, one.statsDetach)
	require.NotNil(t, two.statsDetach)
	done := make(chan error, 1)
	go func() { done <- a.observation.Run(t.Context()) }()
	require.Eventually(t, func() bool { return a.observation.Status().Running }, 3*time.Second, time.Millisecond)
	// Latest child detaches/joins its Store reads before Store.Close, retaining
	// the earlier child as the fallback provider of the same declared database.
	two.Shutdown()
	require.False(t, a.observation.Status().Stopped)
	require.False(t, a.observation.Status().SourceMissing)
	one.Shutdown()
	a.closeOperations()
	require.NoError(t, <-done)
	require.True(t, a.observation.Status().Stopped)
}
func TestObservationAppAssemblyAndNeverRunClose(t *testing.T) {
	a := &App{config: appObservationConfig(), logger: zap.NewNop()}
	require.NoError(t, a.initOperations())
	t.Cleanup(a.closeOperations)
	require.NotNil(t, a.observation)
	require.True(t, a.ownObservation)
	require.Nil(t, a.opsMonitor)
	a.closeOperations()
	require.True(t, a.observation.Status().Stopped)
}
func TestObservationAppRejectsUnsupportedStorageBeforeStartup(t *testing.T) {
	c := appObservationConfig()
	c.Database.Driver = config.DriverPebble
	a := &App{config: c, logger: zap.NewNop()}
	require.Error(t, a.initOperations())
}

func TestObservationAppOwnedPostgresRunCancellation(t *testing.T) {
	if !testOnPostgres() {
		t.Skip("owned PostgreSQL functional fixture only")
	}
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	c := appObservationConfig()
	setTestDatabase(t, c, filepath.Join(t.TempDir(), "db"))
	c.RPC.Endpoint = srv.URL()
	c.RPC.RateLimit = 5
	c.API.Enabled = false
	enableTestChainFeatures(c)
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0700))
	c.Operations.Observation.CheckpointDir = dir
	a, e := NewApp(c, zap.NewNop(), false, "")
	require.NoError(t, e)
	t.Cleanup(a.Shutdown)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	require.Eventually(t, func() bool { st := a.observation.Status(); return st.Running && !st.CheckpointUnknown }, 3*time.Second, time.Millisecond)
	cancel()
	select {
	case e = <-done:
		require.True(t, e == nil || errors.Is(e, context.Canceled), "unexpected mock App result: %v", e)
	case <-time.After(5 * time.Second):
		t.Fatal("App.Run failed to join observation")
	}
	require.True(t, a.observation.Status().Stopped)
}

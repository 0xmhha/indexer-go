package multichain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// A successful automatic restart must retain the manager's lifetime,
// rather than a startup timeout canceled as soon as Start returns.
func TestAutoRestartRetainsManagerLifetime(t *testing.T) {
	ff := &fakeFactory{}
	var lifetime context.Context
	m, err := NewManager(&ManagerConfig{Enabled: true, AutoRestartDelay: time.Nanosecond, Chains: []ChainConfig{*testChainConfig("a")}},
		func(ctx context.Context, cfg *ChainConfig) (Indexer, error) {
			lifetime = ctx
			return ff.build(ctx, cfg)
		}, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, m.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, m.Stop(context.Background())) })
	ci, err := m.GetChain("a")
	require.NoError(t, err)
	ci.setError(errors.New("pipeline failed"))
	m.checkAndRestartFailedChains()
	require.Len(t, ff.built, 2)
	require.True(t, ff.built[0].closed.Load())
	require.NoError(t, lifetime.Err(), "restarted pipeline still has a live context")
	require.Eventually(t, ff.built[1].running.Load, time.Second, time.Millisecond)
	require.NoError(t, m.Stop(context.Background()))
	require.ErrorIs(t, lifetime.Err(), context.Canceled)
	require.True(t, ff.built[1].closed.Load())
}

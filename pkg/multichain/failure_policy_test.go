package multichain

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var errFatal = errors.New("fatal storage fault")

// TestFailurePolicySuppressesAutomaticRestart: a failure the policy marks is
// not restarted by the monitor, other failures are, and an explicit start
// clears the mark.
func TestFailurePolicySuppressesAutomaticRestart(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		suppress bool
	}{
		{"suppressed", errFatal, true},
		{"retried", errors.New("rpc timeout"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var builds atomic.Int32
			failing := atomic.Bool{}
			failing.Store(true)
			factory := func(ctx context.Context, cfg *ChainConfig) (Indexer, error) {
				builds.Add(1)
				if failing.Load() {
					return nil, tc.err
				}
				return (&fakeFactory{}).build(ctx, cfg)
			}
			m, err := NewManager(&ManagerConfig{AutoRestart: true, AutoRestartDelay: time.Nanosecond, HealthCheckInterval: time.Hour}, factory, zap.NewNop())
			require.NoError(t, err)
			var seen []error
			m.SetFailurePolicy(func(id string, err error) bool {
				require.Equal(t, "a", id)
				seen = append(seen, err)
				return errors.Is(err, errFatal)
			})
			_, err = m.RegisterChain(context.Background(), testChainConfig("a"))
			require.NoError(t, err)
			m.ctx, m.cancelFunc = context.WithCancel(context.Background())
			defer m.cancelFunc()
			instance, err := m.GetChain("a")
			require.NoError(t, err)
			require.Error(t, instance.Start(m.ctx))
			require.Len(t, seen, 1)
			require.ErrorIs(t, seen[0], tc.err)
			require.Equal(t, tc.suppress, instance.RestartSuppressed())
			require.Equal(t, tc.suppress, instance.HealthCheck(context.Background()).RestartSuppressed)

			failing.Store(false)
			m.checkAndRestartFailedChains()
			if tc.suppress {
				require.EqualValues(t, 1, builds.Load(), "no automatic restart")
				require.Equal(t, StatusError, instance.Status())
				require.NoError(t, instance.Start(m.ctx), "an explicit start is allowed")
				require.False(t, instance.RestartSuppressed())
			} else {
				require.EqualValues(t, 2, builds.Load())
			}
			require.NoError(t, instance.Stop(context.Background()))
		})
	}
}

// TestFailurePolicySeesRunFailures: a running indexer's failure reaches the
// policy as soon as it happens, not at the next health check.
func TestFailurePolicySeesRunFailures(t *testing.T) {
	failed := make(chan error, 1)
	ci := NewChainInstance(testChainConfig("a"), func(context.Context, *ChainConfig) (Indexer, error) {
		return &failingIndexer{err: errFatal}, nil
	}, zap.NewNop())
	ci.onFailure = func(_ string, err error) bool { failed <- err; return true }
	require.NoError(t, ci.Start(context.Background()))
	select {
	case err := <-failed:
		require.ErrorIs(t, err, errFatal)
	case <-time.After(5 * time.Second):
		t.Fatal("failure not reported")
	}
	require.Eventually(t, ci.RestartSuppressed, time.Second, time.Millisecond)
	require.NoError(t, ci.Stop(context.Background()))
}

type failingIndexer struct {
	fakeIndexer
	err error
}

func (f *failingIndexer) Run(context.Context) error { return f.err }
func (f *failingIndexer) Close()                    {}

package rpcproxy

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// TestStopReleasesGoroutines (refactoring plan R0-7, C1): a proxy leaves
// no goroutine behind once stopped, whether it was started or not (its
// cache cleans up from NewProxy on).
func TestStopReleasesGoroutines(t *testing.T) {
	baseline := goleak.IgnoreCurrent()
	func() {
		started := newTestProxy(t, &fakeNode{}, nil)
		require.NoError(t, started.Start())
		require.NoError(t, started.Stop())
		require.NoError(t, newTestProxy(t, &fakeNode{}, nil).Stop())
	}()
	// newTestProxy's test server and client close in cleanups; only the
	// proxies' own goroutines are checked here.
	goleak.VerifyNone(t, baseline,
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
		goleak.IgnoreAnyFunction("net/http.(*conn).serve"),
		goleak.IgnoreAnyFunction("net/http/httptest.(*Server).goServe.func1"),
	)
}

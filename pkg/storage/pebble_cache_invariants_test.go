//go:build invariants && !race

package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/require"
)

// Pebble's invariants finalizer exits on a leaked cache reference. Exercise
// success and open failure, then collect the now-unreachable caches. This
// checks ownership through the dependency's checker rather than inspecting
// private reference-count fields or relying on noisy heap-size thresholds.
func TestPebbleCacheLifecycle(t *testing.T) {
	for i := range 3 {
		func() {
			s, err := NewPebbleStorage(DefaultConfig(filepath.Join(t.TempDir(), fmt.Sprint(i))))
			require.NoError(t, err)
			require.NoError(t, s.Close())
		}()
	}
	parent := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(parent, []byte("not a directory"), 0o600))
	s, err := NewPebbleStorage(DefaultConfig(filepath.Join(parent, "db")))
	require.Error(t, err)
	require.Nil(t, s)

	// Initialization can fail after Open acquired its cache reference too.
	func() {
		cfg := DefaultConfig(filepath.Join(t.TempDir(), "bad-counter"))
		s, err := NewPebbleStorage(cfg)
		require.NoError(t, err)
		require.NoError(t, s.db.Set(TransactionCountKey(), []byte{1}, pebble.Sync))
		require.NoError(t, s.Close())
		s, err = NewPebbleStorage(cfg)
		require.ErrorContains(t, err, "load transaction count")
		require.Nil(t, s)
	}()

	// GC queues finalizers asynchronously; two collections and a bounded
	// settling interval give Pebble's fatal leak check time to run.
	runtime.GC()
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
}

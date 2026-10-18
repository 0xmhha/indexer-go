package facts_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/stretchr/testify/require"
)

func TestFactsFreshAfterPreflightIsNeverCached(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := dbFactoryWithConns(t, backend, 4)()
			defer func() { require.NoError(t, db.Close()) }()
			f := build(t, db, false)
			src := f.source(t, "fresh-no-cache")
			b, err := src.Block(ctx, 2)
			require.NoError(t, err)
			f.fresh.mu.Lock()
			before := len(f.fresh.calls)
			f.fresh.mu.Unlock()
			require.NoError(t, src.ValidateCommit(ctx, b))
			f.fresh.setWrong(true)
			require.ErrorIs(t, src.ValidateCommit(ctx, b), capture.ErrCanonical)
			f.fresh.mu.Lock()
			after := len(f.fresh.calls)
			f.fresh.mu.Unlock()
			require.Equal(t, 2, after-before, "one NEW live read for each validation/retry")
		})
	}
}

func TestFactsResetDuringFreshAfterPreflight(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := dbFactoryWithConns(t, backend, 4)()
			defer func() { require.NoError(t, db.Close()) }()
			f := build(t, db, false)
			live := &heldFresh{CanonicalReader: f.fresh, number: 2, entered: make(chan struct{}), release: make(chan struct{})}
			src, err := facts.New(ctx, facts.Config{Store: f.store, Profile: f.profile, Canonical: live, Consumer: "reset-after-preflight"})
			require.NoError(t, err)
			b, err := src.Block(ctx, 2)
			require.NoError(t, err)
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			done := make(chan struct{})
			var validation error
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(live.release) }) }
			go func() { validation = src.ValidateCommit(work, b); close(done) }()
			defer func() { release(); cancel(); <-done }()
			select {
			case <-live.entered:
			case <-work.Done():
				t.Fatal("fresh not reached", work.Err())
			}
			require.NoError(t, f.store.Reset(work, 1, f.inputs[0].Manifest.Identity.Block))
			release()
			<-done
			require.ErrorIs(t, validation, capture.ErrStale, "post-RPC epoch barrier remains independent")
		})
	}
}

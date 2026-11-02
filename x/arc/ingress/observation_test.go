package arcingress

import (
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"github.com/0xmhha/indexer-go/internal/nativeobserve/observetest"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestNativeOwnerObservationCompletionsAndACKSeed(t *testing.T) {
	p := rawProgram(t)
	i, tap := observetest.Bind(t, nativeobserve.RawDurableHeight, nativeobserve.NativeExecutedHeight, nativeobserve.SourceACKHeight)
	p.deps.Observation = tap
	batch := rawFixture(t, p.control.Chain, p.identity.Anchor, 1)
	require.NoError(t, p.persist(t.Context(), batch))
	o, ok := i.Poll()
	require.True(t, ok)
	require.Equal(t, nativeobserve.RawDurableHeight, o.Binding.Metric)
	require.EqualValues(t, 1, o.Position.Raw)
	p.deps.Canonical = fixedCanonical{1: batch[0].Block.Hash}
	release := make(chan struct{})
	close(release)
	w := &blockedWriter{identity: p.identity, started: make(chan struct{}), release: release}
	require.NoError(t, p.importNext(t.Context(), w, p.head, p.control.Chain, func([]window.Reference) error { return nil }))
	o, ok = i.Poll()
	require.True(t, ok)
	require.Equal(t, nativeobserve.NativeExecutedHeight, o.Binding.Metric)
	require.Equal(t, nativeobserve.Hash(batch[0].Block.Hash), o.Position.Hash)
	r := &ownedRound{p: p}
	h := execwire.Header{Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash(batch[0].Block.Hash)}}
	r.observeACK(h)
	o, ok = i.Poll()
	require.True(t, ok)
	require.Zero(t, o.EventAt)
	require.Zero(t, o.Position.Read)
	require.Equal(t, nativeobserve.Unverified, o.Coverage)
	h.Ref.Height++
	h.Ref.Hash = execwire.Hash{9}
	r.observeACK(h)
	o, ok = i.Poll()
	require.True(t, ok)
	require.False(t, o.EventAt.IsZero())
	require.Equal(t, h, *r.ack.Load())
	p.Stop()
	r.observeACK(h)
	_, ok = i.Poll()
	require.False(t, ok)
}

func TestNativeACKTapConcurrent(t *testing.T) {
	i, tap := observetest.Bind(t, nativeobserve.SourceACKHeight)
	p := &program{}
	p.deps.Observation = tap
	r := &ownedRound{p: p}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			for n := range 8 {
				r.observeACK(execwire.Header{Ref: execwire.Ref{Generation: 1, Height: uint64(n + 1), Hash: execwire.Hash{byte(n + 1)}}})
			}
		})
	}
	wg.Wait()
	tap.Retire()
	_, ok := i.Poll()
	require.False(t, ok)
}

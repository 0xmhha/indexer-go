package facts_test

import (
	"context"
	"io"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmblob"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	"github.com/stretchr/testify/require"
)

type forbiddenBlobProvider struct{ t *testing.T }

func (p forbiddenBlobProvider) Open(context.Context, model.FeedBlock, []evmblob.Reference) (io.ReadCloser, error) {
	p.t.Error("blobless captured body must not request sidecars")
	return nil, evmblob.ErrUnavailable
}
func TestFactsBlobsDBOnlyCoverageAndEpoch(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			db := dbFactory(t, backend)()
			defer func() { require.NoError(t, db.Close()) }()
			f := build(t, db, false)
			src := f.source(t, "blob-reader")
			d, e := evmblob.Descriptor("blobs-v1", 1, evmblob.Definition{Provider: "fixture-relay", Profile: f.profile.ID(), Format: evmblob.Format})
			require.NoError(t, e)
			_, e = f.store.InitializeSupplementStream(ctx, d)
			require.NoError(t, e)
			_, e = src.Blobs(ctx, 1, d, 0, 1)
			require.ErrorIs(t, e, facts.ErrCoverage)
			supplier, e := evmblob.New(f.store, f.profile, forbiddenBlobProvider{t: t})
			require.NoError(t, e)
			_, e = supplier.Acquire(ctx, 1, f.inputs[0].Manifest.Identity, d)
			require.NoError(t, e)
			f.fresh.mu.Lock()
			before := len(f.fresh.calls)
			f.fresh.mu.Unlock()
			page, e := src.Blobs(ctx, 1, d, 0, 1)
			require.NoError(t, e)
			require.Zero(t, page.Total)
			require.Empty(t, page.Blobs)
			require.Nil(t, page.Next)
			f.fresh.mu.Lock()
			after := len(f.fresh.calls)
			f.fresh.mu.Unlock()
			require.Equal(t, before, after)
			anchor := model.FeedBlock{Chain: f.feed, Hash: f.feed.GenesisHash}
			require.NoError(t, f.store.Reset(ctx, 1, anchor))
			_, e = src.Blobs(ctx, 1, d, 0, 1)
			require.ErrorIs(t, e, capture.ErrStale)
		})
	}
}

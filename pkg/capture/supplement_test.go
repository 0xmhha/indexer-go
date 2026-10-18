package capture_test

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestSupplementDurabilityAndEpoch(t *testing.T) {
	for _, backend := range []string{"pebble", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			open := openFactory(t, backend)
			db := open()
			o := newOracle()
			s := service(t, db, o)
			initialize(t, s)
			a := input(1, 1, common.Hash{2}, chain.GenesisHash)
			require.NoError(t, s.Commit(ctx, a))
			data := []byte(`{"proof":"codec verifies this, not capture"}`)
			in := capture.Supplement{Identity: a.Manifest.Identity, Codec: "fixture.v1", Selection: common.Hash{1}, Digest: sha256.Sum256(data), Data: data}
			broken := service(t, failDB{CaptureDatabase: db, at: 1}, o)
			require.ErrorIs(t, broken.CommitSupplement(ctx, 1, in), errInjected)
			_, e := s.ReadSupplement(ctx, 1, in.Identity, in.Codec, in.Selection)
			require.ErrorIs(t, e, port.ErrNotFound)
			o.fork(1, common.Hash{55})
			require.ErrorIs(t, s.CommitSupplement(ctx, 1, in), capture.ErrCanonical)
			o.fork(1, common.Hash{2})
			require.ErrorIs(t, service(t, uncertainDB{db}, o).CommitSupplement(ctx, 1, in), errInjected)
			require.NoError(t, s.CommitSupplement(ctx, 1, in))
			st, e := s.State(ctx)
			require.NoError(t, e)
			require.EqualValues(t, 1, st.Sequence)
			changed := in
			changed.Data = []byte("different")
			changed.Digest = sha256.Sum256(changed.Data)
			require.ErrorIs(t, s.CommitSupplement(ctx, 1, changed), capture.ErrConflict)
			o.fork(1, common.Hash{77})
			require.ErrorIs(t, s.CommitSupplement(ctx, 1, in), capture.ErrCanonical)
			o.fork(1, common.Hash{2})
			require.NoError(t, db.Close())
			db = open()
			defer func() { require.NoError(t, db.Close()) }()
			s = service(t, db, o)
			got, e := s.ReadSupplement(ctx, 1, in.Identity, in.Codec, in.Selection)
			require.NoError(t, e)
			require.Equal(t, in, got)
			// Retained ancestors can be used under a new epoch, but old handles fail.
			require.NoError(t, s.Reset(ctx, 1, in.Identity.Block))
			_, e = s.ReadSupplement(ctx, 1, in.Identity, in.Codec, in.Selection)
			require.ErrorIs(t, e, capture.ErrStale)
			_, e = s.ReadSupplement(ctx, 2, in.Identity, in.Codec, in.Selection)
			require.NoError(t, e)
			require.NoError(t, s.Reset(ctx, 2, st.Anchor))
			_, e = s.ReadSupplement(ctx, 3, in.Identity, in.Codec, in.Selection)
			require.Error(t, e)
		})
	}
}

func TestSupplementEpochRace(t *testing.T) {
	db := openFactory(t, "pebble")()
	defer func() { require.NoError(t, db.Close()) }()
	s := service(t, db, newOracle())
	initialize(t, s)
	a := input(1, 1, common.Hash{2}, chain.GenesisHash)
	require.NoError(t, s.Commit(ctx, a))
	data := []byte("optional-state")
	in := capture.Supplement{Identity: a.Manifest.Identity, Codec: "fixture.v1", Selection: common.Hash{1}, Digest: sha256.Sum256(data), Data: data}
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() { <-start; done <- s.CommitSupplement(context.Background(), 1, in) }()
	close(start)
	require.NoError(t, s.Reset(ctx, 1, model.FeedBlock{Chain: chain, Hash: chain.GenesisHash}))
	if e := <-done; e != nil {
		require.ErrorIs(t, e, capture.ErrStale)
	}
	_, e := s.ReadSupplement(ctx, 1, in.Identity, in.Codec, in.Selection)
	require.ErrorIs(t, e, capture.ErrStale)
	_, e = s.ReadSupplement(ctx, 2, in.Identity, in.Codec, in.Selection)
	require.Error(t, e)
}

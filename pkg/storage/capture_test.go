package storage

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func TestCaptureIsolationAndDurability(t *testing.T) {
	ctx := context.Background()
	chain := model.FeedChain{ID: "1", GenesisHash: common.Hash{1}}
	for _, mode := range []string{"normal", "read-only", "no-wal"} {
		t.Run(mode, func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir())
			s, err := NewPebbleStorage(cfg)
			require.NoError(t, err)
			if mode == "read-only" || mode == "no-wal" {
				require.NoError(t, s.Close())
				cfg.ReadOnly = mode == "read-only"
				cfg.DisableWAL = mode == "no-wal"
				s, err = NewPebbleStorage(cfg)
				require.NoError(t, err)
			}
			defer func() { require.NoError(t, s.Close()) }()
			key := []byte("facts")
			value := []byte("immutable")
			write := func(w port.CaptureWriter) error { return w.Put(key, value) }
			err = s.CaptureUpdate(ctx, chain, write)
			if mode == "no-wal" {
				require.ErrorContains(t, err, "WAL must be enabled")
				return
			}
			if mode == "read-only" {
				require.ErrorIs(t, err, port.ErrReadOnly)
				return
			}
			require.NoError(t, err)
			txCtx, tx, err := s.BeginBlock(ctx)
			require.NoError(t, err)
			tx.SetHeight(1)
			require.ErrorIs(t, s.CaptureUpdate(txCtx, chain, write), port.ErrInvalidData)
			require.NoError(t, s.SetBlock(txCtx, &model.Block{Number: 1, Hash: common.Hash{2}, Time: 1}))
			require.NoError(t, s.SetLatestHeight(txCtx, 1))
			require.NoError(t, tx.Commit())
			// Projection undo must not touch source facts.
			_, err = s.RollbackTo(ctx, 0, nil)
			require.NoError(t, err)
			require.NoError(t, s.CaptureView(ctx, chain, func(r port.CaptureReader) error {
				got, e := r.Get(key)
				require.NoError(t, e)
				require.Equal(t, value, got)
				return nil
			}))
			require.Equal(t, "capture", KeyOwner(string(append(captureNamespace(chain), key...))))
			for _, p := range PrefixesOf(ChainData) {
				require.NotEqual(t, capturePrefix, p)
			}
		})
	}
}

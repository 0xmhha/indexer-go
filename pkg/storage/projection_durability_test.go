package storage

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProjectionRequiresWAL(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		cfg := DefaultConfig(t.TempDir())
		s, e := NewPebbleStorage(cfg)
		require.NoError(t, e)
		if disabled {
			require.NoError(t, s.Close())
			cfg.DisableWAL = true
			s, e = NewPebbleStorage(cfg)
			require.NoError(t, e)
		}
		require.ErrorIs(t, s.RequireBlockDurability(context.Background()), port.ErrInvalidData)
		c, tx, e := s.BeginBlock(context.Background())
		require.NoError(t, e)
		e = s.RequireBlockDurability(c)
		if disabled {
			require.ErrorContains(t, e, "WAL must be enabled")
		} else {
			require.NoError(t, e)
		}
		tx.Rollback()
		require.NoError(t, s.Close())
	}
}

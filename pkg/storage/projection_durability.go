package storage

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

func (s *PebbleStorage) RequireBlockDurability(ctx context.Context) error {
	if s.boundTx(ctx) == nil {
		return port.ErrInvalidData
	}
	if s.config.DisableWAL {
		return errors.New("projection: Pebble WAL must be enabled")
	}
	return ctx.Err() // BlockTx.Commit already uses pebble.Sync
}

var _ port.BlockDurability = (*PebbleStorage)(nil)

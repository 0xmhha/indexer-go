package storage

import (
	"context"
	"fmt"

	"github.com/cockroachdb/pebble"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// ============================================================================
// Block and Height Methods
// ============================================================================

// GetLatestHeight returns the latest indexed block height
func (s *PebbleStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	value, closer, err := s.kv(ctx).Get(LatestHeightKey())
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, port.ErrNotFound
		}
		return 0, fmt.Errorf("failed to get latest height: %w", err)
	}
	defer func() { _ = closer.Close() }()

	height, err := DecodeUint64(value)
	if err != nil {
		return 0, fmt.Errorf("failed to decode height: %w", err)
	}

	return height, nil
}

// SetLatestHeight updates the latest indexed block height
func (s *PebbleStorage) SetLatestHeight(ctx context.Context, height uint64) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	value := EncodeUint64(height)
	// Use NoSync for performance - caller can use Sync() if needed
	return s.kv(ctx).Set(LatestHeightKey(), value, pebble.NoSync)
}

// Sync forces a sync of all pending writes to disk
func (s *PebbleStorage) Sync() error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	return s.db.Flush()
}

// DeleteBlock removes a block
func (s *PebbleStorage) DeleteBlock(ctx context.Context, height uint64) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get block to find its hash
	block, err := s.GetBlock(ctx, height)
	if err != nil {
		if err == port.ErrNotFound {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to get block for deletion: %w", err)
	}

	// Delete block hash index
	if err := s.kv(ctx).Delete(BlockHashIndexKey(block.Hash), pebble.Sync); err != nil {
		return fmt.Errorf("failed to delete block hash index: %w", err)
	}

	// Delete block timestamp index
	if err := s.kv(ctx).Delete(BlockTimestampKey(block.Time, height), pebble.Sync); err != nil {
		return fmt.Errorf("failed to delete block timestamp index: %w", err)
	}

	// Delete block data
	return s.kv(ctx).Delete(BlockKey(height), pebble.Sync)
}

// HasBlock checks if a block exists at given height
func (s *PebbleStorage) HasBlock(ctx context.Context, height uint64) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	_, closer, err := s.kv(ctx).Get(BlockKey(height))
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	_ = closer.Close()
	return true, nil
}

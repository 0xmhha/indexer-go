package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
)

// Ensure PebbleStorage implements FeeDelegationReader and FeeDelegationWriter
var _ FeeDelegationReader = (*PebbleStorage)(nil)
var _ FeeDelegationWriter = (*PebbleStorage)(nil)

// ============================================================================
// FeeDelegationWriter interface implementation
// ============================================================================

// SetFeeDelegationTxMeta stores fee delegation metadata for a transaction
func (s *PebbleStorage) SetFeeDelegationTxMeta(ctx context.Context, meta *FeeDelegationTxMeta) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if meta == nil {
		return fmt.Errorf("meta cannot be nil")
	}

	// Serialize metadata
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal fee delegation meta: %w", err)
	}

	// Store metadata by tx hash
	key := FeeDelegationMetaKey(meta.TxHash)
	if err := s.kv(ctx).Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store fee delegation meta: %w", err)
	}

	// Create index by fee payer
	indexKey := FeeDelegationPayerIndexKey(meta.FeePayer, meta.BlockNumber, meta.TxHash)
	if err := s.kv(ctx).Set(indexKey, meta.TxHash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("failed to store fee payer index: %w", err)
	}

	return nil
}

// GetFeeDelegationTxMeta returns fee delegation metadata for a transaction
// Returns nil if the transaction is not a fee delegation transaction
func (s *PebbleStorage) GetFeeDelegationTxMeta(ctx context.Context, txHash common.Hash) (*FeeDelegationTxMeta, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := FeeDelegationMetaKey(txHash)
	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil // Not a fee delegation tx
		}
		return nil, fmt.Errorf("failed to get fee delegation meta: %w", err)
	}
	defer closer.Close()

	var meta FeeDelegationTxMeta
	if err := json.Unmarshal(value, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fee delegation meta: %w", err)
	}

	return &meta, nil
}

// GetFeeDelegationTxsByFeePayer returns transaction hashes of fee delegation txs by fee payer
func (s *PebbleStorage) GetFeeDelegationTxsByFeePayer(ctx context.Context, feePayer common.Address, limit, offset int) ([]common.Hash, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 100
	}

	prefix := FeeDelegationPayerPrefix(feePayer)
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var hashes []common.Hash
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Handle offset
		if skipped < offset {
			skipped++
			continue
		}

		// Get tx hash from value
		if len(iter.Value()) == 32 {
			hash := common.BytesToHash(iter.Value())
			hashes = append(hashes, hash)
		}

		// Check limit
		if len(hashes) >= limit {
			break
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return hashes, nil
}

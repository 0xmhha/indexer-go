package storage

import (
	"context"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ============================================================================
// Block and Height Methods
// ============================================================================

// GetLatestHeight returns the latest indexed block height
func (s *PebbleStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	value, closer, err := s.db.Get(LatestHeightKey())
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("failed to get latest height: %w", err)
	}
	defer closer.Close()

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
	return s.db.Set(LatestHeightKey(), value, pebble.NoSync)
}

// Sync forces a sync of all pending writes to disk
func (s *PebbleStorage) Sync() error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	return s.db.Flush()
}

// GetBlock returns a block by height
func (s *PebbleStorage) GetBlock(ctx context.Context, height uint64) (*types.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	value, closer, err := s.db.Get(BlockKey(height))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get block: %w", err)
	}
	defer closer.Close()

	block, err := DecodeBlock(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block: %w", err)
	}

	return block, nil
}

// GetBlockByHash returns a block by hash
func (s *PebbleStorage) GetBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get block height from hash index
	value, closer, err := s.db.Get(BlockHashIndexKey(hash))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get block hash index: %w", err)
	}
	defer closer.Close()

	height, err := DecodeUint64(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block height: %w", err)
	}

	// Get block by height
	return s.GetBlock(ctx, height)
}

// SetBlock stores a block
func (s *PebbleStorage) SetBlock(ctx context.Context, block *types.Block) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if block == nil {
		return fmt.Errorf("block cannot be nil")
	}

	encoded, err := EncodeBlock(block)
	if err != nil {
		return fmt.Errorf("failed to encode block: %w", err)
	}

	height := block.Number().Uint64()

	// Store block data - use NoSync for performance
	if err := s.db.Set(BlockKey(height), encoded, pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set block: %w", err)
	}

	// Store block hash index
	heightBytes := EncodeUint64(height)
	if err := s.db.Set(BlockHashIndexKey(block.Hash()), heightBytes, pebble.NoSync); err != nil {
		return fmt.Errorf("failed to set block hash index: %w", err)
	}

	// Store all transactions in the block
	transactions := block.Transactions()
	for txIndex, tx := range transactions {
		location := &TxLocation{
			BlockHeight: height,
			TxIndex:     uint64(txIndex),
			BlockHash:   block.Hash(),
		}
		if err := s.SetTransaction(ctx, tx, location); err != nil {
			return fmt.Errorf("failed to store transaction %d in block %d: %w", txIndex, height, err)
		}
	}

	return nil
}

// SetBlockWithReceipts stores a block with all its receipts in a single batch operation
// This is the high-performance method for indexing - uses single sync at end
func (s *PebbleStorage) SetBlockWithReceipts(ctx context.Context, block *types.Block, receipts []*types.Receipt) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if block == nil {
		return fmt.Errorf("block cannot be nil")
	}

	// Build receipt map for O(1) lookup
	receiptMap := make(map[common.Hash]*types.Receipt, len(receipts))
	for _, receipt := range receipts {
		if receipt != nil {
			receiptMap[receipt.TxHash] = receipt
		}
	}

	batch := s.db.NewBatch()
	defer batch.Close()

	// Encode and add block
	encoded, err := EncodeBlock(block)
	if err != nil {
		return fmt.Errorf("failed to encode block: %w", err)
	}

	height := block.Number().Uint64()
	if err := batch.Set(BlockKey(height), encoded, nil); err != nil {
		return fmt.Errorf("failed to set block: %w", err)
	}

	heightBytes := EncodeUint64(height)
	if err := batch.Set(BlockHashIndexKey(block.Hash()), heightBytes, nil); err != nil {
		return fmt.Errorf("failed to set block hash index: %w", err)
	}

	// Add all transactions and their receipts
	transactions := block.Transactions()
	txCountDelta := uint64(len(transactions))

	for txIndex, tx := range transactions {
		// Encode transaction
		txEncoded, err := EncodeTransaction(tx)
		if err != nil {
			return fmt.Errorf("failed to encode transaction: %w", err)
		}

		location := &TxLocation{
			BlockHeight: height,
			TxIndex:     uint64(txIndex),
			BlockHash:   block.Hash(),
		}
		locEncoded, err := EncodeTxLocation(location)
		if err != nil {
			return fmt.Errorf("failed to encode location: %w", err)
		}

		if err := batch.Set(TransactionKey(height, uint64(txIndex)), txEncoded, nil); err != nil {
			return fmt.Errorf("failed to set transaction: %w", err)
		}
		if err := batch.Set(TransactionHashIndexKey(tx.Hash()), locEncoded, nil); err != nil {
			return fmt.Errorf("failed to set transaction index: %w", err)
		}

		// Add receipt if available
		if receipt, ok := receiptMap[tx.Hash()]; ok {
			if err := validateReceipt(receipt); err != nil {
				return fmt.Errorf("invalid receipt for tx %s: %w", tx.Hash().Hex(), err)
			}

			receiptEncoded, err := EncodeReceipt(receipt)
			if err != nil {
				return fmt.Errorf("failed to encode receipt: %w", err)
			}
			if err := batch.Set(ReceiptKey(tx.Hash()), receiptEncoded, nil); err != nil {
				return fmt.Errorf("failed to set receipt: %w", err)
			}

			// Store ContractAddress separately (not included in RLP encoding)
			if receipt.ContractAddress != (common.Address{}) {
				if err := batch.Set(ContractAddressKey(tx.Hash()), receipt.ContractAddress.Bytes(), nil); err != nil {
					return fmt.Errorf("failed to set contract address: %w", err)
				}
			}
		}
	}

	// Update transaction count atomically
	newCount := s.txCount.Add(txCountDelta)
	if err := batch.Set(TransactionCountKey(), EncodeUint64(newCount), nil); err != nil {
		return fmt.Errorf("failed to update transaction count: %w", err)
	}

	// Update latest height
	if err := batch.Set(LatestHeightKey(), heightBytes, nil); err != nil {
		return fmt.Errorf("failed to set latest height: %w", err)
	}

	// Single Sync at the end
	return batch.Commit(pebble.Sync)
}

// GetBlocks returns multiple blocks by height range
func (s *PebbleStorage) GetBlocks(ctx context.Context, startHeight, endHeight uint64) ([]*types.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	blocks := make([]*types.Block, 0, endHeight-startHeight+1)

	for height := startHeight; height <= endHeight; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			if err == ErrNotFound {
				continue // Skip missing blocks
			}
			return nil, fmt.Errorf("failed to get block %d: %w", height, err)
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

// SetBlocks stores multiple blocks atomically
func (s *PebbleStorage) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	batch := s.NewBatch()
	defer batch.Close()

	for _, block := range blocks {
		if err := batch.SetBlock(ctx, block); err != nil {
			return fmt.Errorf("failed to add block to batch: %w", err)
		}
	}

	return batch.Commit()
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
		if err == ErrNotFound {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to get block for deletion: %w", err)
	}

	// Delete block hash index
	if err := s.db.Delete(BlockHashIndexKey(block.Hash()), pebble.Sync); err != nil {
		return fmt.Errorf("failed to delete block hash index: %w", err)
	}

	// Delete block data
	return s.db.Delete(BlockKey(height), pebble.Sync)
}

// HasBlock checks if a block exists at given height
func (s *PebbleStorage) HasBlock(ctx context.Context, height uint64) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	_, closer, err := s.db.Get(BlockKey(height))
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	closer.Close()
	return true, nil
}

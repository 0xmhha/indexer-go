package storage

import (
	"context"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// ============================================================================
// Receipt Methods
// ============================================================================

// GetReceipts returns multiple receipts by transaction hashes (batch operation)
func (s *PebbleStorage) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*model.Receipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	receipts := make([]*model.Receipt, len(hashes))
	var firstError error

	for i, hash := range hashes {
		receipt, err := s.GetReceipt(ctx, hash)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			receipts[i] = nil
			continue
		}
		receipts[i] = receipt
	}

	if firstError != nil {
		return receipts, firstError
	}

	return receipts, nil
}

// GetReceiptsByBlockHash returns all receipts for a block by block hash
func (s *PebbleStorage) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*model.Receipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get block to find its height
	block, err := s.GetBlockByHash(ctx, blockHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	return s.GetReceiptsByBlockNumber(ctx, block.Number)
}

// GetReceiptsByBlockNumber returns all receipts for a block by block number
func (s *PebbleStorage) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*model.Receipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// The block's transaction hashes as the chain reports them
	hashes, err := s.blockTxHashes(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	receipts := make([]*model.Receipt, 0, len(hashes))

	// Get receipt for each transaction
	for _, hash := range hashes {
		receipt, err := s.GetReceipt(ctx, hash)
		if err != nil {
			if err == port.ErrNotFound {
				// Skip missing receipts
				continue
			}
			return nil, fmt.Errorf("failed to get receipt for tx %s: %w", hash.Hex(), err)
		}
		receipts = append(receipts, receipt)
	}

	return receipts, nil
}

// HasReceipt checks if a receipt exists for a transaction
func (s *PebbleStorage) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	_, closer, err := s.kv(ctx).Get(ReceiptKey(hash))
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, fmt.Errorf("failed to check receipt: %w", err)
	}
	closer.Close()
	return true, nil
}

// GetMissingReceipts returns transaction hashes that have no stored receipts for a block
func (s *PebbleStorage) GetMissingReceipts(ctx context.Context, blockNumber uint64) ([]common.Hash, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// The block's transaction hashes as the chain reports them
	hashes, err := s.blockTxHashes(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	var missing []common.Hash
	for _, hash := range hashes {
		exists, err := s.HasReceipt(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("failed to check receipt for tx %s: %w", hash.Hex(), err)
		}
		if !exists {
			missing = append(missing, hash)
		}
	}

	return missing, nil
}

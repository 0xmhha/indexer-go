package storage

import (
	"context"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ============================================================================
// Receipt Methods
// ============================================================================

// GetReceipt returns a transaction receipt by hash
func (s *PebbleStorage) GetReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	value, closer, err := s.db.Get(ReceiptKey(hash))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get receipt: %w", err)
	}
	defer closer.Close()

	receipt, err := DecodeReceipt(value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode receipt: %w", err)
	}

	// TxHash is not part of RLP encoding, so we need to restore it
	// from the key used to store the receipt
	receipt.TxHash = hash

	// ContractAddress is not part of RLP encoding, retrieve it separately
	contractAddrValue, contractAddrCloser, err := s.db.Get(ContractAddressKey(hash))
	if err == nil {
		defer contractAddrCloser.Close()
		if len(contractAddrValue) == common.AddressLength {
			receipt.ContractAddress = common.BytesToAddress(contractAddrValue)
		}
	}
	// Ignore error - ContractAddress is optional (only for contract creation txs)

	return receipt, nil
}

// validateReceipt validates a receipt before storage
func validateReceipt(receipt *types.Receipt) error {
	if receipt == nil {
		return fmt.Errorf("%w: receipt cannot be nil", ErrInvalidReceipt)
	}

	// Check that TxHash is set (not zero hash)
	var zeroHash common.Hash
	if receipt.TxHash == zeroHash {
		return fmt.Errorf("%w: transaction hash is not set", ErrInvalidReceipt)
	}

	// Check status is valid (0 = failed, 1 = success)
	if receipt.Status > 1 {
		return fmt.Errorf("%w: invalid status %d (expected 0 or 1)", ErrInvalidReceipt, receipt.Status)
	}

	// Check that CumulativeGasUsed is at least GasUsed
	if receipt.CumulativeGasUsed < receipt.GasUsed {
		return fmt.Errorf("%w: cumulative gas used (%d) is less than gas used (%d)",
			ErrInvalidReceipt, receipt.CumulativeGasUsed, receipt.GasUsed)
	}

	return nil
}

// SetReceipt stores a transaction receipt
func (s *PebbleStorage) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Validate receipt before storing
	if err := validateReceipt(receipt); err != nil {
		return err
	}

	encoded, err := EncodeReceipt(receipt)
	if err != nil {
		return fmt.Errorf("failed to encode receipt: %w", err)
	}

	txHash := receipt.TxHash
	// Use NoSync for performance - caller should use Sync() or batch commit for durability
	if err := s.db.Set(ReceiptKey(txHash), encoded, pebble.NoSync); err != nil {
		return err
	}

	// Store ContractAddress separately (not included in RLP encoding)
	if receipt.ContractAddress != (common.Address{}) {
		if err := s.db.Set(ContractAddressKey(txHash), receipt.ContractAddress.Bytes(), pebble.NoSync); err != nil {
			return fmt.Errorf("failed to store contract address: %w", err)
		}
	}

	return nil
}

// GetReceipts returns multiple receipts by transaction hashes (batch operation)
func (s *PebbleStorage) GetReceipts(ctx context.Context, hashes []common.Hash) ([]*types.Receipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	receipts := make([]*types.Receipt, len(hashes))
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
func (s *PebbleStorage) GetReceiptsByBlockHash(ctx context.Context, blockHash common.Hash) ([]*types.Receipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get block to find its height
	block, err := s.GetBlockByHash(ctx, blockHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	return s.GetReceiptsByBlockNumber(ctx, block.Number().Uint64())
}

// GetReceiptsByBlockNumber returns all receipts for a block by block number
func (s *PebbleStorage) GetReceiptsByBlockNumber(ctx context.Context, blockNumber uint64) ([]*types.Receipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get the block to find all transactions
	block, err := s.GetBlock(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	txs := block.Transactions()
	receipts := make([]*types.Receipt, 0, len(txs))

	// Get receipt for each transaction
	for _, tx := range txs {
		receipt, err := s.GetReceipt(ctx, tx.Hash())
		if err != nil {
			if err == ErrNotFound {
				// Skip missing receipts
				continue
			}
			return nil, fmt.Errorf("failed to get receipt for tx %s: %w", tx.Hash().Hex(), err)
		}
		receipts = append(receipts, receipt)
	}

	return receipts, nil
}

// SetReceipts stores multiple receipts atomically (batch operation)
func (s *PebbleStorage) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	batch := s.NewBatch()
	defer batch.Close()

	for _, receipt := range receipts {
		if err := batch.SetReceipt(ctx, receipt); err != nil {
			return fmt.Errorf("failed to add receipt to batch: %w", err)
		}
	}

	return batch.Commit()
}

// HasReceipt checks if a receipt exists for a transaction
func (s *PebbleStorage) HasReceipt(ctx context.Context, hash common.Hash) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	_, closer, err := s.db.Get(ReceiptKey(hash))
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

	// Get the block to find all transactions
	block, err := s.GetBlock(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	var missing []common.Hash
	for _, tx := range block.Transactions() {
		exists, err := s.HasReceipt(ctx, tx.Hash())
		if err != nil {
			return nil, fmt.Errorf("failed to check receipt for tx %s: %w", tx.Hash().Hex(), err)
		}
		if !exists {
			missing = append(missing, tx.Hash())
		}
	}

	return missing, nil
}

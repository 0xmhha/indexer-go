package storage

import (
	"context"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// Ensure pebbleBatch implements Batch interface
var _ Batch = (*pebbleBatch)(nil)

// pebbleBatch implements Batch interface
type pebbleBatch struct {
	storage *PebbleStorage
	batch   *pebble.Batch
	count   int
	txCount uint64 // Number of transactions added in this batch
	closed  bool
	mu      sync.Mutex
}

// SetLatestHeight adds set latest height operation to batch
func (b *pebbleBatch) SetLatestHeight(ctx context.Context, height uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	value := EncodeUint64(height)
	if err := b.batch.Set(LatestHeightKey(), value, nil); err != nil {
		return err
	}
	b.count++
	return nil
}

// SetBlock adds set block operation to batch
func (b *pebbleBatch) SetBlock(ctx context.Context, block *types.Block) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	encoded, err := EncodeBlock(block)
	if err != nil {
		return fmt.Errorf("failed to encode block: %w", err)
	}

	height := block.Number().Uint64()

	// Add block data to batch
	if err := b.batch.Set(BlockKey(height), encoded, nil); err != nil {
		return err
	}

	// Add block hash index to batch
	heightBytes := EncodeUint64(height)
	if err := b.batch.Set(BlockHashIndexKey(block.Hash()), heightBytes, nil); err != nil {
		return err
	}

	b.count += 2

	// Store all transactions in the block
	transactions := block.Transactions()
	for txIndex, tx := range transactions {
		location := &TxLocation{
			BlockHeight: height,
			TxIndex:     uint64(txIndex),
			BlockHash:   block.Hash(),
		}
		// Unlock before calling SetTransaction to avoid deadlock
		b.mu.Unlock()
		err := b.SetTransaction(ctx, tx, location)
		b.mu.Lock()
		if err != nil {
			return fmt.Errorf("failed to store transaction %d in block %d: %w", txIndex, height, err)
		}
	}

	return nil
}

// SetTransaction adds set transaction operation to batch
func (b *pebbleBatch) SetTransaction(ctx context.Context, tx *types.Transaction, location *TxLocation) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	encoded, err := EncodeTransaction(tx)
	if err != nil {
		return fmt.Errorf("failed to encode transaction: %w", err)
	}

	locEncoded, err := EncodeTxLocation(location)
	if err != nil {
		return fmt.Errorf("failed to encode location: %w", err)
	}

	if err := b.batch.Set(TransactionKey(location.BlockHeight, location.TxIndex), encoded, nil); err != nil {
		return err
	}
	if err := b.batch.Set(TransactionHashIndexKey(tx.Hash()), locEncoded, nil); err != nil {
		return err
	}
	b.count += 2
	b.txCount++ // Increment transaction count
	return nil
}

// SetReceipt adds set receipt operation to batch
func (b *pebbleBatch) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	// Validate receipt before adding to batch
	if err := validateReceipt(receipt); err != nil {
		return err
	}

	encoded, err := EncodeReceipt(receipt)
	if err != nil {
		return fmt.Errorf("failed to encode receipt: %w", err)
	}

	if err := b.batch.Set(ReceiptKey(receipt.TxHash), encoded, nil); err != nil {
		return err
	}

	// Store ContractAddress separately (not included in RLP encoding)
	if receipt.ContractAddress != (common.Address{}) {
		if err := b.batch.Set(ContractAddressKey(receipt.TxHash), receipt.ContractAddress.Bytes(), nil); err != nil {
			return err
		}
	}

	b.count++
	return nil
}

// SetReceipts adds multiple set receipt operations to batch
func (b *pebbleBatch) SetReceipts(ctx context.Context, receipts []*types.Receipt) error {
	for _, receipt := range receipts {
		if err := b.SetReceipt(ctx, receipt); err != nil {
			return err
		}
	}
	return nil
}

// AddTransactionToAddressIndex adds transaction to address index in batch
func (b *pebbleBatch) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	b.storage.addrSeqMu.Lock()
	seq := b.storage.addrSeq[addr]
	b.storage.addrSeq[addr]++
	b.storage.addrSeqMu.Unlock()

	key := AddressTransactionKey(addr, seq)
	if err := b.batch.Set(key, txHash[:], nil); err != nil {
		return err
	}
	b.count++
	return nil
}

// SetBlocks adds multiple set block operations to batch
func (b *pebbleBatch) SetBlocks(ctx context.Context, blocks []*types.Block) error {
	for _, block := range blocks {
		if err := b.SetBlock(ctx, block); err != nil {
			return err
		}
	}
	return nil
}

// DeleteBlock adds delete block operation to batch
func (b *pebbleBatch) DeleteBlock(ctx context.Context, height uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	// Get block to find its hash (need to unlock to call storage method)
	b.mu.Unlock()
	block, err := b.storage.GetBlock(context.Background(), height)
	b.mu.Lock()

	if err != nil {
		if err == ErrNotFound {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to get block for deletion: %w", err)
	}

	// Delete block hash index
	if err := b.batch.Delete(BlockHashIndexKey(block.Hash()), nil); err != nil {
		return err
	}

	// Delete block data
	if err := b.batch.Delete(BlockKey(height), nil); err != nil {
		return err
	}

	b.count += 2
	return nil
}

// Commit writes all batched operations atomically
func (b *pebbleBatch) Commit() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	// Update transaction count using atomic counter for performance
	if b.txCount > 0 {
		// Use atomic Add for lock-free counter update
		newCount := b.storage.txCount.Add(b.txCount)
		if err := b.batch.Set(TransactionCountKey(), EncodeUint64(newCount), nil); err != nil {
			// Rollback atomic counter on error
			b.storage.txCount.Add(^(b.txCount - 1)) // Subtract txCount
			return fmt.Errorf("failed to update transaction count: %w", err)
		}
	}

	return b.batch.Commit(pebble.Sync)
}

// Reset clears all operations in the batch
func (b *pebbleBatch) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.batch.Reset()
	b.count = 0
	b.txCount = 0
}

// Count returns the number of operations in the batch
func (b *pebbleBatch) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.count
}

// Close releases batch resources without committing
func (b *pebbleBatch) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true
	return b.batch.Close()
}

package storage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// PebbleStorage implements Storage interface using PebbleDB
type PebbleStorage struct {
	db     *pebble.DB
	config *Config
	logger *zap.Logger
	closed atomic.Bool

	// Address transaction sequence counters
	// Maps address -> next sequence number
	addrSeqMu sync.RWMutex
	addrSeq   map[common.Address]uint64
}

// NewPebbleStorage creates a new PebbleDB storage
func NewPebbleStorage(cfg *Config) (*PebbleStorage, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Configure PebbleDB options
	opts := &pebble.Options{
		Cache:                       pebble.NewCache(int64(cfg.Cache) << 20), // Convert MB to bytes
		MaxOpenFiles:                cfg.MaxOpenFiles,
		MemTableSize:                uint64(cfg.WriteBuffer) << 20,
		DisableWAL:                  cfg.DisableWAL,
		MaxConcurrentCompactions:    func() int { return cfg.CompactionConcurrency },
		ErrorIfExists:               false,
		ErrorIfNotExists:            false,
	}

	if cfg.ReadOnly {
		opts.ReadOnly = true
	}

	// Open database
	db, err := pebble.Open(cfg.Path, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	logger := zap.NewNop() // Use nop logger by default

	storage := &PebbleStorage{
		db:      db,
		config:  cfg,
		logger:  logger,
		addrSeq: make(map[common.Address]uint64),
	}

	// Load address sequences from database
	if err := storage.loadAddressSequences(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to load address sequences: %w", err)
	}

	return storage, nil
}

// SetLogger sets the logger for the storage
func (s *PebbleStorage) SetLogger(logger *zap.Logger) {
	s.logger = logger
}

// ensureNotClosed checks if storage is closed
func (s *PebbleStorage) ensureNotClosed() error {
	if s.closed.Load() {
		return ErrClosed
	}
	return nil
}

// ensureNotReadOnly checks if storage is read-only
func (s *PebbleStorage) ensureNotReadOnly() error {
	if s.config.ReadOnly {
		return ErrReadOnly
	}
	return nil
}

// Close closes the storage and releases resources
func (s *PebbleStorage) Close() error {
	if s.closed.Swap(true) {
		return nil // Already closed
	}

	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// NewBatch creates a new batch for atomic writes
func (s *PebbleStorage) NewBatch() Batch {
	return &pebbleBatch{
		storage: s,
		batch:   s.db.NewBatch(),
		count:   0,
	}
}

// Compact triggers manual compaction
func (s *PebbleStorage) Compact(ctx context.Context, start, end []byte) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}

	return s.db.Compact(start, end, true)
}

// GetLatestHeight returns the latest indexed block height
func (s *PebbleStorage) GetLatestHeight(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	value, closer, err := s.db.Get(LatestHeightKey())
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, nil // No blocks indexed yet
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
	return s.db.Set(LatestHeightKey(), value, pebble.Sync)
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

	// Store block data
	if err := s.db.Set(BlockKey(height), encoded, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set block: %w", err)
	}

	// Store block hash index
	heightBytes := EncodeUint64(height)
	if err := s.db.Set(BlockHashIndexKey(block.Hash()), heightBytes, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set block hash index: %w", err)
	}

	return nil
}

// GetTransaction returns a transaction and its location by hash
func (s *PebbleStorage) GetTransaction(ctx context.Context, hash common.Hash) (*types.Transaction, *TxLocation, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, nil, err
	}

	// Get transaction location
	locValue, closer, err := s.db.Get(TransactionHashIndexKey(hash))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("failed to get transaction location: %w", err)
	}
	defer closer.Close()

	location, err := DecodeTxLocation(locValue)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode location: %w", err)
	}

	// Get transaction data
	txValue, closer, err := s.db.Get(TransactionKey(location.BlockHeight, location.TxIndex))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("failed to get transaction: %w", err)
	}
	defer closer.Close()

	tx, err := DecodeTransaction(txValue)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	return tx, location, nil
}

// SetTransaction stores a transaction with its location
func (s *PebbleStorage) SetTransaction(ctx context.Context, tx *types.Transaction, location *TxLocation) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if tx == nil {
		return fmt.Errorf("transaction cannot be nil")
	}
	if location == nil {
		return fmt.Errorf("location cannot be nil")
	}

	// Encode transaction
	encoded, err := EncodeTransaction(tx)
	if err != nil {
		return fmt.Errorf("failed to encode transaction: %w", err)
	}

	// Encode location
	locEncoded, err := EncodeTxLocation(location)
	if err != nil {
		return fmt.Errorf("failed to encode location: %w", err)
	}

	// Write transaction data
	if err := s.db.Set(TransactionKey(location.BlockHeight, location.TxIndex), encoded, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set transaction: %w", err)
	}

	// Write transaction hash index
	if err := s.db.Set(TransactionHashIndexKey(tx.Hash()), locEncoded, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set transaction index: %w", err)
	}

	return nil
}

// GetTransactionsByAddress returns transactions for an address with pagination
func (s *PebbleStorage) GetTransactionsByAddress(ctx context.Context, addr common.Address, limit, offset int) ([]common.Hash, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	prefix := AddressTransactionKeyPrefix(addr)
	// Create upper bound by copying prefix and appending 0xff
	// Must copy to avoid modifying the prefix slice
	upperBound := make([]byte, len(prefix), len(prefix)+1)
	copy(upperBound, prefix)
	upperBound = append(upperBound, 0xff)

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upperBound,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var hashes []common.Hash
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		if count < offset {
			count++
			continue
		}

		if len(hashes) >= limit {
			break
		}

		var hash common.Hash
		copy(hash[:], iter.Value())
		hashes = append(hashes, hash)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return hashes, nil
}

// AddTransactionToAddressIndex adds a transaction to an address index
func (s *PebbleStorage) AddTransactionToAddressIndex(ctx context.Context, addr common.Address, txHash common.Hash) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get next sequence number for this address
	s.addrSeqMu.Lock()
	seq := s.addrSeq[addr]
	s.addrSeq[addr]++
	s.addrSeqMu.Unlock()

	key := AddressTransactionKey(addr, seq)
	return s.db.Set(key, txHash[:], pebble.Sync)
}

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

	return receipt, nil
}

// SetReceipt stores a transaction receipt
func (s *PebbleStorage) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	if receipt == nil {
		return fmt.Errorf("receipt cannot be nil")
	}

	encoded, err := EncodeReceipt(receipt)
	if err != nil {
		return fmt.Errorf("failed to encode receipt: %w", err)
	}

	// Note: TxHash might not be set on receipt, using zero hash for now
	// In practice, caller should ensure TxHash is set
	txHash := receipt.TxHash
	return s.db.Set(ReceiptKey(txHash), encoded, pebble.Sync)
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

// HasTransaction checks if a transaction exists
func (s *PebbleStorage) HasTransaction(ctx context.Context, hash common.Hash) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	_, closer, err := s.db.Get(TransactionHashIndexKey(hash))
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	closer.Close()
	return true, nil
}

// loadAddressSequences loads address sequence counters from database
func (s *PebbleStorage) loadAddressSequences() error {
	// For now, we'll initialize sequences to 0
	// In production, we should scan the database to find the max sequence for each address
	// This is acceptable for initial implementation
	return nil
}

// pebbleBatch implements Batch interface
type pebbleBatch struct {
	storage *PebbleStorage
	batch   *pebble.Batch
	count   int
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
	return nil
}

// SetReceipt adds set receipt operation to batch
func (b *pebbleBatch) SetReceipt(ctx context.Context, receipt *types.Receipt) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrClosed
	}

	encoded, err := EncodeReceipt(receipt)
	if err != nil {
		return fmt.Errorf("failed to encode receipt: %w", err)
	}

	if err := b.batch.Set(ReceiptKey(receipt.TxHash), encoded, nil); err != nil {
		return err
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

	return b.batch.Commit(pebble.Sync)
}

// Reset clears all operations in the batch
func (b *pebbleBatch) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.batch.Reset()
	b.count = 0
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

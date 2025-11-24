package storage

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
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
		Cache:                    pebble.NewCache(int64(cfg.Cache) << 20), // Convert MB to bytes
		MaxOpenFiles:             cfg.MaxOpenFiles,
		MemTableSize:             uint64(cfg.WriteBuffer) << 20,
		DisableWAL:               cfg.DisableWAL,
		MaxConcurrentCompactions: func() int { return cfg.CompactionConcurrency },
		ErrorIfExists:            false,
		ErrorIfNotExists:         false,
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

// ============================================================================
// Historical Data Methods
// ============================================================================

// GetBlocksByTimeRange returns blocks within a time range
func (s *PebbleStorage) GetBlocksByTimeRange(ctx context.Context, fromTime, toTime uint64, limit, offset int) ([]*types.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromTime > toTime {
		return nil, fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}

	// Create iterator for timestamp range
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: BlockTimestampKey(fromTime, 0),
		UpperBound: BlockTimestampKey(toTime+1, 0),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var blocks []*types.Block
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if count < offset {
			count++
			continue
		}

		// Stop if limit reached
		if len(blocks) >= limit {
			break
		}

		// Extract height from value
		height, err := DecodeUint64(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode height: %w", err)
		}

		// Get block by height
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			if err == ErrNotFound {
				continue // Skip missing blocks
			}
			return nil, fmt.Errorf("failed to get block %d: %w", height, err)
		}

		blocks = append(blocks, block)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return blocks, nil
}

// GetBlockByTimestamp returns the block closest to the given timestamp
func (s *PebbleStorage) GetBlockByTimestamp(ctx context.Context, timestamp uint64) (*types.Block, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Binary search for closest timestamp
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: BlockTimestampKeyPrefix(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	// Seek to the target timestamp
	iter.SeekGE(BlockTimestampKey(timestamp, 0))

	var closestHeight uint64
	var found bool

	if iter.Valid() {
		// Found exact or later timestamp
		height, err := DecodeUint64(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode height: %w", err)
		}
		closestHeight = height
		found = true
	} else {
		// Seek to last block before timestamp
		iter.Last()
		if iter.Valid() {
			height, err := DecodeUint64(iter.Value())
			if err != nil {
				return nil, fmt.Errorf("failed to decode height: %w", err)
			}
			closestHeight = height
			found = true
		}
	}

	if !found {
		return nil, ErrNotFound
	}

	return s.GetBlock(ctx, closestHeight)
}

// GetTransactionsByAddressFiltered returns filtered transactions for an address
func (s *PebbleStorage) GetTransactionsByAddressFiltered(ctx context.Context, addr common.Address, filter *TransactionFilter, limit, offset int) ([]*TransactionWithReceipt, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if filter == nil {
		filter = DefaultTransactionFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	// Get all transaction hashes for the address
	// We need to scan all because we don't have block-indexed address transactions
	prefix := AddressTransactionKeyPrefix(addr)
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

	var results []*TransactionWithReceipt
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		if len(results) >= limit {
			break
		}

		var txHash common.Hash
		copy(txHash[:], iter.Value())

		// Get transaction and location
		tx, location, err := s.GetTransaction(ctx, txHash)
		if err != nil {
			if err == ErrNotFound {
				continue
			}
			return nil, fmt.Errorf("failed to get transaction: %w", err)
		}

		// Get receipt
		receipt, err := s.GetReceipt(ctx, txHash)
		if err != nil {
			if err == ErrNotFound {
				// Continue without receipt (optional)
				receipt = nil
			} else {
				return nil, fmt.Errorf("failed to get receipt: %w", err)
			}
		}

		// Apply filter
		if filter.MatchTransaction(tx, receipt, location, addr) {
			if count < offset {
				count++
				continue
			}

			results = append(results, &TransactionWithReceipt{
				Transaction: tx,
				Receipt:     receipt,
				Location:    location,
			})
			count++
		}
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return results, nil
}

// GetAddressBalance returns the balance of an address at a specific block
func (s *PebbleStorage) GetAddressBalance(ctx context.Context, addr common.Address, blockNumber uint64) (*big.Int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// If blockNumber is 0, get latest balance
	if blockNumber == 0 {
		value, closer, err := s.db.Get(AddressBalanceLatestKey(addr))
		if err != nil {
			if err == pebble.ErrNotFound {
				return big.NewInt(0), nil // No balance recorded
			}
			return nil, fmt.Errorf("failed to get latest balance: %w", err)
		}
		defer closer.Close()

		return DecodeBigInt(value), nil
	}

	// Get balance at specific block by iterating history
	prefix := AddressBalanceKeyPrefix(addr)
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

	var balance *big.Int = big.NewInt(0)

	// Iterate through all snapshots up to target block
	for iter.First(); iter.Valid(); iter.Next() {
		snapshot, err := DecodeBalanceSnapshot(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode snapshot: %w", err)
		}

		if snapshot.BlockNumber > blockNumber {
			break // Past target block
		}

		balance = snapshot.Balance
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return balance, nil
}

// GetBalanceHistory returns the balance history for an address
func (s *PebbleStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, limit, offset int) ([]BalanceSnapshot, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	prefix := AddressBalanceKeyPrefix(addr)
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

	var snapshots []BalanceSnapshot
	count := 0

	for iter.First(); iter.Valid(); iter.Next() {
		if len(snapshots) >= limit {
			break
		}

		snapshot, err := DecodeBalanceSnapshot(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to decode snapshot: %w", err)
		}

		// Filter by block range
		if snapshot.BlockNumber < fromBlock || snapshot.BlockNumber > toBlock {
			continue
		}

		if count < offset {
			count++
			continue
		}

		snapshots = append(snapshots, *snapshot)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return snapshots, nil
}

// GetBlockCount returns the total number of indexed blocks
func (s *PebbleStorage) GetBlockCount(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	value, closer, err := s.db.Get(BlockCountKey())
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, nil // No blocks indexed yet
		}
		return 0, fmt.Errorf("failed to get block count: %w", err)
	}
	defer closer.Close()

	count, err := DecodeUint64(value)
	if err != nil {
		return 0, fmt.Errorf("failed to decode block count: %w", err)
	}

	return count, nil
}

// GetTransactionCount returns the total number of indexed transactions
func (s *PebbleStorage) GetTransactionCount(ctx context.Context) (uint64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	value, closer, err := s.db.Get(TransactionCountKey())
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, nil // No transactions indexed yet
		}
		return 0, fmt.Errorf("failed to get transaction count: %w", err)
	}
	defer closer.Close()

	count, err := DecodeUint64(value)
	if err != nil {
		return 0, fmt.Errorf("failed to decode transaction count: %w", err)
	}

	return count, nil
}

// GetTopMiners returns the top miners by block count
func (s *PebbleStorage) GetTopMiners(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]MinerStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	// Get the latest height to know how many blocks to scan
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		if err == ErrNotFound {
			return []MinerStats{}, nil
		}
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Determine block range
	startBlock := fromBlock
	endBlock := toBlock
	if toBlock == 0 || toBlock > latestHeight {
		endBlock = latestHeight
	}
	if fromBlock > endBlock {
		return []MinerStats{}, nil
	}

	// Aggregate miner stats
	minerMap := make(map[common.Address]*MinerStats)
	totalBlocks := uint64(0)

	// Scan blocks in range - calculate everything in one pass
	for height := startBlock; height <= endBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue // Skip missing blocks
		}

		totalBlocks++
		miner := block.Coinbase()

		stats, exists := minerMap[miner]
		if !exists {
			stats = &MinerStats{
				Address:         miner,
				BlockCount:      0,
				LastBlockNumber: 0,
				LastBlockTime:   0,
				Percentage:      0,
				TotalRewards:    big.NewInt(0),
			}
			minerMap[miner] = stats
		}

		stats.BlockCount++
		if height > stats.LastBlockNumber {
			stats.LastBlockNumber = height
			stats.LastBlockTime = block.Time()
		}

		// Calculate transaction fees for this block
		// Create transaction map for O(1) lookup
		txMap := make(map[common.Hash]*types.Transaction)
		for _, tx := range block.Transactions() {
			txMap[tx.Hash()] = tx
		}

		// Get receipts and calculate fees
		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err == nil {
			for _, receipt := range receipts {
				if receipt.GasUsed > 0 {
					// O(1) lookup instead of O(n) search
					if tx, found := txMap[receipt.TxHash]; found {
						fee := new(big.Int).Mul(tx.GasPrice(), big.NewInt(int64(receipt.GasUsed)))
						stats.TotalRewards.Add(stats.TotalRewards, fee)
					}
				}
			}
		}
	}

	// Calculate percentage
	for _, stats := range minerMap {
		if totalBlocks > 0 {
			stats.Percentage = float64(stats.BlockCount) / float64(totalBlocks) * 100.0
		}
	}

	// Convert map to slice
	result := make([]MinerStats, 0, len(minerMap))
	for _, stats := range minerMap {
		result = append(result, *stats)
	}

	// Sort by block count (descending)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].BlockCount > result[i].BlockCount {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Apply limit
	if len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// GetTokenBalances returns token balances for an address by scanning Transfer events
func (s *PebbleStorage) GetTokenBalances(ctx context.Context, addr common.Address) ([]TokenBalance, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// ERC-20 Transfer event signature: Transfer(address indexed from, address indexed to, uint256 value)
	transferTopic := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	// Get the latest height
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		if err == ErrNotFound {
			return []TokenBalance{}, nil
		}
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Map to track balances by contract address
	balanceMap := make(map[common.Address]*big.Int)

	// Scan all blocks for receipts
	for height := uint64(0); height <= latestHeight; height++ {
		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		for _, receipt := range receipts {
			for _, log := range receipt.Logs {
				// Check if this is a Transfer event
				if len(log.Topics) < 3 || log.Topics[0] != transferTopic {
					continue
				}

				// Extract from and to addresses from topics
				from := common.BytesToAddress(log.Topics[1].Bytes())
				to := common.BytesToAddress(log.Topics[2].Bytes())

				// Check if this transfer involves our address
				if from != addr && to != addr {
					continue
				}

				// Extract value from data
				if len(log.Data) < 32 {
					continue
				}
				value := new(big.Int).SetBytes(log.Data[:32])

				// Get or create balance entry for this contract
				contract := log.Address
				if _, exists := balanceMap[contract]; !exists {
					balanceMap[contract] = big.NewInt(0)
				}

				// Update balance
				if to == addr {
					// Receiving tokens
					balanceMap[contract].Add(balanceMap[contract], value)
				} else if from == addr {
					// Sending tokens
					balanceMap[contract].Sub(balanceMap[contract], value)
				}
			}
		}
	}

	// Convert map to slice
	result := make([]TokenBalance, 0, len(balanceMap))
	for contract, balance := range balanceMap {
		// Only include non-zero balances
		if balance.Sign() > 0 {
			result = append(result, TokenBalance{
				ContractAddress: contract,
				TokenType:       "ERC20",
				Balance:         balance,
				TokenID:         nil,
			})
		}
	}

	return result, nil
}

// SetBlockTimestamp indexes a block by timestamp
func (s *PebbleStorage) SetBlockTimestamp(ctx context.Context, timestamp uint64, height uint64) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	value := EncodeUint64(height)
	return s.db.Set(BlockTimestampKey(timestamp, height), value, pebble.Sync)
}

// UpdateBalance updates the balance for an address at a specific block
func (s *PebbleStorage) UpdateBalance(ctx context.Context, addr common.Address, blockNumber uint64, delta *big.Int, txHash common.Hash) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get current balance
	currentBalance, err := s.GetAddressBalance(ctx, addr, 0) // Get latest
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	// Calculate new balance
	newBalance := new(big.Int).Add(currentBalance, delta)
	if newBalance.Sign() < 0 {
		return fmt.Errorf("balance cannot be negative")
	}

	// Create snapshot
	snapshot := &BalanceSnapshot{
		BlockNumber: blockNumber,
		Balance:     newBalance,
		Delta:       delta,
		TxHash:      txHash,
	}

	// Encode snapshot
	encoded, err := EncodeBalanceSnapshot(snapshot)
	if err != nil {
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}

	// Get next sequence number (simple counter, could be optimized)
	s.addrSeqMu.Lock()
	seq := s.addrSeq[addr]
	s.addrSeq[addr]++
	s.addrSeqMu.Unlock()

	// Store history entry
	if err := s.db.Set(AddressBalanceKey(addr, seq), encoded, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set balance history: %w", err)
	}

	// Update latest balance
	balanceBytes := EncodeBigInt(newBalance)
	if err := s.db.Set(AddressBalanceLatestKey(addr), balanceBytes, pebble.Sync); err != nil {
		return fmt.Errorf("failed to set latest balance: %w", err)
	}

	return nil
}

// SetBalance sets the balance for an address at a specific block
func (s *PebbleStorage) SetBalance(ctx context.Context, addr common.Address, blockNumber uint64, balance *big.Int) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get current balance to calculate delta
	currentBalance, err := s.GetAddressBalance(ctx, addr, 0)
	if err != nil {
		return fmt.Errorf("failed to get current balance: %w", err)
	}

	// Calculate delta
	delta := new(big.Int).Sub(balance, currentBalance)

	// Use UpdateBalance
	return s.UpdateBalance(ctx, addr, blockNumber, delta, common.Hash{})
}

// System Contract Writer Methods

// StoreMintEvent stores a mint event
func (s *PebbleStorage) StoreMintEvent(ctx context.Context, event *MintEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Find the transaction and log index
	// For now, use a simple counter approach
	// In production, this should be derived from the actual log index
	txIndex := uint64(0)
	logIndex := uint64(0)

	key := MintEventKey(event.BlockNumber, txIndex, logIndex)
	data, err := EncodeMintEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode mint event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store mint event: %w", err)
	}

	return nil
}

// StoreBurnEvent stores a burn event
func (s *PebbleStorage) StoreBurnEvent(ctx context.Context, event *BurnEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)
	logIndex := uint64(0)

	key := BurnEventKey(event.BlockNumber, txIndex, logIndex)
	data, err := EncodeBurnEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode burn event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store burn event: %w", err)
	}

	return nil
}

// StoreMinterConfigEvent stores a minter configuration event
func (s *PebbleStorage) StoreMinterConfigEvent(ctx context.Context, event *MinterConfigEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := MinterConfigEventKey(event.Minter, event.BlockNumber)
	data, err := EncodeMinterConfigEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode minter config event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store minter config event: %w", err)
	}

	return nil
}

// StoreProposal stores a governance proposal
func (s *PebbleStorage) StoreProposal(ctx context.Context, proposal *Proposal) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ProposalKey(proposal.Contract, proposal.ProposalID.String())
	data, err := EncodeProposal(proposal)
	if err != nil {
		return fmt.Errorf("failed to encode proposal: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store proposal: %w", err)
	}

	// Store in status index
	statusKey := ProposalStatusIndexKey(proposal.Contract, uint8(proposal.Status), proposal.ProposalID.String())
	if err := s.db.Set(statusKey, []byte{1}, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store proposal status index: %w", err)
	}

	return nil
}

// UpdateProposalStatus updates the status of a proposal
func (s *PebbleStorage) UpdateProposalStatus(ctx context.Context, contract common.Address, proposalID *big.Int, status ProposalStatus, executedAt uint64) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get existing proposal
	key := ProposalKey(contract, proposalID.String())
	data, closer, err := s.db.Get(key)
	if err != nil {
		return fmt.Errorf("failed to get proposal: %w", err)
	}
	defer closer.Close()

	proposal, err := DecodeProposal(data)
	if err != nil {
		return fmt.Errorf("failed to decode proposal: %w", err)
	}

	// Remove old status index
	oldStatusKey := ProposalStatusIndexKey(contract, uint8(proposal.Status), proposalID.String())
	if err := s.db.Delete(oldStatusKey, pebble.Sync); err != nil {
		return fmt.Errorf("failed to delete old status index: %w", err)
	}

	// Update proposal
	proposal.Status = status
	if executedAt > 0 {
		proposal.ExecutedAt = &executedAt
	}

	// Store updated proposal
	updatedData, err := EncodeProposal(proposal)
	if err != nil {
		return fmt.Errorf("failed to encode updated proposal: %w", err)
	}

	if err := s.db.Set(key, updatedData, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store updated proposal: %w", err)
	}

	// Add new status index
	newStatusKey := ProposalStatusIndexKey(contract, uint8(status), proposalID.String())
	if err := s.db.Set(newStatusKey, []byte{1}, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store new status index: %w", err)
	}

	return nil
}

// StoreProposalVote stores a vote on a proposal
func (s *PebbleStorage) StoreProposalVote(ctx context.Context, vote *ProposalVote) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ProposalVoteKey(vote.Contract, vote.ProposalID.String(), vote.Voter)
	data, err := EncodeProposalVote(vote)
	if err != nil {
		return fmt.Errorf("failed to encode vote: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store vote: %w", err)
	}

	return nil
}

// StoreGasTipUpdateEvent stores a gas tip update event
func (s *PebbleStorage) StoreGasTipUpdateEvent(ctx context.Context, event *GasTipUpdateEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)

	key := GasTipUpdateEventKey(event.BlockNumber, txIndex)
	data, err := EncodeGasTipUpdateEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode gas tip update event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store gas tip update event: %w", err)
	}

	return nil
}

// StoreBlacklistEvent stores a blacklist event
func (s *PebbleStorage) StoreBlacklistEvent(ctx context.Context, event *BlacklistEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := BlacklistEventKey(event.Account, event.BlockNumber)
	data, err := EncodeBlacklistEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode blacklist event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store blacklist event: %w", err)
	}

	return nil
}

// StoreValidatorChangeEvent stores a validator change event
func (s *PebbleStorage) StoreValidatorChangeEvent(ctx context.Context, event *ValidatorChangeEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ValidatorChangeEventKey(event.Validator, event.BlockNumber)
	data, err := EncodeValidatorChangeEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode validator change event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store validator change event: %w", err)
	}

	return nil
}

// StoreMemberChangeEvent stores a member change event
func (s *PebbleStorage) StoreMemberChangeEvent(ctx context.Context, event *MemberChangeEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)

	key := MemberChangeEventKey(event.Contract, event.BlockNumber, txIndex)
	data, err := EncodeMemberChangeEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode member change event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store member change event: %w", err)
	}

	return nil
}

// StoreEmergencyPauseEvent stores an emergency pause event
func (s *PebbleStorage) StoreEmergencyPauseEvent(ctx context.Context, event *EmergencyPauseEvent) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	txIndex := uint64(0)

	key := EmergencyPauseEventKey(event.Contract, event.BlockNumber, txIndex)
	data, err := EncodeEmergencyPauseEvent(event)
	if err != nil {
		return fmt.Errorf("failed to encode emergency pause event: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store emergency pause event: %w", err)
	}

	return nil
}

// StoreDepositMintProposal stores a deposit mint proposal
func (s *PebbleStorage) StoreDepositMintProposal(ctx context.Context, proposal *DepositMintProposal) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := DepositMintProposalKey(proposal.ProposalID.String())
	data, err := EncodeDepositMintProposal(proposal)
	if err != nil {
		return fmt.Errorf("failed to encode deposit mint proposal: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store deposit mint proposal: %w", err)
	}

	return nil
}

// UpdateTotalSupply updates the total supply
func (s *PebbleStorage) UpdateTotalSupply(ctx context.Context, delta *big.Int) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Get current total supply
	key := TotalSupplyKey()
	data, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			// Initialize to 0
			data = EncodeBigInt(big.NewInt(0))
		} else {
			return fmt.Errorf("failed to get total supply: %w", err)
		}
	} else {
		defer closer.Close()
	}

	currentSupply := DecodeBigInt(data)
	newSupply := new(big.Int).Add(currentSupply, delta)

	// Store new total supply
	newData := EncodeBigInt(newSupply)
	if err := s.db.Set(key, newData, pebble.Sync); err != nil {
		return fmt.Errorf("failed to update total supply: %w", err)
	}

	return nil
}

// UpdateActiveMinter updates the active minter status
func (s *PebbleStorage) UpdateActiveMinter(ctx context.Context, minter common.Address, allowance *big.Int, active bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := MinterActiveIndexKey(minter)

	if active {
		// Store minter allowance
		data := EncodeBigInt(allowance)
		if err := s.db.Set(key, data, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set active minter: %w", err)
		}
	} else {
		// Remove minter
		if err := s.db.Delete(key, pebble.Sync); err != nil {
			return fmt.Errorf("failed to remove active minter: %w", err)
		}
	}

	return nil
}

// UpdateActiveValidator updates the active validator status
func (s *PebbleStorage) UpdateActiveValidator(ctx context.Context, validator common.Address, active bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := ValidatorActiveIndexKey(validator)

	if active {
		// Mark validator as active
		if err := s.db.Set(key, []byte{1}, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set active validator: %w", err)
		}
	} else {
		// Remove validator
		if err := s.db.Delete(key, pebble.Sync); err != nil {
			return fmt.Errorf("failed to remove active validator: %w", err)
		}
	}

	return nil
}

// UpdateBlacklistStatus updates the blacklist status of an address
func (s *PebbleStorage) UpdateBlacklistStatus(ctx context.Context, address common.Address, blacklisted bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	key := BlacklistActiveIndexKey(address)

	if blacklisted {
		// Mark address as blacklisted
		if err := s.db.Set(key, []byte{1}, pebble.Sync); err != nil {
			return fmt.Errorf("failed to set blacklist status: %w", err)
		}
	} else {
		// Remove from blacklist
		if err := s.db.Delete(key, pebble.Sync); err != nil {
			return fmt.Errorf("failed to remove blacklist status: %w", err)
		}
	}

	return nil
}

// IndexSystemContractEvent indexes a single system contract event from a log
// This is a placeholder implementation - actual parsing logic should be handled by events package
func (s *PebbleStorage) IndexSystemContractEvent(ctx context.Context, log *types.Log) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// This method should be called by the events package's SystemContractEventParser
	// which will parse the log and call the appropriate Store* methods
	return fmt.Errorf("IndexSystemContractEvent should be called from events package")
}

// IndexSystemContractEvents indexes multiple system contract events from logs (batch operation)
func (s *PebbleStorage) IndexSystemContractEvents(ctx context.Context, logs []*types.Log) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	// Batch index all events
	for _, log := range logs {
		if err := s.IndexSystemContractEvent(ctx, log); err != nil {
			return fmt.Errorf("failed to index event at block %d: %w", log.BlockNumber, err)
		}
	}

	return nil
}

// System Contract Reader Methods

// GetTotalSupply returns the current total supply
func (s *PebbleStorage) GetTotalSupply(ctx context.Context) (*big.Int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := TotalSupplyKey()
	data, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return big.NewInt(0), nil
		}
		return nil, fmt.Errorf("failed to get total supply: %w", err)
	}
	defer closer.Close()

	return DecodeBigInt(data), nil
}

// GetMintEvents returns mint events within a block range
func (s *PebbleStorage) GetMintEvents(ctx context.Context, fromBlock, toBlock uint64, minter common.Address, limit, offset int) ([]*MintEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// This is a simplified implementation - in production, would need proper iteration
	// TODO: Implement proper iteration over block range
	return nil, fmt.Errorf("GetMintEvents not yet implemented")
}

// GetBurnEvents returns burn events within a block range
func (s *PebbleStorage) GetBurnEvents(ctx context.Context, fromBlock, toBlock uint64, burner common.Address, limit, offset int) ([]*BurnEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// This is a simplified implementation - in production, would need proper iteration
	// TODO: Implement proper iteration over block range
	return nil, fmt.Errorf("GetBurnEvents not yet implemented")
}

// GetActiveMinters returns list of active minters
func (s *PebbleStorage) GetActiveMinters(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := MinterActiveIndexKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var minters []common.Address
	for iter.First(); iter.Valid(); iter.Next() {
		// Extract address from key
		key := string(iter.Key())
		addrHex := key[len(string(keyPrefix)):]
		addr := common.HexToAddress(addrHex)
		minters = append(minters, addr)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return minters, nil
}

// GetMinterAllowance returns the allowance for a specific minter
func (s *PebbleStorage) GetMinterAllowance(ctx context.Context, minter common.Address) (*big.Int, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := MinterActiveIndexKey(minter)
	data, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return big.NewInt(0), nil
		}
		return nil, fmt.Errorf("failed to get minter allowance: %w", err)
	}
	defer closer.Close()

	return DecodeBigInt(data), nil
}

// GetMinterHistory returns configuration history for a minter
func (s *PebbleStorage) GetMinterHistory(ctx context.Context, minter common.Address) ([]*MinterConfigEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration over minter config events
	return nil, fmt.Errorf("GetMinterHistory not yet implemented")
}

// GetActiveValidators returns list of active validators
func (s *PebbleStorage) GetActiveValidators(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := ValidatorActiveIndexKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var validators []common.Address
	for iter.First(); iter.Valid(); iter.Next() {
		// Extract address from key
		key := string(iter.Key())
		addrHex := key[len(string(keyPrefix)):]
		addr := common.HexToAddress(addrHex)
		validators = append(validators, addr)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return validators, nil
}

// GetGasTipHistory returns gas tip update history
func (s *PebbleStorage) GetGasTipHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*GasTipUpdateEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration over block range
	return nil, fmt.Errorf("GetGasTipHistory not yet implemented")
}

// GetValidatorHistory returns validator change history
func (s *PebbleStorage) GetValidatorHistory(ctx context.Context, validator common.Address) ([]*ValidatorChangeEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration over validator change events
	return nil, fmt.Errorf("GetValidatorHistory not yet implemented")
}

// GetMinterConfigHistory returns minter configuration history
func (s *PebbleStorage) GetMinterConfigHistory(ctx context.Context, fromBlock, toBlock uint64) ([]*MinterConfigEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration over block range
	return nil, fmt.Errorf("GetMinterConfigHistory not yet implemented")
}

// GetEmergencyPauseHistory returns emergency pause event history
func (s *PebbleStorage) GetEmergencyPauseHistory(ctx context.Context, contract common.Address) ([]*EmergencyPauseEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration over emergency pause events
	return nil, fmt.Errorf("GetEmergencyPauseHistory not yet implemented")
}

// GetDepositMintProposals returns deposit mint proposals
func (s *PebbleStorage) GetDepositMintProposals(ctx context.Context, fromBlock, toBlock uint64, status ProposalStatus) ([]*DepositMintProposal, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration with status filter
	return nil, fmt.Errorf("GetDepositMintProposals not yet implemented")
}

// GetBurnHistory returns burn event history
func (s *PebbleStorage) GetBurnHistory(ctx context.Context, fromBlock, toBlock uint64, user common.Address) ([]*BurnEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration with user filter
	return nil, fmt.Errorf("GetBurnHistory not yet implemented")
}

// GetBlacklistedAddresses returns list of blacklisted addresses
func (s *PebbleStorage) GetBlacklistedAddresses(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := BlacklistActiveIndexKeyPrefix()
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var addresses []common.Address
	for iter.First(); iter.Valid(); iter.Next() {
		// Extract address from key
		key := string(iter.Key())
		addrHex := key[len(string(keyPrefix)):]
		addr := common.HexToAddress(addrHex)
		addresses = append(addresses, addr)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return addresses, nil
}

// GetBlacklistHistory returns blacklist event history for an address
func (s *PebbleStorage) GetBlacklistHistory(ctx context.Context, address common.Address) ([]*BlacklistEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration over blacklist events
	return nil, fmt.Errorf("GetBlacklistHistory not yet implemented")
}

// GetAuthorizedAccounts returns list of authorized accounts
func (s *PebbleStorage) GetAuthorizedAccounts(ctx context.Context) ([]common.Address, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement authorized accounts query
	return nil, fmt.Errorf("GetAuthorizedAccounts not yet implemented")
}

// GetProposals returns proposals with optional status filter
func (s *PebbleStorage) GetProposals(ctx context.Context, contract common.Address, status ProposalStatus, limit, offset int) ([]*Proposal, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := ProposalStatusIndexKeyPrefix(contract, uint8(status))
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var proposals []*Proposal
	count := 0
	skipped := 0

	for iter.First(); iter.Valid(); iter.Next() {
		// Skip offset items
		if skipped < offset {
			skipped++
			continue
		}

		// Check limit
		if limit > 0 && count >= limit {
			break
		}

		// Extract proposal ID from index key and get proposal
		key := string(iter.Key())
		proposalID := key[len(string(keyPrefix)):]

		proposalKey := ProposalKey(contract, proposalID)
		data, closer, err := s.db.Get(proposalKey)
		if err != nil {
			continue // Skip if proposal not found
		}

		proposal, err := DecodeProposal(data)
		closer.Close()
		if err != nil {
			continue // Skip if decode fails
		}

		proposals = append(proposals, proposal)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return proposals, nil
}

// GetProposalById returns a specific proposal by ID
func (s *PebbleStorage) GetProposalById(ctx context.Context, contract common.Address, proposalId *big.Int) (*Proposal, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	key := ProposalKey(contract, proposalId.String())
	data, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get proposal: %w", err)
	}
	defer closer.Close()

	proposal, err := DecodeProposal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode proposal: %w", err)
	}

	return proposal, nil
}

// GetProposalVotes returns votes for a specific proposal
func (s *PebbleStorage) GetProposalVotes(ctx context.Context, contract common.Address, proposalId *big.Int) ([]*ProposalVote, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	keyPrefix := ProposalVoteKeyPrefix(contract, proposalId.String())
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: keyPrefix,
		UpperBound: append(keyPrefix, 0xff),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var votes []*ProposalVote
	for iter.First(); iter.Valid(); iter.Next() {
		vote, err := DecodeProposalVote(iter.Value())
		if err != nil {
			continue // Skip invalid votes
		}
		votes = append(votes, vote)
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterator error: %w", err)
	}

	return votes, nil
}

// GetMemberHistory returns member change history for a contract
func (s *PebbleStorage) GetMemberHistory(ctx context.Context, contract common.Address) ([]*MemberChangeEvent, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// TODO: Implement proper iteration over member change events
	return nil, fmt.Errorf("GetMemberHistory not yet implemented")
}

// GetGasStatsByBlockRange returns gas usage statistics for a block range
func (s *PebbleStorage) GetGasStatsByBlockRange(ctx context.Context, fromBlock, toBlock uint64) (*GasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	stats := &GasStats{
		TotalGasUsed:     0,
		TotalGasLimit:    0,
		AverageGasUsed:   0,
		AverageGasPrice:  big.NewInt(0),
		BlockCount:       0,
		TransactionCount: 0,
	}

	totalGasPrice := big.NewInt(0)
	gasPriceCount := uint64(0)

	// Iterate through blocks
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue // Skip missing blocks
		}

		stats.BlockCount++
		stats.TotalGasLimit += block.GasLimit()
		stats.TotalGasUsed += block.GasUsed()

		// Get receipts to calculate actual gas used and gas prices
		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		stats.TransactionCount += uint64(len(receipts))

		// Calculate gas prices
		txs := block.Transactions()
		for i, tx := range txs {
			if i < len(receipts) {
				gasPrice := tx.GasPrice()
				if gasPrice != nil && gasPrice.Sign() > 0 {
					totalGasPrice.Add(totalGasPrice, gasPrice)
					gasPriceCount++
				}
			}
		}
	}

	// Calculate averages
	if stats.BlockCount > 0 {
		stats.AverageGasUsed = stats.TotalGasUsed / stats.BlockCount
	}

	if gasPriceCount > 0 {
		stats.AverageGasPrice.Div(totalGasPrice, big.NewInt(int64(gasPriceCount)))
	}

	return stats, nil
}

// GetGasStatsByAddress returns gas usage statistics for a specific address
func (s *PebbleStorage) GetGasStatsByAddress(ctx context.Context, addr common.Address, fromBlock, toBlock uint64) (*AddressGasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	stats := &AddressGasStats{
		Address:          addr,
		TotalGasUsed:     0,
		TransactionCount: 0,
		AverageGasPerTx:  0,
		TotalFeesPaid:    big.NewInt(0),
	}

	// Iterate through blocks
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}

		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		txs := block.Transactions()
		for i, tx := range txs {
			// Check if transaction is from this address
			sender, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
			if err != nil {
				continue
			}

			if sender == addr && i < len(receipts) {
				receipt := receipts[i]
				stats.TotalGasUsed += receipt.GasUsed
				stats.TransactionCount++

				// Calculate fees paid (gasUsed * gasPrice)
				gasPrice := tx.GasPrice()
				if gasPrice != nil {
					fee := new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
					stats.TotalFeesPaid.Add(stats.TotalFeesPaid, fee)
				}
			}
		}
	}

	// Calculate average
	if stats.TransactionCount > 0 {
		stats.AverageGasPerTx = stats.TotalGasUsed / stats.TransactionCount
	}

	return stats, nil
}

// GetTopAddressesByGasUsed returns the top addresses by total gas used
func (s *PebbleStorage) GetTopAddressesByGasUsed(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]AddressGasStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 10
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	// Map to track gas usage by address
	addressMap := make(map[common.Address]*AddressGasStats)

	// Iterate through blocks
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}

		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		txs := block.Transactions()
		for i, tx := range txs {
			sender, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
			if err != nil {
				continue
			}

			if i < len(receipts) {
				receipt := receipts[i]

				stats, exists := addressMap[sender]
				if !exists {
					stats = &AddressGasStats{
						Address:          sender,
						TotalGasUsed:     0,
						TransactionCount: 0,
						AverageGasPerTx:  0,
						TotalFeesPaid:    big.NewInt(0),
					}
					addressMap[sender] = stats
				}

				stats.TotalGasUsed += receipt.GasUsed
				stats.TransactionCount++

				// Calculate fees
				gasPrice := tx.GasPrice()
				if gasPrice != nil {
					fee := new(big.Int).Mul(big.NewInt(int64(receipt.GasUsed)), gasPrice)
					stats.TotalFeesPaid.Add(stats.TotalFeesPaid, fee)
				}
			}
		}
	}

	// Convert map to slice
	result := make([]AddressGasStats, 0, len(addressMap))
	for _, stats := range addressMap {
		// Calculate average
		if stats.TransactionCount > 0 {
			stats.AverageGasPerTx = stats.TotalGasUsed / stats.TransactionCount
		}
		result = append(result, *stats)
	}

	// Sort by total gas used (descending)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].TotalGasUsed > result[i].TotalGasUsed {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Apply limit
	if len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// GetTopAddressesByTxCount returns the top addresses by transaction count
func (s *PebbleStorage) GetTopAddressesByTxCount(ctx context.Context, limit int, fromBlock, toBlock uint64) ([]AddressActivityStats, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 10
	}

	if fromBlock > toBlock {
		return nil, fmt.Errorf("fromBlock (%d) cannot be greater than toBlock (%d)", fromBlock, toBlock)
	}

	// Map to track activity by address
	addressMap := make(map[common.Address]*AddressActivityStats)

	// Iterate through blocks
	for height := fromBlock; height <= toBlock; height++ {
		block, err := s.GetBlock(ctx, height)
		if err != nil {
			continue
		}

		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		txs := block.Transactions()
		for i, tx := range txs {
			sender, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
			if err != nil {
				continue
			}

			if i < len(receipts) {
				receipt := receipts[i]

				stats, exists := addressMap[sender]
				if !exists {
					stats = &AddressActivityStats{
						Address:            sender,
						TransactionCount:   0,
						TotalGasUsed:       0,
						LastActivityBlock:  0,
						FirstActivityBlock: height,
					}
					addressMap[sender] = stats
				}

				stats.TransactionCount++
				stats.TotalGasUsed += receipt.GasUsed

				if height > stats.LastActivityBlock {
					stats.LastActivityBlock = height
				}
				if height < stats.FirstActivityBlock {
					stats.FirstActivityBlock = height
				}
			}
		}
	}

	// Convert map to slice
	result := make([]AddressActivityStats, 0, len(addressMap))
	for _, stats := range addressMap {
		result = append(result, *stats)
	}

	// Sort by transaction count (descending)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].TransactionCount > result[i].TransactionCount {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Apply limit
	if len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// GetNetworkMetrics returns network activity metrics for a time range
func (s *PebbleStorage) GetNetworkMetrics(ctx context.Context, fromTime, toTime uint64) (*NetworkMetrics, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if fromTime > toTime {
		return nil, fmt.Errorf("fromTime (%d) cannot be greater than toTime (%d)", fromTime, toTime)
	}

	metrics := &NetworkMetrics{
		TPS:               0,
		BlockTime:         0,
		TotalBlocks:       0,
		TotalTransactions: 0,
		AverageBlockSize:  0,
		TimePeriod:        toTime - fromTime,
	}

	totalGasUsed := uint64(0)
	var firstBlockTime, lastBlockTime uint64

	// Get blocks by time range
	blocks, err := s.GetBlocksByTimeRange(ctx, fromTime, toTime, 10000, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get blocks: %w", err)
	}

	if len(blocks) == 0 {
		return metrics, nil
	}

	metrics.TotalBlocks = uint64(len(blocks))
	firstBlockTime = blocks[0].Time()
	lastBlockTime = blocks[len(blocks)-1].Time()

	// Calculate metrics
	for _, block := range blocks {
		metrics.TotalTransactions += uint64(block.Transactions().Len())
		totalGasUsed += block.GasUsed()
	}

	// Calculate averages
	if metrics.TotalBlocks > 0 {
		metrics.AverageBlockSize = totalGasUsed / metrics.TotalBlocks
	}

	// Calculate block time (in seconds)
	if metrics.TotalBlocks > 1 {
		timeDiff := lastBlockTime - firstBlockTime
		if timeDiff > 0 {
			metrics.BlockTime = float64(timeDiff) / float64(metrics.TotalBlocks-1)
			// Calculate TPS
			if timeDiff > 0 {
				metrics.TPS = float64(metrics.TotalTransactions) / float64(timeDiff)
			}
		}
	}

	return metrics, nil
}

// Search performs a unified search across blocks, transactions, and addresses
func (s *PebbleStorage) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]SearchResult, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if query == "" {
		return []SearchResult{}, nil
	}

	if limit <= 0 {
		limit = 10
	}

	var results []SearchResult
	queryType := detectQueryType(query)

	// Create a type filter map for quick lookup
	typeFilter := make(map[string]bool)
	if len(resultTypes) > 0 {
		for _, t := range resultTypes {
			typeFilter[t] = true
		}
	}

	// Helper function to check if type is allowed
	isTypeAllowed := func(t string) bool {
		if len(typeFilter) == 0 {
			return true
		}
		return typeFilter[t]
	}

	switch queryType {
	case "blockNumber":
		// Search by block number
		if isTypeAllowed("block") {
			blockNum, _ := strconv.ParseUint(query, 10, 64)
			block, err := s.GetBlock(ctx, blockNum)
			if err == nil && block != nil {
				metadata := map[string]interface{}{
					"number":           block.Number().Uint64(),
					"hash":             block.Hash().Hex(),
					"timestamp":        block.Time(),
					"transactionCount": len(block.Transactions()),
					"miner":            block.Coinbase().Hex(),
				}
				results = append(results, SearchResult{
					Type:     "block",
					Value:    fmt.Sprintf("%d", blockNum),
					Label:    fmt.Sprintf("Block #%d", blockNum),
					Metadata: metadata,
				})
			}
		}

	case "hash":
		// Try as block hash
		if isTypeAllowed("block") && len(results) < limit {
			hash := common.HexToHash(query)
			block, err := s.GetBlockByHash(ctx, hash)
			if err == nil && block != nil {
				metadata := map[string]interface{}{
					"number":           block.Number().Uint64(),
					"hash":             block.Hash().Hex(),
					"timestamp":        block.Time(),
					"transactionCount": len(block.Transactions()),
					"miner":            block.Coinbase().Hex(),
				}
				results = append(results, SearchResult{
					Type:     "block",
					Value:    block.Hash().Hex(),
					Label:    fmt.Sprintf("Block #%d", block.Number().Uint64()),
					Metadata: metadata,
				})
			}
		}

		// Try as transaction hash
		if isTypeAllowed("transaction") && len(results) < limit {
			hash := common.HexToHash(query)
			tx, location, err := s.GetTransaction(ctx, hash)
			if err == nil && tx != nil && location != nil {
				// Get sender address from transaction
				from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
				if err != nil {
					// If we can't get sender, skip this result
					from = common.Address{}
				}

				metadata := map[string]interface{}{
					"hash":        tx.Hash().Hex(),
					"from":        from.Hex(),
					"to":          "",
					"blockNumber": location.BlockHeight,
					"blockHash":   location.BlockHash.Hex(),
					"value":       tx.Value().String(),
					"gas":         tx.Gas(),
				}
				if tx.To() != nil {
					metadata["to"] = tx.To().Hex()
				}
				results = append(results, SearchResult{
					Type:     "transaction",
					Value:    tx.Hash().Hex(),
					Label:    fmt.Sprintf("Transaction %s", tx.Hash().Hex()[:10]+"..."),
					Metadata: metadata,
				})
			}
		}

	case "address":
		// Search by address
		addr := common.HexToAddress(query)

		// Check if it's a contract
		if isTypeAllowed("contract") && len(results) < limit {
			// Check if address has an ABI (indicating it's a contract)
			hasABI, _ := s.HasABI(ctx, addr)
			if hasABI {
				metadata := map[string]interface{}{
					"address":    addr.Hex(),
					"isContract": true,
				}

				// Try to get transaction count for this address
				txHashes, err := s.GetTransactionsByAddress(ctx, addr, 1, 0)
				if err == nil {
					metadata["transactionCount"] = len(txHashes)
				}

				results = append(results, SearchResult{
					Type:     "contract",
					Value:    addr.Hex(),
					Label:    fmt.Sprintf("Contract %s", addr.Hex()[:10]+"..."),
					Metadata: metadata,
				})
			}
		}

		// Always include as address if not found as contract or if both types allowed
		if isTypeAllowed("address") && len(results) < limit {
			metadata := map[string]interface{}{
				"address": addr.Hex(),
			}

			// Try to get transaction count
			txHashes, err := s.GetTransactionsByAddress(ctx, addr, 1, 0)
			if err == nil && len(txHashes) > 0 {
				metadata["transactionCount"] = len(txHashes)
			}

			results = append(results, SearchResult{
				Type:     "address",
				Value:    addr.Hex(),
				Label:    fmt.Sprintf("Address %s", addr.Hex()[:10]+"..."),
				Metadata: metadata,
			})
		}
	}

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// detectQueryType determines the type of search query
func detectQueryType(query string) string {
	// Remove 0x prefix if present
	query = strings.TrimPrefix(query, "0x")

	// Check if it's a number (block number)
	if _, err := strconv.ParseUint(query, 10, 64); err == nil {
		return "blockNumber"
	}

	// Check if it's a valid hex hash (64 characters for block/tx hash, 40 for address)
	if len(query) == 64 {
		// Could be block hash or transaction hash
		return "hash"
	} else if len(query) == 40 {
		// Address
		return "address"
	}

	// Default to address for shorter queries (partial address search could be implemented)
	return "address"
}

package storage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
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

	// Transaction count cache to avoid per-transaction reads
	txCount      atomic.Uint64
	txCountReady atomic.Bool

	// Optional token metadata fetcher for on-demand fetching from chain
	// When set, GetTokenBalances will fetch metadata from chain if not found in DB
	tokenMetadataFetcher TokenMetadataFetcher
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

	// Load transaction count into cache
	if err := storage.loadTransactionCount(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to load transaction count: %w", err)
	}

	return storage, nil
}

// loadTransactionCount loads the current transaction count into cache
func (s *PebbleStorage) loadTransactionCount() error {
	value, closer, err := s.db.Get(TransactionCountKey())
	if err != nil {
		if err == pebble.ErrNotFound {
			s.txCount.Store(0)
			s.txCountReady.Store(true)
			return nil
		}
		return fmt.Errorf("failed to get transaction count: %w", err)
	}
	defer closer.Close()

	count, err := DecodeUint64(value)
	if err != nil {
		return fmt.Errorf("failed to decode transaction count: %w", err)
	}

	s.txCount.Store(count)
	s.txCountReady.Store(true)
	return nil
}

// SetLogger sets the logger for the storage
func (s *PebbleStorage) SetLogger(logger *zap.Logger) {
	s.logger = logger
}

// SetTokenMetadataFetcher sets the token metadata fetcher for on-demand fetching
// When set, GetTokenBalances will fetch metadata from chain if not found in DB
func (s *PebbleStorage) SetTokenMetadataFetcher(fetcher TokenMetadataFetcher) {
	s.tokenMetadataFetcher = fetcher
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

// DeleteByPrefix deletes all keys with the given prefix
// Returns the number of deleted keys
func (s *PebbleStorage) DeleteByPrefix(prefix []byte) (int64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return 0, err
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementPrefix(prefix),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	batch := s.db.NewBatch()
	defer batch.Close()

	var count int64
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		if err := batch.Delete(key, nil); err != nil {
			return count, fmt.Errorf("failed to delete key: %w", err)
		}
		count++

		// Commit batch periodically to avoid memory issues
		if count%10000 == 0 {
			if err := batch.Commit(pebble.NoSync); err != nil {
				return count, fmt.Errorf("failed to commit batch: %w", err)
			}
			batch.Reset()
		}
	}

	// Commit remaining deletes
	if batch.Count() > 0 {
		if err := batch.Commit(pebble.NoSync); err != nil {
			return count, fmt.Errorf("failed to commit final batch: %w", err)
		}
	}

	return count, nil
}

// CountByPrefix counts all keys with the given prefix
func (s *PebbleStorage) CountByPrefix(prefix []byte) (int64, error) {
	if err := s.ensureNotClosed(); err != nil {
		return 0, err
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementPrefix(prefix),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var count int64
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	return count, nil
}

// incrementPrefix returns a prefix that is one greater than the input
// Used for creating upper bounds in range scans
func incrementPrefix(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	result := make([]byte, len(prefix))
	copy(result, prefix)
	for i := len(result) - 1; i >= 0; i-- {
		if result[i] < 0xff {
			result[i]++
			return result
		}
		result[i] = 0
	}
	// All bytes were 0xff, extend with a null byte
	return append(result, 0)
}

// ============================================================================
// KVStore interface implementation
// ============================================================================

// Put stores a value with the given key
func (s *PebbleStorage) Put(ctx context.Context, key, value []byte) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	return s.db.Set(key, value, pebble.Sync)
}

// Get retrieves a value by key
func (s *PebbleStorage) Get(ctx context.Context, key []byte) ([]byte, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	value, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer closer.Close()

	// Copy the value as it's only valid until closer.Close()
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Delete removes a key-value pair
func (s *PebbleStorage) Delete(ctx context.Context, key []byte) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}

	return s.db.Delete(key, pebble.Sync)
}

// Iterate iterates over keys with the given prefix
func (s *PebbleStorage) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Make copies of key and value as they're only valid until the next iteration
		key := make([]byte, len(iter.Key()))
		copy(key, iter.Key())
		value := make([]byte, len(iter.Value()))
		copy(value, iter.Value())

		if !fn(key, value) {
			break
		}
	}

	return iter.Error()
}

// Has checks if a key exists
func (s *PebbleStorage) Has(ctx context.Context, key []byte) (bool, error) {
	if err := s.ensureNotClosed(); err != nil {
		return false, err
	}

	_, closer, err := s.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	closer.Close()
	return true, nil
}

// prefixUpperBound returns the upper bound for prefix iteration
func prefixUpperBound(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	upper := make([]byte, len(prefix))
	copy(upper, prefix)
	for i := len(upper) - 1; i >= 0; i-- {
		if upper[i] < 0xff {
			upper[i]++
			return upper[:i+1]
		}
	}
	return nil // All 0xff, no upper bound
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

// loadAddressSequences loads address sequence counters from database
func (s *PebbleStorage) loadAddressSequences() error {
	// For now, we'll initialize sequences to 0
	// In production, we should scan the database to find the max sequence for each address
	// This is acceptable for initial implementation
	return nil
}

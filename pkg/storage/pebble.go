package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// PebbleStorage implements Storage interface using PebbleDB
type PebbleStorage struct {
	db     *pebble.DB
	config *Config
	logger *zap.Logger
	closed atomic.Bool

	// writeMu serializes block transactions (single writer). See BeginBlock.
	writeMu sync.Mutex

	// Lazy genesis allocation lookup (see SetGenesisBalanceResolver).
	genesisMu     sync.Mutex
	genesisClient port.BalanceSource
	genesisTried  map[common.Address]bool

	// Address transaction sequence counters
	// Maps address -> next sequence number
	addrSeqMu sync.RWMutex
	addrSeq   map[seqKey]uint64

	// Transaction count cache to avoid per-transaction reads
	txCount      atomic.Uint64
	txCountReady atomic.Bool

	// orphanRetention is how many reorganization records are kept (0: all).
	orphanRetention atomic.Uint64

	// pageSteps counts the entries scanPage visited, so tests can check
	// that a cursor page does not walk the entries before it.
	pageSteps atomic.Int64

	// Optional token metadata fetcher for on-demand fetching from chain
	// When set, GetTokenBalances will fetch metadata from chain if not found in DB
	tokenMetadataFetcher port.TokenMetadataFetcher
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
		addrSeq: make(map[seqKey]uint64),
	}

	if err := storage.checkSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Load transaction count into cache
	if err := storage.loadTransactionCount(); err != nil {
		_ = db.Close()
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
	defer func() { _ = closer.Close() }()

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

// SetOrphanRetention sets how many reorganization records are kept with the
// blocks they removed; older ones are deleted when a new one is recorded.
// 0 keeps them all (the default).
func (s *PebbleStorage) SetOrphanRetention(n uint64) {
	s.orphanRetention.Store(n)
}

// SetTokenMetadataFetcher sets the token metadata fetcher for on-demand fetching
// When set, GetTokenBalances will fetch metadata from chain if not found in DB
func (s *PebbleStorage) SetTokenMetadataFetcher(fetcher port.TokenMetadataFetcher) {
	s.tokenMetadataFetcher = fetcher
}

// ensureNotClosed checks if storage is closed
func (s *PebbleStorage) ensureNotClosed() error {
	if s.closed.Load() {
		return port.ErrClosed
	}
	return nil
}

// ensureNotReadOnly checks if storage is read-only
func (s *PebbleStorage) ensureNotReadOnly() error {
	if s.config.ReadOnly {
		return port.ErrReadOnly
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
	defer func() { _ = iter.Close() }()

	batch := s.db.NewBatch()
	defer func() { _ = batch.Close() }()

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
	defer func() { _ = iter.Close() }()

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

	return s.kv(ctx).Set(key, value, pebble.Sync)
}

// PutUnsynced stores a value without waiting for the disk: the write is in
// the write-ahead log, so a process crash keeps it, and the next synced
// write (every block commit) makes it durable against a system crash too.
// For records that are recreated or repeated after such a crash, such as
// notifications (each Sync costs an fsync, milliseconds on some systems).
func (s *PebbleStorage) PutUnsynced(ctx context.Context, key, value []byte) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	if err := s.ensureNotReadOnly(); err != nil {
		return err
	}
	return s.kv(ctx).Set(key, value, pebble.NoSync)
}

// Get retrieves a value by key
func (s *PebbleStorage) Get(ctx context.Context, key []byte) ([]byte, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	value, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, port.ErrNotFound
		}
		return nil, err
	}
	defer func() { _ = closer.Close() }()

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

	return s.kv(ctx).Delete(key, pebble.Sync)
}

// Iterate iterates over keys with the given prefix
func (s *PebbleStorage) Iterate(ctx context.Context, prefix []byte, fn func(key, value []byte) bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}

	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	})
	if err != nil {
		return err
	}
	defer func() { _ = iter.Close() }()

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

	_, closer, err := s.kv(ctx).Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	_ = closer.Close()
	return true, nil
}

// Scan implements KV.
func (s *PebbleStorage) Scan(ctx context.Context, lower, upper []byte, reverse bool, fn func(key, value []byte) bool) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return err
	}
	valid := iter.First()
	next := iter.Next
	if reverse {
		valid = iter.Last()
		next = iter.Prev
	}
	for ; valid; valid = next() {
		if err := ctx.Err(); err != nil {
			_ = iter.Close()
			return err
		}
		if !fn(append([]byte(nil), iter.Key()...), append([]byte(nil), iter.Value()...)) {
			break
		}
	}
	return errors.Join(iter.Error(), iter.Close())
}

// NewCursor implements KV.
func (s *PebbleStorage) NewCursor(ctx context.Context, lower, upper []byte) (port.Cursor, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}
	return s.kv(ctx).NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
}

var _ port.KV = (*PebbleStorage)(nil)

// PrefixEnd returns the smallest key greater than every key with the
// prefix (the exclusive upper bound of a prefix scan), or nil if none.
func PrefixEnd(prefix []byte) []byte { return prefixUpperBound(prefix) }

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

// Compact triggers manual compaction
func (s *PebbleStorage) Compact(ctx context.Context, start, end []byte) error {
	if err := s.ensureNotClosed(); err != nil {
		return err
	}

	return s.db.Compact(start, end, true)
}

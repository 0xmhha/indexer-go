package storage

import (
	"fmt"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"
)

// Ensure PebbleBackend implements Backend
var _ Backend = (*PebbleBackend)(nil)

// PebbleBackend implements the Backend interface using PebbleDB
type PebbleBackend struct {
	db     *pebble.DB
	logger *zap.Logger
}

// NewPebbleBackend creates a new PebbleDB backend
func NewPebbleBackend(config *BackendConfig, logger *zap.Logger) (*PebbleBackend, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	// Configure PebbleDB options
	opts := &pebble.Options{
		Cache:                    pebble.NewCache(int64(config.Cache) << 20),
		MaxOpenFiles:             config.MaxOpenFiles,
		MemTableSize:             uint64(config.WriteBuffer) << 20,
		MaxConcurrentCompactions: func() int { return 1 },
		ErrorIfExists:            false,
		ErrorIfNotExists:         false,
	}

	if config.ReadOnly {
		opts.ReadOnly = true
	}

	// Open database
	db, err := pebble.Open(config.Path, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble database: %w", err)
	}

	return &PebbleBackend{
		db:     db,
		logger: logger,
	}, nil
}

// Type returns the backend type
func (b *PebbleBackend) Type() BackendType {
	return BackendTypePebble
}

// Get retrieves a value by key
func (b *PebbleBackend) Get(key []byte) ([]byte, error) {
	value, closer, err := b.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer closer.Close()

	// Copy value since it's only valid until closer is closed
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Set stores a key-value pair
func (b *PebbleBackend) Set(key, value []byte) error {
	return b.db.Set(key, value, pebble.Sync)
}

// Delete removes a key
func (b *PebbleBackend) Delete(key []byte) error {
	return b.db.Delete(key, pebble.Sync)
}

// Has checks if a key exists
func (b *PebbleBackend) Has(key []byte) (bool, error) {
	_, closer, err := b.db.Get(key)
	if err != nil {
		if err == pebble.ErrNotFound {
			return false, nil
		}
		return false, err
	}
	closer.Close()
	return true, nil
}

// NewIterator creates an iterator for range scans
func (b *PebbleBackend) NewIterator(start, end []byte) Iterator {
	opts := &pebble.IterOptions{
		LowerBound: start,
		UpperBound: end,
	}
	iter, _ := b.db.NewIter(opts)
	iter.First()
	return &PebbleIterator{iter: iter}
}

// NewBatch creates a new batch for atomic writes
func (b *PebbleBackend) NewBatch() BackendBatch {
	return &PebbleBatch{
		batch: b.db.NewBatch(),
	}
}

// Close closes the backend
func (b *PebbleBackend) Close() error {
	return b.db.Close()
}

// GetDB returns the underlying pebble.DB instance
// This is useful for advanced operations not covered by the Backend interface
func (b *PebbleBackend) GetDB() *pebble.DB {
	return b.db
}

// =============================================================================
// PebbleIterator
// =============================================================================

// PebbleIterator implements Iterator for PebbleDB
type PebbleIterator struct {
	iter *pebble.Iterator
}

// Valid returns true if the iterator is positioned at a valid item
func (i *PebbleIterator) Valid() bool {
	return i.iter.Valid()
}

// Next advances the iterator to the next item
func (i *PebbleIterator) Next() {
	i.iter.Next()
}

// Key returns the current key
func (i *PebbleIterator) Key() []byte {
	return i.iter.Key()
}

// Value returns the current value
func (i *PebbleIterator) Value() []byte {
	return i.iter.Value()
}

// Close releases iterator resources
func (i *PebbleIterator) Close() error {
	return i.iter.Close()
}

// =============================================================================
// PebbleBatch
// =============================================================================

// PebbleBatch implements BackendBatch for PebbleDB
type PebbleBatch struct {
	batch *pebble.Batch
	count int
}

// Set adds a set operation to the batch
func (b *PebbleBatch) Set(key, value []byte) error {
	b.count++
	return b.batch.Set(key, value, nil)
}

// Delete adds a delete operation to the batch
func (b *PebbleBatch) Delete(key []byte) error {
	b.count++
	return b.batch.Delete(key, nil)
}

// Commit writes all batched operations atomically
func (b *PebbleBatch) Commit() error {
	return b.batch.Commit(pebble.Sync)
}

// Reset clears all operations in the batch
func (b *PebbleBatch) Reset() {
	b.batch.Reset()
	b.count = 0
}

// Count returns the number of operations in the batch
func (b *PebbleBatch) Count() int {
	return b.count
}

// Close releases batch resources without committing
func (b *PebbleBatch) Close() error {
	return b.batch.Close()
}

// =============================================================================
// Registration
// =============================================================================

func init() {
	// Register PebbleDB backend with the global registry
	MustRegisterBackend(
		BackendTypePebble,
		func(config *BackendConfig, logger *zap.Logger) (Backend, error) {
			return NewPebbleBackend(config, logger)
		},
		&BackendMetadata{
			Name:        "PebbleDB",
			Description: "High-performance key-value store from CockroachDB",
			Version:     "1.0.0",
			Features: []string{
				"atomic-batches",
				"range-scans",
				"compaction",
				"snapshots",
			},
		},
	)
}

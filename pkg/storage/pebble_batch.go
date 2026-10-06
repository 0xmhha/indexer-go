package storage

import (
	"context"
	"sync"

	"github.com/cockroachdb/pebble"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// pebbleBatch groups the writes of one storage method (log indexing) and
// commits them through commitBatch, so inside a block transaction they join
// the block batch.
type pebbleBatch struct {
	storage *PebbleStorage
	batch   *pebble.Batch
	ctx     context.Context // nil: commit to the DB; see newBatchCtx
	closed  bool
	mu      sync.Mutex
}

// Commit writes all batched operations atomically
func (b *pebbleBatch) Commit() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return port.ErrClosed
	}

	return b.storage.commitBatch(b.ctx, b.batch, pebble.Sync)
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

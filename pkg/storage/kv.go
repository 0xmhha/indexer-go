package storage

import (
	"context"

	"github.com/cockroachdb/pebble"
)

// kvStore is the subset of *pebble.DB and *pebble.Batch that PebbleStorage
// reads and writes through. Routing every access through kv(ctx) lets a
// caller bind a block transaction (an indexed batch) to ctx so that all
// writes made while indexing one block commit atomically, and reads in the
// same block see those writes.
type kvStore interface {
	pebble.Reader
	pebble.Writer
}

// blockTxKey is the context key under which a block transaction is bound.
type blockTxKey struct{}

// blockTxBinding is what a block transaction stores in a context.
type blockTxBinding struct {
	owner *PebbleStorage
	batch *pebble.Batch
}

// kv returns the store that reads and writes for ctx should use: the batch of
// the block transaction bound to ctx by this storage instance, or the DB.
func (s *PebbleStorage) kv(ctx context.Context) kvStore {
	if b := s.boundBatch(ctx); b != nil {
		return b
	}
	return s.db
}

func (s *PebbleStorage) boundBatch(ctx context.Context) *pebble.Batch {
	if ctx == nil {
		return nil
	}
	if tx, ok := ctx.Value(blockTxKey{}).(*blockTxBinding); ok && tx.owner == s {
		return tx.batch
	}
	return nil
}

// newBatch returns a write batch for a single storage method. Commit it with
// commitBatch so that it joins the bound block transaction when there is one.
func (s *PebbleStorage) newBatch(ctx context.Context) *pebble.Batch {
	return s.db.NewBatch()
}

// commitBatch commits a batch created by newBatch. Inside a block transaction
// the operations are applied to the block batch instead and become durable
// when the block commits. In both cases the caller still closes b.
func (s *PebbleStorage) commitBatch(ctx context.Context, b *pebble.Batch, opts *pebble.WriteOptions) error {
	if outer := s.boundBatch(ctx); outer != nil {
		return outer.Apply(b, nil)
	}
	return b.Commit(opts)
}

// newBatchCtx returns a Batch wrapper whose Commit goes through commitBatch
// with ctx. The exported NewBatch keeps committing straight to the DB.
func (s *PebbleStorage) newBatchCtx(ctx context.Context) *pebbleBatch {
	return &pebbleBatch{storage: s, batch: s.newBatch(ctx), ctx: ctx}
}

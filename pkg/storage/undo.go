package storage

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Undo records let the indexer roll back blocks after a chain reorganization
// (reorg design, section 3). When a block transaction that knows its height
// commits, the previous value of every key it wrote is recorded under
// /undo/<height> in the same batch, so the block's writes and their undo
// commit together. Only the last UndoWindow heights are kept.

// UndoWindow is how many recent blocks can be rolled back.
const UndoWindow = 128

const prefixUndo = "/undo/"

// UndoKey returns the key of the undo record of a height.
func UndoKey(height uint64) []byte { return []byte(fmt.Sprintf("%s%020d", prefixUndo, height)) }

type undoEntry struct {
	Key     []byte
	Existed bool
	Prev    []byte
}

type undoRecord struct {
	Complete bool // false when the block used an operation undo cannot reverse
	Entries  []undoEntry
}

// SetHeight tells the transaction which block it indexes, so Commit records
// the block's undo. Transactions without a height (backfill, tooling) do not.
func (tx *BlockTx) SetHeight(height uint64) { tx.height = &height }

// writeUndo builds the undo record from the batch's operations and adds it,
// with pruning of the record that leaves the window, to the batch.
func (tx *BlockTx) writeUndo() error {
	if tx.height == nil {
		return nil
	}
	h := *tx.height
	rec := undoRecord{Complete: true}
	seen := map[string]bool{}
	r := tx.batch.Reader()
	for {
		kind, key, _, ok, err := r.Next()
		if err != nil {
			return fmt.Errorf("read block batch: %w", err)
		}
		if !ok {
			break
		}
		switch kind {
		case pebble.InternalKeyKindSet, pebble.InternalKeyKindDelete, pebble.InternalKeyKindSingleDelete, pebble.InternalKeyKindSetWithDelete:
		case pebble.InternalKeyKindLogData:
			continue
		default:
			rec.Complete = false // range deletes and merges are not recorded
			continue
		}
		if seen[string(key)] || outboxKey(key) {
			continue // outbox entries are kept on rollback (outbox.go)
		}
		seen[string(key)] = true
		rec.Entries = append(rec.Entries, undoEntry{Key: append([]byte(nil), key...)})
	}
	if err := tx.readPrevious(rec.Entries); err != nil {
		return err
	}
	enc, err := rlp.EncodeToBytes(&rec)
	if err != nil {
		return err
	}
	if err := tx.batch.Set(UndoKey(h), enc, nil); err != nil {
		return err
	}
	if h >= UndoWindow {
		if err := tx.batch.Delete(UndoKey(h-UndoWindow), nil); err != nil {
			return err
		}
	}
	return nil
}

// undoBlock rolls back block h in one transaction, archiving it as an
// orphan of reorg first (with the reorganization record when first). onUndo
// runs in the same transaction after the block's keys are restored.
func (s *PebbleStorage) undoBlock(ctx context.Context, h uint64, reorg *port.Reorg, first bool, onUndo port.UndoHook) (*port.OrphanedBlock, error) {
	raw, closer, err := s.kv(ctx).Get(UndoKey(h))
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, fmt.Errorf("%w %d", port.ErrNoUndo, h)
	}
	if err != nil {
		return nil, err
	}
	var rec undoRecord
	decErr := rlp.DecodeBytes(raw, &rec)
	if err := closer.Close(); err != nil {
		return nil, fmt.Errorf("read undo record %d: %w", h, err)
	}
	if decErr != nil {
		return nil, fmt.Errorf("decode undo record %d: %w", h, decErr)
	}
	if !rec.Complete {
		return nil, fmt.Errorf("%w %d (incomplete)", port.ErrNoUndo, h)
	}

	txCtx, tx, err := s.beginBlock(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ob, err := s.archiveOrphan(txCtx, tx, h, reorg, first)
	if err != nil {
		return nil, err
	}
	for i := len(rec.Entries) - 1; i >= 0; i-- {
		e := rec.Entries[i]
		if e.Existed {
			err = tx.batch.Set(e.Key, e.Prev, nil)
		} else {
			err = tx.batch.Delete(e.Key, nil)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := tx.batch.Delete(UndoKey(h), nil); err != nil {
		return nil, err
	}
	if onUndo != nil {
		if err := onUndo(txCtx, reorg, ob, first); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ob, nil
}

// resetCaches drops in-memory state derived from stored data, so it is read
// again after a rollback changed that data.
func (s *PebbleStorage) resetCaches() {
	s.addrSeqMu.Lock()
	s.addrSeq = map[seqKey]uint64{}
	s.addrSeqMu.Unlock()
	if err := s.loadTransactionCount(); err != nil {
		s.logger.Warn("reload transaction count after rollback failed")
	}
	s.resetGenesisTried()
}

// DropUndo deletes the undo record of a height, making the block impossible
// to roll back.
func (s *PebbleStorage) DropUndo(ctx context.Context, height uint64) error {
	return s.kv(ctx).Delete(UndoKey(height), pebble.Sync)
}

// checkUndo verifies that a height has a complete undo record.
func (s *PebbleStorage) checkUndo(ctx context.Context, h uint64) error {
	raw, closer, err := s.kv(ctx).Get(UndoKey(h))
	if errors.Is(err, pebble.ErrNotFound) {
		return fmt.Errorf("%w %d", port.ErrNoUndo, h)
	}
	if err != nil {
		return err
	}
	var rec undoRecord
	decErr := rlp.DecodeBytes(raw, &rec)
	if err := closer.Close(); err != nil {
		return fmt.Errorf("read undo record %d: %w", h, err)
	}
	if decErr != nil {
		return fmt.Errorf("decode undo record %d: %w", h, decErr)
	}
	if !rec.Complete {
		return fmt.Errorf("%w %d (incomplete)", port.ErrNoUndo, h)
	}
	return nil
}

// parallelUndoMin is the key count from which previous values are read on
// several cores; a large block writes thousands of keys.
const parallelUndoMin = 256

// readPrevious fills each entry with the committed value of its key. It
// reads the database, not the batch, which already holds this block's write.
func (tx *BlockTx) readPrevious(entries []undoEntry) error {
	read := func(e *undoEntry) error {
		prev, closer, err := tx.s.db.Get(e.Key)
		switch {
		case err == nil:
			e.Existed, e.Prev = true, append([]byte(nil), prev...)
			return closer.Close()
		case errors.Is(err, pebble.ErrNotFound):
			return nil
		default:
			return fmt.Errorf("read previous value: %w", err)
		}
	}
	if len(entries) < parallelUndoMin {
		for i := range entries {
			if err := read(&entries[i]); err != nil {
				return err
			}
		}
		return nil
	}

	workers := runtime.GOMAXPROCS(0)
	errs := make([]error, workers)
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				i := int(next.Add(1) - 1)
				if i >= len(entries) || errs[w] != nil {
					return
				}
				errs[w] = read(&entries[i])
			}
		}(w)
	}
	wg.Wait()
	return errors.Join(errs...)
}

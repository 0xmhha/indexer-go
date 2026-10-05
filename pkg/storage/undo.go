package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/rlp"
)

// Undo records let the indexer roll back blocks after a chain reorganization
// (reorg design, section 3). When a block transaction that knows its height
// commits, the previous value of every key it wrote is recorded under
// /undo/<height> in the same batch, so the block's writes and their undo
// commit together. Only the last UndoWindow heights are kept.

// UndoWindow is how many recent blocks can be rolled back.
const UndoWindow = 128

// ErrNoUndo means a block cannot be rolled back: its undo record was pruned,
// never written, or the block used an operation that cannot be undone.
var ErrNoUndo = errors.New("storage: no undo record for block")

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
		if seen[string(key)] {
			continue
		}
		seen[string(key)] = true
		e := undoEntry{Key: append([]byte(nil), key...)}
		// Read the committed value, not the batch: the batch already holds
		// this block's write.
		prev, closer, err := tx.s.db.Get(key)
		switch {
		case err == nil:
			e.Existed, e.Prev = true, append([]byte(nil), prev...)
			closer.Close()
		case !errors.Is(err, pebble.ErrNotFound):
			return fmt.Errorf("read previous value: %w", err)
		}
		rec.Entries = append(rec.Entries, e)
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

// RollbackTo undoes the indexed blocks above height to, newest first, one
// transaction per block, and returns the storage to the state it had after
// block `to` committed. If any of those blocks has no complete undo record it
// fails before changing anything.
func (s *PebbleStorage) RollbackTo(ctx context.Context, to uint64) error {
	latest, err := s.GetLatestHeight(ctx)
	if err != nil {
		return err
	}
	for h := latest; h > to; h-- {
		if err := s.checkUndo(ctx, h); err != nil {
			return err
		}
	}
	for h := latest; h > to; h-- {
		if err := s.undoBlock(ctx, h); err != nil {
			return err
		}
	}
	s.resetCaches()
	return nil
}

func (s *PebbleStorage) undoBlock(ctx context.Context, h uint64) error {
	raw, closer, err := s.kv(ctx).Get(UndoKey(h))
	if errors.Is(err, pebble.ErrNotFound) {
		return fmt.Errorf("%w %d", ErrNoUndo, h)
	}
	if err != nil {
		return err
	}
	var rec undoRecord
	err = rlp.DecodeBytes(raw, &rec)
	closer.Close()
	if err != nil {
		return fmt.Errorf("decode undo record %d: %w", h, err)
	}
	if !rec.Complete {
		return fmt.Errorf("%w %d (incomplete)", ErrNoUndo, h)
	}

	_, tx, err := s.BeginBlock(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := len(rec.Entries) - 1; i >= 0; i-- {
		e := rec.Entries[i]
		if e.Existed {
			err = tx.batch.Set(e.Key, e.Prev, nil)
		} else {
			err = tx.batch.Delete(e.Key, nil)
		}
		if err != nil {
			return err
		}
	}
	if err := tx.batch.Delete(UndoKey(h), nil); err != nil {
		return err
	}
	return tx.Commit()
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
		return fmt.Errorf("%w %d", ErrNoUndo, h)
	}
	if err != nil {
		return err
	}
	defer closer.Close()
	var rec undoRecord
	if err := rlp.DecodeBytes(raw, &rec); err != nil {
		return fmt.Errorf("decode undo record %d: %w", h, err)
	}
	if !rec.Complete {
		return fmt.Errorf("%w %d (incomplete)", ErrNoUndo, h)
	}
	return nil
}

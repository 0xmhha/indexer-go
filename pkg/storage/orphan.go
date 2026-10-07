package storage

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Orphaned blocks are the blocks a rollback removed from the canonical
// index. After a reorganization the node forgets the abandoned branch, but
// applications that showed its data need both the old and the new version
// to correct themselves, so each rolled-back block is kept here with its
// transactions and receipts, together with a record of the reorganization.
// The block is archived in the same transaction that rolls it back: it is
// either canonical or orphaned, never lost. Orphan keys are written by
// transactions without a height, so they have no undo and survive later
// rollbacks.

const (
	prefixOrphanReorg  = "/orphan/reorg/"
	prefixOrphanBlock  = "/orphan/block/"
	prefixOrphanHeight = "/orphan/height/"
	prefixOrphanTx     = "/orphan/tx/"
	keyOrphanSeq       = "/meta/orphan/seq"
)

// OrphanReorgKey returns the key of the reorganization record seq.
func OrphanReorgKey(seq uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", prefixOrphanReorg, seq))
}

// OrphanBlockKey returns the key of an orphaned block.
func OrphanBlockKey(hash common.Hash) []byte {
	return []byte(prefixOrphanBlock + hash.Hex())
}

// OrphanHeightKey returns the index key of an orphaned block by height.
func OrphanHeightKey(height uint64, hash common.Hash) []byte {
	return []byte(fmt.Sprintf("%s%020d/%s", prefixOrphanHeight, height, hash.Hex()))
}

func orphanHeightPrefix(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d/", prefixOrphanHeight, height))
}

// OrphanTxKey returns the index key of a transaction of an orphaned block.
func OrphanTxKey(txHash, blockHash common.Hash) []byte {
	return []byte(prefixOrphanTx + txHash.Hex() + "/" + blockHash.Hex())
}

func orphanTxPrefix(txHash common.Hash) []byte {
	return []byte(prefixOrphanTx + txHash.Hex() + "/")
}

// orphanBlockRecord is the stored form of OrphanedBlock. Fields may only be
// appended.
type orphanBlockRecord struct {
	Block    []byte
	Receipts [][]byte
	ReorgSeq uint64
}

// RollbackTo undoes the indexed blocks above height to, newest first, one
// transaction per block, and returns the storage to the state it had after
// block `to` committed. Each rolled-back block is archived as an orphan in
// its rollback transaction, and the first transaction records the
// reorganization. If any block in the range has no complete undo record,
// nothing is changed and the error wraps port.ErrNoUndo.
func (s *PebbleStorage) RollbackTo(ctx context.Context, to uint64) (*port.Reorg, error) {
	latest, err := s.GetLatestHeight(ctx)
	if err != nil {
		return nil, err
	}
	if latest <= to {
		return nil, nil
	}
	for h := latest; h > to; h-- {
		if err := s.checkUndo(ctx, h); err != nil {
			return nil, err
		}
	}

	rec := &port.Reorg{OldHead: latest, ForkNumber: to, DetectedAt: uint64(time.Now().Unix())}
	if fork, err := s.GetBlock(ctx, to); err == nil {
		rec.ForkHash = fork.Hash
	} else if !errors.Is(err, port.ErrNotFound) {
		return nil, fmt.Errorf("read fork block %d: %w", to, err)
	}
	for h := latest; h > to; h-- {
		b, err := s.GetBlock(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("read block %d to roll back: %w", h, err)
		}
		rec.Removed = append(rec.Removed, port.BlockRef{Number: h, Hash: b.Hash})
	}
	seq, err := s.lastReorgSeq(ctx)
	if err != nil {
		return nil, err
	}
	rec.Seq = seq + 1

	for h := latest; h > to; h-- {
		ob, err := s.undoBlock(ctx, h, rec, h == latest)
		if err != nil {
			return nil, err
		}
		rec.Blocks = append(rec.Blocks, ob)
	}
	s.resetCaches()
	return rec, nil
}

// archiveOrphan writes block h, about to be rolled back in tx, as an orphan
// of reorganization rec, and the reorganization record itself when first.
func (s *PebbleStorage) archiveOrphan(txCtx context.Context, tx *BlockTx, h uint64, rec *port.Reorg, first bool) (*port.OrphanedBlock, error) {
	b, err := s.GetBlock(txCtx, h)
	if err != nil {
		return nil, fmt.Errorf("read block %d to archive: %w", h, err)
	}
	ob := &port.OrphanedBlock{Block: b, ReorgSeq: rec.Seq}
	stored := orphanBlockRecord{ReorgSeq: rec.Seq}
	if stored.Block, err = model.EncodeBlock(b); err != nil {
		return nil, fmt.Errorf("encode orphaned block %d: %w", h, err)
	}
	for _, t := range b.Transactions {
		r, err := s.GetReceipt(txCtx, t.Hash)
		if err != nil {
			return nil, fmt.Errorf("read receipt of %s in block %d: %w", t.Hash.Hex(), h, err)
		}
		enc, err := model.EncodeReceipt(r)
		if err != nil {
			return nil, fmt.Errorf("encode receipt of %s: %w", t.Hash.Hex(), err)
		}
		ob.Receipts = append(ob.Receipts, r)
		stored.Receipts = append(stored.Receipts, enc)
	}
	value, err := rlp.EncodeToBytes(&stored)
	if err != nil {
		return nil, err
	}
	set := func(key, value []byte) error { return tx.batch.Set(key, value, nil) }
	if err := set(OrphanBlockKey(b.Hash), value); err != nil {
		return nil, err
	}
	if err := set(OrphanHeightKey(h, b.Hash), nil); err != nil {
		return nil, err
	}
	for _, t := range b.Transactions {
		if err := set(OrphanTxKey(t.Hash, b.Hash), nil); err != nil {
			return nil, err
		}
	}
	if first {
		value, err := rlp.EncodeToBytes(rec)
		if err != nil {
			return nil, err
		}
		if err := set(OrphanReorgKey(rec.Seq), value); err != nil {
			return nil, err
		}
		var seq [8]byte
		binary.BigEndian.PutUint64(seq[:], rec.Seq)
		if err := set([]byte(keyOrphanSeq), seq[:]); err != nil {
			return nil, err
		}
		if keep := s.orphanRetention.Load(); keep > 0 && rec.Seq > keep {
			if err := s.pruneOrphans(txCtx, tx, rec.Seq-keep); err != nil {
				return nil, fmt.Errorf("prune reorganization records: %w", err)
			}
		}
	}
	return ob, nil
}

// pruneOrphans deletes, in tx, the reorganization records numbered up to
// cutoff and the blocks they archived. A block a later reorganization
// archived again (its record names that reorganization) is kept, with its
// height and transaction entries.
func (s *PebbleStorage) pruneOrphans(txCtx context.Context, tx *BlockTx, cutoff uint64) error {
	var records [][]byte
	err := s.Scan(txCtx, []byte(prefixOrphanReorg), OrphanReorgKey(cutoff+1), false, func(_, value []byte) bool {
		records = append(records, value)
		return true
	})
	if err != nil {
		return err
	}
	var del [][]byte
	for _, raw := range records {
		var rec port.Reorg
		if err := rlp.DecodeBytes(raw, &rec); err != nil {
			return fmt.Errorf("decode reorganization record: %w", err)
		}
		for _, ref := range rec.Removed {
			ob, err := s.GetOrphanedBlock(txCtx, ref.Hash)
			if errors.Is(err, port.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if ob.ReorgSeq != rec.Seq {
				continue // archived again by a later reorganization
			}
			del = append(del, OrphanBlockKey(ref.Hash), OrphanHeightKey(ref.Number, ref.Hash))
			for _, t := range ob.Block.Transactions {
				del = append(del, OrphanTxKey(t.Hash, ref.Hash))
			}
		}
		del = append(del, OrphanReorgKey(rec.Seq))
	}
	for _, k := range del {
		if err := tx.batch.Delete(k, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *PebbleStorage) lastReorgSeq(ctx context.Context) (uint64, error) {
	v, err := s.get(ctx, []byte(keyOrphanSeq), "reorg sequence")
	if errors.Is(err, port.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if len(v) != 8 {
		return 0, fmt.Errorf("reorg sequence has %d bytes", len(v))
	}
	return binary.BigEndian.Uint64(v), nil
}

var (
	_ port.OrphanReader = (*PebbleStorage)(nil)
	_ port.Rollbacker   = (*PebbleStorage)(nil)
)

// GetReorgs implements OrphanReader. Record keys hold the zero-padded
// sequence number, so a reverse scan lists them newest first and the cursor
// is the last record's key. Pruned records (SetOrphanRetention) have no key.
func (s *PebbleStorage) GetReorgs(ctx context.Context, page port.Page) ([]*port.Reorg, string, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, "", err
	}
	prefix := []byte(prefixOrphanReorg)
	entries, next, err := s.scanPageOrAll(ctx, prefix, prefixUpperBound(prefix), true, page, nil)
	if err != nil {
		return nil, "", err
	}
	out := make([]*port.Reorg, len(entries))
	for i, e := range entries {
		var r port.Reorg
		if err := rlp.DecodeBytes(e.Value, &r); err != nil {
			return nil, "", fmt.Errorf("decode reorg record %s: %w", e.Key[len(prefix):], err)
		}
		out[i] = &r
	}
	return out, next, nil
}

// GetReorg implements OrphanReader.
func (s *PebbleStorage) GetReorg(ctx context.Context, seq uint64) (*port.Reorg, error) {
	v, err := s.get(ctx, OrphanReorgKey(seq), "reorg record")
	if err != nil {
		return nil, err
	}
	var r port.Reorg
	if err := rlp.DecodeBytes(v, &r); err != nil {
		return nil, fmt.Errorf("decode reorg record %d: %w", seq, err)
	}
	return &r, nil
}

// GetOrphanedBlock implements OrphanReader.
func (s *PebbleStorage) GetOrphanedBlock(ctx context.Context, hash common.Hash) (*port.OrphanedBlock, error) {
	v, err := s.get(ctx, OrphanBlockKey(hash), "orphaned block")
	if err != nil {
		return nil, err
	}
	var rec orphanBlockRecord
	if err := rlp.DecodeBytes(v, &rec); err != nil {
		return nil, fmt.Errorf("decode orphaned block %s: %w", hash.Hex(), err)
	}
	b, err := model.DecodeBlock(rec.Block)
	if err != nil {
		return nil, fmt.Errorf("decode orphaned block %s: %w", hash.Hex(), err)
	}
	ob := &port.OrphanedBlock{Block: b, ReorgSeq: rec.ReorgSeq}
	for _, enc := range rec.Receipts {
		r, err := model.DecodeReceipt(enc)
		if err != nil {
			return nil, fmt.Errorf("decode orphaned receipt in %s: %w", hash.Hex(), err)
		}
		ob.Receipts = append(ob.Receipts, r)
	}
	return ob, nil
}

// GetOrphanedBlocksAt implements OrphanReader.
func (s *PebbleStorage) GetOrphanedBlocksAt(ctx context.Context, height uint64) ([]*port.OrphanedBlock, error) {
	return s.orphansUnder(ctx, orphanHeightPrefix(height))
}

// GetOrphanedTransaction implements OrphanReader.
func (s *PebbleStorage) GetOrphanedTransaction(ctx context.Context, txHash common.Hash) ([]*port.OrphanedBlock, error) {
	return s.orphansUnder(ctx, orphanTxPrefix(txHash))
}

// orphansUnder loads the orphaned blocks named by an index prefix whose keys
// end in a block hash.
func (s *PebbleStorage) orphansUnder(ctx context.Context, prefix []byte) ([]*port.OrphanedBlock, error) {
	iter, err := s.kv(ctx).NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: prefixUpperBound(prefix)})
	if err != nil {
		return nil, err
	}
	var hashes []common.Hash
	for iter.First(); iter.Valid(); iter.Next() {
		hashes = append(hashes, common.HexToHash(string(iter.Key()[len(prefix):])))
	}
	if err := errors.Join(iter.Error(), iter.Close()); err != nil {
		return nil, err
	}
	out := make([]*port.OrphanedBlock, 0, len(hashes))
	for _, h := range hashes {
		ob, err := s.GetOrphanedBlock(ctx, h)
		if err != nil {
			return nil, err
		}
		out = append(out, ob)
	}
	return out, nil
}

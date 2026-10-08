package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/jackc/pgx/v5"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var (
	_ port.OrphanReader = (*Store)(nil)
	_ port.Rollbacker   = (*Store)(nil)
)

// metaReorgSeq is the meta row of the last reorganization's sequence; it is
// written by rollback transactions, so no undo restores it.
const metaReorgSeq = "reorg_seq"

// SetOrphanRetention sets how many reorganization records are kept with
// their orphaned blocks; 0 keeps all. Older ones are deleted when a
// rollback records a new one.
func (s *Store) SetOrphanRetention(n uint64) { s.orphanRetention.Store(n) }

// RollbackTo implements port.Rollbacker: the blocks above to are rolled
// back newest first, one transaction each, which archives the block as an
// orphan, restores the rows it changed (undo_block) and runs onUndo; the
// first records the reorganization. If a block in the range has no undo,
// nothing changes.
func (s *Store) RollbackTo(ctx context.Context, to uint64, onUndo port.UndoHook) (*port.Reorg, error) {
	if err := s.write(); err != nil {
		return nil, err
	}
	latest, err := s.GetLatestHeight(ctx)
	if err != nil {
		return nil, err
	}
	if latest <= to {
		return nil, nil
	}
	var missing int64
	err = s.q(ctx).QueryRow(ctx, `SELECT h FROM generate_series($1::bigint, $2::bigint, -1) AS h
		WHERE NOT EXISTS (SELECT 1 FROM undo_blocks WHERE height = h) LIMIT 1`, i64(latest), i64(to+1)).Scan(&missing)
	if err == nil {
		return nil, fmt.Errorf("%w %d", port.ErrNoUndo, missing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
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
	seq, err := s.metaUint(ctx, metaReorgSeq)
	if err != nil && !errors.Is(err, port.ErrNotFound) {
		return nil, err
	}
	rec.Seq = seq + 1

	for h := latest; h > to; h-- {
		ob, err := s.undoBlock(ctx, h, rec, h == latest, onUndo)
		if err != nil {
			return nil, err
		}
		rec.Blocks = append(rec.Blocks, ob)
	}
	return rec, nil
}

// undoBlock rolls block h back in one transaction.
func (s *Store) undoBlock(ctx context.Context, h uint64, rec *port.Reorg, first bool, onUndo port.UndoHook) (*port.OrphanedBlock, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	// Bound like a block transaction without a height: the hook's writes
	// belong to it, and nothing is recorded for undo.
	bt := &blockTx{owner: s, tx: tx}
	defer bt.Rollback()
	txCtx := context.WithValue(ctx, blockTxKey{}, bt)

	ok, err := s.exists(txCtx, "SELECT EXISTS (SELECT 1 FROM undo_blocks WHERE height = $1)", i64(h))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w %d", port.ErrNoUndo, h)
	}
	ob, err := s.archiveOrphan(txCtx, tx, h, rec, first)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SELECT undo_block($1)", i64(h)); err != nil {
		return nil, fmt.Errorf("undo block %d: %w", h, err)
	}
	if onUndo != nil {
		if err := onUndo(txCtx, rec, ob, first); err != nil {
			return nil, err
		}
	}
	if err := bt.Commit(); err != nil {
		return nil, err
	}
	return ob, nil
}

// archiveOrphan writes block h, about to be rolled back in tx, as an orphan
// of rec, and rec itself when first.
func (s *Store) archiveOrphan(txCtx context.Context, tx pgx.Tx, h uint64, rec *port.Reorg, first bool) (*port.OrphanedBlock, error) {
	b, err := s.GetBlock(txCtx, h)
	if err != nil {
		return nil, fmt.Errorf("read block %d to archive: %w", h, err)
	}
	ob := &port.OrphanedBlock{Block: b, ReorgSeq: rec.Seq}
	block, err := model.EncodeBlock(b)
	if err != nil {
		return nil, fmt.Errorf("encode orphaned block %d: %w", h, err)
	}
	receipts := [][]byte{}
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
		receipts = append(receipts, enc)
	}

	batch := &pgx.Batch{}
	// A block orphaned again (by a later reorganization) is replaced.
	batch.Queue(`INSERT INTO orphaned_blocks (hash, number, reorg_seq, block, receipts) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (hash) DO UPDATE SET number = EXCLUDED.number, reorg_seq = EXCLUDED.reorg_seq,
			block = EXCLUDED.block, receipts = EXCLUDED.receipts`,
		b.Hash.Bytes(), i64(h), i64(rec.Seq), block, receipts)
	for _, t := range b.Transactions {
		batch.Queue("INSERT INTO orphaned_transactions (tx_hash, block_hash) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			t.Hash.Bytes(), b.Hash.Bytes())
	}
	if first {
		data, err := rlp.EncodeToBytes(rec)
		if err != nil {
			return nil, err
		}
		batch.Queue("INSERT INTO reorgs (seq, data) VALUES ($1, $2)", i64(rec.Seq), data)
		if keep := s.orphanRetention.Load(); keep > 0 && rec.Seq > keep {
			// The records up to the cutoff and the blocks they archived; a
			// block archived again by a later reorganization names it and
			// stays.
			cutoff := i64(rec.Seq - keep)
			batch.Queue(`DELETE FROM orphaned_transactions WHERE block_hash IN
				(SELECT hash FROM orphaned_blocks WHERE reorg_seq <= $1)`, cutoff)
			batch.Queue("DELETE FROM orphaned_blocks WHERE reorg_seq <= $1", cutoff)
			batch.Queue("DELETE FROM reorgs WHERE seq <= $1", cutoff)
		}
	}
	if err := sendBatch(txCtx, tx, batch); err != nil {
		return nil, fmt.Errorf("archive block %d: %w", h, err)
	}
	if first {
		if err := s.setMetaUint(txCtx, metaReorgSeq, rec.Seq); err != nil {
			return nil, err
		}
	}
	return ob, nil
}

// GetReorgs implements port.OrphanReader: newest first; Limit <= 0 lists
// every record.
func (s *Store) GetReorgs(ctx context.Context, page port.Page) ([]*port.Reorg, string, error) {
	return listQuery[*port.Reorg]{
		list:       "reorgs",
		sql:        "SELECT data FROM reorgs WHERE true",
		keys:       []keyCol{{"seq", kindInt, true}},
		defaultAll: true,
		scan:       scanReorg,
		keyOf:      func(r *port.Reorg) []string { return []string{u64s(r.Seq)} },
	}.run(ctx, s.q(ctx), page)
}

func scanReorg(row pgx.CollectableRow) (*port.Reorg, error) {
	var data []byte
	if err := row.Scan(&data); err != nil {
		return nil, err
	}
	r := new(port.Reorg)
	if err := rlp.DecodeBytes(data, r); err != nil {
		return nil, fmt.Errorf("decode reorg record: %w", err)
	}
	return r, nil
}

// GetReorg implements port.OrphanReader.
func (s *Store) GetReorg(ctx context.Context, seq uint64) (*port.Reorg, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT data FROM reorgs WHERE seq = $1", i64(seq))
	if err != nil {
		return nil, err
	}
	r, err := pgx.CollectExactlyOneRow(rows, scanReorg)
	return r, notFound(err)
}

const orphanColumns = "block, receipts, reorg_seq"

func scanOrphan(row pgx.CollectableRow) (*port.OrphanedBlock, error) {
	var (
		block    []byte
		receipts [][]byte
		seq      int64
	)
	if err := row.Scan(&block, &receipts, &seq); err != nil {
		return nil, err
	}
	b, err := model.DecodeBlock(block)
	if err != nil {
		return nil, fmt.Errorf("decode orphaned block: %w", err)
	}
	ob := &port.OrphanedBlock{Block: b, ReorgSeq: uint64(seq)}
	for _, enc := range receipts {
		r, err := model.DecodeReceipt(enc)
		if err != nil {
			return nil, fmt.Errorf("decode orphaned receipt in %s: %w", b.Hash.Hex(), err)
		}
		ob.Receipts = append(ob.Receipts, r)
	}
	return ob, nil
}

// GetOrphanedBlock implements port.OrphanReader.
func (s *Store) GetOrphanedBlock(ctx context.Context, hash common.Hash) (*port.OrphanedBlock, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT "+orphanColumns+" FROM orphaned_blocks WHERE hash = $1", hash.Bytes())
	if err != nil {
		return nil, err
	}
	ob, err := pgx.CollectExactlyOneRow(rows, scanOrphan)
	return ob, notFound(err)
}

// GetOrphanedBlocksAt implements port.OrphanReader: by block hash.
func (s *Store) GetOrphanedBlocksAt(ctx context.Context, height uint64) ([]*port.OrphanedBlock, error) {
	rows, err := s.q(ctx).Query(ctx, "SELECT "+orphanColumns+" FROM orphaned_blocks WHERE number = $1 ORDER BY hash", i64(height))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanOrphan)
}

// GetOrphanedTransaction implements port.OrphanReader: by block hash.
func (s *Store) GetOrphanedTransaction(ctx context.Context, txHash common.Hash) ([]*port.OrphanedBlock, error) {
	rows, err := s.q(ctx).Query(ctx, `SELECT `+orphanColumns+` FROM orphaned_blocks
		WHERE hash IN (SELECT block_hash FROM orphaned_transactions WHERE tx_hash = $1) ORDER BY hash`, txHash.Bytes())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanOrphan)
}

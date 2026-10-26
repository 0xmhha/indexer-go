package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
)

const executionRewindSQL = `SELECT request_generation,from_generation,from_height,from_hash,from_root,generation,target_height,target_hash,target_root FROM exec_rewinds WHERE id`

var _ port.ExecutionRewindStore = (*Store)(nil)

func scanExecutionRewind(row pgx.Row) (port.ExecutionRewind, error) {
	var r port.ExecutionRewind
	var request, fromGen, fromH, gen, targetH int64
	var fromHash, fromRoot, targetHash, targetRoot []byte
	err := row.Scan(&request, &fromGen, &fromH, &fromHash, &fromRoot, &gen, &targetH, &targetHash, &targetRoot)
	if err != nil {
		return r, notFound(err)
	}
	r.RequestGeneration = uint64(request)
	r.From = execwire.Ref{Generation: uint64(fromGen), Height: uint64(fromH)}
	r.Target = execwire.Ref{Generation: uint64(gen), Height: uint64(targetH)}
	copy(r.From.Hash[:], fromHash)
	copy(r.FromRoot[:], fromRoot)
	copy(r.Target.Hash[:], targetHash)
	copy(r.Root[:], targetRoot)
	return r, nil
}
func (s *Store) ExecutionRewindStatus(ctx context.Context) (port.ExecutionRewind, error) {
	return scanExecutionRewind(s.q(ctx).QueryRow(ctx, executionRewindSQL))
}
func (s *Store) rewindWriteAllowed(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.write(); err != nil {
		return err
	}
	if s.executionReadBound(ctx) {
		return port.ErrReadOnly
	}
	if s.boundTx(ctx) != nil || s.pool.Config().MaxConns != 4 {
		return port.ErrInvalidData
	}
	return nil
}
func rawExecutionBlock(ctx context.Context, q querier, height uint64) (*model.Block, error) {
	var data []byte
	if err := q.QueryRow(ctx, "SELECT data FROM blocks WHERE number=$1", int64(height)).Scan(&data); err != nil {
		return nil, notFound(err)
	}
	return model.DecodeBlock(data)
}
func validateExecutionRewind(ctx context.Context, tx pgx.Tx, c port.ExecutionControl, from, target execwire.Ref, root execwire.Hash) error {
	if err := consensusRewindAllowed(ctx, tx, target); err != nil {
		return err
	}
	if target.Height < c.Anchor.Height || target.Height > from.Height || from.Height-target.Height > UndoWindow {
		return port.ErrNoUndo
	}
	b, err := rawExecutionBlock(ctx, tx, target.Height)
	if err != nil {
		return err
	}
	if execwire.Hash(b.Hash) != target.Hash || execwire.Hash(b.StateRoot) != root {
		return port.ErrExecutionConflict
	}
	fromBlock, err := rawExecutionBlock(ctx, tx, from.Height)
	if err != nil {
		return err
	}
	if execwire.Hash(fromBlock.Hash) != from.Hash || execwire.Hash(fromBlock.StateRoot) != c.Root {
		return port.ErrExecutionConflict
	}
	expected := from.Hash
	for h := from.Height; h > target.Height; h-- {
		b, err := rawExecutionBlock(ctx, tx, h)
		if err != nil {
			return err
		}
		if execwire.Hash(b.Hash) != expected {
			return port.ErrExecutionConflict
		}
		expected = execwire.Hash(b.ParentHash)
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM undo_blocks WHERE height=$1)", int64(h)).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w %d", port.ErrNoUndo, h)
		}
	}
	if expected != target.Hash {
		return port.ErrExecutionConflict
	}
	return nil
}
func (s *Store) BeginExecutionRewind(ctx context.Context, target execwire.Ref, root execwire.Hash) (port.ExecutionRewind, error) {
	var marker port.ExecutionRewind
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return marker, err
	}
	if target.Generation == 0 || target.Generation >= math.MaxInt64 || target.Height > math.MaxInt64 || target.Hash == (execwire.Hash{}) || root == (execwire.Hash{}) {
		return marker, port.ErrInvalidData
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		c, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
		if err != nil {
			return err
		}
		if !c.BaselineReady {
			return port.ErrExecutionCoverage
		}
		from, fromRoot := c.Visible, c.Root
		if c.Rewinding {
			old, err := scanExecutionRewind(tx.QueryRow(ctx, executionRewindSQL))
			if err != nil {
				return err
			}
			if target.Height == old.Target.Height && target.Hash == old.Target.Hash && root == old.Root && target.Generation == old.RequestGeneration {
				marker = old
				return nil
			}
			// A second canonical change may move the target farther back, but may
			// never move it forward or reuse an old generation's request.
			if target.Generation != c.Generation {
				return port.ErrExecutionStale
			}
			if target.Height >= old.Target.Height {
				return port.ErrExecutionConflict
			}
			from, fromRoot = old.From, old.FromRoot
		} else if target.Generation != c.Generation {
			return port.ErrExecutionStale
		}
		if c.Generation >= math.MaxInt64 {
			return port.ErrInvalidData
		}
		if err := validateExecutionRewind(ctx, tx, c, from, target, root); err != nil {
			return err
		}
		if err := executionDurable(ctx, tx); err != nil {
			return err
		}
		marker = port.ExecutionRewind{RequestGeneration: target.Generation, From: from, FromRoot: fromRoot, Target: target, Root: root}
		marker.Target.Generation = c.Generation + 1
		_, err = tx.Exec(ctx, `INSERT INTO exec_rewinds(id,request_generation,from_generation,from_height,from_hash,from_root,generation,target_height,target_hash,target_root)
 VALUES(true,$1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET request_generation=EXCLUDED.request_generation,generation=EXCLUDED.generation,target_height=EXCLUDED.target_height,target_hash=EXCLUDED.target_hash,target_root=EXCLUDED.target_root`, int64(marker.RequestGeneration), int64(from.Generation), int64(from.Height), from.Hash[:], fromRoot[:], int64(marker.Target.Generation), int64(target.Height), target.Hash[:], root[:])
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE exec_control SET generation=$1,rewinding=true WHERE id", int64(marker.Target.Generation))
		return err
	})
	return marker, err
}
func (s *Store) ResumeExecutionRewind(ctx context.Context, onUndo port.UndoHook, fresh func(context.Context, execwire.Ref) error) (*port.Reorg, error) {
	return s.resumeExecutionRewind(ctx, onUndo, fresh, nil)
}
func (s *Store) resumeExecutionRewind(ctx context.Context, onUndo port.UndoHook, fresh func(context.Context, execwire.Ref) error, phase func(string) error) (*port.Reorg, error) {
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return nil, err
	}
	if fresh == nil {
		return nil, port.ErrInvalidData
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	bt := &blockTx{owner: s, tx: tx}
	defer bt.Rollback()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
		return nil, err
	}
	c, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
	if err != nil {
		return nil, err
	}
	marker, err := scanExecutionRewind(tx.QueryRow(ctx, executionRewindSQL))
	if err != nil {
		return nil, err
	}
	if !c.Rewinding || c.Generation != marker.Target.Generation || c.Visible.Height != marker.From.Height || c.Visible.Hash != marker.From.Hash || c.Root != marker.FromRoot {
		return nil, port.ErrExecutionConflict
	}
	if err := validateExecutionRewind(ctx, tx, c, marker.From, marker.Target, marker.Root); err != nil {
		return nil, err
	}
	if err := executionDurable(ctx, tx); err != nil {
		return nil, err
	}
	bound := context.WithValue(ctx, blockTxKey{}, bt)
	check := func(stage string) error {
		if phase != nil {
			return phase(stage)
		}
		return nil
	}
	if err := archiveExecutionCalls(ctx, tx, marker); err != nil {
		return nil, err
	}
	if err := check("call_archive"); err != nil {
		return nil, err
	}
	var rec *port.Reorg
	if marker.From.Height > marker.Target.Height {
		seq, err := s.metaUint(bound, metaReorgSeq)
		if err != nil && !errors.Is(err, port.ErrNotFound) {
			return nil, err
		}
		rec = &port.Reorg{Seq: seq + 1, OldHead: marker.From.Height, ForkNumber: marker.Target.Height, ForkHash: common.Hash(marker.Target.Hash), DetectedAt: uint64(time.Now().Unix())}
		for h := marker.From.Height; h > marker.Target.Height; h-- {
			b, err := rawExecutionBlock(bound, tx, h)
			if err != nil {
				return nil, err
			}
			rec.Removed = append(rec.Removed, port.BlockRef{Number: h, Hash: b.Hash})
		}
		for h := marker.From.Height; h > marker.Target.Height; h-- {
			ob, err := s.archiveOrphan(bound, tx, h, rec, h == marker.From.Height)
			if err != nil {
				return nil, err
			}
			if onUndo != nil {
				if err := onUndo(bound, rec, ob, h == marker.From.Height); err != nil {
					return nil, err
				}
			}
			if err := bt.flush(ctx); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, "SELECT undo_block($1)", int64(h)); err != nil {
				return nil, err
			}
			rec.Blocks = append(rec.Blocks, ob)
		}
	}
	if err := check("feature_undo"); err != nil {
		return nil, err
	}
	if err := s.rewindExecutionState(bound, marker.Target.Height); err != nil {
		return nil, err
	}
	if err := check("state_restore"); err != nil {
		return nil, err
	}
	for _, table := range []string{"logs", "receipts", "transactions", "blocks"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE "+map[string]string{"blocks": "number", "transactions": "block_number", "receipts": "block_number", "logs": "block_number"}[table]+">$1", int64(marker.Target.Height)); err != nil {
			return nil, err
		}
	}
	if err := check("core_purge"); err != nil {
		return nil, err
	}
	b, err := rawExecutionBlock(ctx, tx, marker.Target.Height)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "UPDATE exec_cursors SET generation=$1,height=$2,hash=$3,parent=$4,digest=$5", int64(marker.Target.Generation), int64(marker.Target.Height), marker.Target.Hash[:], b.ParentHash.Bytes(), make([]byte, 32)); err != nil {
		return nil, err
	}
	for _, table := range []string{"exec_state_proofs", "exec_blob_blocks", "exec_call_blocks", "exec_parts", "exec_publications"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{table}.Sanitize()+" WHERE height>$1", int64(marker.Target.Height)); err != nil {
			return nil, err
		}
	}
	if err := s.SetLatestHeight(bound, marker.Target.Height); err != nil {
		return nil, err
	}
	if _, err := bt.q().Exec(ctx, "UPDATE meta SET value=int8send((SELECT count(*) FROM transactions)) WHERE name=$1", metaTransactionCount); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "UPDATE exec_control SET visible_height=$1,visible_hash=$2,visible_root=$3,rewinding=false WHERE id", int64(marker.Target.Height), marker.Target.Hash[:], marker.Root[:]); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM exec_rewinds WHERE id"); err != nil {
		return nil, err
	}
	if err := bt.flush(ctx); err != nil {
		return nil, err
	}
	if err := check("cursor_reset"); err != nil {
		return nil, err
	}
	// Fence every cursor, not just the maximum height, before reopening reads.
	var count int
	var valid bool
	if err := tx.QueryRow(ctx, "SELECT count(*),COALESCE(bool_and(generation=$1 AND height=$2 AND hash=$3 AND digest=$4),false) FROM exec_cursors", int64(marker.Target.Generation), int64(marker.Target.Height), marker.Target.Hash[:], make([]byte, 32)).Scan(&count, &valid); err != nil {
		return nil, err
	}
	if count != 7 || !valid {
		return nil, port.ErrExecutionConflict
	}
	if err := fresh(bound, marker.Target); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	after, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL))
	if err != nil {
		return nil, err
	}
	if after.Rewinding || after.Visible != marker.Target || !bytes.Equal(after.Root[:], marker.Root[:]) {
		return nil, port.ErrExecutionStale
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	bt.done = true
	s.resetGenesisTried()
	return rec, nil
}

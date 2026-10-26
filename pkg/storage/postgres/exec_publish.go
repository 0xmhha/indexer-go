package postgres

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

var _ port.ExecutionPublisher = (*Store)(nil)

func (s *Store) CommitExecution(ctx context.Context, p port.ExecutionPublication, apply, fresh func(context.Context) error) (bool, error) {
	if s.executionReadBound(ctx) {
		return false, port.ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := s.write(); err != nil {
		return false, err
	}
	if apply == nil || fresh == nil || s.boundTx(ctx) != nil || s.pool.Config().MaxConns != 4 || p.Root == (execwire.Hash{}) || p.Parent == (execwire.Hash{}) {
		return false, port.ErrInvalidData
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	bt := &blockTx{owner: s, tx: tx}
	defer bt.Rollback()
	var locked bool
	if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock_shared("+executionLockSQL+")").Scan(&locked); err != nil {
		return false, err
	}
	if !locked {
		return false, port.ErrExecutionRewinding
	}
	c, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
	if err != nil {
		return false, err
	}
	if c.Rewinding {
		return false, port.ErrExecutionRewinding
	}
	if c.Generation != p.Ref.Generation {
		return false, port.ErrExecutionStale
	}
	if !c.BaselineReady {
		return false, port.ErrExecutionCoverage
	}
	if err := execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: c.Chain, Ref: p.Ref, Digest: p.Digest}}); err != nil {
		return false, err
	}
	if err := executionDurable(ctx, tx); err != nil {
		return false, err
	}
	bound := context.WithValue(ctx, blockTxKey{}, bt)
	duplicate := p.Ref.Height <= c.Visible.Height
	if err := validateCallReceipt(ctx, tx, p, true); err != nil {
		return false, err
	}
	if _, e := readBlobReceipt(ctx, tx, p, true); e != nil {
		return false, e
	}
	if duplicate {
		var hash, parent, digest, root []byte
		var calls, blobs bool
		err := tx.QueryRow(ctx, "SELECT hash,parent,digest,root,call_traces,blob_sidecars FROM exec_publications WHERE generation=$1 AND height=$2", int64(p.Ref.Generation), int64(p.Ref.Height)).Scan(&hash, &parent, &digest, &root, &calls, &blobs)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, port.ErrExecutionConflict
		}
		if err != nil {
			return false, err
		}
		if blobs != p.Blobs || calls != p.CallTraces || !bytes.Equal(p.Ref.Hash[:], hash) || !bytes.Equal(p.Parent[:], parent) || !bytes.Equal(p.Digest[:], digest) || !bytes.Equal(p.Root[:], root) {
			return false, port.ErrExecutionConflict
		}
	} else {
		if p.Ref.Height != c.Visible.Height+1 || p.Parent != c.Visible.Hash {
			return false, port.ErrExecutionSequence
		}
		parts, err := s.ExecutionParts(bound, p.Ref.Height)
		if err != nil {
			return false, err
		}
		if len(parts) != 7 {
			return false, execwire.ErrIncomplete
		}
		for _, part := range parts {
			if part.Ref != p.Ref || part.Parent != p.Parent || part.Digest != p.Digest {
				return false, port.ErrExecutionConflict
			}
		}
		var data []byte
		if err := tx.QueryRow(ctx, "SELECT data FROM blocks WHERE number=$1", int64(p.Ref.Height)).Scan(&data); err != nil {
			return false, notFound(err)
		}
		b, err := model.DecodeBlock(data)
		if err != nil {
			return false, err
		}
		if execwire.Hash(b.Hash) != p.Ref.Hash || execwire.Hash(b.ParentHash) != p.Parent || execwire.Hash(b.StateRoot) != p.Root {
			return false, port.ErrExecutionConflict
		}
		if _, err := tx.Exec(ctx, "SET LOCAL indexer.undo = 'on'"); err != nil {
			return false, err
		}
		bt.SetHeight(p.Ref.Height)
		// Candidate visibility is local to this uncommitted transaction. Features
		// read their own block while external API snapshots retain the old fence.
		if _, err := bt.q().Exec(ctx, "UPDATE exec_control SET visible_height=$1,visible_hash=$2,visible_root=$3 WHERE id", int64(p.Ref.Height), p.Ref.Hash[:], p.Root[:]); err != nil {
			return false, err
		}
		if err := apply(bound); err != nil {
			return false, err
		}
		if err := s.SetLatestHeight(bound, p.Ref.Height); err != nil {
			return false, err
		}
		if _, err := bt.q().Exec(ctx, "INSERT INTO exec_publications(generation,height,hash,parent,digest,root,call_traces,blob_sidecars) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", int64(p.Ref.Generation), int64(p.Ref.Height), p.Ref.Hash[:], p.Parent[:], p.Digest[:], p.Root[:], p.CallTraces, p.Blobs); err != nil {
			return false, err
		}
		if err := bt.flush(ctx); err != nil {
			return false, err
		}
		if err := bt.recordUndo(ctx); err != nil {
			return false, err
		}
	}
	// No queued writes, undo preparation or application DML occurs after fresh.
	if err := fresh(bound); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	after, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL))
	if err != nil {
		return false, err
	}
	if after.Rewinding || after.Generation != p.Ref.Generation {
		return false, port.ErrExecutionStale
	}
	if !duplicate && (after.Visible != p.Ref || after.Root != p.Root) {
		return false, port.ErrExecutionConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	bt.done = true
	s.publishGenesisTried(bt.genesisSeen)
	return !duplicate, nil
}

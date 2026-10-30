package postgres

import (
	"context"
	"errors"
	"io"
	"strconv"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

func (s *Store) WriteExecutionPart(ctx context.Context, p port.ExecutionPart, apply func(context.Context) error) error {
	if s.executionReadBound(ctx) {
		return port.ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.write(); err != nil {
		return err
	}
	if apply == nil || !validExecutionWriter(p.Writer) || p.Parent == (execwire.Hash{}) || s.boundTx(ctx) != nil || s.pool.Config().MaxConns != 4 {
		return port.ErrInvalidData
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var locked bool
		if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock_shared("+executionLockSQL+")").Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return port.ErrExecutionRewinding
		}
		c, err := scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL))
		if err != nil {
			return err
		}
		if c.Rewinding {
			return port.ErrExecutionRewinding
		}
		if p.Ref.Generation != c.Generation {
			return port.ErrExecutionStale
		}
		if err := execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: c.Chain, Ref: p.Ref, Digest: p.Digest}}); err != nil {
			return err
		}
		cursor, err := scanExecutionPart(tx.QueryRow(ctx, "SELECT "+executionPartColumns+" FROM exec_cursors WHERE writer=$1 FOR UPDATE", string(p.Writer)))
		if err != nil {
			return err
		}
		if cursor.Ref.Generation != c.Generation {
			return port.ErrExecutionStale
		}
		old, err := scanExecutionPart(tx.QueryRow(ctx, "SELECT "+executionPartColumns+" FROM exec_parts WHERE writer=$1 AND generation=$2 AND height=$3", string(p.Writer), int64(p.Ref.Generation), int64(p.Ref.Height)))
		if err == nil {
			if old != p || cursor.Ref.Height < p.Ref.Height || cursor.Ref.Height == p.Ref.Height && cursor != old {
				return port.ErrExecutionConflict
			}
			if c.IngressOwned {
				return requireIngressPart(ctx, tx, p)
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if p.Ref.Height != cursor.Ref.Height+1 || p.Parent != cursor.Ref.Hash {
			return port.ErrExecutionSequence
		}
		if err := executionDurable(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT set_config('indexer.height',$1,true)", strconv.FormatUint(p.Ref.Height, 10)); err != nil {
			return err
		}
		bt := &blockTx{owner: s, tx: tx}
		bound := context.WithValue(ctx, blockTxKey{}, bt)
		scope := &executionPartScope{part: p, chain: c.Chain}
		scope.active.Store(true)
		defer scope.active.Store(false)
		bound = context.WithValue(bound, executionPartKey{}, scope)
		if err := apply(bound); err != nil {
			return err
		}
		if c.IngressOwned {
			if err := requireIngressPart(ctx, tx, p); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := bt.flush(ctx); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO exec_parts("+executionPartColumns+") VALUES($1,$2,$3,$4,$5,$6)", string(p.Writer), int64(p.Ref.Generation), int64(p.Ref.Height), p.Ref.Hash[:], p.Parent[:], p.Digest[:])
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE exec_cursors SET generation=$2,height=$3,hash=$4,parent=$5,digest=$6 WHERE writer=$1", string(p.Writer), int64(p.Ref.Generation), int64(p.Ref.Height), p.Ref.Hash[:], p.Parent[:], p.Digest[:])
		return err
	})
}

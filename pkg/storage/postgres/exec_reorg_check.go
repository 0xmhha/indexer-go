package postgres

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

const executionReorgCheckSQL = "SELECT generation,height,hash,root FROM exec_reorg_checks WHERE id"

var _ port.ExecutionReorgCheckStore = (*Store)(nil)

func scanExecutionReorgCheck(row pgx.Row) (port.ExecutionReorgCheck, error) {
	var c port.ExecutionReorgCheck
	var generation, height int64
	var hash, root []byte
	if err := row.Scan(&generation, &height, &hash, &root); err != nil {
		return c, notFound(err)
	}
	c.Generation = uint64(generation)
	c.From.Generation = c.Generation
	c.From.Height = uint64(height)
	copy(c.From.Hash[:], hash)
	copy(c.Root[:], root)
	return c, nil
}
func (s *Store) BeginExecutionReorgCheck(ctx context.Context) (port.ExecutionReorgCheck, error) {
	var result port.ExecutionReorgCheck
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return result, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		c, err := scanExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
		if err != nil {
			return err
		}
		if c.Maintenance {
			return port.ErrExecutionMaintenance
		}
		if c.Rewinding {
			return port.ErrExecutionRewinding
		}
		if !c.BaselineReady {
			return port.ErrExecutionCoverage
		}
		result = port.ExecutionReorgCheck{Generation: c.Generation, From: c.Visible, Root: c.Root}
		old, err := scanExecutionReorgCheck(tx.QueryRow(ctx, executionReorgCheckSQL))
		if err == nil {
			if old != result {
				return port.ErrExecutionConflict
			}
			return nil
		}
		if !errors.Is(err, port.ErrNotFound) {
			return err
		}
		if c.ReorgPending {
			return port.ErrExecutionConflict
		}
		if err := executionDurable(ctx, tx); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO exec_reorg_checks(id,generation,height,hash,root) VALUES(true,$1,$2,$3,$4)", int64(c.Generation), int64(c.Visible.Height), c.Visible.Hash[:], c.Root[:])
		return err
	})
	return result, err
}
func (s *Store) ExecutionReorgCheckStatus(ctx context.Context) (port.ExecutionReorgCheck, error) {
	return scanExecutionReorgCheck(s.q(ctx).QueryRow(ctx, executionReorgCheckSQL))
}

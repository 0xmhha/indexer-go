package postgres

import (
	"bytes"
	"context"

	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

// WithExecutionRetentionRelease holds the existing E3 EX lock after completion
// while the owner releases exact filesystem receipts. The synchronous callback
// issues no RPC and joins owned children before returning. It creates no pool.
func (s *Store) WithExecutionRetentionRelease(ctx context.Context, p retention.Permit, r retention.Receipt, apply func(context.Context) error) error {
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return err
	}
	if p.Validate() != nil || r.Match(p) != nil || apply == nil || s.boundTx(ctx) != nil {
		return port.ErrInvalidData
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		current, pending, receipt, err := readRetention(ctx, tx)
		if err != nil {
			return err
		}
		if pending || current != p || receipt == nil || *receipt != r {
			return port.ErrExecutionMaintenance
		}
		c, err := scanExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
		if err != nil {
			return err
		}
		if c.Maintenance || c.Rewinding || c.ReorgPending || !c.BaselineReady || c.Visible != p.Visible || c.Root != p.Root || c.Generation != p.Visible.Generation || c.Chain != p.Request.Chain || c.Anchor != p.Request.Anchor {
			return port.ErrExecutionConflict
		}
		var coverage string
		var baseline []byte
		if err = tx.QueryRow(ctx, "SELECT coverage,bootstrap_digest FROM exec_control WHERE id").Scan(&coverage, &baseline); err != nil {
			return err
		}
		if coverage != p.Coverage || !bytes.Equal(baseline, p.BaselineDigest[:]) {
			return port.ErrExecutionCoverage
		}
		var rawPending bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM exec_raw_deletions WHERE NOT completed)").Scan(&rawPending); err != nil {
			return err
		}
		if rawPending {
			return port.ErrExecutionMaintenance
		}
		bound := context.WithValue(ctx, blockTxKey{}, &blockTx{owner: s, tx: tx})
		if err = apply(bound); err != nil {
			return err
		}
		return ctx.Err()
	})
}

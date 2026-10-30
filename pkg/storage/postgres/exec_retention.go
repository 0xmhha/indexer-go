package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

// ExecutionRetentionStatus is diagnostic; pending authority remains in the DB,
// not in the separately observed source/request file. The last receipt is bounded.
func (s *Store) ExecutionRetentionStatus(ctx context.Context) (p retention.Permit, pending bool, receipt *retention.Receipt, err error) {
	return readRetention(ctx, s.q(ctx))
}
func readRetention(ctx context.Context, q querier) (p retention.Permit, pending bool, receipt *retention.Receipt, err error) {
	var token, data, completed []byte
	err = q.QueryRow(ctx, "SELECT token,permit,pending,receipt FROM exec_retention WHERE id").Scan(&token, &data, &pending, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		err = port.ErrNotFound
		return
	}
	if err != nil {
		return
	}
	if json.Unmarshal(data, &p) != nil || p.Validate() != nil || !bytes.Equal(token, p.Token[:]) {
		err = retention.ErrUnsafe
		return
	}
	canonical, e := json.Marshal(p)
	if e != nil || !bytes.Equal(canonical, data) {
		err = retention.ErrUnsafe
		return
	}
	if completed != nil {
		receipt = new(retention.Receipt)
		if json.Unmarshal(completed, receipt) != nil || receipt.Match(p) != nil {
			err = retention.ErrUnsafe
			return
		}
	}
	if pending && receipt != nil || !pending && receipt == nil {
		err = retention.ErrUnsafe
	}
	return
}

// BeginExecutionRetention pins durable public data before the offline owner
// may remove source files. An uncertain commit is never permission to delete.
// The exclusive admission lock drains existing read/part/publication leases;
// the committed pending record then replaces that transient connection lock.
func (s *Store) BeginExecutionRetention(ctx context.Context, r retention.Request, fresh func(context.Context) error) (p retention.Permit, err error) {
	if err = s.rewindWriteAllowed(ctx); err != nil {
		return
	}
	if fresh == nil || r.Validate() != nil {
		return p, retention.ErrUnsafe
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); e != nil {
			return e
		}
		c, e := scanExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
		if e != nil {
			return e
		}
		if c.Rewinding || c.ReorgPending {
			return port.ErrExecutionRewinding
		}
		if !c.BaselineReady {
			return port.ErrExecutionCoverage
		}
		var coverage string
		var baseline []byte
		if e = tx.QueryRow(ctx, "SELECT coverage,bootstrap_digest FROM exec_control WHERE id").Scan(&coverage, &baseline); e != nil {
			return e
		}
		if coverage != "full" || len(baseline) != 32 {
			return port.ErrExecutionCoverage
		}
		var baselineDigest execwire.Hash
		copy(baselineDigest[:], baseline)

		if c.Chain != r.Chain || c.Anchor != r.Anchor || c.Generation != r.ACK.Generation || c.Visible.Height < r.ACK.Height {
			return port.ErrExecutionConflict
		}
		if c.Maintenance {
			old, pending, _, e := readRetention(ctx, tx)
			if e != nil {
				return e
			}
			if !pending || old.Request != r || old.Visible != c.Visible || old.Root != c.Root || old.Coverage != coverage || old.BaselineDigest != baselineDigest {
				return port.ErrExecutionMaintenance
			}
			p = old
		} else {
			// Every writer is idle at exactly the public tip, not merely beyond cutoff.
			var count int
			if e = tx.QueryRow(ctx, `SELECT count(*) FROM exec_cursors WHERE generation=$1 AND height=$2 AND hash=$3`, int64(c.Generation), int64(c.Visible.Height), c.Visible.Hash[:]).Scan(&count); e != nil {
				return e
			}
			if count != 7 {
				return execwire.ErrIncomplete
			}
			var ahead bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM exec_parts WHERE height>$1)`, int64(c.Visible.Height)).Scan(&ahead); e != nil {
				return e
			}
			if ahead {
				return execwire.ErrIncomplete
			}
			var ack bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM exec_publications p JOIN blocks b ON b.number=p.height AND b.hash=p.hash AND b.parent_hash=p.parent WHERE p.height=$1 AND p.hash=$2)`, int64(r.ACK.Height), r.ACK.Hash[:]).Scan(&ack); e != nil {
				return e
			}
			if !ack {
				return port.ErrExecutionConflict
			}
			// A publication proves the final feature/undo/outbox commit; all seven
			// exact part proofs must still cover each permanent canonical block.
			var covered int64
			e = tx.QueryRow(ctx, `SELECT count(*) FROM blocks b WHERE b.number>$1 AND b.number<=$2 AND EXISTS(
    SELECT 1 FROM exec_publications p WHERE p.height=b.number AND p.hash=b.hash AND p.parent=b.parent_hash AND
     (SELECT count(*) FROM exec_parts q WHERE q.generation=p.generation AND q.height=p.height AND q.hash=p.hash AND q.parent=p.parent AND q.digest=p.digest)=7)`, int64(c.Anchor.Height), int64(r.Through)).Scan(&covered)
			if e != nil {
				return e
			}
			if uint64(covered) != r.Through-c.Anchor.Height {
				return port.ErrExecutionCoverage
			}
			p = retention.Permit{Coverage: coverage, BaselineDigest: baselineDigest, Version: retention.Version, Request: r, Visible: c.Visible, Root: c.Root}
			if _, e = rand.Read(p.Token[:]); e != nil {
				return e
			}
			data, e := json.Marshal(p)
			if e != nil {
				return e
			}
			if e = executionDurable(ctx, tx); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO exec_retention(id,token,permit,pending,receipt) VALUES(true,$1,$2,true,NULL)
    ON CONFLICT(id) DO UPDATE SET token=EXCLUDED.token,permit=EXCLUDED.permit,pending=true,receipt=NULL`, p.Token[:], data); e != nil {
				return e
			}
		}
		return s.retentionFresh(ctx, tx, p, true, fresh)
	})
	if err != nil {
		return retention.Permit{}, err
	}
	return p, nil
}

// CompleteExecutionRetention clears only the exact durable permit after BOTH
// filesystem receipts have been verified by the offline producer. It never
// infers success from missing paths or expires an interrupted deletion.
func (s *Store) CompleteExecutionRetention(ctx context.Context, p retention.Permit, r retention.Receipt, fresh func(context.Context) error) error {
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return err
	}
	if fresh == nil || r.Match(p) != nil {
		return retention.ErrUnsafe
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); e != nil {
			return e
		}
		old, pending, receipt, e := readRetention(ctx, tx)
		if e != nil {
			return e
		}
		if old != p {
			return port.ErrExecutionConflict
		}
		var rawPending bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM exec_raw_deletions WHERE NOT completed)").Scan(&rawPending); e != nil {
			return e
		}
		if rawPending {
			return port.ErrExecutionMaintenance
		}
		if !pending {
			if receipt == nil || *receipt != r {
				return port.ErrExecutionConflict
			}
			return s.retentionFresh(ctx, tx, p, false, fresh)
		}
		if e = executionDurable(ctx, tx); e != nil {
			return e
		}
		data, e := json.Marshal(r)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE exec_retention SET pending=false,receipt=$1 WHERE id AND token=$2", data, p.Token[:]); e != nil {
			return e
		}
		return s.retentionFresh(ctx, tx, p, false, fresh)
	})
}
func (s *Store) retentionFresh(ctx context.Context, tx pgx.Tx, p retention.Permit, pending bool, fresh func(context.Context) error) error {
	// All maintenance DML is flushed before this separate uncached canonical read.
	bound := context.WithValue(ctx, blockTxKey{}, &blockTx{owner: s, tx: tx})
	if err := fresh(bound); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, e := scanExecutionControl(tx.QueryRow(ctx, executionControlSQL))
	if e != nil {
		return e
	}
	var coverage string
	var baseline []byte
	if e = tx.QueryRow(ctx, "SELECT coverage,bootstrap_digest FROM exec_control WHERE id").Scan(&coverage, &baseline); e != nil {
		return e
	}
	if coverage != p.Coverage || !bytes.Equal(baseline, p.BaselineDigest[:]) {
		return port.ErrExecutionCoverage
	}

	if c.Maintenance != pending || (c.Rewinding || c.ReorgPending) || c.Chain != p.Request.Chain || c.Anchor != p.Request.Anchor || c.Generation != p.Visible.Generation || c.Visible != p.Visible || c.Root != p.Root || !c.BaselineReady {
		return port.ErrExecutionConflict
	}
	return nil
}

// WithExecutionRetention owns a synchronous live lease in addition to the
// durable pending fence. Only a current pending permit can launch a producer;
// completed/stale artifact files never authorize a second collection.
func (s *Store) WithExecutionRetention(ctx context.Context, p retention.Permit, apply func(context.Context) error) error {
	return s.withExecutionRetention(ctx, p, false, apply)
}

func (s *Store) withExecutionRetention(ctx context.Context, p retention.Permit, rawRecovery bool, apply func(context.Context) error) error {
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return err
	}
	if p.Validate() != nil || apply == nil {
		return retention.ErrUnsafe
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		current, pending, _, err := readRetention(ctx, tx)
		if err != nil {
			return err
		}
		if !pending || current != p {
			return port.ErrExecutionMaintenance
		}
		if !rawRecovery {
			var rawPending bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM exec_raw_deletions WHERE NOT completed)").Scan(&rawPending); err != nil {
				return err
			}
			if rawPending {
				return port.ErrExecutionMaintenance
			}
		}
		c, err := scanExecutionControl(tx.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
		if err != nil {
			return err
		}
		if !c.Maintenance || (c.Rewinding || c.ReorgPending) || c.Visible != p.Visible || c.Root != p.Root || c.Generation != p.Visible.Generation || c.Chain != p.Request.Chain || c.Anchor != p.Request.Anchor {
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
		return apply(context.WithValue(ctx, blockTxKey{}, &blockTx{owner: s, tx: tx}))
	})
}

// ValidateExecutionBackup compares an externally authenticated backup receipt
// against the exact live publication under the caller's E3 lease. Receipt bytes
// alone never establish authority. SQL backup installation and WAL replay remain
// external operations; this validates admission without opening another pool.
func (s *Store) ValidateExecutionBackup(ctx context.Context, p retention.Permit, r retention.IndexerBackup) error {
	if r.Validate(p) != nil {
		return retention.ErrUnsafe
	}
	current, pending, _, err := readRetention(ctx, s.q(ctx))
	if err != nil {
		return err
	}
	if !pending || current != p {
		return port.ErrExecutionMaintenance
	}
	c, err := scanExecutionControl(s.q(ctx).QueryRow(ctx, executionControlSQL))
	if err != nil {
		return err
	}
	if !c.Maintenance || (c.Rewinding || c.ReorgPending) || !c.BaselineReady || c.Visible != p.Visible || c.Root != p.Root || c.Generation != p.Visible.Generation || c.Chain != p.Request.Chain || c.Anchor != p.Request.Anchor {
		return port.ErrExecutionConflict
	}
	var identity string
	var digest []byte
	var walAvailable bool
	err = s.q(ctx).QueryRow(ctx, `SELECT current_database() || '/' || current_schema(), digest, $3::pg_lsn <= pg_current_wal_lsn()
      FROM exec_publications WHERE height=$1 AND hash=$2 AND generation=$4 AND root=$5`, int64(p.Visible.Height), p.Visible.Hash[:], r.WALBoundary, int64(p.Visible.Generation), p.Root[:]).Scan(&identity, &digest, &walAvailable)
	if err != nil {
		return err
	}
	if identity != r.DatabaseIdentity || !bytes.Equal(digest, r.PublicationDigest[:]) || !walAvailable {
		return port.ErrExecutionConflict
	}
	return nil
}

// CheckExecutionRestore retains the final NEW canonical read followed by the
// exact generation/publication fence in the same E3 transaction.
func (s *Store) CheckExecutionRestore(ctx context.Context, p retention.Permit, fresh func(context.Context) error) error {
	if fresh == nil {
		return retention.ErrUnsafe
	}
	return s.WithExecutionRetention(ctx, p, func(bound context.Context) error {
		return s.retentionFresh(bound, s.boundTx(bound).tx, p, true, fresh)
	})
}

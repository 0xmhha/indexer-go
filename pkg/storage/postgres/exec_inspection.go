package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/jackc/pgx/v5"
)

// InspectExecutionIngress borrows this Store's existing pool; never opens one.
func (s *Store) InspectExecutionIngress(ctx context.Context) (out ingress.Inspection, err error) {
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if s.boundTx(ctx) != nil || s.executionReadBound(ctx) {
		return out, port.ErrInvalidData
	}
	conn, release, err := s.executionReadConnection(ctx)
	if err != nil {
		return out, err
	}
	defer release()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	err = func() error {
		out.Version, out.Scope = 1, ingress.InspectionScope
		var e error
		out.Control, e = scanExecutionControl(tx.QueryRow(ctx, executionControlSQL))
		if e != nil {
			return e
		}
		owner, e := readIngressOwner(ctx, tx)
		if e == nil {
			out.Owner = &owner
		} else if !errors.Is(e, port.ErrNotFound) {
			return e
		} else if out.Control.IngressOwned {
			return port.ErrExecutionConflict
		}
		p, pending, receipt, e := readRetention(ctx, tx)
		if e == nil {
			out.Retention = &ingress.RetentionObservation{Permit: p, Pending: pending, Receipt: receipt}
		} else if !errors.Is(e, port.ErrNotFound) {
			return e
		}
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM exec_raw_deletions WHERE NOT completed)").Scan(&out.RawPending); e != nil {
			return e
		}
		if out.Owner != nil && out.Owner.Binding != nil {
			t, e := readRawTicket(ctx, tx, out.Owner.Binding.RawStoreID, nil)
			if e == nil {
				_, digest, e := t.Evidence.Bytes(t.Permit)
				if e != nil {
					return e
				}
				pd, e := retention.Digest(t.Permit)
				if e != nil {
					return e
				}
				out.Raw = &ingress.RawDeletionObservation{StoreID: execwire.Hash(t.Evidence.Record.StoreID), OwnerDigest: t.Evidence.OwnerDigest, PermitDigest: pd, EvidenceDigest: digest, Segment: t.Evidence.Record.Segment, RawEpoch: t.Evidence.RawEpoch, NativeEpoch: t.Evidence.NativeEpoch, NativeReadEpoch: t.Evidence.NativeReadEpoch, Completed: t.Completed}
			} else if !errors.Is(e, port.ErrNotFound) {
				return e
			}
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		return out.Validate()
	}()
	if err != nil {
		return ingress.Inspection{}, err
	}
	return out, nil
}

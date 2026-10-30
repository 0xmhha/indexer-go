package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
	"github.com/jackc/pgx/v5"
)

type rawDeletionKey struct{}
type rawDeletionScope struct {
	owner  *Store
	digest execwire.Hash
	active atomic.Bool
}

func readRawTicket(ctx context.Context, q querier, store execwire.Hash, segment *uint64) (out ingress.RawDeletionTicket, err error) {
	var data, digest, owner, id []byte
	var n int64
	var completed bool
	query := `SELECT evidence,digest,owner_digest,store_id,segment,completed FROM exec_raw_deletions WHERE store_id=$1`
	args := []any{store[:]}
	if segment != nil {
		query += " AND segment=$2"
		args = append(args, int64(*segment))
	}
	query += " ORDER BY completed,segment DESC LIMIT 1"
	err = q.QueryRow(ctx, query, args...).Scan(&data, &digest, &owner, &id, &n, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, port.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if len(data) > 8<<20 || json.Unmarshal(data, &out) != nil {
		return out, port.ErrExecutionConflict
	}
	out.Completed = completed
	canonical, want, e := out.Evidence.Bytes(out.Permit)
	if e != nil || !bytes.Equal(data, canonical) || !bytes.Equal(digest, want[:]) || !bytes.Equal(owner, out.Evidence.OwnerDigest[:]) || !bytes.Equal(id, out.Evidence.Record.StoreID[:]) || n != int64(out.Evidence.Record.Segment) {
		return out, port.ErrExecutionConflict
	}
	return out, nil
}

// ExecutionRawDeletion observes durable evidence only; it grants no live lease.
func (s *Store) ExecutionRawDeletion(ctx context.Context, id execwire.Hash) (ingress.RawDeletionTicket, error) {
	if id == (execwire.Hash{}) {
		return ingress.RawDeletionTicket{}, port.ErrInvalidData
	}
	return readRawTicket(ctx, s.q(ctx), id, nil)
}

// Commit on a second connection of the SAME pool4 before unlink. The outer
// E3 EX lease stays live; its callback transaction cannot commit before IO.
func (s *Store) persistRawTicket(ctx context.Context, p retention.Permit, e ingress.RawDeletion, complete bool) error {
	data, digest, err := e.Bytes(p)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := executionDurable(ctx, tx); err != nil {
			return err
		}
		current, pending, _, err := readRetention(ctx, tx)
		if err != nil {
			return err
		}
		if !pending || current != p {
			return port.ErrExecutionMaintenance
		}
		if complete {
			_, err = tx.Exec(ctx, "UPDATE exec_raw_deletions SET completed=true WHERE digest=$1 AND evidence=$2", digest[:], data)
			if err == nil {
				// The window also keeps one completion receipt. Preserve any pending
				// evidence and the newest completion; immutable payloads are separate.
				_, err = tx.Exec(ctx, "DELETE FROM exec_raw_deletions WHERE completed AND owner_digest=$1 AND digest<>$2", e.OwnerDigest[:], digest[:])
			}
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO exec_raw_deletions(digest,owner_digest,store_id,segment,evidence)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, digest[:], e.OwnerDigest[:], e.Record.StoreID[:], int64(e.Record.Segment), data)
		}
		if err != nil {
			return err
		}
		got, err := readRawTicket(ctx, tx, execwire.Hash(e.Record.StoreID), &e.Record.Segment)
		if errors.Is(err, port.ErrNotFound) {
			return port.ErrExecutionMaintenance
		}
		if err != nil {
			return err
		}
		stored, sd, err := got.Evidence.Bytes(got.Permit)
		if err != nil || sd != digest || !bytes.Equal(stored, data) || got.Completed != complete {
			return port.ErrExecutionConflict
		}
		return nil
	})
}

func (s *Store) WithExecutionRawDeletion(ctx context.Context, p retention.Permit, e ingress.RawDeletion, apply func(context.Context) error) error {
	if apply == nil {
		return port.ErrInvalidData
	}
	_, digest, err := e.Bytes(p)
	if err != nil {
		return err
	}
	// Freeze caller-owned slices before persisting or granting a capability.
	encoded, _ := json.Marshal(e)
	var owned ingress.RawDeletion
	if json.Unmarshal(encoded, &owned) != nil {
		return port.ErrInvalidData
	}
	e = owned
	return s.withExecutionRetention(ctx, p, true, func(bound context.Context) error {
		if err := s.validateRawDeletion(bound, p, e); err != nil {
			return err
		}
		if err := s.persistRawTicket(ctx, p, e, false); err != nil {
			return err
		}
		scope := &rawDeletionScope{owner: s, digest: digest}
		scope.active.Store(true)
		defer scope.active.Store(false)
		bound = context.WithValue(bound, rawDeletionKey{}, scope)
		if err := apply(bound); err != nil {
			return err
		}
		if err := bound.Err(); err != nil {
			return err
		}
		return s.persistRawTicket(ctx, p, e, true)
	})
}

// Recovery checks BEFORE OpenOwnedChecked may unlink. Evidence is usable only
// inside the exact live synchronous E3 callback, never as an archived permit.
func (s *Store) CheckExecutionRawDeletion(ctx context.Context, p retention.Permit, e ingress.RawDeletion, r window.DeletionRecord) error {
	if ctx == nil {
		return port.ErrClosed
	}
	scope, _ := ctx.Value(rawDeletionKey{}).(*rawDeletionScope)
	if scope == nil || scope.owner != s || !scope.active.Load() || s.boundTx(ctx) == nil {
		return port.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, digest, err := e.Bytes(p)
	if err != nil || scope.digest != digest || r != e.Record {
		return port.ErrExecutionConflict
	}
	got, err := readRawTicket(ctx, s.q(ctx), execwire.Hash(r.StoreID), &r.Segment)
	if err != nil {
		return err
	}
	_, stored, err := got.Evidence.Bytes(got.Permit)
	if err != nil || stored != digest || got.Completed {
		return port.ErrExecutionConflict
	}
	return nil
}

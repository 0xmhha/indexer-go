package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"sync/atomic"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

type executionPartKey struct{}
type executionPartScope struct {
	part   port.ExecutionPart
	active atomic.Bool
}

var _ port.ExecutionStateStore = (*Store)(nil)

func validateExecutionBaseline(b port.ExecutionBaseline) ([]byte, error) {
	if len(b.Accounts)+len(b.Storage)+len(b.Code) > 65536 {
		return nil, port.ErrInvalidData
	}
	for i, a := range b.Accounts {
		if !a.Baseline || a.StorageCleared || a.Before != a.After || (!a.After.Exists && a.After != (execwire.Account{})) {
			return nil, port.ErrInvalidData
		}
		if i > 0 && bytes.Compare(b.Accounts[i-1].Address[:], a.Address[:]) >= 0 {
			return nil, port.ErrInvalidData
		}
	}
	for i, v := range b.Storage {
		if !v.Baseline || v.Before != v.After {
			return nil, port.ErrInvalidData
		}
		if i > 0 {
			prev := b.Storage[i-1]
			order := bytes.Compare(prev.Address[:], v.Address[:])
			if order > 0 || order == 0 && bytes.Compare(prev.Slot[:], v.Slot[:]) >= 0 {
				return nil, port.ErrInvalidData
			}
		}
	}
	for i, code := range b.Code {
		if err := validExecutionCode(code); err != nil {
			return nil, err
		}
		if i > 0 && bytes.Compare(b.Code[i-1].Hash[:], code.Hash[:]) >= 0 {
			return nil, port.ErrInvalidData
		}
	}
	data, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	if len(data) > execwire.MaxBlockBytes {
		return nil, execwire.ErrLimit
	}
	return data, nil
}
func (s *Store) SeedExecution(ctx context.Context, b port.ExecutionBaseline) error {
	if s.executionReadBound(ctx) {
		return port.ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.write(); err != nil {
		return err
	}
	if s.boundTx(ctx) != nil || s.pool.Config().MaxConns != 4 {
		return port.ErrInvalidData
	}
	data, err := validateExecutionBaseline(b)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		c, err := scanExecutionControl(tx.QueryRow(ctx, executionControlSQL))
		if err != nil {
			return err
		}
		if b.Ref != c.Anchor || b.Root != c.AnchorRoot {
			return port.ErrExecutionConflict
		}
		var old []byte
		if err := tx.QueryRow(ctx, "SELECT baseline_digest FROM exec_control WHERE id FOR UPDATE").Scan(&old); err != nil {
			return err
		}
		if old != nil {
			if !bytes.Equal(old, digest[:]) {
				return port.ErrExecutionConflict
			}
			return nil
		}
		if c.Rewinding {
			return port.ErrExecutionRewinding
		}
		var parts int64
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM exec_parts").Scan(&parts); err != nil {
			return err
		}
		if parts != 0 {
			return port.ErrExecutionConflict
		}
		if err := executionDurable(ctx, tx); err != nil {
			return err
		}
		bt := &blockTx{owner: s, tx: tx}
		bound := context.WithValue(ctx, blockTxKey{}, bt)
		anchor, err := s.GetBlock(bound, b.Ref.Height)
		if err != nil {
			return err
		}
		if execwire.Hash(anchor.Hash) != b.Ref.Hash || execwire.Hash(anchor.StateRoot) != b.Root || anchor.Time > math.MaxInt64 {
			return port.ErrExecutionConflict
		}
		step := execwire.Step{Cause: execwire.Baseline, TxIndex: -1, Accounts: b.Accounts}
		if b.Ref.Height == 0 {
			step.Cause = execwire.Genesis
		}
		step.Storage = b.Storage
		step.Code = b.Code
		result := execwire.Result{Ref: b.Ref, StateRoot: b.Root, Time: anchor.Time, Steps: []execwire.Step{step}}
		if err := s.writeExecutionAccounts(bound, c, result, true); err != nil {
			return err
		}
		if err := s.writeExecutionStorage(bound, c, result, true); err != nil {
			return err
		}
		if err := s.writeExecutionContracts(bound, c, result, true, nil); err != nil {
			return err
		}
		if err := bt.flush(ctx); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE exec_control SET baseline_digest=$1 WHERE id", digest[:])
		return err
	})
}
func (s *Store) ApplyExecutionState(ctx context.Context, w port.ExecutionWriter, r execwire.Result, creations ...port.ExecutionCreation) error {
	scope, _ := ctx.Value(executionPartKey{}).(*executionPartScope)
	if scope == nil || !scope.active.Load() || s.boundTx(ctx) == nil {
		return port.ErrClosed
	}
	if scope.part.Writer != w || scope.part.Ref != r.Ref || scope.part.Parent != r.Parent {
		return port.ErrExecutionConflict
	}
	encoded, err := execwire.MarshalResult(r)
	if err != nil {
		return err
	}
	if execwire.Hash(sha256.Sum256(encoded)) != scope.part.Digest {
		return port.ErrExecutionConflict
	}
	if r.Time > math.MaxInt64 {
		return port.ErrInvalidData
	}
	for _, step := range r.Steps {
		if step.Cause == execwire.Baseline || step.Cause == execwire.Genesis {
			return port.ErrInvalidData
		}
		for _, a := range step.Accounts {
			if a.Baseline {
				return port.ErrInvalidData
			}
		}
		for _, v := range step.Storage {
			if v.Baseline {
				return port.ErrInvalidData
			}
		}
	}
	c, err := s.ExecutionStatus(ctx)
	if err != nil {
		return err
	}
	if !c.BaselineReady {
		return port.ErrExecutionCoverage
	}
	if len(creations) > 0 && w != port.ExecutionContracts {
		return port.ErrInvalidData
	}
	switch w {
	case port.ExecutionAccounts:
		return s.writeExecutionAccounts(ctx, c, r, false)
	case port.ExecutionStorage:
		return s.writeExecutionStorage(ctx, c, r, false)
	case port.ExecutionContracts:
		return s.writeExecutionContracts(ctx, c, r, false, creations)
	default:
		return port.ErrInvalidData
	}
}

func (s *Store) ExecutionAccountAt(ctx context.Context, addr execwire.Address, height uint64) (execwire.Account, error) {
	if height > math.MaxInt64 {
		return execwire.Account{}, port.ErrInvalidData
	}
	a, err := scanExecutionAccount(s.q(ctx).QueryRow(ctx, "SELECT "+executionAccountAfterColumns+" FROM state_account_changes WHERE address=$1 AND height<=$2 ORDER BY height DESC,ordinal DESC LIMIT 1", addr[:], int64(height)))
	if errors.Is(err, pgx.ErrNoRows) {
		return a, port.ErrExecutionCoverage
	}
	return a, err
}

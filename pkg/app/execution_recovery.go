package app

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
)

type executionRecovery struct {
	store    execution.ControllerStore
	target   func(context.Context) (execwire.Ref, execwire.Hash, error)
	sql      func(context.Context) error
	validate func(context.Context, port.ExecutionControl) error
}

func (r executionRecovery) Run(ctx context.Context, owned ingress.Program) error {
	c, err := r.store.ExecutionStatus(ctx)
	if err != nil {
		return err
	}
	if c.Maintenance {
		return port.ErrExecutionMaintenance
	}
	if c.ReorgPending {
		checks, ok := r.store.(port.ExecutionReorgCheckStore)
		if !ok || owned == nil || r.target == nil {
			return port.ErrExecutionConflict
		}
		check, err := checks.ExecutionReorgCheckStatus(ctx)
		if err != nil {
			return err
		}
		if c.Rewinding || check != (port.ExecutionReorgCheck{Generation: c.Generation, From: c.Visible, Root: c.Root}) {
			return port.ErrExecutionConflict
		}
		target, root, err := r.target(ctx)
		if err != nil {
			return err
		}
		if _, err = r.store.BeginExecutionRewind(ctx, target, root); err != nil {
			return err
		}
		c, err = r.store.ExecutionStatus(ctx)
		if err != nil {
			return err
		}
	}
	var bridge ingress.RecoveryProgram
	if c.Rewinding {
		if owned != nil {
			var ok bool
			bridge, ok = owned.(ingress.RecoveryProgram)
			if !ok {
				return port.ErrExecutionConflict
			}
			marker, err := r.store.ExecutionRewindStatus(ctx)
			if err != nil {
				return err
			}
			if c.Generation != marker.Target.Generation {
				return port.ErrExecutionStale
			}
			if err = bridge.Recover(ctx, marker); err != nil {
				return err
			}
		}
		if err = r.sql(ctx); err != nil {
			return err
		}
		c, err = r.store.ExecutionStatus(ctx)
		if err != nil {
			return err
		}
		if c.Rewinding || c.ReorgPending {
			return port.ErrExecutionRewinding
		}
	}
	if err = r.validate(ctx, c); err != nil {
		return err
	}
	if bridge != nil {
		return bridge.Recovered(ctx, c)
	}
	return nil
}

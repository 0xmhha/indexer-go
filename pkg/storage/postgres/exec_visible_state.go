package postgres

import (
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

var _ port.ExecutionStateReader = (*Store)(nil)

func (s *Store) executionStatePoint(ctx context.Context) (port.ExecutionStatePoint, error) {
	c, err := s.ExecutionStatus(ctx)
	if err != nil {
		return port.ExecutionStatePoint{}, err
	}
	if c.Maintenance {
		return port.ExecutionStatePoint{}, port.ErrExecutionMaintenance
	}
	if c.Rewinding {
		return port.ExecutionStatePoint{}, port.ErrExecutionRewinding
	}
	if !c.BaselineReady {
		return port.ExecutionStatePoint{}, port.ErrExecutionCoverage
	}
	return port.ExecutionStatePoint{Ref: c.Visible, Root: c.Root}, nil
}
func (s *Store) VisibleExecutionAccount(ctx context.Context, addr execwire.Address) (v port.ExecutionAccountView, err error) {
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		var err error
		v.ExecutionStatePoint, err = s.executionStatePoint(ctx)
		if err != nil {
			return err
		}
		v.Account, err = s.ExecutionAccountAt(ctx, addr, v.Ref.Height)
		return err
	})
	return v, err
}
func (s *Store) VisibleExecutionStorage(ctx context.Context, addr execwire.Address, slot execwire.Word) (v port.ExecutionStorageView, err error) {
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		var err error
		v.ExecutionStatePoint, err = s.executionStatePoint(ctx)
		if err != nil {
			return err
		}
		v.Value, err = s.ExecutionStorageAt(ctx, addr, slot, v.Ref.Height)
		return err
	})
	return v, err
}
func (s *Store) VisibleExecutionCode(ctx context.Context, addr execwire.Address) (v port.ExecutionCodeView, err error) {
	err = s.ReadSnapshot(ctx, func(ctx context.Context) error {
		var err error
		v.ExecutionStatePoint, err = s.executionStatePoint(ctx)
		if err != nil {
			return err
		}
		v.Code, err = s.ExecutionCodeAt(ctx, addr, v.Ref.Height)
		return err
	})
	return v, err
}

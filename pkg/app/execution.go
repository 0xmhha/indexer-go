package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

func preflightExecution(cfg *config.Config, opts StartOptions) error {
	if cfg.Source.Execution == nil {
		return nil
	}
	if opts.ClearData || opts.Reindex || opts.GapRecovery {
		return fmt.Errorf("source.execution does not permit legacy clear/reindex/gap-recovery; use explicit verified checkpoint recovery")
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("source.execution: %w", err)
	}
	if err := validateConfig(cfg); err != nil {
		return fmt.Errorf("source.execution: %w", err)
	}
	return nil
}

func validateExecutionControl(e *config.ExecutionSourceConfig, c port.ExecutionControl, profile chains.Profile) error {
	if e == nil || !chains.SupportsExecution(profile) {
		return port.ErrInvalidData
	}
	if c.Maintenance {
		return port.ErrExecutionMaintenance
	}
	if !c.BaselineReady {
		return port.ErrExecutionCoverage
	}
	if c.Anchor.Height != e.AnchorHeight || c.Anchor.Hash != execwire.Hash(common.HexToHash(e.AnchorHash)) || c.AnchorRoot != execwire.Hash(common.HexToHash(e.AnchorRoot)) {
		return port.ErrExecutionConflict
	}
	return nil
}

type executionService interface {
	Run(context.Context) error
	Stop()
}

func (a *App) checkExecutionStorage(ctx context.Context) error {
	store, ok := a.storage.(port.ExecutionStore)
	if !ok || a.profile == nil {
		return port.ErrInvalidData
	}
	c, err := store.ExecutionStatus(ctx)
	if err != nil {
		return fmt.Errorf("execution requires an explicitly prepared checkpoint: %w", err)
	}
	if err = validateExecutionControl(a.config.Source.Execution, c, a.profile); err != nil {
		return err
	}
	id, err := a.client.GetChainID(ctx)
	if err != nil {
		return err
	}
	genesis, err := a.profileSrc.HashAt(ctx, 0)
	if err != nil {
		return err
	}
	if c.Chain.ID != id.String() || c.Chain.Genesis != execwire.Hash(genesis) {
		return port.ErrExecutionConflict
	}
	return nil
}

func (a *App) initExecution(ctx context.Context) error {
	store, ok := a.storage.(interface {
		execution.ControllerStore
		execution.PartStore
		port.KVStore
	})
	profile, binary := a.profile.(chains.BinaryProfile)
	if !ok || !binary {
		return port.ErrInvalidData
	}
	e := a.config.Source.Execution
	limits := execution.Limits{MaxBlocks: e.QueueBlocks, MaxBytes: e.QueueBytes}
	if limits.MaxBlocks == 0 {
		limits.MaxBlocks = 64
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = 256 << 20
	}
	a.fetcher.SetExecutionCanonical(a.profileSrc)
	recoverExecution := func(ctx context.Context) error {
		c, err := store.ExecutionStatus(ctx)
		if err != nil {
			return err
		}
		if c.Maintenance {
			return port.ErrExecutionMaintenance
		}
		if c.Rewinding {
			if err = a.fetcher.RecoverExecution(ctx); err != nil {
				return err
			}
			c, err = store.ExecutionStatus(ctx)
			if err != nil {
				return err
			}
		}
		return validateExecutionFeatures(ctx, store, c, a.features.Units())
	}
	// Recovery completes before newApp exposes the API, and is checked again
	// before the relay/listener starts. No legacy backfill or Fetcher.Run runs.
	if err := recoverExecution(ctx); err != nil {
		return err
	}
	backend := &execution.StoreBackend{Store: store, Publisher: a.fetcher}
	r := &execution.Receiver{Chain: execwire.Chain{}, Profile: profile, Store: store, Window: 8, Timeout: e.Timeout, Confirm: a.fetcher.ConfirmExecution, ValidateReorg: a.fetcher.ValidateExecutionReorg, ReorgOnError: func(err error) bool { return errors.Is(err, fetch.ErrReorg) }}
	c, err := store.ExecutionStatus(ctx)
	if err != nil {
		return err
	}
	r.Chain = c.Chain
	r.OpenController = func(ctx context.Context) (*execution.Controller, error) {
		return execution.NewController(ctx, limits, store, backend, recoverExecution)
	}
	r.Reorg = func(ctx context.Context, c *execution.Controller) error {
		target, root, err := a.fetcher.ExecutionRecoveryTarget(ctx)
		if err != nil {
			return err
		}
		return c.Rewind(ctx, target, root)
	}
	r.OnSessionError = func(err error) { a.logger.Warn("Execution session ended", zap.Error(err)) }
	a.executionFeed = &executionRuntime{recover: recoverExecution, relay: a.fetcher.StartRelay, serve: func(ctx context.Context) error { return r.Run(ctx, e.Socket) }}
	return nil
}

package arcingress

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/window"
)

func (p *program) maintenanceGate(parent context.Context) error {
	if !filepath.IsAbs(p.cfg.Native.DataDir) {
		return nil
	}
	timeout := p.cfg.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	var r maintenanceRecord
	if err := readArtifact(maintenancePath(p.cfg.Native.DataDir), &r); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := r.validate(); err != nil {
		return err
	}
	if r.Phase != "complete" {
		return port.ErrExecutionMaintenance
	}
	// This is a read-only gate. It opens no coordinator lock or data store.
	p.mu.Lock()
	c := maintenanceCoordinator{cfg: p.cfg, deps: p.deps, raw: p.opts}
	p.mu.Unlock()
	var err error
	for _, path := range []*string{&c.cfg.Native.DataDir, &c.cfg.Collector.SpoolDir, &c.raw.Path, &c.raw.Volume.Root} {
		*path, err = resolvePlanned(*path)
		if err != nil {
			return err
		}
	}
	if r.Scope != (maintenanceScope{Native: c.cfg.Native.DataDir, Raw: c.raw.Path, Source: c.cfg.Collector.SpoolDir, VolumeRoot: c.raw.Volume.Root, VolumeID: c.raw.Volume.ID, Reservation: c.raw.Reservation}) {
		return port.ErrExecutionConflict
	}
	s, ok := c.deps.Store.(MaintenanceStore)
	if !ok {
		return port.ErrInvalidData
	}
	c.store = s
	if _, err = c.checkOwner(ctx, r); err != nil {
		return err
	}
	permit, pending, receipt, err := s.ExecutionRetentionStatus(ctx)
	if err != nil {
		return err
	}
	if pending || permit != *r.Permit || receipt == nil || *receipt != *r.Receipt {
		return port.ErrExecutionMaintenance
	}
	return nil
}
func (p *program) Maintain(ctx context.Context, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	return p.maintenanceCall(ctx, o, false)
}
func (p *program) RecoverMaintenance(ctx context.Context, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	return p.maintenanceCall(ctx, o, true)
}
func (p *program) maintenanceCall(parent context.Context, o ingress.MaintenanceOptions, recovering bool) (ingress.MaintenanceResult, error) {
	p.maintenanceMu.Lock()
	defer p.maintenanceMu.Unlock()
	p.mu.Lock()
	if !p.started || p.stopped || p.runCtx == nil {
		p.mu.Unlock()
		return ingress.MaintenanceResult{}, port.ErrClosed
	}
	run := p.runCtx
	c := maintenanceCoordinator{cfg: p.cfg, deps: p.deps, raw: p.opts, options: o}
	parked := p.draining != nil && p.draining.parked
	p.mu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	detach := context.AfterFunc(run, cancel)
	defer func() { detach(); cancel() }()
	if !filepath.IsAbs(o.Executable) {
		return ingress.MaintenanceResult{}, port.ErrInvalidData
	}
	i, err := os.Lstat(o.Executable)
	if err != nil {
		return ingress.MaintenanceResult{}, err
	}
	if !ownedExecutable(i) {
		return ingress.MaintenanceResult{}, ErrNative
	}
	var point *ingress.DrainPoint
	if !recovering {
		if err = p.maintenanceGate(ctx); err != nil {
			return ingress.MaintenanceResult{}, err
		}
		var drained ingress.DrainPoint
		if parked {
			p.mu.Lock()
			drained = p.draining.point
			p.mu.Unlock()
		} else {
			drained, err = p.Drain(ctx)
			if err != nil {
				return ingress.MaintenanceResult{}, err
			}
		}
		point = &drained
	} else if !parked {
		if err = p.maintenanceGate(ctx); err != nil {
			return ingress.MaintenanceResult{}, err
		}
	}
	p.recoveryMu.Lock()
	defer p.recoveryMu.Unlock()
	p.mu.Lock()
	valid := !p.stopped && run.Err() == nil && (point == nil || p.draining != nil && p.draining.parked && p.draining.point == *point)
	p.mu.Unlock()
	if !valid {
		return ingress.MaintenanceResult{}, port.ErrClosed
	}
	result, err := c.run(ctx, point)
	if err != nil {
		return result, err
	}
	p.mu.Lock()
	parked = p.draining != nil && p.draining.parked
	p.mu.Unlock()
	if parked {
		if err = p.resumeLocked(ctx); err != nil {
			return result, err
		}
	}
	result.Resumed = run.Err() == nil
	return result, nil
}

// RecoverMaintenance uses only caller-owned prepared resources. It does not
// start an application, receiver, peer or native importer, nor resume workers.
func RecoverMaintenance(ctx context.Context, cfg ingress.Config, d ingress.Dependencies, raw window.Options, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	c := maintenanceCoordinator{cfg: cfg, deps: d, raw: raw, options: o}
	return c.run(ctx, nil)
}

// Maintain is an explicit offline entry point for an already stopped owner.
// Native run EX/source ownership and the normal E3 gates independently verify
// the frozen boundary. No App, worker, dialer or pool is started here.
func Maintain(ctx context.Context, cfg ingress.Config, d ingress.Dependencies, raw window.Options, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	c := maintenanceCoordinator{cfg: cfg, deps: d, raw: raw, options: o}
	if _, _, err := c.normalize(ctx); err != nil {
		return ingress.MaintenanceResult{}, err
	}
	control, err := c.store.ExecutionStatus(ctx)
	if err != nil {
		return ingress.MaintenanceResult{}, err
	}
	if control.Maintenance || control.Rewinding || control.ReorgPending {
		return ingress.MaintenanceResult{}, port.ErrExecutionMaintenance
	}
	owner, err := c.owner(ctx)
	if err != nil {
		return ingress.MaintenanceResult{}, err
	}
	point := ingress.DrainPoint{Chain: control.Chain, Ref: control.Visible, Root: control.Root}
	err = spool.WithRetentionEvidence(ctx, c.source(owner), execwire.Ref{Height: point.Ref.Height, Hash: point.Ref.Hash}, point.Root, c.options.KeepBlocks, func(r retention.Request, digest execwire.Hash) error {
		if r.ACK != point.Ref || r.Tail != point.Ref {
			return port.ErrExecutionConflict
		}
		point.Digest = digest
		return nil
	})
	if err != nil {
		return ingress.MaintenanceResult{}, err
	}
	return c.run(ctx, &point)
}

var _ ingress.MaintenanceProgram = (*program)(nil)

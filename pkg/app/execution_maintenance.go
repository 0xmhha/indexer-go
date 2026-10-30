package app

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
)

type executionMaintenance interface {
	Maintain(context.Context, ingress.MaintenanceOptions) (ingress.MaintenanceResult, error)
	RecoverMaintenance(context.Context, ingress.MaintenanceOptions) (ingress.MaintenanceResult, error)
}

// MaintainExecution explicitly drains, maintains and verifies resume of an
// optional input owner while this application's receiver and Store stay alive.
func (a *App) MaintainExecution(ctx context.Context, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	if m, ok := a.executionFeed.(executionMaintenance); ok {
		return m.Maintain(ctx, o)
	}
	return ingress.MaintenanceResult{}, port.ErrInvalidData
}
func (a *App) RecoverExecutionMaintenance(ctx context.Context, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	if m, ok := a.executionFeed.(executionMaintenance); ok {
		return m.RecoverMaintenance(ctx, o)
	}
	return ingress.MaintenanceResult{}, port.ErrInvalidData
}
func (r *executionRuntime) maintainer() (ingress.MaintenanceProgram, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started || r.stopped {
		return nil, port.ErrClosed
	}
	m, ok := r.ingress.(ingress.MaintenanceProgram)
	if !ok {
		return nil, port.ErrInvalidData
	}
	return m, nil
}
func (r *executionRuntime) Maintain(ctx context.Context, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	m, err := r.maintainer()
	if err != nil {
		return ingress.MaintenanceResult{}, err
	}
	return m.Maintain(ctx, o)
}
func (r *executionRuntime) RecoverMaintenance(ctx context.Context, o ingress.MaintenanceOptions) (ingress.MaintenanceResult, error) {
	m, err := r.maintainer()
	if err != nil {
		return ingress.MaintenanceResult{}, err
	}
	return m.RecoverMaintenance(ctx, o)
}

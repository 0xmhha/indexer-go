package app

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
)

type executionDrain interface {
	Drain(context.Context) (ingress.DrainPoint, error)
	Resume(context.Context) error
}

// DrainExecution parks an optional execution-input owner while the receiver,
// API and existing Store remain alive. It grants no maintenance permission.
func (a *App) DrainExecution(ctx context.Context) (ingress.DrainPoint, error) {
	if d, ok := a.executionFeed.(executionDrain); ok {
		return d.Drain(ctx)
	}
	return ingress.DrainPoint{}, port.ErrInvalidData
}
func (a *App) ResumeExecution(ctx context.Context) error {
	if d, ok := a.executionFeed.(executionDrain); ok {
		return d.Resume(ctx)
	}
	return port.ErrInvalidData
}
func (r *executionRuntime) drainer() (ingress.DrainProgram, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started || r.stopped {
		return nil, port.ErrClosed
	}
	d, ok := r.ingress.(ingress.DrainProgram)
	if !ok {
		return nil, port.ErrInvalidData
	}
	return d, nil
}
func (r *executionRuntime) Drain(ctx context.Context) (ingress.DrainPoint, error) {
	d, err := r.drainer()
	if err != nil {
		return ingress.DrainPoint{}, err
	}
	return d.Drain(ctx)
}
func (r *executionRuntime) Resume(ctx context.Context) error {
	d, err := r.drainer()
	if err != nil {
		return err
	}
	return d.Resume(ctx)
}

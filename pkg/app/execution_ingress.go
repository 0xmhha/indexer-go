package app

import (
	"context"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/0xmhha/indexer-go/pkg/source"
)

func prepareExecutionIngress(ctx context.Context, cfg *config.Config, store ingress.StatusReader, canonical source.CanonicalReader, profile chains.BinaryProfile, recover func(context.Context) error) (ingress.Program, error) {
	if cfg.Source.Execution.Ingress == nil {
		return nil, recover(ctx)
	}
	c, err := store.ExecutionStatus(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateExecutionControl(cfg.Source.Execution, c, profile); err != nil {
		return nil, err
	}
	p, err := ingress.Prepare(ctx, *cfg.Source.Execution.Ingress, ingress.Dependencies{Store: store, Canonical: canonical, Profile: profile, Control: c, Layout: cfg.WindowStorage, Socket: cfg.Source.Execution.Socket})
	if err != nil {
		return nil, err
	}
	if err = recover(ctx); err == nil {
		err = p.Check(ctx)
	}
	if err != nil {
		p.Stop()
		return nil, err
	}
	return p, nil
}

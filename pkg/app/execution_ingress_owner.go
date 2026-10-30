package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"github.com/ethereum/go-ethereum/common"
)

func executionIngressPolicy(cfg *config.Config, c port.ExecutionControl) (*port.ExecutionIngressPolicy, error) {
	e := cfg.Source.Execution
	if e == nil || e.Ingress == nil || !ingress.OwnsOrigins(e.Ingress.Module) {
		return nil, nil
	}
	var proofs any
	if e.RequiredStateProofs != nil {
		selection, _, err := e.RequiredStateProofs.Normalize()
		if err != nil {
			return nil, err
		}
		proofs = selection
	}
	requirements, err := json.Marshal(struct {
		RequireBlobs bool
		StateProofs  any
		Consensus    any
	}{e.RequireBlobSidecars, proofs, e.Consensus})
	if err != nil {
		return nil, err
	}
	p := &port.ExecutionIngressPolicy{Version: 1, Module: e.Ingress.Module, Chain: c.Chain, Anchor: execwire.Ref{Generation: c.Anchor.Generation, Height: e.AnchorHeight, Hash: execwire.Hash(common.HexToHash(e.AnchorHash))}, AnchorRoot: execwire.Hash(common.HexToHash(e.AnchorRoot)), Requirements: execwire.Hash(sha256.Sum256(append([]byte("indexer.ingress.requirements.v1\x00"), requirements...)))}
	return p, p.Validate()
}

func (a *App) configureExecutionIngressOwner(ctx context.Context) error {
	wanted := a.config.Source.Execution != nil && a.config.Source.Execution.Ingress != nil && ingress.OwnsOrigins(a.config.Source.Execution.Ingress.Module)
	store, ok := a.storage.(port.ExecutionIngressOwnerStore)
	if !ok {
		if wanted {
			return port.ErrInvalidData
		}
		return nil
	}
	var policy *port.ExecutionIngressPolicy
	readOnly := a.config.NodeRole() == config.RoleAPI
	if wanted && !readOnly {
		status, ok := a.storage.(ingress.StatusReader)
		if !ok {
			return port.ErrInvalidData
		}
		c, err := status.ExecutionStatus(ctx)
		if err != nil {
			return err
		}
		policy, err = executionIngressPolicy(a.config, c)
		if err != nil {
			return err
		}
	}
	return store.ConfigureExecutionIngress(ctx, policy, readOnly)
}

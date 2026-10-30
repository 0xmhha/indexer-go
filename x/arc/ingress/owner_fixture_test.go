package arcingress

import (
	"context"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Unit fixture only; production SQL never creates ownership while reading it.
func (s *mutableStatus) ExecutionIngressOwner(ctx context.Context) (port.ExecutionIngressOwner, error) {
	if err := ctx.Err(); err != nil {
		return port.ExecutionIngressOwner{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner == nil {
		p := port.ExecutionIngressPolicy{Version: 1, Module: ModuleName, Chain: s.control.Chain, Anchor: s.control.Anchor, AnchorRoot: s.control.AnchorRoot, Requirements: execwire.Hash{9}}
		d, err := p.Digest()
		if err != nil {
			return port.ExecutionIngressOwner{}, err
		}
		s.owner = &port.ExecutionIngressOwner{Policy: p, PolicyDigest: d}
	}
	out := *s.owner
	if out.Binding != nil {
		b := *out.Binding
		out.Binding = &b
	}
	return out, nil
}
func (s *mutableStatus) BindExecutionIngress(ctx context.Context, b port.ExecutionIngressBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d, err := b.Digest()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner == nil || s.owner.PolicyDigest != b.PolicyDigest {
		return port.ErrExecutionConflict
	}
	if s.owner.Binding != nil && s.owner.BindingDigest != d {
		return port.ErrExecutionConflict
	}
	s.owner.Binding = &b
	s.owner.BindingDigest = d
	return nil
}

type preparedStatusWithOwner struct {
	originOwnerStore
	status preparedStatusFunc
}

func (s preparedStatusWithOwner) ExecutionStatus(ctx context.Context) (port.ExecutionControl, error) {
	return s.status(ctx)
}

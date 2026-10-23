package execution

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/stretchr/testify/require"
	"testing"
)

type backendKey struct{}
type partProbe struct {
	port.ExecutionStateStore
	core, state []port.ExecutionWriter
	parts       []port.ExecutionPart
	fail        error
}

func (p *partProbe) WriteExecutionPart(ctx context.Context, part port.ExecutionPart, fn func(context.Context) error) error {
	p.parts = append(p.parts, part)
	if p.fail != nil {
		return p.fail
	}
	return fn(context.WithValue(ctx, backendKey{}, part.Writer))
}
func (p *partProbe) ApplyExecutionCore(ctx context.Context, w port.ExecutionWriter, _ *model.Block, _ []*model.Receipt) error {
	if ctx.Value(backendKey{}) != w {
		return errors.New("lost bound transaction")
	}
	p.core = append(p.core, w)
	return nil
}
func (p *partProbe) ApplyExecutionState(ctx context.Context, w port.ExecutionWriter, _ execwire.Result, _ ...port.ExecutionCreation) error {
	if ctx.Value(backendKey{}) != w {
		return errors.New("lost bound transaction")
	}
	p.state = append(p.state, w)
	return nil
}
func TestStoreBackendUsesBoundPartAndSeparatesPublication(t *testing.T) {
	chain, _, raw := preparedFixture(t)
	p, err := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, err)
	store := &partProbe{}
	b := &StoreBackend{Store: store}
	for _, w := range port.ExecutionWriters() {
		require.NoError(t, b.Part(t.Context(), w, p))
	}
	require.Len(t, store.parts, 7)
	require.Len(t, store.core, 4)
	require.Len(t, store.state, 3)
	for _, part := range store.parts {
		require.Equal(t, p.Ref(), part.Ref)
		require.Equal(t, p.Digest(), part.Digest)
	}
	require.Error(t, b.Publish(t.Context(), p), "missing publication must not ACK")
	failure := errors.New("rollback")
	store.fail = failure
	require.ErrorIs(t, b.Part(t.Context(), port.ExecutionBlocks, p), failure)
}

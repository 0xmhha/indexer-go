package feature

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

type writerPreparer struct {
	BlockHandler
	prepare func(context.Context, *Block) (PreparedHandler, error)
}

func (h writerPreparer) PrepareBlock(ctx context.Context, b *Block) (PreparedHandler, error) {
	return h.prepare(ctx, b)
}

func TestExecutionWritersOwnUnitsAndCheckpointAfterAllHooks(t *testing.T) {
	var calls []string
	hook := func(name string) BlockHandler {
		return BlockHandlerFunc(func(context.Context, *Block) error { calls = append(calls, name); return nil })
	}
	p := &Pipeline{handlers: []namedHandler{
		{feature: "a", h: hook("a1")}, {feature: "a", h: hook("a2")},
		{feature: "b", part: "x", def: "dx", h: writerPreparer{hook("legacy"), func(context.Context, *Block) (PreparedHandler, error) {
			calls = append(calls, "prepare")
			return PreparedHandler{Handler: hook("b/x"), Bytes: 5}, nil
		}}},
	}}
	b := &Block{Model: &model.Block{Number: 7, Hash: common.Hash{7}}}
	plan, err := p.PrepareWriters(t.Context(), b, true, 5)
	require.NoError(t, err)
	require.Equal(t, []string{"prepare"}, calls, "preparation never applies a writer")
	require.NoError(t, plan.Apply(t.Context(), b, func(_ context.Context, unit Unit) error {
		calls = append(calls, "checkpoint:"+unit.Name+":"+unit.Definition)
		return nil
	}))
	require.Equal(t, []string{"prepare", "a1", "a2", "checkpoint:a:", "b/x", "checkpoint:b/x:dx"}, calls)
}

func TestExecutionWritersFailClosedOnBoundsIdentityCancelAndUnitError(t *testing.T) {
	b := &Block{Model: &model.Block{Number: 1, Hash: common.Hash{1}}}
	boom := errors.New("unit failed")
	p := &Pipeline{handlers: []namedHandler{{feature: "a", h: writerPreparer{BlockHandlerFunc(func(context.Context, *Block) error { return boom }), func(context.Context, *Block) (PreparedHandler, error) { return PreparedHandler{Bytes: 6}, nil }}}}}
	_, err := p.PrepareWriters(t.Context(), b, true, 5)
	require.Error(t, err)
	plan, err := p.PrepareWriters(t.Context(), b, false, 5)
	require.NoError(t, err)
	checked := false
	checkpoint := func(context.Context, Unit) error { checked = true; return nil }
	require.ErrorIs(t, plan.Apply(t.Context(), &Block{Model: &model.Block{Number: 1, Hash: common.Hash{2}}}, checkpoint), port.ErrExecutionConflict)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, plan.Apply(ctx, b, checkpoint), context.Canceled)
	require.ErrorIs(t, plan.Apply(t.Context(), b, checkpoint), boom)
	require.False(t, checked)
	p.handlers[0].h = BlockHandlerFunc(func(_ context.Context, b *Block) error { b.Model.Hash = common.Hash{9}; return nil })
	plan, err = p.PrepareWriters(t.Context(), b, false, 5)
	require.NoError(t, err)
	require.ErrorIs(t, plan.Apply(t.Context(), b, checkpoint), port.ErrExecutionConflict)
	require.False(t, checked)
}

func TestExecutionWriterPlansForDifferentBlocksAreIndependent(t *testing.T) {
	p := &Pipeline{handlers: []namedHandler{{feature: "a", h: writerPreparer{nil, func(_ context.Context, b *Block) (PreparedHandler, error) {
		n := b.Model.Number
		return PreparedHandler{Handler: BlockHandlerFunc(func(_ context.Context, b *Block) error {
			if b.Model.Number != n {
				return port.ErrExecutionConflict
			}
			return nil
		})}, nil
	}}}}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for n := uint64(1); n <= 2; n++ {
		wg.Go(func() {
			b := &Block{Model: &model.Block{Number: n, Hash: common.Hash{byte(n)}}}
			plan, err := p.PrepareWriters(t.Context(), b, true, 10)
			if err == nil {
				err = plan.Apply(t.Context(), b, func(context.Context, Unit) error { return nil })
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestExecutionWritersCheckpointFailureStopsDependentAndTotalInputsAreBounded(t *testing.T) {
	b := &Block{Model: &model.Block{Number: 1, Hash: common.Hash{1}}}
	called := false
	first := BlockHandlerFunc(func(context.Context, *Block) error { return nil })
	second := BlockHandlerFunc(func(context.Context, *Block) error { called = true; return nil })
	p := &Pipeline{handlers: []namedHandler{{feature: "a", h: first}, {feature: "b", h: second}}}
	plan, err := p.PrepareWriters(t.Context(), b, false, 10)
	require.NoError(t, err)
	failure := errors.New("checkpoint persistence failed")
	require.ErrorIs(t, plan.Apply(t.Context(), b, func(context.Context, Unit) error { return failure }), failure)
	require.False(t, called)
	prepare := func(context.Context, *Block) (PreparedHandler, error) { return PreparedHandler{Bytes: 6}, nil }
	p.handlers[0].h = writerPreparer{first, prepare}
	p.handlers[1].h = writerPreparer{second, prepare}
	_, err = p.PrepareWriters(t.Context(), b, true, 10)
	require.Error(t, err, "the cap is shared across writers")
	p.handlers = append(p.handlers, namedHandler{feature: "a", h: first})
	_, err = p.PrepareWriters(t.Context(), b, false, 10)
	require.ErrorIs(t, err, port.ErrInvalidData, "a unit cannot checkpoint twice")
}

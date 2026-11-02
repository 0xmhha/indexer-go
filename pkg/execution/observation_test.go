package execution

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"github.com/0xmhha/indexer-go/internal/nativeobserve/observetest"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/stretchr/testify/require"
	"testing"
)

type observedPublisher struct{ err error }

func (p observedPublisher) PublishExecution(context.Context, execwire.Result, *model.Block, []*model.Receipt, execwire.Hash) error {
	return p.err
}
func TestPublicationObservationOnlyAfterSuccessAndNeverInventsCommitTime(t *testing.T) {
	i, tap := observetest.Bind(t, nativeobserve.SQLVisibleHeight)
	b := &StoreBackend{Observation: tap, Publisher: observedPublisher{errors.New("fresh failed")}}
	p := &Prepared{result: execwire.Result{Ref: execwire.Ref{Generation: 1, Height: 9, Hash: execwire.Hash{9}}}}
	require.Error(t, b.Publish(t.Context(), p))
	_, ok := i.Poll()
	require.False(t, ok)
	b.Publisher = observedPublisher{}
	require.NoError(t, b.Publish(t.Context(), p))
	o, ok := i.Poll()
	require.True(t, ok)
	require.EqualValues(t, 9, o.Number.Value)
	require.Zero(t, o.EventAt)
	require.Zero(t, o.Position.Native)
	require.Zero(t, o.Position.Read)
	for range 8 {
		require.NoError(t, b.Publish(t.Context(), p))
	}
	require.True(t, i.Lost(), "full observer must not change publication result")
}

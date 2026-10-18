package ethpeer

import (
	"context"
	"io"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/stretchr/testify/require"
)

var _ chainfeed.BatchStream = (*stream)(nil)

func TestNextBatchUsesExistingBoundedFrames(t *testing.T) {
	ctx := context.Background()
	c := &Client{ctx: ctx}
	inputs := []chainfeed.Envelope{{Kind: model.DatasetHeaders}, {Kind: model.DatasetBodies}, {Kind: model.DatasetReceipts}}
	newStream := func() *stream {
		return &stream{client: c, ctx: ctx, finite: true, next: 2, end: 1, pending: append([]chainfeed.Envelope(nil), inputs...)}
	}
	s := newStream()
	got, err := s.NextBatch(ctx)
	require.NoError(t, err)
	require.Equal(t, inputs, got)
	require.Empty(t, s.pending)
	_, err = s.NextBatch(ctx)
	require.ErrorIs(t, err, io.EOF)
	s = newStream()
	first, err := s.Next(ctx)
	require.NoError(t, err)
	rest, err := s.NextBatch(ctx)
	require.NoError(t, err)
	require.Equal(t, inputs, append([]chainfeed.Envelope{first}, rest...))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = newStream().NextBatch(canceled)
	require.ErrorIs(t, err, context.Canceled)
}

package arcfeed_test

import (
	"context"
	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmtrace"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/source/facts"
	arcfeed "github.com/0xmhha/indexer-go/x/arc/feed"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestArcFactsSourceFactory(t *testing.T) {
	c, _, fresh, peer, session := fixtures(t)
	db := db(t)
	run, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.ErrorIs(t, arcfeed.Run(run, db, fresh, peer, session, capture.PipelineOptions{OnCaptured: until3(cancel)}), context.Canceled)
	store, e := capture.New(db, peer.Chain, fresh)
	require.NoError(t, e)
	src, e := arcfeed.NewFactsSource(context.Background(), store, fresh, "arc-projection")
	require.NoError(t, e)
	require.Equal(t, "arc", src.Profile().ID())
	b, rs, e := src.BlockWithReceipts(context.Background(), 2)
	require.NoError(t, e)
	require.Equal(t, c.Block(2).Block.Hash(), b.Hash)
	require.Len(t, rs, 1)
	require.NoError(t, src.ValidateCommit(context.Background(), b))
	// Complete the real local RLPx -> window -> capture -> facts -> existing
	// writer path, without enabling unsupported state-reading features.
	writer := fetch.NewFetcher(src, db, &fetch.Config{StartHeight: 1, NumWorkers: 2, MaxRetries: 1, PipelineMetrics: true}, zap.NewNop(), nil)
	writer.SetSource(src)
	defer writer.Close()
	require.NoError(t, writer.FetchRange(context.Background(), 1, 3))
	for n := uint64(1); n <= 3; n++ {
		block, err := db.GetBlock(context.Background(), n)
		require.NoError(t, err)
		require.Equal(t, c.Block(n).Block.Hash(), block.Hash)
		id, err := store.CanonicalAt(context.Background(), 1, n)
		require.NoError(t, err)
		_, err = store.ProjectionAck(context.Background(), "arc-projection", 1, id)
		require.NoError(t, err)
	}
	require.EqualValues(t, 3, writer.GetPipelineMetrics().SourceValidate.Count)
	require.EqualValues(t, 3, writer.GetPipelineMetrics().SourceAck.Count)
	provider, e := arcfeed.NewStateSupplier(context.Background(), store, unavailableStateRPC{})
	require.NoError(t, e)
	id, e := store.CanonicalAt(context.Background(), 1, 2)
	require.NoError(t, e)
	selection := evmstate.Selection{Targets: []evmstate.Target{{Code: true}}}
	_, e = provider.Read(context.Background(), 1, id, selection)
	require.ErrorIs(t, e, port.ErrNotFound)
	_, e = provider.Acquire(context.Background(), 1, id, selection)
	require.ErrorIs(t, e, evmstate.ErrUnavailable)
	_, e = src.StateSnapshot(context.Background(), 2, selection)
	require.ErrorIs(t, e, facts.ErrCoverage)
	traceSupplier, e := arcfeed.NewCallTraceSupplier(context.Background(), store, unavailableStateRPC{})
	require.NoError(t, e)
	anchor, _ := store.State(context.Background())
	d, e := (evmtrace.Config{Name: "call-v1", Provider: "fixture", StartHeight: 2}).Descriptor(anchor.Anchor, "arc")
	require.NoError(t, e)
	_, e = store.InitializeSupplementStream(context.Background(), d)
	require.NoError(t, e)
	_, e = traceSupplier.Acquire(context.Background(), 1, id, d)
	require.ErrorIs(t, e, evmtrace.ErrUnavailable)
	_, e = src.CallTraces(context.Background(), 2, d, 0, 10)
	require.ErrorIs(t, e, facts.ErrCoverage)

}

type unavailableStateRPC struct{}

func (unavailableStateRPC) BatchCallContext(context.Context, []rpc.BatchElem) error {
	return evmstate.ErrUnavailable
}

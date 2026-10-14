package fetch

import (
	"context"
	"testing"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/source"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
)

func TestBlockAtAvoidsAncestorReceipts(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	rc, err := gethrpc.Dial(h.srv.URL())
	require.NoError(t, err)
	t.Cleanup(rc.Close)
	h.f.SetSource(sourcerpc.New(rc, evm.New("evm")))
	const height = 2
	before := h.srv.Calls()
	b, err := h.f.BlockAt(context.Background(), height)
	require.NoError(t, err)
	require.Equal(t, h.chain.Block(height).Block.Hash(), b.Hash)
	require.Equal(t, before["eth_getBlockByNumber"]+1, h.srv.Calls()["eth_getBlockByNumber"])
	require.Equal(t, before["eth_getBlockReceipts"], h.srv.Calls()["eth_getBlockReceipts"], "ancestors do not need receipts")
	// The normal ingest path still obtains and checks this block's receipts.
	require.NoError(t, h.f.FetchBlock(context.Background(), height))
	require.Greater(t, h.srv.Calls()["eth_getBlockReceipts"], before["eth_getBlockReceipts"])
	_, err = h.f.BlockAt(context.Background(), h.chain.Head()+1)
	require.ErrorIs(t, err, source.ErrNotFound)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = h.f.BlockAt(ctx, height)
	require.ErrorIs(t, err, context.Canceled)
}

func TestBlockAtSourceCompatibilityAndFailure(t *testing.T) {
	h := newChainHarness(t, &Config{}, nil)
	// A wrapper exposing only Source retains the original receipt path.
	before := h.srv.Calls()
	_, err := h.f.BlockAt(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, before["eth_getBlockReceipts"]+1, h.srv.Calls()["eth_getBlockReceipts"])
	h.src.failNext(1)
	_, err = h.f.BlockAt(context.Background(), 2)
	require.ErrorIs(t, err, errFlaky)
	h.f.SetSource(nil)
	_, err = h.f.BlockAt(context.Background(), 2)
	require.ErrorIs(t, err, errNoSource)
}

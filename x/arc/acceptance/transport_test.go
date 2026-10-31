//go:build arc_native

package acceptance

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

// This new mock's cancellation/range boundary can be race-qualified without
// repeating the actual SDK/PostgreSQL acceptance200 times.
func TestArcAcceptanceLoopbackTransportAndCancel(t *testing.T) {
	chain := testchain.NewChain(5042002, nil)
	one := chain.AddBlock()
	body, err := rlp.EncodeToBytes(one.Block.Body())
	require.NoError(t, err)
	receipts, err := rlp.EncodeToBytes(one.Receipts)
	require.NoError(t, err)
	n := &network{blocks: map[uint64]blockInput{0: {block: chain.Block(0).Block}, 1: {block: one.Block, body: body, receipts: receipts}}, height: 1, pause: 1, reached: make(chan struct{}), release: make(chan struct{})}
	peer := n.peer(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	id := model.FeedChain{ID: "5042002", GenesisHash: chain.Block(0).Block.Hash()}
	anchor := model.FeedBlock{Chain: id, Hash: id.GenesisHash}
	client, err := ethpeer.New(ethpeer.Config{Chain: id, NetworkID: 5042002, Checkpoint: anchor, ForkID: ethpeer.ForkID{Hash: [4]byte{1, 2, 3, 4}}, Key: key, Peers: []*enode.Node{peer.Self()}, ListenAddr: "127.0.0.1:0", Timeout: 5 * time.Second, PollInterval: time.Millisecond})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream, err := client.Subscribe(ctx, anchor, chainfeed.ReadLimits{MaxPayloadBytes: 64 << 10})
	require.NoError(t, err)
	defer func() { require.NoError(t, stream.Close()) }()
	batch, err := stream.(chainfeed.BatchStream).NextBatch(ctx)
	require.NoError(t, err)
	require.Len(t, batch, 3)
	require.Equal(t, one.Block.Hash(), batch[0].Block.Hash)
	server := httptest.NewServer(n)
	defer server.Close()
	requestContext, stop := context.WithCancel(ctx)
	defer stop()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, server.URL, bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x1",false]}`))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		response, e := server.Client().Do(request)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		done <- e
	}()
	select {
	case <-n.reached:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	stop()
	require.ErrorIs(t, <-done, context.Canceled)
	close(n.release)
}

package nodefeed_test

import (
	"context"
	"sync"
	"testing"
	"time"

	feed "github.com/0xmhha/indexer-go/internal/source/nodefeed"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/nodefeed"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
func setup(t *testing.T) (*feed.Source, *testchain.NodeFeedServer, *sourcerpc.Source, *testchain.Server) {
	t.Helper()
	sc := testchain.BuildDefault()
	rpcServer := testchain.NewServer(sc.Chain)
	t.Cleanup(rpcServer.Close)
	client, err := gethrpc.Dial(rpcServer.URL())
	require.NoError(t, err)
	t.Cleanup(client.Close)
	canonical := sourcerpc.New(client, evm.New("test"))
	server, err := testchain.NewNodeFeedServer(sc.Chain)
	require.NoError(t, err)
	t.Cleanup(server.Close)
	source, err := feed.New(t.Context(), feed.Config{Socket: server.Socket(), ChainID: uint64(testchain.DefaultChainID), Timeout: time.Second}, canonical)
	require.NoError(t, err)
	t.Cleanup(source.Close)
	return source, server, canonical, rpcServer
}

func TestNodeFeedModelsAndFreshCanonical(t *testing.T) {
	s, server, canonical, rpcServer := setup(t)
	head, err := s.Head(t.Context())
	require.NoError(t, err)
	for n := uint64(0); n <= head; n++ {
		want, wr, err := canonical.BlockWithReceipts(t.Context(), n)
		require.NoError(t, err)
		got, gr, err := s.BlockWithReceipts(t.Context(), n)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, wr, gr)
	}
	before := rpcServer.Calls()["eth_getBlockByNumber"]
	_, err = s.HashAt(t.Context(), 1)
	require.NoError(t, err)
	_, err = s.HashAt(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, before+2, rpcServer.Calls()["eth_getBlockByNumber"], "canonical lookups must each reach RPC")
	for _, r := range server.Requests() {
		require.NotEqual(t, "ack", r.Kind, "acquisition never acknowledges")
	}
}

func TestNodeFeedRejectsCorruptionAndStateLoss(t *testing.T) {
	for _, kind := range []string{"hash", "height", "body", "receipts", "version", "chain", "balance"} {
		t.Run(kind, func(t *testing.T) {
			s, server, _, _ := setup(t)
			server.SetHook(func(r nodefeed.Request, response *nodefeed.Response) {
				if r.Kind == "balance" {
					response.Balances = nil
					return
				}
				if r.Kind != "block" {
					return
				}
				switch kind {
				case "hash":
					response.Block.Hash[0] ^= 1
				case "height":
					response.Block.Height++
				case "body":
					response.Block.Body = []byte{0xc2, 0xc0, 0xc0}
				case "receipts":
					response.Block.Receipts = []byte{0xc0}
				case "version":
					response.Version++
				case "chain":
					response.ChainID++
				}
			})
			if kind == "balance" {
				hash, err := s.HashAt(t.Context(), 1)
				require.NoError(t, err)
				_, _, err = s.BalanceBatchAtHash(t.Context(), []common.Address{{}}, hash)
				require.ErrorContains(t, err, "incomplete")
			} else {
				_, _, err := s.BlockWithReceipts(t.Context(), 1)
				require.Error(t, err)
			}
		})
	}
}

func TestNodeFeedReconnectAndBounds(t *testing.T) {
	s, server, _, _ := setup(t)
	server.ResetSession()
	_, err := s.Head(t.Context())
	require.NoError(t, err)
	requests := server.Requests()
	hellos := 0
	for _, r := range requests {
		if r.Kind == "hello" {
			hellos++
		}
	}
	require.Equal(t, 2, hellos)
	server.SetFrameLength(feed.MaxFrame + 1)
	_, _, err = s.BlockWithReceipts(t.Context(), 1)
	require.ErrorContains(t, err, "frame bound")
}

func TestNodeFeedCloseCancelsAndJoins(t *testing.T) {
	s, server, _, _ := setup(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	var once sync.Once
	server.SetHook(func(r nodefeed.Request, _ *nodefeed.Response) {
		if r.Kind == "head" {
			once.Do(func() { close(entered) })
			<-release
		}
	})
	done := make(chan error, 1)
	go func() { _, err := s.Head(context.Background()); done <- err }()
	<-entered
	s.Close()
	require.ErrorIs(t, <-done, context.Canceled)
	s.Close()
}

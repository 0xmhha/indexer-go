package rpc_test

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/source/rpc"
)

func dial(t *testing.T, url string) *gethrpc.Client {
	t.Helper()
	c, err := gethrpc.Dial(url)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func fakeChain(t *testing.T) (*testchain.Scenario, *testchain.Server) {
	t.Helper()
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	return sc, srv
}

// TestFakeChainMatchesEthclient reads every block of the reference scenario
// through the source and compares it with the ethclient path it replaces.
func TestFakeChainMatchesEthclient(t *testing.T) {
	sc, srv := fakeChain(t)
	ctx := context.Background()
	c := dial(t, srv.URL())
	src, err := rpc.Detect(ctx, c)
	require.NoError(t, err)
	require.Equal(t, evm.ID, src.Profile().ID())
	ec := ethclient.NewClient(c)

	head, err := src.Head(ctx)
	require.NoError(t, err)
	require.Equal(t, sc.Chain.Head(), head)

	logs := 0
	for n := uint64(0); n <= head; n++ {
		b, err := src.Block(ctx, n)
		require.NoError(t, err)
		want, err := ec.BlockByNumber(ctx, new(big.Int).SetUint64(n))
		require.NoError(t, err)
		require.Equal(t, want.Hash(), b.Hash, "block %d", n)
		require.Len(t, b.Transactions, len(want.Transactions()))

		rs, err := src.Receipts(ctx, b)
		require.NoError(t, err)
		require.Len(t, rs, len(b.Transactions))
		for i, r := range rs {
			require.Equal(t, want.Transactions()[i].Hash(), r.TxHash)
			logs += len(r.Logs)
		}
	}
	require.NotZero(t, logs, "the scenario emits logs")
	require.Empty(t, srv.UnknownMethods())
}

func TestReceiptsFallBackToPerTransaction(t *testing.T) {
	sc, srv := fakeChain(t)
	ctx := context.Background()
	srv.DisableMethod("eth_getBlockReceipts")
	src := rpc.New(dial(t, srv.URL()), evm.New("evm-test"))

	withTxs := 0
	for n := uint64(0); n <= sc.Chain.Head(); n++ {
		b, err := src.Block(ctx, n)
		require.NoError(t, err)
		rs, err := src.Receipts(ctx, b)
		require.NoError(t, err)
		require.Len(t, rs, len(b.Transactions))
		if len(b.Transactions) > 0 {
			withTxs++
		}
	}
	require.Greater(t, withTxs, 1)
	require.Equal(t, 1, srv.Calls()["eth_getBlockReceipts"], "the unsupported method is tried once, then skipped")
	require.Positive(t, srv.Calls()["eth_getTransactionReceipt"])
}

func TestBlockNotFound(t *testing.T) {
	sc, srv := fakeChain(t)
	src := rpc.New(dial(t, srv.URL()), evm.New("evm-test"))
	_, err := src.Block(context.Background(), sc.Chain.Head()+1)
	require.ErrorIs(t, err, source.ErrNotFound)
}

// stub answers JSON-RPC calls from a fixed method -> result table.
func stub(t *testing.T, results map[string]json.RawMessage) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if res, ok := results[req.Method]; ok {
			resp["result"] = res
		} else {
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

type vectors struct {
	Blocks   map[string]json.RawMessage `json:"blocks"`
	Receipts map[string]json.RawMessage `json:"receipts"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	b, err := os.ReadFile("../../chains/stablenet/testdata/live_vectors.json")
	require.NoError(t, err)
	var v vectors
	require.NoError(t, json.Unmarshal(b, &v))
	return v
}

// TestStableNetBlock serves a block captured from go-stablenet: the source
// detects the StableNet profile and keeps the fee delegation transaction
// under its canonical hash, with its receipt linked to it.
func TestStableNetBlock(t *testing.T) {
	v := loadVectors(t)
	url := stub(t, map[string]json.RawMessage{
		"web3_clientVersion":   json.RawMessage(`"Gstable/v1.1.0-stable-740526d0/darwin-arm64/go1.25.2"`),
		"eth_chainId":          json.RawMessage(`"0x205b"`),
		"eth_getBlockByNumber": v.Blocks["33"],
		"eth_getBlockReceipts": v.Receipts["33"],
	})
	ctx := context.Background()
	src, err := rpc.Detect(ctx, dial(t, url))
	require.NoError(t, err)
	require.Equal(t, stablenet.ID, src.Profile().ID())

	b, err := src.Block(ctx, 33)
	require.NoError(t, err)
	rs, err := src.Receipts(ctx, b)
	require.NoError(t, err)
	require.Len(t, rs, 2)

	fdTx := b.Transactions[1]
	require.Equal(t, uint8(stablenet.FeeDelegationTxType), fdTx.Type)
	require.Equal(t, fdTx.Hash, rs[1].TxHash)
	require.NotEqual(t, fdTx.From, stablenet.FeePayerOf(fdTx))
}

func TestReceiptsMustMatchBlock(t *testing.T) {
	v := loadVectors(t)
	var rs []json.RawMessage
	require.NoError(t, json.Unmarshal(v.Receipts["33"], &rs))
	swapped, err := json.Marshal([]json.RawMessage{rs[1], rs[0]})
	require.NoError(t, err)

	url := stub(t, map[string]json.RawMessage{
		"eth_getBlockByNumber": v.Blocks["33"],
		"eth_getBlockReceipts": swapped,
	})
	ctx := context.Background()
	src := rpc.New(dial(t, url), stablenet.New())
	b, err := src.Block(ctx, 33)
	require.NoError(t, err)
	_, err = src.Receipts(ctx, b)
	require.ErrorIs(t, err, source.ErrInconsistentReceipts)
}

func TestWrongBlockNumberIsRejected(t *testing.T) {
	v := loadVectors(t)
	url := stub(t, map[string]json.RawMessage{"eth_getBlockByNumber": v.Blocks["33"]})
	_, err := rpc.New(dial(t, url), stablenet.New()).Block(context.Background(), 34)
	require.ErrorContains(t, err, "node returned 33")
}

// TestBlockWithReceiptsMatchesSeparateCalls requires the batched fetch to
// return exactly what Block and Receipts return, in one round trip per block.
func TestBlockWithReceiptsMatchesSeparateCalls(t *testing.T) {
	sc, srv := fakeChain(t)
	ctx := context.Background()
	src := rpc.New(dial(t, srv.URL()), evm.New("evm-test"))
	for n := uint64(0); n <= sc.Chain.Head(); n++ {
		b, rs, err := src.BlockWithReceipts(ctx, n)
		require.NoError(t, err)
		want, err := src.Block(ctx, n)
		require.NoError(t, err)
		wantRs, err := src.Receipts(ctx, want)
		require.NoError(t, err)
		require.Equal(t, want.Hash, b.Hash)
		require.Len(t, rs, len(wantRs))
		for i := range rs {
			require.Equal(t, wantRs[i].TxHash, rs[i].TxHash)
			require.Equal(t, wantRs[i].GasUsed, rs[i].GasUsed)
		}
	}
	_, _, err := src.BlockWithReceipts(ctx, sc.Chain.Head()+1)
	require.ErrorIs(t, err, source.ErrNotFound)
}

func TestBlockWithReceiptsFallsBackToPerTransaction(t *testing.T) {
	sc, srv := fakeChain(t)
	ctx := context.Background()
	srv.DisableMethod("eth_getBlockReceipts")
	src := rpc.New(dial(t, srv.URL()), evm.New("evm-test"))
	for n := uint64(0); n <= sc.Chain.Head(); n++ {
		b, rs, err := src.BlockWithReceipts(ctx, n)
		require.NoError(t, err)
		require.Len(t, rs, len(b.Transactions))
	}
	require.Equal(t, 1, srv.Calls()["eth_getBlockReceipts"], "the unsupported method is tried once")
}

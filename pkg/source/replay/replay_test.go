package replay_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/source/replay"
)

type probe struct {
	method string
	args   []any
}

// probes are the kinds of calls the indexer makes.
func probes(sc *testchain.Scenario) []probe {
	head := hexutil.EncodeUint64(sc.Chain.Head())
	return []probe{
		{"web3_clientVersion", nil},
		{"eth_chainId", nil},
		{"eth_getBlockByNumber", []any{"0x3", true}},
		{"eth_getBlockByNumber", []any{"0x3", false}},
		{"eth_getBlockReceipts", []any{"0x3"}},
		{"eth_getBlockByNumber", []any{head, true}},
		{"eth_getBalance", []any{sc.Accounts[0].Address, "0x2"}},
	}
}

func call(t *testing.T, c *gethrpc.Client, method string, args ...any) json.RawMessage {
	t.Helper()
	var out json.RawMessage
	require.NoError(t, c.CallContext(context.Background(), &out, method, args...), method)
	return out
}

func TestRecordAndReplay(t *testing.T) {
	sc := testchain.BuildDefault()
	node := testchain.NewServer(sc.Chain)
	defer node.Close()
	dir := t.TempDir()

	// Record through the proxy, including one batch.
	w, err := replay.NewWriter(dir)
	require.NoError(t, err)
	rec, err := replay.Serve(replay.NewRecorder(node.URL(), w))
	require.NoError(t, err)
	rc, err := gethrpc.Dial(rec.URL)
	require.NoError(t, err)
	want := map[int]json.RawMessage{}
	for i, p := range probes(sc) {
		want[i] = call(t, rc, p.method, p.args...)
	}
	var b5, r5 json.RawMessage
	require.NoError(t, rc.BatchCallContext(context.Background(), []gethrpc.BatchElem{
		{Method: "eth_getBlockByNumber", Args: []any{"0x5", true}, Result: &b5},
		{Method: "eth_getBlockReceipts", Args: []any{"0x5"}, Result: &r5},
	}))
	_ = call(t, rc, "eth_getBlockByNumber", "latest", true) // tags are not recorded
	rc.Close()
	require.NoError(t, rec.Close())
	require.NoError(t, w.Close())

	for _, compress := range []bool{false, true} {
		if compress {
			require.NoError(t, replay.Compress(dir))
		}
		a, err := replay.Open(dir)
		require.NoError(t, err)
		require.Equal(t, sc.Chain.Head(), a.Manifest().Last)
		srv := replay.NewServer(a)
		ep, err := replay.Serve(srv)
		require.NoError(t, err)
		c, err := gethrpc.Dial(ep.URL)
		require.NoError(t, err)

		for i, p := range probes(sc) {
			require.JSONEq(t, string(want[i]), string(call(t, c, p.method, p.args...)), "%s %v", p.method, p.args)
		}
		require.JSONEq(t, string(b5), string(call(t, c, "eth_getBlockByNumber", "0x5", true)))
		require.JSONEq(t, string(want[5]), string(call(t, c, "eth_getBlockByNumber", "latest", true)), "latest is the last recorded block")
		require.Equal(t, `"`+hexutil.EncodeUint64(sc.Chain.Head())+`"`, string(call(t, c, "eth_blockNumber")))
		require.Equal(t, "null", string(call(t, c, "eth_getBlockByNumber", hexutil.EncodeUint64(sc.Chain.Head()+1), true)), "beyond the head")

		// Lookups by hash come from the recorded blocks.
		var blk struct {
			Hash         common.Hash `json:"hash"`
			Transactions []struct {
				Hash common.Hash `json:"hash"`
			} `json:"transactions"`
		}
		require.NoError(t, json.Unmarshal(b5, &blk))
		require.JSONEq(t, string(b5), string(call(t, c, "eth_getBlockByHash", blk.Hash, true)))
		require.NotEmpty(t, blk.Transactions)
		var receipts []json.RawMessage
		require.NoError(t, json.Unmarshal(r5, &receipts))
		require.JSONEq(t, string(receipts[0]), string(call(t, c, "eth_getTransactionReceipt", blk.Transactions[0].Hash)))

		require.Empty(t, srv.Unrecorded())
		var out json.RawMessage
		require.Error(t, c.CallContext(context.Background(), &out, "eth_getBalance", sc.Accounts[1].Address, "0x2"))
		require.Equal(t, map[string]int{"eth_getBalance": 1}, srv.Unrecorded())
		c.Close()
		require.NoError(t, ep.Close())
	}
}

func TestParseEndpoint(t *testing.T) {
	for in, want := range map[string]string{"replay:///tmp/a": "/tmp/a", "replay:rel/dir": "rel/dir"} {
		dir, ok := replay.ParseEndpoint(in)
		require.True(t, ok)
		require.Equal(t, want, dir)
	}
	_, ok := replay.ParseEndpoint("http://127.0.0.1:8545")
	require.False(t, ok)
}

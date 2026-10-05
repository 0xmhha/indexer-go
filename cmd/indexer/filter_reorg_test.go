package main

import (
	"context"
	"encoding/json"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
)

// TestFilterChangesAcrossReorg polls JSON-RPC filters created before a
// reorganization. The log filter must first return every log of the
// removed blocks it had seen, marked removed, then the logs of the new
// branch; the block filter must return the new branch's block hashes.
func TestFilterChangesAcrossReorg(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()
	keep := head / 2

	app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))

	rpc := jsonrpc.NewServer(app.storage, zap.NewNop())
	call := func(method string, params ...any) any {
		raw, err := json.Marshal(params)
		require.NoError(t, err)
		res, rpcErr := rpc.HandleMethodDirect(ctx, method, raw)
		require.Nil(t, rpcErr, "%s: %v", method, rpcErr)
		return res
	}
	logFilter := call("eth_newFilter", map[string]any{}).(string)
	blockFilter := call("eth_newBlockFilter").(string)

	var wantRemoved []*types.Log // newest block first, logs in reverse
	for n := head; n > keep; n-- {
		b := sc.Chain.Block(n)
		for i := len(b.Receipts) - 1; i >= 0; i-- {
			for j := len(b.Receipts[i].Logs) - 1; j >= 0; j-- {
				wantRemoved = append(wantRemoved, b.Receipts[i].Logs[j])
			}
		}
	}
	require.NotEmpty(t, wantRemoved)

	// New branch, longer than the old one, with one log.
	sc.Chain.Reorg(keep)
	from, to := sc.Accounts[3], sc.Accounts[0]
	emitter := common.HexToAddress("0x00000000000000000000000000000000000000e1")
	for i := 0; i < int(head-keep)+2; i++ {
		spec := testchain.TxSpec{From: from, Tx: &types.LegacyTx{To: &to.Address, Value: big.NewInt(int64(i + 1)), Gas: 21000, GasPrice: big.NewInt(1_000_000_000)}}
		if i == 0 {
			spec.Logs = []*types.Log{{Address: emitter, Topics: []common.Hash{{0xee}}, Data: []byte{1}}}
		}
		sc.Chain.AddBlockWithExtra([]byte("fork"), spec)
	}
	newHead := sc.Chain.Head()

	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(loopCtx) }()
	require.Eventually(t, func() bool {
		h, err := app.storage.GetLatestHeight(ctx)
		return err == nil && h == newHead
	}, time.Minute, 20*time.Millisecond)
	stop()
	<-done

	logs := call("eth_getFilterChanges", logFilter).([]any)
	require.Len(t, logs, len(wantRemoved)+1)
	for i, want := range wantRemoved {
		l := logs[i].(map[string]any)
		require.Equal(t, true, l["removed"], "log %d", i)
		require.Equal(t, want.TxHash.Hex(), l["transactionHash"])
		require.Equal(t, want.BlockHash.Hex(), l["blockHash"])
	}
	added := logs[len(wantRemoved)].(map[string]any)
	require.Equal(t, false, added["removed"])
	require.Equal(t, emitter.Hex(), common.HexToAddress(added["address"].(string)).Hex())

	hashes := call("eth_getFilterChanges", blockFilter).([]string)
	require.Len(t, hashes, int(newHead-keep))
	for i, h := range hashes {
		require.Equal(t, sc.Chain.Block(keep+1+uint64(i)).Block.Hash().Hex(), h)
	}

	require.Empty(t, call("eth_getFilterChanges", logFilter), "nothing new on the next poll")
	require.Empty(t, call("eth_getFilterChanges", blockFilter))
}

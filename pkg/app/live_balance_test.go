package app

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// TestLiveBalances indexes the most recent blocks of a running node and
// compares every native balance the indexer recorded with eth_getBalance at
// the same block. It checks the chain's fee and value rules (on StableNet:
// native transfer logs, the gas tip, the base fee distribution and fee
// payers) against the node's own state.
//
// The node must still hold the state of the indexed blocks: a full node
// keeps the last 128, so the default window is 100 blocks. Skipped unless
// INDEXER_LIVE_RPC is set, e.g.
//
//	INDEXER_LIVE_RPC=http://127.0.0.1:8600 \
//	INDEXER_LIVE_BALANCE_BLOCKS=100 \
//	go test ./pkg/app -run TestLiveBalances -v
func TestLiveBalances(t *testing.T) {
	rpc := os.Getenv("INDEXER_LIVE_RPC")
	if rpc == "" {
		t.Skip("INDEXER_LIVE_RPC not set")
	}
	window := uint64(100)
	if v := os.Getenv("INDEXER_LIVE_BALANCE_BLOCKS"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		require.NoError(t, err, "INDEXER_LIVE_BALANCE_BLOCKS")
		window = n
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ec, err := ethclient.DialContext(ctx, rpc)
	require.NoError(t, err)
	defer ec.Close()
	head, err := ec.BlockNumber(ctx)
	require.NoError(t, err)
	from := uint64(1)
	if head > window {
		from = head - window
	}
	t.Logf("indexing blocks %d..%d from %s", from, head, rpc)

	app := startAppAt(t, rpc, filepath.Join(t.TempDir(), "db"), atomicMode)
	defer app.Shutdown()
	require.NoError(t, app.fetcher.FetchRange(ctx, from, head))

	// Every account with a recorded balance has a latest entry.
	var accounts []common.Address
	require.NoError(t, app.storage.(port.KVStore).Iterate(ctx, []byte("/index/balance/"), func(key, _ []byte) bool {
		if k := string(key); strings.HasSuffix(k, "/latest") {
			accounts = append(accounts, common.HexToAddress(strings.TrimSuffix(strings.TrimPrefix(k, "/index/balance/"), "/latest")))
		}
		return true
	}))
	if len(accounts) == 0 {
		t.Skipf("no transactions in blocks %d..%d; send some and rerun", from, head)
	}

	r := app.storage.(port.HistoricalReader)
	var mismatches []string
	snapshots := 0
	for _, addr := range accounts {
		hist, _, err := r.GetBalanceHistory(ctx, addr, from, head, port.FirstPage(1<<20))
		require.NoError(t, err)
		for i, s := range hist {
			// A block can have several entries (the starting balance of an
			// account seen for the first time, then one per transaction);
			// the last one is the balance after the block.
			if i+1 < len(hist) && hist[i+1].BlockNumber == s.BlockNumber {
				continue
			}
			snapshots++
			want, err := ec.BalanceAt(ctx, addr, new(big.Int).SetUint64(s.BlockNumber))
			require.NoError(t, err, "eth_getBalance %s at %d", addr.Hex(), s.BlockNumber)
			if s.Balance.Cmp(want) != 0 {
				mismatches = append(mismatches, fmt.Sprintf("%s after block %d: indexed %s, node %s", addr.Hex(), s.BlockNumber, s.Balance, want))
				break // the first divergence per account is enough
			}
		}
	}
	t.Logf("%d accounts, %d block balances compared", len(accounts), snapshots)
	require.Empty(t, mismatches, "indexed native balances differ from the node")
}

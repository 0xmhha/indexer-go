package main

import (
	"bytes"
	"context"
	"math/big"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/source"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// TestLiveStableNetIdentity indexes a running StableNet node and checks that
// everything is stored under the identities the chain reports (D13, D16):
//
//   - every block is found by the node's block hash, every transaction and
//     receipt by the node's transaction hash;
//   - fee delegation transactions (type 0x16) keep their type, their fee payer
//     is recorded and indexed, and their logs are indexed;
//   - the hashes the go-ethereum rules would give instead (the Ethereum block
//     hash of WBFT headers, the inner hash of fee delegation transactions)
//     appear nowhere in the database, in keys or values.
//
// Skipped unless INDEXER_LIVE_RPC is set.
func TestLiveStableNetIdentity(t *testing.T) {
	endpoint := os.Getenv("INDEXER_LIVE_RPC")
	if endpoint == "" {
		t.Skip("INDEXER_LIVE_RPC not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	c, err := rpc.DialContext(ctx, endpoint)
	require.NoError(t, err)
	defer c.Close()
	src, err := source.Detect(ctx, c)
	require.NoError(t, err)
	require.Equal(t, stablenet.ID, src.Profile().ID())
	head, err := src.Head(ctx)
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "db")
	app := startAppAt(t, endpoint, dir, atomicMode)
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))

	mr, ok := app.storage.(storage.ModelReader)
	require.True(t, ok)
	fdReader, ok := app.storage.(storage.FeeDelegationReader)
	require.True(t, ok)
	logReader, ok := app.storage.(storage.LogReader)
	require.True(t, ok)

	// wrong maps each hash that must not be stored to what it stands for.
	// The inner hash of a fee delegation transaction is kept on purpose in
	// the transaction's own record (the fee delegation extension), so only
	// that place is allowed to hold it.
	wrong := map[common.Hash]string{}
	innerHash := map[common.Hash]bool{}
	// expected balance changes of fee delegation transactions, per account.
	type change struct {
		addr  common.Address
		block uint64
		tx    common.Hash
		delta *big.Int
		role  string
	}
	var changes []change
	var txs, fdTxs, logs int
	for n := uint64(0); n <= head; n++ {
		b, err := src.Block(ctx, n)
		require.NoError(t, err)
		rs, err := src.Receipts(ctx, b)
		require.NoError(t, err)

		stored, err := mr.GetModelBlockByHash(ctx, b.Hash)
		require.NoError(t, err, "block %d by node hash %s", n, b.Hash.Hex())
		require.Equal(t, n, stored.Number)
		if g, err := gethconv.BlockToGeth(b); err == nil && g.Hash() != b.Hash {
			wrong[g.Hash()] = "Ethereum-rule hash of block " + b.Hash.Hex()
		}

		blockLogs := 0
		for i, tx := range b.Transactions {
			txs++
			got, loc, err := mr.GetModelTransaction(ctx, tx.Hash)
			require.NoError(t, err, "tx %s", tx.Hash.Hex())
			require.Equal(t, tx.Type, got.Type)
			require.Equal(t, uint64(i), loc.TxIndex)
			require.Equal(t, b.Hash, loc.BlockHash)
			r, err := mr.GetModelReceipt(ctx, tx.Hash)
			require.NoError(t, err, "receipt %s", tx.Hash.Hex())
			require.Equal(t, rs[i].GasUsed, r.GasUsed)
			blockLogs += len(rs[i].Logs)

			fd, isFD := stablenet.FeeDelegationOf(tx)
			if !isFD {
				continue
			}
			fdTxs++
			wrong[fd.SenderHash] = "inner hash of fee delegation tx " + tx.Hash.Hex()
			innerHash[fd.SenderHash] = true
			gas := new(big.Int).Mul(new(big.Int).SetUint64(rs[i].GasUsed), rs[i].EffectiveGasPrice)
			changes = append(changes,
				change{fd.FeePayer, n, tx.Hash, new(big.Int).Neg(gas), "fee payer"},
				change{tx.From, n, tx.Hash, new(big.Int).Neg(tx.Value), "sender"})
			meta, err := fdReader.GetFeeDelegationTxMeta(ctx, tx.Hash)
			require.NoError(t, err)
			require.NotNil(t, meta, "fee delegation meta of %s", tx.Hash.Hex())
			require.Equal(t, fd.FeePayer, meta.FeePayer)
			payerTxs, err := app.storage.GetTransactionsByAddress(ctx, fd.FeePayer, 10000, 0)
			require.NoError(t, err)
			require.Contains(t, payerTxs, tx.Hash, "fee payer address index")
		}
		indexed, err := logReader.GetLogsByBlock(ctx, n)
		require.NoError(t, err)
		require.Len(t, indexed, blockLogs, "logs of block %d", n)
		logs += blockLogs
	}

	// The gas of a fee delegation transaction is charged to the fee payer and
	// only the value to the sender. Compare the recorded change per
	// transaction (whole-account balances also move by untracked income such
	// as validator fees, so they are not comparable with the node).
	hist, ok := app.storage.(storage.HistoricalReader)
	require.True(t, ok)
	for _, c := range changes {
		snaps, err := hist.GetBalanceHistory(ctx, c.addr, c.block, c.block, 100, 0)
		require.NoError(t, err)
		got := new(big.Int)
		for _, s := range snaps {
			if s.TxHash == c.tx {
				got.Add(got, s.Delta)
			}
		}
		require.Zero(t, got.Cmp(c.delta), "%s %s change in %s: recorded %s, expected %s", c.role, c.addr.Hex(), c.tx.Hex(), got, c.delta)
	}
	app.Shutdown()
	t.Logf("blocks 0..%d: %d txs (%d fee delegation), %d logs, %d hashes that must not appear, %d balance changes checked", head, txs, fdTxs, logs, len(wrong), len(changes))
	require.Positive(t, fdTxs, "the chain needs fee delegation transactions for this test")

	// Look for the wrong hashes anywhere in the database.
	leaks := map[string]int{}
	for _, e := range dumpDir(t, dir) {
		family := keyFamily(e.Key)
		for h, what := range wrong {
			if innerHash[h] && (family == "/data/blocks" || family == "/data/txs") && !bytes.Contains(e.Key, h.Bytes()) {
				continue
			}
			forms := [][]byte{h.Bytes(), []byte(h.Hex()), []byte(strings.TrimPrefix(h.Hex(), "0x"))}
			for _, f := range forms {
				if bytes.Contains(e.Key, f) || bytes.Contains(e.Value, f) {
					leaks[family+" <- "+what[:strings.Index(what, " of ")]]++
					break
				}
			}
		}
	}
	var report []string
	for k, n := range leaks {
		report = append(report, fmt.Sprintf("%s: %d", k, n))
	}
	sort.Strings(report)
	require.Empty(t, report, "hashes computed with go-ethereum rules are stored")
}

// keyFamily returns the first two path segments of a key, e.g. /data/blocks.
func keyFamily(k []byte) string {
	parts := strings.SplitN(string(k), "/", 4)
	if len(parts) < 3 {
		return string(k)
	}
	return "/" + parts[1] + "/" + parts[2]
}

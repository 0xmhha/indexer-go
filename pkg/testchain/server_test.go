package testchain

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

// TestServerServesConsistentChain checks that go-ethereum's client decodes
// every scenario block and receipt and that hashes match the built chain.
func TestServerServesConsistentChain(t *testing.T) {
	sc := BuildDefault()
	srv := NewServer(sc.Chain)
	defer srv.Close()

	ctx := context.Background()
	ec, err := ethclient.DialContext(ctx, srv.URL())
	require.NoError(t, err)
	defer ec.Close()

	chainID, err := ec.ChainID(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(DefaultChainID), chainID.Int64())

	head, err := ec.BlockNumber(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(sc.Chain.Len()-1), head)

	for n := uint64(0); n <= head; n++ {
		want := sc.Chain.blocks[n]
		got, err := ec.BlockByNumber(ctx, new(big.Int).SetUint64(n))
		require.NoError(t, err, "block %d", n)
		require.Equal(t, want.Block.Hash(), got.Hash(), "block %d hash", n)
		require.Equal(t, len(want.Block.Transactions()), len(got.Transactions()))

		receipts, err := ec.BlockReceipts(ctx, rpcBlockNumber(n))
		require.NoError(t, err, "receipts %d", n)
		require.Len(t, receipts, len(want.Receipts))
		for i, r := range receipts {
			require.Equal(t, want.Receipts[i].TxHash, r.TxHash)
			require.Equal(t, want.Receipts[i].Status, r.Status)
			require.Len(t, r.Logs, len(want.Receipts[i].Logs))
		}
	}
	require.Empty(t, srv.UnknownMethods())
}

func TestServerHidesBlocksAboveHead(t *testing.T) {
	sc := BuildDefault()
	sc.Chain.SetHead(3)
	srv := NewServer(sc.Chain)
	defer srv.Close()

	ctx := context.Background()
	ec, err := ethclient.DialContext(ctx, srv.URL())
	require.NoError(t, err)
	defer ec.Close()

	head, err := ec.BlockNumber(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(3), head)

	_, err = ec.BlockByNumber(ctx, big.NewInt(4))
	require.Error(t, err)
}

func TestBalancesFollowIndexerRule(t *testing.T) {
	sc := BuildDefault()
	a := sc.Accounts[0].Address
	// Genesis allocation is visible at block 0 and never negative later.
	require.Equal(t, ether(1000), sc.Chain.balanceAt(a, 0))
	for n := uint64(0); n < uint64(sc.Chain.Len()); n++ {
		require.GreaterOrEqual(t, sc.Chain.balanceAt(a, n).Sign(), 0)
	}
}

func rpcBlockNumber(n uint64) rpc.BlockNumberOrHash {
	return rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(n))
}

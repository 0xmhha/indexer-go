package arc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/arc"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// RPC block, receipts and eth_getBalance at heights 66904956/66904957
// were fetched from rpc.testnet.arc.io on 2026-10-13, independently of
// the accounting implementation. Ethereum's rules failed on this block.
func TestLiveBalanceOracle(t *testing.T) {
	checkBalanceOracle(t, 66904957)
}

// Independent before/after node states collected on 2026-10-14 cover
// current system-emitter mint, burn, non-top-level moves and failed fees.
func TestExpandedBalanceOracles(t *testing.T) {
	for _, height := range []uint64{67017992, 67018549, 67018552, 67018564} {
		t.Run(fmt.Sprint(height), func(t *testing.T) { checkBalanceOracle(t, height) })
	}
}

func checkBalanceOracle(t *testing.T, height uint64) {
	t.Helper()
	p := arc.New()
	raw, err := os.ReadFile(fmt.Sprintf("testdata/block-%d.json", height))
	require.NoError(t, err)
	b, err := p.DecodeBlock(raw)
	require.NoError(t, err)
	raw, err = os.ReadFile(fmt.Sprintf("testdata/receipts-%d.json", height))
	require.NoError(t, err)
	rs, err := p.DecodeReceipts(raw)
	require.NoError(t, err)
	deltas, err := chains.AccountingOf(p).NativeDeltas(context.Background(), b, rs, nil)
	require.NoError(t, err)
	got := totals(deltas)
	raw, err = os.ReadFile(fmt.Sprintf("testdata/balances-%d.json", height))
	require.NoError(t, err)
	var balances map[common.Address]struct{ Before, After string }
	require.NoError(t, json.Unmarshal(raw, &balances))
	for addr, balance := range balances {
		before, ok := new(big.Int).SetString(balance.Before, 10)
		require.True(t, ok)
		after, ok := new(big.Int).SetString(balance.After, 10)
		require.True(t, ok)
		want := after.Sub(after, before)
		if got[addr] == nil {
			got[addr] = new(big.Int)
		}
		require.Zero(t, want.Cmp(got[addr]), "balance delta for %s: node %s, indexed %s", addr, want, got[addr])
	}
	for addr := range got {
		_, ok := balances[addr]
		require.True(t, ok, "oracle includes every changed address")
	}
	// The node's miner delta rejects Ethereum's base-fee burn assumption.
	old, err := (chains.EthereumAccounting{}).NativeDeltas(context.Background(), b, rs, nil)
	require.NoError(t, err)
	require.NotZero(t, totals(old)[b.Miner].Cmp(got[b.Miner]))
}

func totals(ds []chains.BalanceDelta) map[common.Address]*big.Int {
	got := map[common.Address]*big.Int{}
	for _, d := range ds {
		if got[d.Address] == nil {
			got[d.Address] = new(big.Int)
		}
		got[d.Address].Add(got[d.Address], d.Delta)
	}
	return got
}

func TestNativeMovesAreCountedOnce(t *testing.T) {
	from, to := common.HexToAddress("0x01"), common.HexToAddress("0x02")
	amount := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	event := &model.Log{Address: arc.NativeEmitter, Topics: []common.Hash{
		crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")),
		common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes()),
	}, Data: common.LeftPadBytes(amount.Bytes(), 32)}
	erc := *event
	erc.Address = common.HexToAddress("0x3600000000000000000000000000000000000000")
	erc.Data = common.LeftPadBytes(big.NewInt(1_000_000).Bytes(), 32)
	b := &model.Block{Transactions: []*model.Transaction{{From: from, To: &to, Value: amount}}}
	rs := []*model.Receipt{{Status: 1, Logs: []*model.Log{event, &erc}}}
	deltas, err := (arc.Accounting{}).NativeDeltas(context.Background(), b, rs, nil)
	require.NoError(t, err)
	got := totals(deltas)
	require.Equal(t, amount.String(), got[to].String())
	require.Equal(t, new(big.Int).Neg(amount).String(), got[from].String())
	event.Data = nil
	_, err = (arc.Accounting{}).NativeDeltas(context.Background(), b, rs, nil)
	require.ErrorContains(t, err, "malformed native transfer event")
}

func TestLegacyMintAndBurn(t *testing.T) {
	addr := common.HexToAddress("0x02")
	var logs []*model.Log
	for _, name := range []string{"NativeCoinMinted(address,uint256)", "NativeCoinBurned(address,uint256)"} {
		logs = append(logs, &model.Log{Address: common.HexToAddress("0x1800000000000000000000000000000000000000"),
			Topics: []common.Hash{crypto.Keccak256Hash([]byte(name)), common.BytesToHash(addr.Bytes())},
			Data:   common.LeftPadBytes(big.NewInt(7).Bytes(), 32)})
	}
	b := &model.Block{Transactions: []*model.Transaction{{}}}
	deltas, err := (arc.Accounting{}).NativeDeltas(context.Background(), b, []*model.Receipt{{Status: 1, Logs: logs}}, nil)
	require.NoError(t, err)
	require.Len(t, deltas, 2)
	require.Zero(t, totals(deltas)[addr].Sign())
	_, zero := totals(deltas)[common.Address{}]
	require.False(t, zero, "mint and burn do not create a zero-address balance")
}

func TestDetection(t *testing.T) {
	for _, id := range []uint64{5042, 5042002} {
		p, err := chains.Detect(chains.NodeInfo{ChainID: id})
		require.NoError(t, err)
		require.Equal(t, arc.ID, p.ID())
	}
}

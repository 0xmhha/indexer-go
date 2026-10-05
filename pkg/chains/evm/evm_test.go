package evm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

type rawSource struct {
	rpc *rpc.Client
	ec  *ethclient.Client
}

func dial(t *testing.T, sc *testchain.Scenario) *rawSource {
	t.Helper()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	c, err := rpc.Dial(srv.URL())
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return &rawSource{rpc: c, ec: ethclient.NewClient(c)}
}

func (s *rawSource) block(t *testing.T, n uint64) json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	require.NoError(t, s.rpc.CallContext(context.Background(), &raw, "eth_getBlockByNumber", hexNum(n), true))
	return raw
}

func (s *rawSource) receipts(t *testing.T, n uint64) json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	require.NoError(t, s.rpc.CallContext(context.Background(), &raw, "eth_getBlockReceipts", hexNum(n)))
	return raw
}

func hexNum(n uint64) string { return "0x" + new(big.Int).SetUint64(n).Text(16) }

// TestDecodeMatchesGoEthereum is the CP-1 acceptance check: for every block of
// the reference and load scenarios, the profile's model must carry the same
// hashes, senders, fields and canonical encodings as go-ethereum's decoding.
func TestDecodeMatchesGoEthereum(t *testing.T) {
	p := evm.New("evm-test")
	for name, sc := range map[string]*testchain.Scenario{
		"default": testchain.BuildDefault(),
		"load":    testchain.BuildLoad(10, 20, 0),
	} {
		t.Run(name, func(t *testing.T) {
			src := dial(t, sc)
			ctx := context.Background()
			for n := uint64(0); n <= sc.Chain.Head(); n++ {
				got, err := p.DecodeBlock(src.block(t, n))
				require.NoError(t, err, "block %d", n)
				want, err := src.ec.BlockByNumber(ctx, new(big.Int).SetUint64(n))
				require.NoError(t, err)

				require.Equal(t, want.Hash(), got.Hash)
				require.Equal(t, want.NumberU64(), got.Number)
				require.Equal(t, want.ParentHash(), got.ParentHash)
				require.Equal(t, want.Time(), got.Time)
				require.Equal(t, want.GasUsed(), got.GasUsed)
				require.Len(t, got.Transactions, len(want.Transactions()))
				for i, wtx := range want.Transactions() {
					checkTx(t, wtx, got.Transactions[i])
					require.Equal(t, uint(i), got.Transactions[i].Index)
					require.Equal(t, got.Hash, got.Transactions[i].BlockHash)
				}

				gotR, err := p.DecodeReceipts(src.receipts(t, n))
				require.NoError(t, err)
				wantR, err := src.ec.BlockReceipts(ctx, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(n)))
				require.NoError(t, err)
				require.Len(t, gotR, len(wantR))
				for i, wr := range wantR {
					gr := gotR[i]
					require.Equal(t, wr.TxHash, gr.TxHash)
					require.Equal(t, wr.Type, gr.Type)
					require.Equal(t, wr.Status, gr.Status)
					require.Equal(t, wr.GasUsed, gr.GasUsed)
					require.Equal(t, wr.CumulativeGasUsed, gr.CumulativeGasUsed)
					require.Len(t, gr.Logs, len(wr.Logs))
					for j, wl := range wr.Logs {
						require.Equal(t, wl.Address, gr.Logs[j].Address)
						require.Equal(t, wl.Topics, gr.Logs[j].Topics)
						require.Equal(t, wl.Index, gr.Logs[j].Index)
					}
				}
			}
		})
	}
}

func checkTx(t *testing.T, want *types.Transaction, got *model.Transaction) {
	t.Helper()
	require.Equal(t, want.Hash(), got.Hash)
	require.Equal(t, want.Type(), got.Type)
	from, err := types.Sender(types.LatestSignerForChainID(want.ChainId()), want)
	require.NoError(t, err)
	require.Equal(t, from, got.From)
	require.Equal(t, want.Nonce(), got.Nonce)
	require.Equal(t, want.To(), got.To)
	require.Zero(t, want.Value().Cmp(got.Value))
	require.Equal(t, want.Gas(), got.Gas)
	require.Zero(t, want.GasPrice().Cmp(got.GasPrice))
	require.True(t, bytes.Equal(want.Data(), got.Input))
	enc, err := want.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, enc, got.Raw, "canonical encoding")
	require.Len(t, got.AuthList, len(want.SetCodeAuthorizations()))
	require.False(t, got.Opaque)
}

// firstTxJSON returns block n's first transaction object as a JSON map.
func firstTxJSON(t *testing.T, src *rawSource, n uint64) map[string]any {
	t.Helper()
	var blk map[string]any
	require.NoError(t, json.Unmarshal(src.block(t, n), &blk))
	return blk["transactions"].([]any)[0].(map[string]any)
}

func blockWithTx(t *testing.T, src *rawSource, n uint64, tx map[string]any) json.RawMessage {
	t.Helper()
	var blk map[string]any
	require.NoError(t, json.Unmarshal(src.block(t, n), &blk))
	blk["transactions"] = []any{tx}
	out, err := json.Marshal(blk)
	require.NoError(t, err)
	return out
}

func TestUnknownTypeIsKeptOpaque(t *testing.T) {
	sc := testchain.BuildDefault()
	src := dial(t, sc)
	tx := firstTxJSON(t, src, 1)
	tx["type"] = "0x7f"

	b, err := evm.New("evm-test").DecodeBlock(blockWithTx(t, src, 1, tx))
	require.NoError(t, err, "an unknown type must not fail the block")
	got := b.Transactions[0]
	require.True(t, got.Opaque)
	require.Equal(t, uint8(0x7f), got.Type)
	require.Equal(t, tx["hash"], got.Hash.Hex())
	require.Equal(t, strings.ToLower(tx["from"].(string)), strings.ToLower(got.From.Hex()))
	require.Empty(t, got.Raw)
}

func TestReportedHashAndSenderAreVerified(t *testing.T) {
	sc := testchain.BuildDefault()
	src := dial(t, sc)
	p := evm.New("evm-test", evm.WithHeaderHashCheck(false))

	tx := firstTxJSON(t, src, 1)
	tx["hash"] = "0x" + strings.Repeat("ab", 32)
	_, err := p.DecodeBlock(blockWithTx(t, src, 1, tx))
	require.ErrorIs(t, err, evm.ErrHashMismatch)

	tx = firstTxJSON(t, src, 1)
	tx["from"] = "0x00000000000000000000000000000000000000ff"
	_, err = p.DecodeBlock(blockWithTx(t, src, 1, tx))
	require.ErrorIs(t, err, evm.ErrSenderMismatch)
}

func TestHeaderHashIsVerified(t *testing.T) {
	sc := testchain.BuildDefault()
	src := dial(t, sc)
	var blk map[string]any
	require.NoError(t, json.Unmarshal(src.block(t, 2), &blk))
	blk["hash"] = "0x" + strings.Repeat("cd", 32)
	raw, err := json.Marshal(blk)
	require.NoError(t, err)

	_, err = evm.New("evm-test").DecodeBlock(raw)
	require.ErrorIs(t, err, evm.ErrHashMismatch)
	_, err = evm.New("evm-test", evm.WithHeaderHashCheck(false)).DecodeBlock(raw)
	require.NoError(t, err, "chains with extra header fields can opt out")
}

func TestChainSpecificDecoderTakesPrecedence(t *testing.T) {
	sc := testchain.BuildDefault()
	src := dial(t, sc)
	tx := firstTxJSON(t, src, 1)
	tx["type"] = "0x16"

	called := false
	p := evm.New("custom", evm.WithTxDecoder(0x16, func(raw json.RawMessage) (*model.Transaction, error) {
		called = true
		return &model.Transaction{Type: 0x16}, nil
	}))
	b, err := p.DecodeBlock(blockWithTx(t, src, 1, tx))
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, uint8(0x16), b.Transactions[0].Type)
	require.Equal(t, uint(0), b.Transactions[0].Index, "position fields are filled by the profile")
}

// TestParallelDecodeMatchesGoEthereum covers blocks large enough to decode
// their transactions on several cores: order, hashes and senders must match
// go-ethereum exactly.
func TestParallelDecodeMatchesGoEthereum(t *testing.T) {
	sc := testchain.BuildLoad(3, 150, 0)
	src := dial(t, sc)
	p := evm.New("evm-test")
	ctx := context.Background()
	large := 0
	for n := uint64(1); n <= sc.Chain.Head(); n++ {
		got, err := p.DecodeBlock(src.block(t, n))
		require.NoError(t, err)
		want, err := src.ec.BlockByNumber(ctx, new(big.Int).SetUint64(n))
		require.NoError(t, err)
		require.Len(t, got.Transactions, len(want.Transactions()))
		if len(got.Transactions) >= 150 {
			large++
		}
		for i, wtx := range want.Transactions() {
			checkTx(t, wtx, got.Transactions[i])
			require.Equal(t, uint(i), got.Transactions[i].Index)
		}
	}
	require.Positive(t, large, "the scenario has blocks above the parallel threshold")
}

func TestParallelDecodeReportsFailingTransaction(t *testing.T) {
	sc := testchain.BuildLoad(1, 60, 0)
	src := dial(t, sc)
	var blk map[string]any
	require.NoError(t, json.Unmarshal(src.block(t, sc.Chain.Head()), &blk))
	txs := blk["transactions"].([]any)
	txs[41].(map[string]any)["hash"] = "0x" + strings.Repeat("ab", 32) // forged
	raw, err := json.Marshal(blk)
	require.NoError(t, err)
	_, err = evm.New("evm-test").DecodeBlock(raw)
	require.ErrorIs(t, err, evm.ErrHashMismatch)
	require.ErrorContains(t, err, "tx 41")
}

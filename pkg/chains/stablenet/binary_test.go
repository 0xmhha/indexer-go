package stablenet_test

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// consensusReceipts encodes JSON receipts in their consensus form, as era1
// archives store them.
func consensusReceipts(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &items))
	out := make([]any, len(items))
	for i, item := range items {
		var r types.Receipt
		require.NoError(t, r.UnmarshalJSON(item))
		enc, err := rlp.EncodeToBytes([]any{[]byte{byte(r.Status)}, r.CumulativeGasUsed, r.Bloom, r.Logs})
		require.NoError(t, err)
		if r.Type == types.LegacyTxType {
			out[i] = rlp.RawValue(enc)
		} else {
			out[i] = append([]byte{r.Type}, enc...)
		}
	}
	b, err := rlp.EncodeToBytes(out)
	require.NoError(t, err)
	return b
}

// TestBinaryMatchesLiveJSON rebuilds the consensus encodings of the live
// vector blocks (block 33 holds a fee delegation transaction) and requires
// the binary decoding to equal the JSON decoding, including the derived
// receipt fields and effective gas prices.
func TestBinaryMatchesLiveJSON(t *testing.T) {
	v := loadVectors(t)
	p := stablenet.New()
	var _ chains.BinaryProfile = p

	for n, raw := range v.Blocks {
		want, err := p.DecodeBlock(raw)
		require.NoError(t, err)

		var head types.Header
		require.NoError(t, json.Unmarshal(raw, &head))
		header, err := rlp.EncodeToBytes(&head)
		require.NoError(t, err)
		txs := make([]any, len(want.Transactions))
		for i, tx := range want.Transactions {
			enc := hexutil.MustDecode(v.RawTransactions[tx.Hash.Hex()])
			if enc[0] > 0x7f {
				txs[i] = rlp.RawValue(enc)
			} else {
				txs[i] = enc
			}
		}
		body, err := rlp.EncodeToBytes([]any{txs, []any{}})
		require.NoError(t, err)

		got, err := p.DecodeBlockRLP(header, body)
		require.NoError(t, err)
		requireSameBlock(t, want, got, "block %s", n)

		if rr, ok := v.Receipts[n]; ok {
			wantR, err := p.DecodeReceipts(rr)
			require.NoError(t, err)
			gotR, err := p.DeriveReceipts(got, consensusReceipts(t, rr))
			require.NoError(t, err)
			require.Len(t, gotR, len(wantR))
			for i := range wantR {
				requireSameReceipt(t, wantR[i], gotR[i], "block %s receipt %d", n, i)
			}
		}
	}
}

// requireSameBlock compares blocks by their storage encoding, which is what
// the indexer keeps; it ignores representation details such as a zero
// big.Int with or without backing words.
func requireSameBlock(t *testing.T, want, got *model.Block, msg string, args ...any) {
	t.Helper()
	we, err := model.EncodeBlock(want)
	require.NoError(t, err)
	ge, err := model.EncodeBlock(got)
	require.NoError(t, err)
	require.Equal(t, we, ge, append([]any{msg}, args...)...)
}

func requireSameReceipt(t *testing.T, want, got *model.Receipt, msg string, args ...any) {
	t.Helper()
	we, err := model.EncodeReceipt(want)
	require.NoError(t, err)
	ge, err := model.EncodeReceipt(got)
	require.NoError(t, err)
	require.Equal(t, we, ge, append([]any{msg}, args...)...)
}

func TestDecodeFeeDelegationTxBinaryMarksBadFeePayerSignature(t *testing.T) {
	v := loadVectors(t)
	for _, h := range v.RawTransactions {
		enc := hexutil.MustDecode(h)
		if enc[0] != stablenet.FeeDelegationTxType {
			continue
		}
		_, err := stablenet.DecodeFeeDelegationTxBinary(enc)
		require.NoError(t, err)
		bad := append([]byte(nil), enc...)
		bad[len(bad)-1] ^= 1 // fee payer signature
		tx, err := stablenet.DecodeFeeDelegationTxBinary(bad)
		require.NoError(t, err, "accepted as go-stablenet v1.0.0 consensus did")
		fd, _ := stablenet.FeeDelegationOf(tx)
		require.True(t, fd.Invalid)
		return
	}
	t.Fatal("no fee delegation transaction in the vectors")
}

func wbftExtra(t *testing.T, gasTip *big.Int) []byte {
	t.Helper()
	b, err := rlp.EncodeToBytes([]any{[]byte{}, []byte{}, uint32(0), []any{}, []any{}, uint32(0), []any{}, []any{}, gasTip, []any{}})
	require.NoError(t, err)
	return b
}

func TestAnzeonEffectiveGasPrice(t *testing.T) {
	tx := &model.Transaction{GasTipCap: big.NewInt(3), GasFeeCap: big.NewInt(100), GasPrice: big.NewInt(100)}
	plain := &model.Receipt{}
	authorized := &model.Receipt{Logs: []*model.Log{{
		Address: stablenet.AccountManagerAddress, Topics: []common.Hash{stablenet.AuthorizedTxExecutedTopic},
	}}}
	block := &model.Block{BaseFee: big.NewInt(10), Extra: wbftExtra(t, big.NewInt(7))}

	require.Equal(t, big.NewInt(7), stablenet.HeaderGasTip(block.Extra))
	require.Nil(t, stablenet.HeaderGasTip(wbftExtra(t, nil)), "an empty tip is no tip")
	require.Equal(t, big.NewInt(17), stablenet.EffectiveGasPrice(block, tx, plain), "header tip")
	require.Equal(t, big.NewInt(13), stablenet.EffectiveGasPrice(block, tx, authorized), "own tip")
	require.Equal(t, big.NewInt(13), stablenet.EffectiveGasPrice(&model.Block{BaseFee: big.NewInt(10)}, tx, plain), "no WBFT extra")
	require.Equal(t, big.NewInt(100), stablenet.EffectiveGasPrice(&model.Block{Extra: block.Extra}, tx, plain), "no base fee")
	capped := &model.Block{BaseFee: big.NewInt(95), Extra: block.Extra}
	require.Equal(t, big.NewInt(100), stablenet.EffectiveGasPrice(capped, tx, plain), "fee cap")
}

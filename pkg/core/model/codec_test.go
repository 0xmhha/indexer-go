package model_test

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestRoundTripFakeChain encodes every block and receipt of the reference and
// load scenarios and requires decode(encode(x)) to encode to the same bytes
// and to keep the identifying fields.
func TestRoundTripFakeChain(t *testing.T) {
	p := evm.New("evm-test")
	for name, sc := range map[string]*testchain.Scenario{
		"default": testchain.BuildDefault(),
		"load":    testchain.BuildLoad(5, 20, 0),
	} {
		t.Run(name, func(t *testing.T) {
			srv := testchain.NewServer(sc.Chain)
			t.Cleanup(srv.Close)
			c, err := rpc.Dial(srv.URL())
			require.NoError(t, err)
			t.Cleanup(c.Close)
			ctx := context.Background()

			creations := 0
			for n := uint64(0); n <= sc.Chain.Head(); n++ {
				var raw json.RawMessage
				require.NoError(t, c.CallContext(ctx, &raw, "eth_getBlockByNumber", hexutil.EncodeUint64(n), true))
				b, err := p.DecodeBlock(raw)
				require.NoError(t, err)

				enc, err := model.EncodeBlock(b)
				require.NoError(t, err)
				got, err := model.DecodeBlock(enc)
				require.NoError(t, err)
				again, err := model.EncodeBlock(got)
				require.NoError(t, err)
				require.Equal(t, enc, again, "block %d", n)
				require.Equal(t, b.Hash, got.Hash)
				require.Len(t, got.Transactions, len(b.Transactions))
				for i, tx := range b.Transactions {
					g := got.Transactions[i]
					require.Equal(t, tx.Hash, g.Hash)
					require.Equal(t, tx.Raw, g.Raw)
					require.Equal(t, tx.From, g.From)
					require.Equal(t, tx.To == nil, g.To == nil)
					require.Equal(t, tx.ChainID == nil, g.ChainID == nil)
					if tx.To == nil {
						creations++
					}
					txEnc, err := model.EncodeTransaction(tx)
					require.NoError(t, err)
					gtx, err := model.DecodeTransaction(txEnc)
					require.NoError(t, err)
					require.Equal(t, tx.Hash, gtx.Hash)
				}

				raw = nil
				require.NoError(t, c.CallContext(ctx, &raw, "eth_getBlockReceipts", hexutil.EncodeUint64(n)))
				rs, err := p.DecodeReceipts(raw)
				require.NoError(t, err)
				for _, r := range rs {
					enc, err := model.EncodeReceipt(r)
					require.NoError(t, err)
					got, err := model.DecodeReceipt(enc)
					require.NoError(t, err)
					again, err := model.EncodeReceipt(got)
					require.NoError(t, err)
					require.Equal(t, enc, again)
					require.Equal(t, r.TxHash, got.TxHash)
					require.Len(t, got.Logs, len(r.Logs))
					require.Equal(t, r.ContractAddress, got.ContractAddress)
				}
			}
			if name == "default" {
				require.Positive(t, creations, "the reference scenario creates contracts")
			}
		})
	}
}

func TestNilAndZeroStayDistinct(t *testing.T) {
	zero := new(big.Int)
	b := &model.Block{Number: 1, BaseFee: zero, Transactions: []*model.Transaction{
		{Value: zero, ChainID: nil, To: &common.Address{}},
	}}
	enc, err := model.EncodeBlock(b)
	require.NoError(t, err)
	got, err := model.DecodeBlock(enc)
	require.NoError(t, err)
	require.NotNil(t, got.BaseFee)
	require.Zero(t, got.BaseFee.Sign())
	require.Nil(t, got.Difficulty)
	require.Nil(t, got.WithdrawalsRoot)
	tx := got.Transactions[0]
	require.NotNil(t, tx.Value)
	require.Nil(t, tx.ChainID)
	require.NotNil(t, tx.To, "a transfer to the zero address is not a contract creation")
}

func TestOtherEncodingVersionIsRejected(t *testing.T) {
	enc, err := model.EncodeReceipt(&model.Receipt{Status: 1})
	require.NoError(t, err)
	enc[0] = model.EncodingVersion + 1
	_, err = model.DecodeReceipt(enc)
	require.ErrorIs(t, err, model.ErrEncodingVersion)
	_, err = model.DecodeReceipt(nil)
	require.ErrorIs(t, err, model.ErrEncodingVersion)
}

func TestExtensions(t *testing.T) {
	stored := model.NewExtKey("model_test.stored")
	model.RegisterExtCodec(stored, model.ExtCodec{
		Encode: func(v any) ([]byte, error) { return []byte(v.(string)), nil },
		Decode: func(b []byte) (any, error) { return string(b), nil },
	})

	r := &model.Receipt{}
	r.Ext.Set(stored, "kept")
	enc, err := model.EncodeReceipt(r)
	require.NoError(t, err)
	got, err := model.DecodeReceipt(enc)
	require.NoError(t, err)
	require.Equal(t, "kept", got.Ext.Get(stored))

	unregistered := model.NewExtKey("model_test.unregistered")
	r.Ext.Set(unregistered, "lost")
	_, err = model.EncodeReceipt(r)
	require.ErrorIs(t, err, model.ErrUnknownExtension, "an extension that cannot be stored must not be dropped silently")

	require.Panics(t, func() {
		model.RegisterExtCodec(model.NewExtKey("model_test.stored"), model.ExtCodec{})
	})
}

func TestUnclesAndWithdrawals(t *testing.T) {
	for name, ws := range map[string][]model.Withdrawal{
		"absent": nil,
		"empty":  {},
		"some":   {{Index: 1, Validator: 2, Address: common.Address{3}, Amount: 4}},
	} {
		t.Run(name, func(t *testing.T) {
			b := &model.Block{Number: 1, Uncles: []common.Hash{{1}, {2}}, Withdrawals: ws}
			enc, err := model.EncodeBlock(b)
			require.NoError(t, err)
			got, err := model.DecodeBlock(enc)
			require.NoError(t, err)
			require.Equal(t, b.Uncles, got.Uncles)
			require.Equal(t, ws, got.Withdrawals)
		})
	}
}

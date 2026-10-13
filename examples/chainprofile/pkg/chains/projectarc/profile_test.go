package projectarc_test

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/examples/chainprofile/pkg/chains/projectarc"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/chains/arc"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

func TestSelection(t *testing.T) {
	for _, tc := range []struct {
		chain uint64
		id    string
	}{
		{5042002, projectarc.ID},
		{5042, arc.ID},
		{11155111, evm.ID},
	} {
		p, err := chains.Detect(chains.NodeInfo{ChainID: tc.chain})
		require.NoError(t, err)
		require.Equal(t, tc.id, p.ID())
	}
	p, ok := chains.Lookup("project-arc-testnet")
	require.True(t, ok)
	require.Equal(t, projectarc.ID, p.ID())
}

// Dropping the optional interfaces when wrapping a profile would silently
// restore Ethereum accounting and classify the native emitter as an ERC-20.
func TestInheritedAccounting(t *testing.T) {
	p := projectarc.New()
	emitter, ok := chains.NativeCoinContract(p)
	require.True(t, ok)
	require.Equal(t, arc.NativeEmitter, emitter)

	sender := common.HexToAddress("0x1111")
	miner := common.HexToAddress("0x2222")
	to := common.HexToAddress("0x3333")
	b := &model.Block{
		Miner: miner, BaseFee: big.NewInt(10),
		Transactions: []*model.Transaction{{From: sender, To: &to, Value: big.NewInt(7), GasPrice: big.NewInt(15)}},
	}
	rs := []*model.Receipt{{Status: 1, GasUsed: 3, EffectiveGasPrice: big.NewInt(15)}}
	deltas, err := chains.AccountingOf(p).NativeDeltas(context.Background(), b, rs, nil)
	require.NoError(t, err)
	totals := map[common.Address]*big.Int{}
	for _, d := range deltas {
		if totals[d.Address] == nil {
			totals[d.Address] = new(big.Int)
		}
		totals[d.Address].Add(totals[d.Address], d.Delta)
	}
	require.Equal(t, map[common.Address]*big.Int{sender: big.NewInt(-45), miner: big.NewInt(45)}, totals,
		"Arc credits the full gas fee and reads value moves from native logs, not tx.value")
	require.Equal(t, int64(10), b.BaseFee.Int64(), "accounting must not mutate the header")
}

func TestVerificationSurvivesCustomization(t *testing.T) {
	account := testchain.NewAccount(1)
	to := common.HexToAddress("0x3333")
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(5042002), Gas: 21000, To: &to,
		GasFeeCap: big.NewInt(15), GasTipCap: big.NewInt(5), Value: big.NewInt(7),
	}), types.LatestSignerForChainID(big.NewInt(5042002)), account.Key)
	require.NoError(t, err)
	txJSON, err := tx.MarshalJSON()
	require.NoError(t, err)
	var txFields map[string]any
	require.NoError(t, json.Unmarshal(txJSON, &txFields))
	txFields["from"] = account.Address.Hex()
	h := &types.Header{Number: big.NewInt(1), Difficulty: new(big.Int), GasLimit: 30000000, BaseFee: big.NewInt(10)}
	headJSON, err := h.MarshalJSON()
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(headJSON, &fields))
	fields["hash"] = h.Hash().Hex()
	fields["transactions"] = []any{txFields}
	decode := func() (*model.Block, error) {
		raw, err := json.Marshal(fields)
		require.NoError(t, err)
		return projectarc.New().DecodeBlock(raw)
	}
	b, err := decode()
	require.NoError(t, err)
	require.Equal(t, h.Hash(), b.Hash)
	require.Equal(t, tx.Hash(), b.Transactions[0].Hash)
	require.Equal(t, account.Address, b.Transactions[0].From)

	fields["hash"] = common.HexToHash("0xbad").Hex()
	_, err = decode()
	require.ErrorIs(t, err, evm.ErrHashMismatch)
	fields["hash"] = h.Hash().Hex()
	txFields["hash"] = common.HexToHash("0xbad").Hex()
	_, err = decode()
	require.ErrorIs(t, err, evm.ErrHashMismatch)
	txFields["hash"] = tx.Hash().Hex()
	txFields["from"] = to.Hex()
	_, err = decode()
	require.ErrorIs(t, err, evm.ErrSenderMismatch)
}

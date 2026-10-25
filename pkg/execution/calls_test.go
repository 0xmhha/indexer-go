package execution

import (
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func tracedPreparedFixture(t *testing.T, failed ...bool) (execwire.Chain, execwire.Result) {
	t.Helper()
	a := testchain.NewAccount(1)
	to := common.Address{2}
	chain, result, raw := preparedFixture(t, testchain.TxSpec{From: a, Failed: len(failed) > 0 && failed[0], Tx: &types.DynamicFeeTx{To: &to, Gas: 60000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(3)}})
	p, err := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, err)
	tx, receipt := p.block.Transactions[0], p.receipts[0]
	target, value := execwire.Address(*tx.To), execwire.Word{}
	tx.Value.FillBytes(value[:])
	result.CallTraces = &execwire.CallTraces{Version: 1, Profile: "evm", Frames: []execwire.CallFrame{{TransactionIndex: 0, TransactionHash: execwire.Hash(tx.Hash), Parent: -1, Type: "CALL", From: execwire.Address(tx.From), To: &target, Value: &value, Gas: tx.Gas, GasUsed: receipt.GasUsed, Input: append([]byte{}, tx.Input...), Output: []byte{}}}}
	if receipt.Status == 0 {
		result.CallTraces.Frames[0].Error = "execution reverted"
	}
	return chain, result
}

func TestPrepareCallTreeKeepsAncestorRevertedAttempts(t *testing.T) {
	chain, r := tracedPreparedFixture(t, true)
	child := r.CallTraces.Frames[0]
	child.Parent = 0
	child.Error = ""
	child.From = *child.To
	grandchild := child
	grandchild.Parent = 1
	grandchild.Type = "DELEGATECALL"
	sibling := child
	sibling.Type = "STATICCALL"
	sibling.Value = nil
	r.CallTraces.Frames = append(r.CallTraces.Frames, child, grandchild, sibling)
	raw, e := execwire.MarshalResult(r)
	require.NoError(t, e)
	p, e := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, e)
	require.Len(t, p.calls, 4)
	for i, f := range p.calls {
		require.True(t, f.Reverted)
		require.Equal(t, i > 0, f.AncestorReverted)
		require.Equal(t, i > 0, f.Internal)
	}
	require.Equal(t, []uint32{0, 0}, p.calls[2].Path)
	require.Equal(t, []uint32{1}, p.calls[3].Path)
	require.Nil(t, p.calls[3].Value, "absent STATICCALL value is not an inferred zero transfer")
}

func TestPrepareCallTraceRootsMatchVerifiedTransactionsAndReceipts(t *testing.T) {
	for name, mutate := range map[string]func(*execwire.Result){
		"from":     func(r *execwire.Result) { r.CallTraces.Frames[0].From[0] ^= 1 },
		"to":       func(r *execwire.Result) { r.CallTraces.Frames[0].To[0] ^= 1 },
		"value":    func(r *execwire.Result) { r.CallTraces.Frames[0].Value[0] ^= 1 },
		"gas":      func(r *execwire.Result) { r.CallTraces.Frames[0].Gas++ },
		"gas used": func(r *execwire.Result) { r.CallTraces.Frames[0].GasUsed++ },
		"input":    func(r *execwire.Result) { r.CallTraces.Frames[0].Input = []byte{1} },
		"status":   func(r *execwire.Result) { r.CallTraces.Frames[0].Error = "reverted" },
		"profile":  func(r *execwire.Result) { r.CallTraces.Profile = "arc" },
	} {
		t.Run(name, func(t *testing.T) {
			chain, r := tracedPreparedFixture(t)
			raw, e := execwire.MarshalResult(r)
			require.NoError(t, e)
			_, e = Prepare(evm.New("evm"), chain, raw)
			require.NoError(t, e)
			mutate(&r)
			raw, e = execwire.MarshalResult(r)
			require.NoError(t, e)
			_, e = Prepare(evm.New("evm"), chain, raw)
			require.Error(t, e, "a trace is not trusted merely because the journal roots match")
		})
	}
}

package execution

import (
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func preparedFixture(t *testing.T, specs ...testchain.TxSpec) (execwire.Chain, execwire.Result, []byte) {
	t.Helper()
	a := testchain.NewAccount(1)
	c := testchain.NewChain(1, map[common.Address]*big.Int{a.Address: big.NewInt(1_000_000_000_000_000_000)})
	b := c.AddBlock(specs...)
	h := b.Block.Header()
	h.Root = common.HexToHash("0x1234")
	block := types.NewBlockWithHeader(h).WithBody(*b.Block.Body())
	raw, err := rlp.EncodeToBytes(block)
	require.NoError(t, err)
	receipts, err := rlp.EncodeToBytes(b.Receipts)
	require.NoError(t, err)
	r := execwire.Result{Ref: execwire.Ref{Generation: 1, Height: 1, Hash: execwire.Hash(block.Hash())}, Parent: execwire.Hash(block.ParentHash()), Time: block.Time(), StateRoot: execwire.Hash(h.Root), ReceiptsRoot: execwire.Hash(h.ReceiptHash), TxCount: uint32(len(specs)), BlockRLP: raw, ReceiptsRLP: receipts}
	for i, tx := range block.Transactions() {
		r.Steps = append(r.Steps, execwire.Step{Ordinal: uint32(i), Cause: execwire.Transaction, TxIndex: int32(i), TxHash: execwire.Hash(tx.Hash())})
	}
	r.Steps = append(r.Steps, execwire.Step{Ordinal: uint32(len(specs)), Cause: execwire.Finalize, TxIndex: -1})
	payload, err := execwire.MarshalResult(r)
	require.NoError(t, err)
	return execwire.Chain{ID: "1", Genesis: execwire.Hash(c.Block(0).Block.Hash())}, r, payload
}
func TestPrepareAuthenticatesAndOwnsInput(t *testing.T) {
	chain, r, raw := preparedFixture(t)
	p, err := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, err)
	require.Equal(t, r, p.result)
	require.Equal(t, execwire.Hash(sha256.Sum256(raw)), p.digest)
	require.Equal(t, r.Ref.Hash, execwire.Hash(p.block.Hash))
	require.Greater(t, p.bytes, int64(len(raw)))
	raw[len(raw)-1] ^= 1
	require.Equal(t, r.BlockRLP, p.result.BlockRLP)
}
func TestPrepareRejectsJournalMetadataMismatch(t *testing.T) {
	chain, r, _ := preparedFixture(t)
	for _, mutate := range []func(*execwire.Result){
		func(r *execwire.Result) { r.Parent[0] ^= 1 }, func(r *execwire.Result) { r.StateRoot[0] ^= 1 }, func(r *execwire.Result) { r.Time++ }, func(r *execwire.Result) { r.ReceiptsRoot[0] ^= 1 }, func(r *execwire.Result) { r.Ref.Hash[0] ^= 1 },
	} {
		bad := r
		mutate(&bad)
		raw, err := execwire.MarshalResult(bad)
		require.NoError(t, err)
		_, err = Prepare(evm.New("evm"), chain, raw)
		require.Error(t, err)
	}
}
func TestPrepareCreationEvidenceIncludesEmptyAndExcludesFailure(t *testing.T) {
	a := testchain.NewAccount(1)
	tx := func() *types.DynamicFeeTx {
		return &types.DynamicFeeTx{Gas: 60000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(0)}
	}
	chain, _, raw := preparedFixture(t, testchain.TxSpec{From: a, Tx: tx(), Creates: true}, testchain.TxSpec{From: a, Tx: tx(), Creates: true, Failed: true})
	p, err := Prepare(evm.New("evm"), chain, raw)
	require.NoError(t, err)
	require.Len(t, p.creations, 1)
	require.Equal(t, execwire.Address(a.Address), p.creations[0].Creator)
	require.Equal(t, uint32(0), p.creations[0].TxIndex)
	require.Equal(t, execwire.Hash(p.block.Transactions[0].Hash), p.creations[0].TxHash)
}

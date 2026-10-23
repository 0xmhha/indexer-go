package evmdata_test

import (
	"math/big"
	"testing"

	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/testchain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func boundedFixture(t *testing.T) (model.FeedBlock, []byte, []byte, []byte) {
	t.Helper()
	a, to := testchain.NewAccount(1), testchain.NewAccount(2).Address
	c := testchain.NewChain(1, map[common.Address]*big.Int{a.Address: big.NewInt(1_000_000_000_000_000_000)})
	b := c.AddBlock(testchain.TxSpec{From: a, Tx: &types.DynamicFeeTx{To: &to, Gas: 21000, GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000), Value: big.NewInt(0)}, GasUsed: 21000})
	h, err := rlp.EncodeToBytes(b.Block.Header())
	require.NoError(t, err)
	body, err := rlp.EncodeToBytes(b.Block.Body())
	require.NoError(t, err)
	rs, err := rlp.EncodeToBytes(b.Receipts)
	require.NoError(t, err)
	return model.FeedBlock{Chain: model.FeedChain{ID: "1", GenesisHash: b.Block.ParentHash()}, Number: 1, Hash: b.Block.Hash()}, h, body, rs
}
func TestDecodeBoundedPreservesProfileCommitments(t *testing.T) {
	id, h, b, r := boundedFixture(t)
	p := evm.New("evm")
	want, wantR, err := evmdata.Decode(p, id, h, b, r)
	require.NoError(t, err)
	got, gotR, err := evmdata.DecodeBounded(p, id, h, b, r, evmdata.DecodeLimits{PartBytes: 64 << 20, Items: 65536, Logs: 262144})
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, wantR, gotR)
	bad := append([]byte(nil), r...)
	bad[len(bad)-1] ^= 1
	_, _, err = evmdata.DecodeBounded(p, id, h, b, bad, evmdata.DecodeLimits{PartBytes: 64 << 20, Items: 65536, Logs: 262144})
	require.Error(t, err)
}
func TestDecodeBoundedRejectsBeforeProfileAllocation(t *testing.T) {
	id, h, _, r := boundedFixture(t)
	txs, err := rlp.EncodeToBytes([]rlp.RawValue{{0xc0}, {0xc0}})
	require.NoError(t, err)
	b, err := rlp.EncodeToBytes([]rlp.RawValue{txs, {0xc0}})
	require.NoError(t, err)
	p := &guardedDecoder{}
	_, _, err = evmdata.DecodeBounded(p, id, h, b, r, evmdata.DecodeLimits{PartBytes: 64 << 20, Items: 1, Logs: 1})
	require.Error(t, err)
	require.False(t, p.called)
}

package evm

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"
)

func TestCheckpointHeaderAuthentication(t *testing.T) {
	raw, e := os.ReadFile("testdata/sepolia-header-11901474.json")
	require.NoError(t, e)
	for _, check := range []bool{true, false} {
		p := New("evm", WithHeaderHashCheck(check))
		header, e := p.EncodeHeaderJSON(raw)
		require.NoError(t, e)
		block, e := p.DecodeHeaderRLP(header)
		require.NoError(t, e)
		require.EqualValues(t, 11901474, block.Number)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &fields))
		fields["stateRoot"], e = json.Marshal(common.Hash{1})
		require.NoError(t, e)
		bad, e := json.Marshal(fields)
		require.NoError(t, e)
		_, e = p.EncodeHeaderJSON(bad)
		require.ErrorIs(t, e, ErrHashMismatch)
	}
	// Known new suffix commitments round-trip, including hex slotNumber.
	p := New("evm")
	h := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(0), GasLimit: 100000, Root: types.EmptyRootHash}
	zero := uint64(0)
	empty := types.EmptyWithdrawalsHash
	hash0 := common.Hash{}
	h.BaseFee = big.NewInt(0)
	h.WithdrawalsHash = &empty
	h.BlobGasUsed = &zero
	h.ExcessBlobGas = &zero
	h.ParentBeaconRoot = &hash0
	h.RequestsHash = &hash0
	slot := uint64(42)
	extra := &HeaderFields{SlotNumber: &slot}
	hash, e := p.hashHeader(h, extra)
	require.NoError(t, e)
	raw, e = json.Marshal(h)
	require.NoError(t, e)
	var fields map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &fields))
	fields["hash"] = hash
	fields["slotNumber"] = "0x2a"
	raw, e = json.Marshal(fields)
	require.NoError(t, e)
	header, e := p.EncodeHeaderJSON(raw)
	require.NoError(t, e)
	expected, e := encodeHeader(h, extra)
	require.NoError(t, e)
	require.Equal(t, expected, header)
	block, e := p.DecodeHeaderRLP(header)
	require.NoError(t, e)
	require.Equal(t, hash, block.Hash)
	// Oversized/truncated/overflow headers are not anchors.
	_, e = p.DecodeHeaderRLP([]byte{0xc0})
	require.Error(t, e)
	h.Number = new(big.Int).Lsh(big.NewInt(1), 65)
	header, e = rlp.EncodeToBytes(h)
	require.NoError(t, e)
	_, e = p.DecodeHeaderRLP(header)
	require.Error(t, e)
}

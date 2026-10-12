package evm_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/0xmhha/indexer-go/pkg/core/model"
)

// The fixture is the header of Sepolia block 11901474, fetched with
// eth_getBlockByNumber from https://ethereum-sepolia-rpc.publicnode.com
// on 2026-10-13. Transaction bodies were omitted; the header commitments
// and reported block hash are unchanged. It reproduces the live mismatch
// caused by dropping blockAccessListHash and slotNumber (EIP-7928/7843).
func TestExtendedHeaderMatchesLiveHash(t *testing.T) {
	raw, err := os.ReadFile("testdata/sepolia-header-11901474.json")
	require.NoError(t, err)
	p := evm.New("evm-test")
	b, err := p.DecodeBlock(raw)
	require.NoError(t, err)
	wantHash := common.HexToHash("0xad0cb0e98dc3938e8809dc242ba86b6dc6d8b84188c454aff16587bf4799f3d2")
	require.Equal(t, wantHash, b.Hash)
	fields, ok := evm.HeaderFieldsOf(b)
	require.True(t, ok)
	require.NotNil(t, fields.SlotNumber)
	require.Equal(t, uint64(0xad1133), *fields.SlotNumber)
	require.NotNil(t, fields.BlockAccessListHash)
	require.Equal(t, common.HexToHash("0x59d70f04686a5db0c5e47cb5360c45c12ad48afc8dc7f909cdaea3bbba89395f"), *fields.BlockAccessListHash)

	encoded, err := model.EncodeBlock(b)
	require.NoError(t, err)
	stored, err := model.DecodeBlock(encoded)
	require.NoError(t, err)
	storedFields, ok := evm.HeaderFieldsOf(stored)
	require.True(t, ok)
	require.Equal(t, fields, storedFields, "the storage codec retains the commitments")

	for _, field := range []string{"blockAccessListHash", "slotNumber"} {
		t.Run("changed "+field, func(t *testing.T) {
			var data map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &data))
			data[field] = json.RawMessage("null")
			mutated, err := json.Marshal(data)
			require.NoError(t, err)
			_, err = p.DecodeBlock(mutated)
			require.ErrorIs(t, err, evm.ErrHashMismatch)
		})
	}

	var header types.Header
	require.NoError(t, json.Unmarshal(raw, &header))
	base, err := rlp.EncodeToBytes(&header)
	require.NoError(t, err)
	var parts []rlp.RawValue
	require.NoError(t, rlp.DecodeBytes(base, &parts))
	accessList, err := rlp.EncodeToBytes(*fields.BlockAccessListHash)
	require.NoError(t, err)
	slot, err := rlp.EncodeToBytes(*fields.SlotNumber)
	require.NoError(t, err)
	parts = append(parts, accessList, slot)
	headerRLP, err := rlp.EncodeToBytes(parts)
	require.NoError(t, err)
	body, err := rlp.EncodeToBytes([]any{[]any{}, []any{}, b.Withdrawals})
	require.NoError(t, err)
	binary, err := p.DecodeBlockRLP(headerRLP, body)
	require.NoError(t, err)
	require.Equal(t, wantHash, binary.Hash)
	binaryFields, ok := evm.HeaderFieldsOf(binary)
	require.True(t, ok)
	require.Equal(t, fields, binaryFields)

	parts = append(parts, rlp.RawValue{0x01})
	unknown, err := rlp.EncodeToBytes(parts)
	require.NoError(t, err)
	_, err = p.DecodeBlockRLP(unknown, body)
	require.ErrorContains(t, err, "unsupported header field count")

	_, err = evm.New("custom", evm.WithHeaderHasher((*types.Header).Hash)).DecodeBlock(raw)
	require.ErrorContains(t, err, "matching custom hash rule", "do not replace a chain's custom hash rule")
}

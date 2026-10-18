package evmstate

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

type proofFixture struct {
	root            common.Hash
	selection       Selection
	bundle          Bundle
	account, absent common.Address
	slot, missing   common.Hash
	state           *trie.Trie
}

func ptr[T any](v T) *T { return &v }
func proofNodes(t *testing.T, tr *trie.Trie, key []byte) Nodes {
	t.Helper()
	db := memorydb.New()
	defer func() { require.NoError(t, db.Close()) }()
	require.NoError(t, tr.Prove(key, db))
	it := db.NewIterator(nil, nil)
	defer it.Release()
	out := Nodes{}
	for it.Next() {
		out = append(out, append(hexutil.Bytes{}, it.Value()...))
	}
	require.NoError(t, it.Error())
	return out
}
func fixture(t *testing.T) proofFixture {
	t.Helper()
	// Geth's in-memory trie builder/prover is independent of our verifier.
	// Empty tries need no backing node DB; all fixture nodes stay in memory.
	st, storage := trie.NewEmpty(nil), trie.NewEmpty(nil)
	f := proofFixture{account: common.Address{19: 1}, absent: common.Address{19: 2}, slot: common.Hash{31: 3}, missing: common.Hash{31: 4}, state: st}
	v, e := rlp.EncodeToBytes([]byte{42})
	require.NoError(t, e)
	require.NoError(t, storage.Update(crypto.Keccak256(f.slot[:]), v))
	code := hexutil.Bytes{0x60, 0x01, 0x60, 0x00, 0x55}
	codeHash := crypto.Keccak256Hash(code)
	root := storage.Hash()
	a := types.StateAccount{Nonce: 7, Balance: uint256.NewInt(123456), Root: root, CodeHash: codeHash[:]}
	v, e = rlp.EncodeToBytes(&a)
	require.NoError(t, e)
	require.NoError(t, st.Update(crypto.Keccak256(f.account[:]), v))
	f.root = st.Hash()
	f.selection = Selection{Targets: []Target{{Address: f.account, Code: true, Slots: []common.Hash{f.slot, f.missing}}, {Address: f.absent, Code: true, Slots: []common.Hash{f.slot}}}}
	f.bundle = Bundle{Version: 1, Selection: f.selection, Evidence: []Evidence{
		{Proof: Proof{Address: ptr(f.account), AccountProof: proofNodes(t, st, crypto.Keccak256(f.account[:])), Balance: (*hexutil.Big)(big.NewInt(123456)), Nonce: ptr(hexutil.Uint64(7)), CodeHash: &codeHash, StorageHash: &root, StorageProof: []StorageProof{
			{Key: f.slot.Hex(), Value: (*hexutil.Big)(big.NewInt(42)), Proof: proofNodes(t, storage, crypto.Keccak256(f.slot[:]))},
			{Key: "0x4", Value: new(hexutil.Big), Proof: proofNodes(t, storage, crypto.Keccak256(f.missing[:]))},
		}}, Code: &code},
		{Proof: Proof{Address: ptr(f.absent), AccountProof: proofNodes(t, st, crypto.Keccak256(f.absent[:])), Balance: new(hexutil.Big), Nonce: ptr(hexutil.Uint64(0)), CodeHash: ptr(common.Hash{}), StorageHash: ptr(common.Hash{}), StorageProof: []StorageProof{{Key: f.slot.Hex(), Value: new(hexutil.Big), Proof: Nodes{}}}}, Code: ptr(hexutil.Bytes{})},
	}}
	return f
}
func cloneBundle(t *testing.T, in Bundle) Bundle {
	b, e := json.Marshal(in)
	require.NoError(t, e)
	var out Bundle
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}
func TestProofCommitmentsAndAbsence(t *testing.T) {
	f := fixture(t)
	out, e := Verify(f.root, f.selection, cloneBundle(t, f.bundle))
	require.NoError(t, e)
	require.True(t, out[0].Exists)
	require.False(t, out[1].Exists)
	require.EqualValues(t, 7, out[0].Nonce)
	require.Equal(t, "123456", out[0].Balance.ToInt().String())
	require.Equal(t, common.Hash{31: 42}, out[0].Storage[0].Value)
	require.Zero(t, out[0].Storage[1].Value)
	require.Equal(t, types.EmptyCodeHash, out[1].CodeHash)
	require.NotNil(t, out[1].Code)
	ordered, first, e := f.selection.Normalize()
	require.NoError(t, e)
	ordered.Targets[0].Slots[0], ordered.Targets[0].Slots[1] = ordered.Targets[0].Slots[1], ordered.Targets[0].Slots[0]
	ordered.Targets[0], ordered.Targets[1] = ordered.Targets[1], ordered.Targets[0]
	_, second, e := ordered.Normalize()
	require.NoError(t, e)
	require.Equal(t, first, second)
	_, e = Verify(f.root, ordered, f.bundle)
	require.NoError(t, e)
	for name, mutate := range map[string]func(*Bundle){
		"balance":         func(b *Bundle) { b.Evidence[0].Proof.Balance = new(hexutil.Big) },
		"nonce":           func(b *Bundle) { b.Evidence[0].Proof.Nonce = ptr(hexutil.Uint64(8)) },
		"address":         func(b *Bundle) { b.Evidence[0].Proof.Address = ptr(f.absent) },
		"root":            func(b *Bundle) { b.Evidence[0].Proof.StorageHash = ptr(common.Hash{5}) },
		"codeHash":        func(b *Bundle) { b.Evidence[0].Proof.CodeHash = ptr(common.Hash{6}) },
		"code":            func(b *Bundle) { b.Evidence[0].Code = ptr(hexutil.Bytes{0}) },
		"missingCode":     func(b *Bundle) { b.Evidence[0].Code = nil },
		"missingPath":     func(b *Bundle) { b.Evidence[0].Proof.AccountProof = nil },
		"wrongPath":       func(b *Bundle) { b.Evidence[0].Proof.AccountProof[0][0] ^= 1 },
		"slotValue":       func(b *Bundle) { b.Evidence[0].Proof.StorageProof[0].Value = new(hexutil.Big) },
		"missingSlot":     func(b *Bundle) { b.Evidence[0].Proof.StorageProof = b.Evidence[0].Proof.StorageProof[:1] },
		"duplicateSlot":   func(b *Bundle) { b.Evidence[0].Proof.StorageProof[1] = b.Evidence[0].Proof.StorageProof[0] },
		"unprovenZero":    func(b *Bundle) { b.Evidence[0].Proof.StorageProof[1].Proof = nil },
		"inventedAbsence": func(b *Bundle) { b.Evidence[1].Proof.AccountProof = nil },
		"oversizeCode":    func(b *Bundle) { b.Evidence[0].Code = ptr(make(hexutil.Bytes, MaxCodeBytes+1)) },
		"selection":       func(b *Bundle) { b.Selection.Targets[0].Code = false },
	} {
		t.Run(name, func(t *testing.T) {
			b := cloneBundle(t, f.bundle)
			mutate(&b)
			_, e := Verify(f.root, f.selection, b)
			require.ErrorIs(t, e, ErrProof)
		})
	}
	_, e = Verify(common.Hash{99}, f.selection, f.bundle)
	require.ErrorIs(t, e, ErrProof)
}
func TestEmptyStateAndSelectionBounds(t *testing.T) {
	sel := Selection{Targets: []Target{{Code: true, Slots: []common.Hash{{}}}}}
	b := Bundle{Version: 1, Selection: sel, Evidence: []Evidence{{Proof: Proof{Address: ptr(common.Address{}), Balance: new(hexutil.Big), Nonce: ptr(hexutil.Uint64(0)), CodeHash: ptr(types.EmptyCodeHash), StorageHash: ptr(types.EmptyRootHash), StorageProof: []StorageProof{{Key: "0x0", Value: new(hexutil.Big), Proof: Nodes{}}}}, Code: ptr(hexutil.Bytes{})}}}
	out, e := Verify(types.EmptyRootHash, sel, cloneBundle(t, b))
	require.NoError(t, e)
	require.False(t, out[0].Exists)
	_, digest, e := sel.Normalize()
	require.NoError(t, e)
	sel.Targets[0].Code = false
	_, different, e := sel.Normalize()
	require.NoError(t, e)
	require.NotEqual(t, digest, different)
	for _, bad := range []Selection{{}, {Targets: make([]Target, MaxAccounts+1)}, {Targets: []Target{{Slots: make([]common.Hash, MaxSlots+1)}}}, {Targets: []Target{{}, {}}}, {Targets: []Target{{Slots: []common.Hash{{}, {}}}}}} {
		_, _, e := bad.Normalize()
		require.ErrorIs(t, e, ErrProof)
	}
	var p Proof
	require.Error(t, json.Unmarshal([]byte(`{}`), &p))
	require.Error(t, json.Unmarshal([]byte(`null`), &p))
	var nodes Nodes
	require.Error(t, json.Unmarshal([]byte(`["0x`+strings.Repeat("00", MaxNodeBytes+1)+`"]`), &nodes))
	require.Error(t, json.Unmarshal([]byte(`[`+strings.Repeat(`"0x80",`, MaxNodes)+`"0x80"]`), &nodes))
	var bundle Bundle
	require.Error(t, json.Unmarshal([]byte(`{"evidence":[`+strings.Repeat(`{},`, MaxAccounts)+`{}]}`), &bundle))
}

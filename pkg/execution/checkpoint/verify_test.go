package checkpoint

import (
	"bytes"
	"context"
	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/chains/evm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
)

func fixture(t *testing.T) (format.Manifest, []format.Record) {
	t.Helper()
	addr := execwire.Address{19: 1}
	slot := execwire.Word{31: 2}
	value := execwire.Word{31: 42}
	st := trie.NewStackTrie(nil)
	sv, _ := rlp.EncodeToBytes([]byte{42})
	require.NoError(t, st.Update(crypto.Keccak256(slot[:]), sv))
	a := execwire.Account{Exists: true, Nonce: 7, Extra: 9, StorageRoot: execwire.Hash(st.Hash()), CodeHash: execwire.Hash(crypto.Keccak256Hash([]byte{0}))}
	a.Balance[31] = 123
	at := trie.NewStackTrie(nil)
	// Optional fifth consensus field is StableNet Extra, including exact zero omission.
	av, _ := rlp.EncodeToBytes(struct {
		Nonce    uint64
		Balance  *big.Int
		Root     common.Hash
		CodeHash []byte
		Extra    uint64 `rlp:"optional"`
	}{a.Nonce, new(big.Int).SetBytes(a.Balance[:]), common.Hash(a.StorageRoot), a.CodeHash[:], a.Extra})
	require.NoError(t, at.Update(crypto.Keccak256(addr[:]), av))
	h := &types.Header{Number: big.NewInt(0), Root: at.Hash(), GasLimit: 30_000_000, Difficulty: big.NewInt(1)}
	header, _ := rlp.EncodeToBytes(h)
	m := format.Manifest{Version: 1, Mode: "genesis", Coverage: "full", Chain: execwire.Chain{ID: "1", Genesis: execwire.Hash(h.Hash())}, Ref: execwire.Ref{Generation: 1, Hash: execwire.Hash(h.Hash())}, Root: execwire.Hash(h.Root), Header: header}
	return m, []format.Record{{Kind: "manifest", Manifest: &m}, {Kind: "account", Address: addr, Account: &a, Code: []byte{0}}, {Kind: "storage", Address: addr, Slot: slot, Value: value}, {Kind: "end"}}
}
func encode(t *testing.T, rs []format.Record) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, r := range rs {
		require.NoError(t, format.Write(&b, r))
	}
	return b.Bytes()
}
func TestCheckpointFullRootsAndExtra(t *testing.T) {
	m, rs := fixture(t)
	out, err := Verify(t.Context(), bytes.NewReader(encode(t, rs)), m, evm.New("evm"), nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, out.Accounts)
	require.EqualValues(t, 1, out.Slots)
}
func TestCheckpointRejectsTamperingAndTruncation(t *testing.T) {
	m, rs := fixture(t)
	for _, name := range []string{"slot", "extra", "code", "omission", "end", "trailing", "identity", "coverage"} {
		t.Run(name, func(t *testing.T) {
			m2, r := fixture(t)
			switch name {
			case "slot":
				r[2].Value[31]++
			case "extra":
				r[1].Account.Extra++
			case "code":
				r[1].Code = []byte{1}
			case "omission":
				r = append(r[:2], r[3:]...)
			case "end":
				r = r[:3]
			case "trailing":
				r = append(r, r[3])
			case "identity":
				m2.Ref.Hash[0]++
			case "coverage":
				r[0].Manifest.Coverage = "partial"
			}
			_, err := Verify(t.Context(), bytes.NewReader(encode(t, r)), m2, evm.New("evm"), nil)
			require.Error(t, err)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Verify(ctx, bytes.NewReader(encode(t, rs)), m, evm.New("evm"), nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestCheckpointPartialInclusionAbsenceAndUnknownScope(t *testing.T) {
	m, rs := fixture(t)
	m.Mode = "checkpoint"
	m.Coverage = "partial"
	rs[0].Manifest = &m
	// A proof consists of the same single-leaf consensus trie node. Its leaf
	// encoding is obtained independently from Trie.Prove, never handcrafted.
	disk := rawdb.NewMemoryDatabase()
	defer func() { _ = disk.Close() }()
	db := triedb.NewDatabase(disk, nil)
	defer func() { _ = db.Close() }()
	at := trie.NewEmpty(db)
	value, err := AccountRLP(*rs[1].Account)
	require.NoError(t, err)
	require.NoError(t, at.Update(crypto.Keccak256(rs[1].Address[:]), value))
	getProof := func(tr *trie.Trie, key []byte) [][]byte {
		proof := memorydb.New()
		defer func() { _ = proof.Close() }()
		require.NoError(t, tr.Prove(key, proof))
		it := proof.NewIterator(nil, nil)
		defer it.Release()
		var nodes [][]byte
		for it.Next() {
			nodes = append(nodes, bytes.Clone(it.Value()))
		}
		require.NoError(t, it.Error())
		return nodes
	}
	rs[1].Proof = getProof(at, crypto.Keccak256(rs[1].Address[:]))
	st := trie.NewEmpty(db)
	sv, _ := rlp.EncodeToBytes([]byte{42})
	require.NoError(t, st.Update(crypto.Keccak256(rs[2].Slot[:]), sv))
	rs[2].Proof = getProof(st, crypto.Keccak256(rs[2].Slot[:]))
	_, err = Verify(t.Context(), bytes.NewReader(encode(t, rs)), m, evm.New("evm"), nil)
	require.NoError(t, err)
	// An explicitly proven absent account is known absent; no other address is covered.
	absent := format.Record{Kind: "account", Address: execwire.Address{19: 9}, Account: &execwire.Account{}}
	absent.Proof = getProof(at, crypto.Keccak256(absent.Address[:]))
	only := []format.Record{rs[0], absent, {Kind: "end"}}
	_, err = Verify(t.Context(), bytes.NewReader(encode(t, only)), m, evm.New("evm"), nil)
	require.NoError(t, err)
	only[1].Proof = nil
	_, err = Verify(t.Context(), bytes.NewReader(encode(t, only)), m, evm.New("evm"), nil)
	require.Error(t, err)
	rs[2].Value[31]++
	_, err = Verify(t.Context(), bytes.NewReader(encode(t, rs)), m, evm.New("evm"), nil)
	require.Error(t, err)
}

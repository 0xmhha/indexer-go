package checkpoint

import (
	"bytes"
	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	"math/big"
	"testing"
)

func TestExportFullActualStateWithExtraStorageAndCode(t *testing.T) {
	disk := rawdb.NewMemoryDatabase()
	defer func() { _ = disk.Close() }()
	db := state.NewDatabaseWithConfig(disk, &triedb.Config{Preimages: true})
	defer func() { _ = db.TrieDB().Close() }()
	s, err := state.New(types.EmptyRootHash, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	addr := common.Address{19: 1}
	s.SetBalance(addr, uint256.NewInt(123))
	s.SetExtra(addr, 9)
	s.SetCode(addr, []byte{0})
	s.SetState(addr, common.Hash{31: 2}, common.Hash{31: 42})
	root, err := s.Commit(0, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.TrieDB().Commit(root, false); err != nil {
		t.Fatal(err)
	}
	h := &types.Header{Number: big.NewInt(0), Root: root, Difficulty: big.NewInt(1)}
	header, _ := rlp.EncodeToBytes(h)
	m := format.Manifest{Version: 1, Mode: "genesis", Coverage: "full", Chain: execwire.Chain{ID: "1", Genesis: execwire.Hash(h.Hash())}, Ref: execwire.Ref{Generation: 1, Hash: execwire.Hash(h.Hash())}, Root: execwire.Hash(root), Header: header}
	var b bytes.Buffer
	if err = Export(t.Context(), &b, m, db, nil); err != nil {
		t.Fatal(err)
	}
	d := format.NewDecoder(&b)
	a, _ := d.Next()
	if a.Kind != "manifest" {
		t.Fatal("manifest missing")
	}
	a, _ = d.Next()
	if a.Kind != "account" || a.Account.Extra != 9 || a.Account.Balance[31] != 123 || !bytes.Equal(a.Code, []byte{0}) {
		t.Fatal("wrong account")
	}
	a, _ = d.Next()
	if a.Kind != "storage" || a.Value[31] != 42 {
		t.Fatal("missing slot")
	}
	a, _ = d.Next()
	if a.Kind != "end" {
		t.Fatal("missing completion")
	}
}

func TestExportPartialAbsentSlotsAreSecureSorted(t *testing.T) {
	disk := rawdb.NewMemoryDatabase()
	defer func() { _ = disk.Close() }()
	db := state.NewDatabase(disk)
	defer func() { _ = db.TrieDB().Close() }()
	a, b := execwire.Word{31: 1}, execwire.Word{31: 2}
	if bytes.Compare(crypto.Keccak256(a[:]), crypto.Keccak256(b[:])) < 0 {
		a, b = b, a
	}
	m := format.Manifest{Coverage: "partial", Root: execwire.Hash(types.EmptyRootHash)}
	var out bytes.Buffer
	if err := Export(t.Context(), &out, m, db, []Target{{Address: execwire.Address{1}, Slots: []execwire.Word{a, b}}}); err != nil {
		t.Fatal(err)
	}
	d := format.NewDecoder(&out)
	_, _ = d.Next()
	_, _ = d.Next()
	first, e := d.Next()
	if e != nil {
		t.Fatal(e)
	}
	second, e := d.Next()
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Compare(crypto.Keccak256(first.Slot[:]), crypto.Keccak256(second.Slot[:])) >= 0 {
		t.Fatal("absent slot selection was not normalized into secure-key order")
	}
}

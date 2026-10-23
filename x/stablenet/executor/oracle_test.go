package executor

import (
	"bytes"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

func leaves(t *testing.T, s state.Trie) map[string][]byte {
	t.Helper()
	nodes, err := s.NodeIterator(nil)
	if err != nil {
		t.Fatal(err)
	}
	it := trie.NewIterator(nodes)
	out := map[string][]byte{}
	for it.Next() {
		out[string(bytes.Clone(it.Key))] = bytes.Clone(it.Value)
	}
	if it.Err != nil {
		t.Fatal(it.Err)
	}
	return out
}

func readLeafAccount(t *testing.T, p []byte) *types.StateAccount {
	t.Helper()
	if p == nil {
		return nil
	}
	var a types.StateAccount
	if err := rlp.DecodeBytes(p, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

func checkAccountFields(t *testing.T, raw []byte, w execwire.Account) {
	t.Helper()
	a := readLeafAccount(t, raw)
	if a == nil {
		if w != (execwire.Account{}) {
			t.Fatal("journal invents an account")
		}
		return
	}
	if !w.Exists || w.Nonce != a.Nonce || w.Balance != execwire.Word(a.Balance.Bytes32()) || w.Extra != a.Extra || common.Hash(w.CodeHash) != common.BytesToHash(a.CodeHash) || common.Hash(w.StorageRoot) != a.Root {
		t.Fatal("journal differs from independently decoded account leaf")
	}
}

func storageWord(t *testing.T, p []byte) execwire.Word {
	t.Helper()
	if p == nil {
		return execwire.Word{}
	}
	var value []byte
	if err := rlp.DecodeBytes(p, &value); err != nil {
		t.Fatal(err)
	}
	return execwire.Word(common.BytesToHash(value))
}

// This oracle enumerates both complete tries. It does not reconstruct a trie
// with the product verifier or use the recorder's account-value conversion.
func assertBidirectionalJournal(t *testing.T, r *recorder, s execwire.Step) {
	t.Helper()
	before, after := leaves(t, r.before), leaves(t, r.main.Trie)
	accountRecords := map[string]execwire.AccountChange{}
	for _, a := range s.Accounts {
		key := string(crypto.Keccak256(a.Address[:]))
		accountRecords[key] = a
		checkAccountFields(t, before[key], a.Before)
		checkAccountFields(t, after[key], a.After)
		if bytes.Equal(before[key], after[key]) && !a.StorageCleared {
			t.Fatal("journal includes an unchanged account")
		}
	}
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	for k := range keys {
		if !bytes.Equal(before[k], after[k]) {
			if _, ok := accountRecords[k]; !ok {
				t.Fatal("account trie diff missing from journal")
			}
		}
	}
	for key, a := range accountRecords {
		addr := common.Address(a.Address)
		b, afterAccount := readLeafAccount(t, before[key]), readLeafAccount(t, after[key])
		beforeRoot, afterRoot := types.EmptyRootHash, types.EmptyRootHash
		if b != nil {
			beforeRoot = b.Root
		}
		if afterAccount != nil {
			afterRoot = afterAccount.Root
		}
		if beforeRoot == afterRoot && !a.StorageCleared {
			continue
		}
		bt := r.storageBefore[addr]
		if bt == nil {
			var err error
			bt, err = r.db.Database.OpenStorageTrie(r.parent, addr, beforeRoot, r.before)
			if err != nil {
				t.Fatal(err)
			}
		}
		if bt.Hash() != beforeRoot {
			t.Fatal("oracle storage before snapshot is not authenticated by before leaf")
		}
		at := r.storageCurrent[addr]
		if afterRoot == types.EmptyRootHash {
			var err error
			at, err = r.db.Database.OpenStorageTrie(r.parent, addr, afterRoot, r.main.Trie)
			if err != nil {
				t.Fatal(err)
			}
		}
		if at == nil || at.Hash() != afterRoot {
			t.Fatal("oracle storage after snapshot is not authenticated by after leaf")
		}
		bs, as := leaves(t, bt), leaves(t, at)
		records := map[string]execwire.StorageChange{}
		for _, slot := range s.Storage {
			if slot.Address != a.Address {
				continue
			}
			k := string(crypto.Keccak256(slot.Slot[:]))
			records[k] = slot
			if storageWord(t, bs[k]) != slot.Before || storageWord(t, as[k]) != slot.After {
				t.Fatal("journal differs from independently decoded storage leaf")
			}
		}
		sk := map[string]bool{}
		for k := range bs {
			sk[k] = true
		}
		for k := range as {
			sk[k] = true
		}
		for k := range sk {
			if bytes.Equal(bs[k], as[k]) {
				continue
			}
			if _, ok := records[k]; !ok && (!a.StorageCleared || as[k] != nil) {
				t.Fatal("storage trie diff missing from journal")
			}
		}
	}
	for _, code := range s.Code {
		found := false
		for _, a := range s.Accounts {
			if a.After.Exists && a.After.CodeHash == code.Hash {
				found = bytes.Equal(code.Bytes, r.state.GetCode(common.Address(a.Address)))
			}
		}
		if !found {
			t.Fatal("journal code differs from actual state code")
		}
	}
}

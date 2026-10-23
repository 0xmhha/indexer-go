package executor

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/holiman/uint256"
)

var account = common.BytesToAddress([]byte{42})
var slot = common.Hash{1}
var initialValue = common.Hash{2}

func parentState(t *testing.T) (ethdb.Database, state.Database, common.Hash) {
	t.Helper()
	disk := rawdb.NewMemoryDatabase()
	t.Cleanup(func() { _ = disk.Close() })
	db := state.NewDatabase(disk)
	t.Cleanup(func() { _ = db.TrieDB().Close() })
	s, err := state.New(types.EmptyRootHash, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetBalance(account, uint256.NewInt(100))
	s.SetExtra(account, 7)
	s.SetState(account, slot, initialValue)
	root, err := s.Commit(0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.TrieDB().Commit(root, false); err != nil {
		t.Fatal(err)
	}
	return disk, db, root
}

func dump(t *testing.T, disk ethdb.Database) map[string][]byte {
	t.Helper()
	it := disk.NewIterator(nil, nil)
	defer it.Release()
	out := map[string][]byte{}
	for it.Next() {
		out[string(bytes.Clone(it.Key()))] = bytes.Clone(it.Value())
	}
	if err := it.Error(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRecorderBeforeAfterExtraRevertAndNoCommit(t *testing.T) {
	disk, db, root := parentState(t)
	before := dump(t, disk)
	r, err := newRecorder(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.beginStep(execwire.Transaction, 0, common.Hash{9}); err != nil {
		t.Fatal(err)
	}
	r.state.SetBalance(account, uint256.NewInt(90))
	r.state.SetNonce(account, 1)
	r.state.SetExtra(account, 9)
	r.state.SetState(account, slot, common.Hash{3})
	snapshot := r.state.Snapshot()
	r.state.SetBalance(account, uint256.NewInt(50))
	r.state.SetState(account, slot, common.Hash{6})
	r.state.RevertToSnapshot(snapshot)
	step, err := r.finishStep(true)
	if err != nil {
		t.Fatal(err)
	}
	assertBidirectionalJournal(t, r, step)
	if len(step.Accounts) != 1 || len(step.Storage) != 1 {
		t.Fatalf("lost changes: accounts %d slots %d", len(step.Accounts), len(step.Storage))
	}
	a := step.Accounts[0]
	if !a.Before.Exists || !a.After.Exists || a.Before.Extra != 7 || a.After.Extra != 9 || a.Before.Nonce != 0 || a.After.Nonce != 1 {
		t.Fatalf("wrong account transition: %+v", a)
	}
	if a.Before.Balance != execwire.Word(uint256.NewInt(100).Bytes32()) || a.After.Balance != execwire.Word(uint256.NewInt(90).Bytes32()) {
		t.Fatal("lost balance or recorded reverted value")
	}
	if step.Storage[0].Before != execwire.Word(initialValue) || step.Storage[0].After != execwire.Word(common.Hash{3}) {
		t.Fatal("lost slot or recorded reverted value")
	}
	if err := r.beginStep(execwire.Transaction, 1, common.Hash{10}); err != nil {
		t.Fatal(err)
	}
	r.state.SetState(account, slot, initialValue)
	second, err := r.finishStep(true)
	if err != nil || len(second.Storage) != 1 || second.Storage[0].Before != execwire.Word(common.Hash{3}) || second.Storage[0].After != execwire.Word(initialValue) {
		t.Fatalf("cross-transaction net-zero change lost: %v", err)
	}
	assertBidirectionalJournal(t, r, second)
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("second pass wrote backing database")
	}
}

func TestRecorderStorageOnlyCodeAndDeletion(t *testing.T) {
	_, db, root := parentState(t)
	r, err := newRecorder(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.beginStep(execwire.Transaction, 0, common.Hash{9}); err != nil {
		t.Fatal(err)
	}
	r.state.SetState(account, slot, common.Hash{4})
	code := []byte{0x60, 0x00, 0x56}
	r.state.SetCode(account, code)
	step, err := r.finishStep(true)
	if err != nil {
		t.Fatal(err)
	}
	assertBidirectionalJournal(t, r, step)
	if len(step.Code) != 1 || !bytes.Equal(step.Code[0].Bytes, code) || step.Code[0].Hash != execwire.Hash(crypto.Keccak256Hash(code)) {
		t.Fatal("code not bound to hash")
	}
	if err := r.beginStep(execwire.Transaction, 1, common.Hash{10}); err != nil {
		t.Fatal(err)
	}
	r.state.SelfDestruct(account)
	deleted, err := r.finishStep(true)
	if err != nil {
		t.Fatal(err)
	}
	assertBidirectionalJournal(t, r, deleted)
	if len(deleted.Accounts) != 1 || deleted.Accounts[0].After.Exists || !deleted.Accounts[0].StorageCleared {
		t.Fatal("deletion did not clear storage")
	}
}

func TestRecorderDeleteAndRecreateClearsUntouchedSlots(t *testing.T) {
	_, db, root := parentState(t)
	r, err := newRecorder(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.beginStep(execwire.Transaction, 0, common.Hash{9}); err != nil {
		t.Fatal(err)
	}
	r.state.SelfDestruct(account)
	r.state.CreateAccount(account)
	r.state.SetNonce(account, 1)
	r.state.SetState(account, common.Hash{5}, common.Hash{6})
	s, err := r.finishStep(true)
	if err != nil {
		t.Fatal(err)
	}
	assertBidirectionalJournal(t, r, s)
	if len(s.Accounts) != 1 || !s.Accounts[0].StorageCleared || !s.Accounts[0].After.Exists {
		t.Fatal("recreated account did not preserve clear marker")
	}
	if r.state.GetState(account, slot) != (common.Hash{}) {
		t.Fatal("old untouched slot survived recreation")
	}
}

func TestRecorderCodeRemovalAndForbiddenCodeBatch(t *testing.T) {
	disk, db, root := parentState(t)
	s, err := state.New(root, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetCode(account, []byte{0x60, 0x00, 0x56})
	root, err = s.Commit(0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.TrieDB().Commit(root, false); err != nil {
		t.Fatal(err)
	}
	r, err := newRecorder(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.beginStep(execwire.Transaction, 0, common.Hash{9}); err != nil {
		t.Fatal(err)
	}
	r.state.SetCode(account, []byte{})
	step, err := r.finishStep(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(step.Code) != 0 || step.Accounts[0].After.CodeHash != execwire.Hash(types.EmptyCodeHash) {
		t.Fatal("empty code was exported as a new code blob")
	}
	if err := r.beginStep(execwire.Transaction, 1, common.Hash{10}); err != nil {
		t.Fatal(err)
	}
	r.state.SetCode(account, []byte{0x60, 0x01, 0x56})
	if _, err := r.finishStep(true); err != nil {
		t.Fatal(err)
	}
	before := dump(t, disk)
	if _, err := r.state.Commit(1, true); !errors.Is(err, ErrCommitForbidden) {
		t.Fatalf("code commit accepted: %v", err)
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("code batch reached backing DB before forbidden trie commit")
	}
}

func TestRecorderRejectsRecordOnlyOmission(t *testing.T) {
	for _, kind := range []string{"account", "storage", "code"} {
		t.Run(kind, func(t *testing.T) {
			_, db, root := parentState(t)
			expected, err := state.New(root, db, nil)
			if err != nil {
				t.Fatal(err)
			}
			apply := func(s *state.StateDB) {
				s.SetNonce(account, 1)
				s.SetState(account, slot, common.Hash{4})
				s.SetCode(account, []byte{0x60, 0x00, 0x56})
			}
			apply(expected)
			want := expected.IntermediateRoot(true)
			r, err := newRecorder(db, root)
			if err != nil {
				t.Fatal(err)
			}
			r.omit = func(k string) bool { return k == kind }
			if err := r.beginStep(execwire.Transaction, 0, common.Hash{9}); err != nil {
				t.Fatal(err)
			}
			apply(r.state)
			if _, err := r.finishStep(true); !errors.Is(err, ErrJournalMismatch) {
				t.Fatalf("missing %s record accepted: %v", kind, err)
			}
			if got := r.state.IntermediateRoot(true); got != want {
				t.Fatal("fault also suppressed actual trie mutation; invalid omission test")
			}
		})
	}
}

func TestRecorderCopyDoesNotContaminatePrimaryAndCommitForbidden(t *testing.T) {
	disk, db, root := parentState(t)
	before := dump(t, disk)
	r, err := newRecorder(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.beginStep(execwire.Transaction, 0, common.Hash{9}); err != nil {
		t.Fatal(err)
	}
	copy := r.state.Copy()
	copy.SetBalance(account, uint256.NewInt(999))
	copy.IntermediateRoot(true)
	r.state.SetNonce(account, 1)
	step, err := r.finishStep(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(step.Accounts) != 1 || step.Accounts[0].After.Balance != execwire.Word(uint256.NewInt(100).Bytes32()) {
		t.Fatal("copied state contaminated journal")
	}
	if _, err := r.state.Commit(1, true); !errors.Is(err, ErrCommitForbidden) {
		t.Fatalf("second-pass commit accepted: %v", err)
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("forbidden commit wrote database")
	}
}

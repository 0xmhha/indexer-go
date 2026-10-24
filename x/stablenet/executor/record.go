package executor

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
)

type recorder struct {
	state          *state.StateDB
	db             *recordingDB
	main           *recordingTrie
	parent         common.Hash
	before         state.Trie
	storageBefore  map[common.Address]state.Trie
	storageCurrent map[common.Address]state.Trie
	accounts       map[common.Address]bool
	retainAccount  *common.Address
	slots          map[common.Address]map[common.Hash]bool
	cleared        map[common.Address]bool
	codes          map[common.Hash][]byte
	active         bool
	ordinal        uint32
	step           execwire.Step
	// A private fault seam drops exported records, never actual trie mutations.
	omit func(string) bool
}

func newRecorder(base state.Database, root common.Hash) (*recorder, error) {
	r := &recorder{parent: root, storageCurrent: make(map[common.Address]state.Trie)}
	r.db = &recordingDB{Database: base, owner: r}
	s, err := state.New(root, r.db, nil)
	if err != nil {
		return nil, err
	}
	r.state = s
	return r, nil
}

func (r *recorder) beginStep(cause execwire.Cause, index int32, hash common.Hash) error {
	if r.active || r.main == nil {
		return fmt.Errorf("%w: step ownership", ErrJournalMismatch)
	}
	r.before = r.db.Database.CopyTrie(r.main.Trie)
	r.storageBefore = make(map[common.Address]state.Trie)
	r.accounts = make(map[common.Address]bool)
	r.retainAccount = nil
	r.slots = make(map[common.Address]map[common.Hash]bool)
	r.cleared = make(map[common.Address]bool)
	r.codes = make(map[common.Hash][]byte)
	r.step = execwire.Step{Ordinal: r.ordinal, Cause: cause, TxIndex: index, TxHash: execwire.Hash(hash)}
	r.active = true
	return nil
}

func (r *recorder) beforeStorage(addr common.Address) (state.Trie, error) {
	if tr := r.storageBefore[addr]; tr != nil {
		return tr, nil
	}
	a, err := r.before.GetAccount(addr)
	if err != nil {
		return nil, err
	}
	root := types.EmptyRootHash
	if a != nil {
		root = a.Root
	}
	var tr state.Trie
	if current := r.storageCurrent[addr]; current != nil && current.Hash() == root {
		tr = r.db.Database.CopyTrie(current)
	} else {
		tr, err = r.db.Database.OpenStorageTrie(r.parent, addr, root, r.before)
		if err != nil {
			return nil, err
		}
	}
	r.storageBefore[addr] = tr
	return tr, nil
}

func (r *recorder) touchSlot(addr common.Address, key []byte) error {
	if !r.active || len(key) != 32 {
		return fmt.Errorf("%w: storage boundary", ErrJournalMismatch)
	}
	if _, err := r.beforeStorage(addr); err != nil {
		return err
	}
	if r.slots[addr] == nil {
		r.slots[addr] = make(map[common.Hash]bool)
	}
	r.slots[addr][common.BytesToHash(key)] = true
	r.accounts[addr] = true
	return nil
}

func accountValue(a *types.StateAccount) execwire.Account {
	if a == nil {
		return execwire.Account{}
	}
	return execwire.Account{Exists: true, Nonce: a.Nonce, Balance: execwire.Word(a.Balance.Bytes32()), CodeHash: execwire.Hash(common.BytesToHash(a.CodeHash)), StorageRoot: execwire.Hash(a.Root), Extra: a.Extra}
}
func stateAccount(a execwire.Account) *types.StateAccount {
	return &types.StateAccount{Nonce: a.Nonce, Balance: new(uint256.Int).SetBytes(a.Balance[:]), Root: common.Hash(a.StorageRoot), CodeHash: bytes.Clone(a.CodeHash[:]), Extra: a.Extra}
}

func (r *recorder) finishStep(deleteEmpty bool) (execwire.Step, error) {
	if !r.active {
		return execwire.Step{}, fmt.Errorf("%w: no active step", ErrJournalMismatch)
	}
	root := r.state.IntermediateRoot(deleteEmpty)
	defer func() { r.active = false }()
	if err := r.state.Error(); err != nil {
		return execwire.Step{}, err
	}
	s := r.step
	for addr := range r.accounts {
		before, err := r.before.GetAccount(addr)
		if err != nil {
			return execwire.Step{}, err
		}
		after, err := r.main.GetAccount(addr)
		if err != nil {
			return execwire.Step{}, err
		}
		cleared := before != nil && before.Root != types.EmptyRootHash && (r.cleared[addr] || after == nil || after.Root == types.EmptyRootHash)
		if cleared {
			r.cleared[addr] = true
		}
		b, a := accountValue(before), accountValue(after)
		if b != a || cleared || r.retainAccount != nil && addr == *r.retainAccount {
			s.Accounts = append(s.Accounts, execwire.AccountChange{Address: execwire.Address(addr), Before: b, After: a, StorageCleared: cleared})
		}
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return bytes.Compare(s.Accounts[i].Address[:], s.Accounts[j].Address[:]) < 0 })
	for addr, keys := range r.slots {
		before, err := r.beforeStorage(addr)
		if err != nil {
			return execwire.Step{}, err
		}
		for key := range keys {
			old, err := before.GetStorage(addr, key[:])
			if err != nil {
				return execwire.Step{}, err
			}
			b, a := execwire.Word(common.BytesToHash(old)), execwire.Word(r.state.GetState(addr, key))
			if b != a || r.cleared[addr] {
				s.Storage = append(s.Storage, execwire.StorageChange{Address: execwire.Address(addr), Slot: execwire.Word(key), Before: b, After: a})
			}
		}
	}
	sort.Slice(s.Storage, func(i, j int) bool {
		a, b := s.Storage[i], s.Storage[j]
		n := bytes.Compare(a.Address[:], b.Address[:])
		return n < 0 || n == 0 && bytes.Compare(a.Slot[:], b.Slot[:]) < 0
	})
	for hash, code := range r.codes {
		s.Code = append(s.Code, execwire.Code{Hash: execwire.Hash(hash), Bytes: bytes.Clone(code)})
	}
	sort.Slice(s.Code, func(i, j int) bool { return bytes.Compare(s.Code[i].Hash[:], s.Code[j].Hash[:]) < 0 })
	if r.omit != nil {
		if r.omit("account") {
			s.Accounts = nil
		}
		if r.omit("storage") {
			s.Storage = nil
		}
		if r.omit("code") {
			s.Code = nil
		}
	}
	if err := r.verify(s, root); err != nil {
		return execwire.Step{}, err
	}
	r.ordinal++
	return s, nil
}

func (r *recorder) verify(s execwire.Step, root common.Hash) error {
	accounts := make(map[common.Address]execwire.AccountChange, len(s.Accounts))
	codes := make(map[common.Hash][]byte, len(s.Code))
	for _, c := range s.Code {
		if crypto.Keccak256Hash(c.Bytes) != common.Hash(c.Hash) {
			return fmt.Errorf("%w: code hash", ErrJournalMismatch)
		}
		codes[common.Hash(c.Hash)] = c.Bytes
	}
	copy := r.db.Database.CopyTrie(r.before)
	for _, a := range s.Accounts {
		addr := common.Address(a.Address)
		before, err := r.before.GetAccount(addr)
		if err != nil {
			return err
		}
		if accountValue(before) != a.Before {
			return fmt.Errorf("%w: before account", ErrJournalMismatch)
		}
		accounts[addr] = a
		if a.After.Exists && a.After.CodeHash != execwire.Hash(types.EmptyCodeHash) && a.After.CodeHash != a.Before.CodeHash {
			if _, ok := codes[common.Hash(a.After.CodeHash)]; !ok {
				return fmt.Errorf("%w: missing code", ErrJournalMismatch)
			}
		}
		if a.After.Exists {
			if err := copy.UpdateAccount(addr, stateAccount(a.After)); err != nil {
				return err
			}
		} else {
			if err := copy.DeleteAccount(addr); err != nil {
				return err
			}
		}
	}
	for hash := range codes {
		used := false
		for _, a := range s.Accounts {
			if a.After.Exists && common.Hash(a.After.CodeHash) == hash {
				used = true
				break
			}
		}
		if !used {
			return fmt.Errorf("%w: unreferenced code", ErrJournalMismatch)
		}
	}
	byAddress := make(map[common.Address][]execwire.StorageChange)
	for _, slot := range s.Storage {
		addr := common.Address(slot.Address)
		byAddress[addr] = append(byAddress[addr], slot)
	}
	for addr, a := range accounts {
		if a.Before.StorageRoot != a.After.StorageRoot || a.StorageCleared {
			if _, ok := byAddress[addr]; !ok {
				byAddress[addr] = nil
			}
		}
	}
	for addr, slots := range byAddress {
		a, ok := accounts[addr]
		if !ok {
			return fmt.Errorf("%w: missing storage account", ErrJournalMismatch)
		}
		before, err := r.beforeStorage(addr)
		if err != nil {
			return err
		}
		target := r.db.Database.CopyTrie(before)
		if a.StorageCleared {
			target, err = r.db.Database.OpenStorageTrie(r.parent, addr, types.EmptyRootHash, r.before)
			if err != nil {
				return err
			}
		} else if a.Before.Exists && !a.After.Exists && a.Before.StorageRoot != execwire.Hash(types.EmptyRootHash) {
			return fmt.Errorf("%w: missing storage clear", ErrJournalMismatch)
		}
		for _, slot := range slots {
			old, err := before.GetStorage(addr, slot.Slot[:])
			if err != nil {
				return err
			}
			if execwire.Word(common.BytesToHash(old)) != slot.Before {
				return fmt.Errorf("%w: before storage", ErrJournalMismatch)
			}
			v := bytes.TrimLeft(slot.After[:], "\x00")
			if len(v) == 0 {
				err = target.DeleteStorage(addr, slot.Slot[:])
			} else {
				err = target.UpdateStorage(addr, slot.Slot[:], v)
			}
			if err != nil {
				return err
			}
		}
		want := types.EmptyRootHash
		if a.After.Exists {
			want = common.Hash(a.After.StorageRoot)
		}
		if target.Hash() != want {
			return fmt.Errorf("%w: storage journal root", ErrJournalMismatch)
		}
	}
	if copy.Hash() != root {
		return fmt.Errorf("%w: account journal root", ErrJournalMismatch)
	}
	return nil
}

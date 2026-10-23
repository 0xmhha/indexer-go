package executor

import (
	"bytes"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie/trienode"
)

type recordingDB struct {
	state.Database
	owner *recorder
}
type recordingTrie struct {
	state.Trie
	owner   *recorder
	primary bool
	storage bool
	address common.Address
}

func unwrap(t state.Trie) state.Trie {
	if r, ok := t.(*recordingTrie); ok {
		return r.Trie
	}
	return t
}

func (d *recordingDB) OpenTrie(root common.Hash) (state.Trie, error) {
	t, err := d.Database.OpenTrie(root)
	if err != nil {
		return nil, err
	}
	r := &recordingTrie{Trie: t, owner: d.owner, primary: true}
	d.owner.main = r
	return r, nil
}
func (d *recordingDB) OpenStorageTrie(stateRoot common.Hash, addr common.Address, root common.Hash, account state.Trie) (state.Trie, error) {
	primary := true
	if t, ok := account.(*recordingTrie); ok {
		primary = t.primary
	}
	if primary && d.owner.active {
		if _, err := d.owner.beforeStorage(addr); err != nil {
			return nil, err
		}
		before, err := d.owner.before.GetAccount(addr)
		if err != nil {
			return nil, err
		}
		if root == types.EmptyRootHash && before != nil && before.Root != types.EmptyRootHash {
			d.owner.cleared[addr] = true
		}
	}
	t, err := d.Database.OpenStorageTrie(stateRoot, addr, root, unwrap(account))
	if err != nil {
		return nil, err
	}
	r := &recordingTrie{Trie: t, owner: d.owner, primary: primary, storage: true, address: addr}
	if primary {
		d.owner.storageCurrent[addr] = t
	}
	return r, nil
}
func (d *recordingDB) CopyTrie(t state.Trie) state.Trie {
	copy := d.Database.CopyTrie(unwrap(t))
	r := &recordingTrie{Trie: copy, owner: d.owner}
	if old, ok := t.(*recordingTrie); ok {
		r.storage = old.storage
		r.address = old.address
	}
	return r
}
func (d *recordingDB) DiskDB() ethdb.KeyValueStore {
	return stagingDisk{KeyValueStore: d.Database.DiskDB()}
}

func (t *recordingTrie) touchAccount(addr common.Address) error {
	if !t.primary {
		return nil
	}
	if !t.owner.active {
		return fmt.Errorf("%w: mutation outside step", ErrJournalMismatch)
	}
	t.owner.accounts[addr] = true
	return nil
}
func (t *recordingTrie) UpdateAccount(addr common.Address, a *types.StateAccount) error {
	if err := t.touchAccount(addr); err != nil {
		return err
	}
	return t.Trie.UpdateAccount(addr, a)
}
func (t *recordingTrie) DeleteAccount(addr common.Address) error {
	if err := t.touchAccount(addr); err != nil {
		return err
	}
	if t.primary {
		if _, err := t.owner.beforeStorage(addr); err != nil {
			return err
		}
		t.owner.cleared[addr] = true
	}
	return t.Trie.DeleteAccount(addr)
}
func (t *recordingTrie) slot(addr common.Address, key []byte) error {
	if !t.storage || addr != t.address {
		return fmt.Errorf("%w: storage trie identity", ErrJournalMismatch)
	}
	if !t.primary {
		return nil
	}
	return t.owner.touchSlot(addr, key)
}
func (t *recordingTrie) UpdateStorage(addr common.Address, key, value []byte) error {
	if err := t.slot(addr, key); err != nil {
		return err
	}
	return t.Trie.UpdateStorage(addr, key, value)
}
func (t *recordingTrie) DeleteStorage(addr common.Address, key []byte) error {
	if err := t.slot(addr, key); err != nil {
		return err
	}
	return t.Trie.DeleteStorage(addr, key)
}
func (t *recordingTrie) UpdateContractCode(addr common.Address, hash common.Hash, code []byte) error {
	if err := t.touchAccount(addr); err != nil {
		return err
	}
	if t.primary {
		if crypto.Keccak256Hash(code) != hash {
			return fmt.Errorf("%w: code hash", ErrJournalMismatch)
		}
		if old, ok := t.owner.codes[hash]; ok && !bytes.Equal(old, code) {
			return fmt.Errorf("%w: conflicting code", ErrJournalMismatch)
		}
		if len(code) > 0 {
			t.owner.codes[hash] = bytes.Clone(code)
		}
	}
	return t.Trie.UpdateContractCode(addr, hash, code)
}
func (*recordingTrie) Commit(bool) (common.Hash, *trienode.NodeSet, error) {
	return common.Hash{}, nil, ErrCommitForbidden
}

// StateDB stages code before calling Trie.Commit. This private disk view never
// writes those batches to the backing store; Trie.Commit always rejects the
// overall commit. Read operations continue to use the original node database.
type stagingDisk struct{ ethdb.KeyValueStore }

func (stagingDisk) Put([]byte, []byte) error         { return ErrCommitForbidden }
func (stagingDisk) Delete([]byte) error              { return ErrCommitForbidden }
func (stagingDisk) Compact([]byte, []byte) error     { return ErrCommitForbidden }
func (stagingDisk) Close() error                     { return ErrCommitForbidden }
func (stagingDisk) NewBatch() ethdb.Batch            { return &stagingBatch{} }
func (stagingDisk) NewBatchWithSize(int) ethdb.Batch { return &stagingBatch{} }

type stagingBatch struct{ size int }

func (b *stagingBatch) Put(k, v []byte) error           { b.size += len(k) + len(v); return nil }
func (b *stagingBatch) Delete(k []byte) error           { b.size += len(k); return nil }
func (b *stagingBatch) ValueSize() int                  { return b.size }
func (*stagingBatch) Write() error                      { return nil }
func (b *stagingBatch) Reset()                          { b.size = 0 }
func (*stagingBatch) Replay(ethdb.KeyValueWriter) error { return ErrCommitForbidden }

// Package checkpoint exports offline state; it never starts a node or accesses PostgreSQL.
package checkpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

type Target struct {
	Address execwire.Address
	Slots   []execwire.Word
}

func account(a *types.StateAccount) execwire.Account {
	if a == nil {
		return execwire.Account{}
	}
	v := execwire.Account{Exists: true, Nonce: a.Nonce, Extra: a.Extra, CodeHash: execwire.Hash(common.BytesToHash(a.CodeHash)), StorageRoot: execwire.Hash(a.Root)}
	a.Balance.WriteToSlice(v.Balance[:])
	return v
}
func nodes(t state.Trie, key []byte) ([][]byte, error) {
	db := memorydb.New()
	defer func() { _ = db.Close() }()
	if t.Hash() == types.EmptyRootHash {
		return nil, nil
	}
	if err := t.Prove(crypto.Keccak256(key), db); err != nil {
		return nil, err
	}
	it := db.NewIterator(nil, nil)
	defer it.Release()
	var out [][]byte
	for it.Next() {
		out = append(out, bytes.Clone(it.Value()))
	}
	return out, it.Error()
}

// Export walks full state in secure-key order, or proves an explicit selection.
// Missing preimages or trie nodes are fatal; an incomplete file has no end record.
func Export(ctx context.Context, w io.Writer, m format.Manifest, db state.Database, targets []Target) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil || m.Coverage != "full" && m.Coverage != "partial" || m.Coverage == "full" && targets != nil {
		return execwire.ErrProtocol
	}
	t, err := db.OpenTrie(common.Hash(m.Root))
	if err != nil {
		return err
	}
	if err = format.Write(w, format.Record{Kind: "manifest", Manifest: &m}); err != nil {
		return err
	}
	write := func(v format.Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return format.Write(w, v)
	}
	emit := func(addr common.Address, slots []execwire.Word, full bool) error {
		a, e := t.GetAccount(addr)
		if e != nil {
			return e
		}
		v := account(a)
		r := format.Record{Kind: "account", Address: execwire.Address(addr), Account: &v}
		if !full {
			r.Proof, e = nodes(t, addr[:])
			if e != nil {
				return e
			}
		}
		if a != nil && common.BytesToHash(a.CodeHash) != types.EmptyCodeHash {
			r.Code, e = db.ContractCode(addr, common.BytesToHash(a.CodeHash))
			if e != nil {
				return e
			}
		}
		if v.Exists && execwire.Hash(crypto.Keccak256Hash(r.Code)) != v.CodeHash {
			return execwire.ErrProtocol
		}
		if e = write(r); e != nil {
			return e
		}
		sorted := append([]execwire.Word(nil), slots...)
		sort.Slice(sorted, func(i, j int) bool {
			return bytes.Compare(crypto.Keccak256(sorted[i][:]), crypto.Keccak256(sorted[j][:])) < 0
		})
		for i := 1; i < len(sorted); i++ {
			if sorted[i] == sorted[i-1] {
				return execwire.ErrProtocol
			}
		}
		if a == nil {
			for _, s := range sorted {
				if e = write(format.Record{Kind: "storage", Address: r.Address, Slot: s}); e != nil {
					return e
				}
			}
			return nil
		}
		st, e := db.OpenStorageTrie(common.Hash(m.Root), addr, a.Root, t)
		if e != nil {
			return e
		}
		if full {
			ni, e := st.NodeIterator(nil)
			if e != nil {
				return e
			}
			it := trie.NewIterator(ni)
			for it.Next() {
				if e = ctx.Err(); e != nil {
					return e
				}
				pre := st.GetKey(it.Key)
				if len(pre) != 32 || !bytes.Equal(crypto.Keccak256(pre), it.Key) {
					return fmt.Errorf("checkpoint: missing storage preimage for %x", it.Key)
				}
				var scalar []byte
				if e = rlp.DecodeBytes(it.Value, &scalar); e != nil || len(scalar) == 0 || len(scalar) > 32 {
					return execwire.ErrProtocol
				}
				if e = write(format.Record{Kind: "storage", Address: r.Address, Slot: execwire.Word(common.BytesToHash(pre)), Value: execwire.Word(common.BytesToHash(scalar))}); e != nil {
					return e
				}
			}
			return it.Err
		}
		for i, s := range sorted {
			if i > 0 && s == sorted[i-1] {
				return execwire.ErrProtocol
			}
			value, e := st.GetStorage(addr, s[:])
			if e != nil {
				return e
			}
			proof, e := nodes(st, s[:])
			if e != nil {
				return e
			}
			if e = write(format.Record{Kind: "storage", Address: r.Address, Slot: s, Value: execwire.Word(common.BytesToHash(value)), Proof: proof}); e != nil {
				return e
			}
		}
		return nil
	}
	if m.Coverage == "full" {
		ni, e := t.NodeIterator(nil)
		if e != nil {
			return e
		}
		it := trie.NewIterator(ni)
		for it.Next() {
			if e = ctx.Err(); e != nil {
				return e
			}
			pre := t.GetKey(it.Key)
			if len(pre) != 20 || !bytes.Equal(crypto.Keccak256(pre), it.Key) {
				return fmt.Errorf("checkpoint: missing account preimage for %x", it.Key)
			}
			if e = emit(common.BytesToAddress(pre), nil, true); e != nil {
				return e
			}
		}
		if it.Err != nil {
			return it.Err
		}
	} else {
		sorted := append([]Target(nil), targets...)
		sort.Slice(sorted, func(i, j int) bool {
			return bytes.Compare(crypto.Keccak256(sorted[i].Address[:]), crypto.Keccak256(sorted[j].Address[:])) < 0
		})
		for i, v := range sorted {
			if i > 0 && v.Address == sorted[i-1].Address {
				return execwire.ErrProtocol
			}
			if err = emit(common.Address(v.Address), v.Slots, false); err != nil {
				return err
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return write(format.Record{Kind: "end"})
}

// Genesis uses only an ephemeral memory database and the official allocation.
func Genesis(ctx context.Context, w io.Writer, g *core.Genesis, m format.Manifest) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if g == nil || g.Config == nil || g.Config.ChainID == nil || g.Config.ChainID.String() != m.Chain.ID || m.Mode != "genesis" || m.Coverage != "full" || m.Ref.Height != 0 {
		return execwire.ErrProtocol
	}
	disk := rawdb.NewMemoryDatabase()
	defer func() { err = errors.Join(err, disk.Close()) }()
	db := state.NewDatabaseWithConfig(disk, &triedb.Config{Preimages: true})
	defer func() { err = errors.Join(err, db.TrieDB().Close()) }()
	b, err := g.Commit(disk, db.TrieDB())
	if err != nil {
		return err
	}
	if execwire.Hash(b.Hash()) != m.Ref.Hash || m.Ref.Hash != m.Chain.Genesis || execwire.Hash(b.Root()) != m.Root {
		return execwire.ErrProtocol
	}
	m.Header, err = rlp.EncodeToBytes(b.Header())
	if err != nil {
		return err
	}
	return Export(ctx, w, m, db, nil)
}

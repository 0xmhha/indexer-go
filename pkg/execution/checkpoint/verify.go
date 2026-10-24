// Package checkpoint verifies offline bootstrap state independently of the producer.
package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

type Report struct {
	Manifest        format.Manifest
	Digest          execwire.Hash
	Accounts, Slots uint64
	Block           *model.Block `json:"-"`
}

// AccountRLP preserves StableNet's optional Extra field without importing its fork.
func AccountRLP(a execwire.Account) ([]byte, error) {
	if !a.Exists {
		if a != (execwire.Account{}) {
			return nil, execwire.ErrProtocol
		}
		return nil, nil
	}
	if a.CodeHash == (execwire.Hash{}) || a.StorageRoot == (execwire.Hash{}) {
		return nil, execwire.ErrProtocol
	}
	return rlp.EncodeToBytes(struct {
		Nonce    uint64
		Balance  *big.Int
		Root     common.Hash
		CodeHash []byte
		Extra    uint64 `rlp:"optional"`
	}{a.Nonce, new(big.Int).SetBytes(a.Balance[:]), common.Hash(a.StorageRoot), a.CodeHash[:], a.Extra})
}
func proof(root execwire.Hash, key []byte, nodes [][]byte, want []byte) error {
	if len(nodes) > 128 {
		return execwire.ErrLimit
	}
	if common.Hash(root) == types.EmptyRootHash {
		if len(nodes) != 0 || len(want) != 0 {
			return execwire.ErrProtocol
		}
		return nil
	}
	db := memorydb.New()
	defer func() { _ = db.Close() }()
	for _, n := range nodes {
		if len(n) == 0 || len(n) > 64<<10 {
			return execwire.ErrLimit
		}
		if err := db.Put(crypto.Keccak256(n), n); err != nil {
			return err
		}
	}
	got, err := trie.VerifyProof(common.Hash(root), key, db)
	if err != nil || !bytes.Equal(got, want) {
		return fmt.Errorf("%w: trie proof", execwire.ErrProtocol)
	}
	return nil
}

// Verify streams in secure-key order. A visitor may stage writes, but must not
// commit or publish them until Verify returns successfully (including EOF).
// Expected chain/ref/root/mode/coverage are trusted operator inputs, not file trust.
func Verify(ctx context.Context, r io.Reader, expected format.Manifest, profile chains.HeaderCodec, visit func(format.Record) error) (out Report, err error) {
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if r == nil || profile == nil {
		return out, execwire.ErrProtocol
	}
	digest := sha256.New()
	d := format.NewDecoder(io.TeeReader(r, digest))
	first, err := d.Next()
	if err != nil {
		return out, err
	}
	if first.Kind != "manifest" || first.Manifest == nil || !reflect.DeepEqual(first, format.Record{Kind: "manifest", Manifest: first.Manifest}) {
		return out, execwire.ErrProtocol
	}
	m := *first.Manifest
	if m.Version != 1 || m.Mode != "genesis" && m.Mode != "checkpoint" || m.Coverage != "full" && m.Coverage != "partial" || m.Ref.Generation != 1 || m.Ref.Height >= math.MaxInt64 || m.Root == (execwire.Hash{}) || len(m.Header) > execwire.MaxHeaderBytes {
		return out, execwire.ErrProtocol
	}
	if m.Mode == "genesis" && (m.Ref.Height != 0 || m.Ref.Hash != m.Chain.Genesis || m.Coverage != "full") {
		return out, execwire.ErrProtocol
	}
	if m.Chain != expected.Chain || m.Ref != expected.Ref || m.Root != expected.Root || m.Mode != expected.Mode || m.Coverage != expected.Coverage {
		return out, execwire.ErrProtocol
	}
	if err = execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: m.Chain, Ref: m.Ref}}); err != nil {
		return out, err
	}
	b, err := profile.DecodeHeaderRLP(m.Header)
	if err != nil {
		return out, err
	}
	if b.Number != m.Ref.Height || execwire.Hash(b.Hash) != m.Ref.Hash || execwire.Hash(b.StateRoot) != m.Root || b.Time > math.MaxInt64 {
		return out, execwire.ErrProtocol
	}
	out.Manifest, out.Block = m, b
	if visit != nil {
		if err = visit(first); err != nil {
			return out, err
		}
	}
	full := m.Coverage == "full"
	accounts := trie.NewStackTrie(nil)
	var storage *trie.StackTrie
	var current *execwire.Account
	var addr execwire.Address
	var lastAccount, lastSlot []byte
	finish := func() error {
		if full && current != nil && execwire.Hash(storage.Hash()) != current.StorageRoot {
			return fmt.Errorf("%w: full storage root", execwire.ErrProtocol)
		}
		return nil
	}
	for {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		v, e := d.Next()
		if e != nil {
			if e == io.EOF {
				e = execwire.ErrIncomplete
			}
			return out, e
		}
		switch v.Kind {
		case "account":
			if v.Account == nil || v.Manifest != nil || v.Slot != (execwire.Word{}) || v.Value != (execwire.Word{}) {
				return out, execwire.ErrProtocol
			}
			if err = finish(); err != nil {
				return out, err
			}
			key := crypto.Keccak256(v.Address[:])
			if lastAccount != nil && bytes.Compare(lastAccount, key) >= 0 {
				return out, execwire.ErrProtocol
			}
			lastAccount = key
			value, e := AccountRLP(*v.Account)
			if e != nil {
				return out, e
			}
			if len(v.Code) > 1<<20 || !v.Account.Exists && len(v.Code) != 0 || v.Account.Exists && execwire.Hash(crypto.Keccak256Hash(v.Code)) != v.Account.CodeHash {
				return out, execwire.ErrProtocol
			}
			if full {
				if !v.Account.Exists || len(v.Proof) != 0 {
					return out, execwire.ErrProtocol
				}
				if err = accounts.Update(key, value); err != nil {
					return out, err
				}
			} else if err = proof(m.Root, key, v.Proof, value); err != nil {
				return out, err
			}
			current, addr = v.Account, v.Address
			storage = trie.NewStackTrie(nil)
			lastSlot = nil
			out.Accounts++
		case "storage":
			if current == nil || v.Address != addr || v.Account != nil || v.Manifest != nil || len(v.Code) != 0 {
				return out, execwire.ErrProtocol
			}
			key := crypto.Keccak256(v.Slot[:])
			if lastSlot != nil && bytes.Compare(lastSlot, key) >= 0 {
				return out, execwire.ErrProtocol
			}
			lastSlot = key
			var value []byte
			if v.Value != (execwire.Word{}) {
				value, e = rlp.EncodeToBytes(bytes.TrimLeft(v.Value[:], "\x00"))
				if e != nil {
					return out, e
				}
			}
			if !current.Exists {
				if len(value) != 0 || len(v.Proof) != 0 {
					return out, execwire.ErrProtocol
				}
			} else if full {
				if len(v.Proof) != 0 || len(value) == 0 {
					return out, execwire.ErrProtocol
				}
				if err = storage.Update(key, value); err != nil {
					return out, err
				}
			} else if err = proof(current.StorageRoot, key, v.Proof, value); err != nil {
				return out, err
			}
			out.Slots++
		case "end":
			if !reflect.DeepEqual(v, format.Record{Kind: "end"}) {
				return out, execwire.ErrProtocol
			}
			if err = finish(); err != nil {
				return out, err
			}
			if full && execwire.Hash(accounts.Hash()) != m.Root {
				return out, fmt.Errorf("%w: full account root", execwire.ErrProtocol)
			}
			if _, err = d.Next(); err != io.EOF {
				if err == nil {
					err = execwire.ErrProtocol
				}
				return out, err
			}
			if err = ctx.Err(); err != nil {
				return out, err
			}
			copy(out.Digest[:], digest.Sum(nil))
			return out, nil
		default:
			return out, execwire.ErrProtocol
		}
		if visit != nil {
			if err = visit(v); err != nil {
				return out, err
			}
		}
	}
}

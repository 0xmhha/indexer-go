package checkpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sort"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type stateStorageProof struct {
	Key   string   `json:"key"`
	Value string   `json:"value"`
	Proof []string `json:"proof"`
}
type stateAccountProof struct {
	Address      execwire.Address    `json:"address"`
	AccountProof []string            `json:"accountProof"`
	Balance      string              `json:"balance"`
	CodeHash     common.Hash         `json:"codeHash"`
	Nonce        string              `json:"nonce"`
	StorageHash  common.Hash         `json:"storageHash"`
	StorageProof []stateStorageProof `json:"storageProof"`
}
type stateEvidence struct {
	Proof stateAccountProof `json:"proof"`
	Code  *string           `json:"code,omitempty"`
}
type stateSelection struct {
	Targets []execwire.StateProofTarget `json:"targets"`
}
type stateBundle struct {
	Version   uint32          `json:"version"`
	Selection stateSelection  `json:"selection"`
	Evidence  []stateEvidence `json:"evidence"`
}

func proofStrings(t state.Trie, key []byte) ([]string, error) {
	values, e := nodes(t, key)
	if e != nil {
		return nil, e
	}
	if len(values) > 65 {
		return nil, execwire.ErrLimit
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if len(value) == 0 || len(value) > 1024 {
			return nil, execwire.ErrLimit
		}
		out = append(out, hexutil.Encode(value))
	}
	return out, nil
}

// ExportStateProofs reads an explicit retained standard-MPT root. It starts no
// service or EVM, mutates no source and writes no partial successful evidence.
// Custom (non-four-field) account encodings require a different verifier.
func ExportStateProofs(ctx context.Context, w io.Writer, db state.Database, root common.Hash, targets []execwire.StateProofTarget) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if db == nil || w == nil || root == (common.Hash{}) || len(targets) == 0 || len(targets) > 8 {
		return execwire.ErrProtocol
	}
	selected := append([]execwire.StateProofTarget{}, targets...)
	total := 0
	for i := range selected {
		selected[i].Slots = append([]execwire.Word{}, selected[i].Slots...)
		total += len(selected[i].Slots)
		if total > 128 {
			return execwire.ErrLimit
		}
		sort.Slice(selected[i].Slots, func(a, b int) bool { return bytes.Compare(selected[i].Slots[a][:], selected[i].Slots[b][:]) < 0 })
		for j := 1; j < len(selected[i].Slots); j++ {
			if selected[i].Slots[j] == selected[i].Slots[j-1] {
				return execwire.ErrProtocol
			}
		}
	}
	sort.Slice(selected, func(i, j int) bool { return bytes.Compare(selected[i].Address[:], selected[j].Address[:]) < 0 })
	for i := 1; i < len(selected); i++ {
		if selected[i].Address == selected[i-1].Address {
			return execwire.ErrProtocol
		}
	}
	tr, e := db.OpenTrie(root)
	if e != nil {
		return e
	}
	bundle := stateBundle{Version: 1, Selection: stateSelection{Targets: selected}, Evidence: []stateEvidence{}}
	for _, target := range selected {
		if e = ctx.Err(); e != nil {
			return e
		}
		addr := common.Address(target.Address)
		a, e := tr.GetAccount(addr)
		if e != nil {
			return e
		}
		p := stateAccountProof{Address: target.Address, Balance: "0x0", Nonce: "0x0", CodeHash: types.EmptyCodeHash, StorageHash: types.EmptyRootHash, StorageProof: []stateStorageProof{}}
		p.AccountProof, e = proofStrings(tr, addr[:])
		if e != nil {
			return e
		}
		if a != nil {
			if a.Extra != 0 || a.Balance == nil || len(a.CodeHash) != 32 {
				return execwire.ErrProtocol
			}
			p.Balance = hexutil.EncodeBig(a.Balance.ToBig())
			p.Nonce = hexutil.EncodeUint64(a.Nonce)
			p.CodeHash = common.BytesToHash(a.CodeHash)
			p.StorageHash = a.Root
		}
		ev := stateEvidence{Proof: p}
		if target.Code {
			code := []byte{}
			if a != nil && p.CodeHash != types.EmptyCodeHash {
				code, e = db.ContractCode(addr, p.CodeHash)
				if e != nil {
					return e
				}
			}
			if len(code) > 256<<10 {
				return execwire.ErrLimit
			}
			if crypto.Keccak256Hash(code) != p.CodeHash {
				return execwire.ErrProtocol
			}
			text := hexutil.Encode(code)
			ev.Code = &text
		}
		for _, slot := range target.Slots {
			if e = ctx.Err(); e != nil {
				return e
			}
			sp := stateStorageProof{Key: hexutil.Encode(slot[:]), Value: "0x0", Proof: []string{}}
			if a != nil {
				storage, e := db.OpenStorageTrie(root, addr, a.Root, tr)
				if e != nil {
					return e
				}
				value, e := storage.GetStorage(addr, slot[:])
				if e != nil {
					return e
				}
				sp.Value = hexutil.EncodeBig(common.BytesToHash(value).Big())
				sp.Proof, e = proofStrings(storage, slot[:])
				if e != nil {
					return e
				}
			}
			ev.Proof.StorageProof = append(ev.Proof.StorageProof, sp)
		}
		bundle.Evidence = append(bundle.Evidence, ev)
	}
	data, e := json.Marshal(bundle)
	if e != nil {
		return e
	}
	if len(data) > execwire.MaxStateProofBytes {
		return execwire.ErrLimit
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	n, e := w.Write(data)
	if e == nil && n != len(data) {
		e = io.ErrShortWrite
	}
	if e != nil {
		return e
	}
	return ctx.Err()
}

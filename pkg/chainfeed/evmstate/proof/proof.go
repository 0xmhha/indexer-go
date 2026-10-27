// Package proof verifies bounded, tracked EIP-1186 state snapshots. It does
// not execute transactions or infer complete state changes from receipts.
package proof

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

const (
	Codec          = "evm.state-proof.v1"
	MaxAccounts    = 8
	MaxSlots       = 128
	MaxNodes       = 65
	MaxNodeBytes   = 1024
	MaxProofBytes  = 2 << 20
	MaxCodeBytes   = 256 << 10
	MaxBundleBytes = 8 << 20
)

var ErrProof = errors.New("evmstate: invalid or incomplete proof")

type Target struct {
	Address common.Address `json:"address"`
	Code    bool           `json:"code"`
	Slots   []common.Hash  `json:"slots"`
}
type Selection struct {
	Targets []Target `json:"targets"`
}

func (s *Selection) UnmarshalJSON(raw []byte) error {
	if len(raw) > 16<<10 {
		return ErrProof
	}
	type plain Selection
	var out plain
	if e := configfile.JSON(raw, &out); e != nil {
		return e
	}
	*s = Selection(out)
	return nil
}

func boundedArray[T any](raw []byte, max int) ([]T, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('[') {
		return nil, ErrProof
	}
	var out []T
	for d.More() {
		if len(out) == max {
			return nil, ErrProof
		}
		var v T
		if e := d.Decode(&v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if _, e := d.Token(); e != nil {
		return nil, e
	}
	return out, nil
}

// Normalize copies/sorts the explicit tracked selection; duplicates are errors.
// The digest includes code intent, addresses and every exact 256-bit slot.
func (s Selection) Normalize() (Selection, common.Hash, error) {
	out := Selection{}
	if len(s.Targets) == 0 || len(s.Targets) > MaxAccounts {
		return out, common.Hash{}, ErrProof
	}
	n := 0
	for _, t := range s.Targets {
		n += len(t.Slots)
		if n > MaxSlots {
			return out, common.Hash{}, ErrProof
		}
		t.Slots = append([]common.Hash{}, t.Slots...)
		sort.Slice(t.Slots, func(i, j int) bool { return bytes.Compare(t.Slots[i][:], t.Slots[j][:]) < 0 })
		for i := 1; i < len(t.Slots); i++ {
			if t.Slots[i] == t.Slots[i-1] {
				return out, common.Hash{}, ErrProof
			}
		}
		out.Targets = append(out.Targets, t)
	}
	sort.Slice(out.Targets, func(i, j int) bool { return bytes.Compare(out.Targets[i].Address[:], out.Targets[j].Address[:]) < 0 })
	for i := 1; i < len(out.Targets); i++ {
		if out.Targets[i].Address == out.Targets[i-1].Address {
			return Selection{}, common.Hash{}, ErrProof
		}
	}
	b, _ := json.Marshal(out)
	return out, sha256.Sum256(append([]byte(Codec+"\x00"), b...)), nil
}

// Nodes limits cardinality before growing the slice and node hex before decode.
type Nodes []hexutil.Bytes

func (p *Nodes) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(raw, []byte("null")) {
		*p = nil
		return nil
	}
	if len(raw) > MaxNodes*(MaxNodeBytes*2+5)+2 {
		return ErrProof
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('[') {
		return ErrProof
	}
	var out Nodes
	for d.More() {
		if len(out) == MaxNodes {
			return ErrProof
		}
		var s string
		if e := d.Decode(&s); e != nil || len(s) < 4 || len(s) > MaxNodeBytes*2+2 {
			return ErrProof
		}
		b, e := hexutil.Decode(s)
		if e != nil {
			return ErrProof
		}
		out = append(out, b)
	}
	if _, e := d.Token(); e != nil {
		return ErrProof
	}
	*p = out
	return nil
}

type StorageProof struct {
	Key   string       `json:"key"`
	Value *hexutil.Big `json:"value"`
	Proof Nodes        `json:"proof"`
}
type Proof struct {
	Address      *common.Address `json:"address"`
	AccountProof Nodes           `json:"accountProof"`
	Balance      *hexutil.Big    `json:"balance"`
	CodeHash     *common.Hash    `json:"codeHash"`
	Nonce        *hexutil.Uint64 `json:"nonce"`
	StorageHash  *common.Hash    `json:"storageHash"`
	StorageProof []StorageProof  `json:"storageProof"`
}

func (p *Proof) UnmarshalJSON(raw []byte) error {
	if len(raw) > MaxProofBytes {
		return ErrProof
	}
	type plain Proof
	var v struct {
		*plain
		StorageProof json.RawMessage `json:"storageProof"`
	}
	v.plain = new(plain)
	if e := json.Unmarshal(raw, &v); e != nil {
		return fmt.Errorf("%w: response encoding", ErrProof)
	}
	storageRaw := v.StorageProof
	if bytes.Equal(bytes.TrimSpace(storageRaw), []byte("null")) {
		storageRaw = []byte("[]")
	}
	storage, e := boundedArray[StorageProof](storageRaw, MaxSlots)
	if e != nil {
		return ErrProof
	}
	v.plain.StorageProof = storage
	if v.Address == nil || v.Balance == nil || v.CodeHash == nil || v.Nonce == nil || v.StorageHash == nil {
		return ErrProof
	}
	*p = Proof(*v.plain)
	return nil
}

type Evidence struct {
	Proof Proof          `json:"proof"`
	Code  *hexutil.Bytes `json:"code,omitempty"`
}
type Bundle struct {
	Header    hexutil.Bytes `json:"header,omitempty"`
	Version   uint32        `json:"version"`
	Selection Selection     `json:"selection"`
	Evidence  []Evidence    `json:"evidence"`
}

func (b *Bundle) UnmarshalJSON(raw []byte) error {
	if len(raw) > MaxBundleBytes {
		return ErrProof
	}
	var v struct {
		Header    hexutil.Bytes   `json:"header,omitempty"`
		Version   uint32          `json:"version"`
		Selection Selection       `json:"selection"`
		Evidence  json.RawMessage `json:"evidence"`
	}
	if e := json.Unmarshal(raw, &v); e != nil {
		return e
	}
	if len(v.Header) > 64<<10 {
		return ErrProof
	}
	evidence, e := boundedArray[Evidence](v.Evidence, MaxAccounts)
	if e != nil {
		return e
	}
	*b = Bundle{Version: v.Version, Selection: v.Selection, Evidence: evidence, Header: v.Header}
	return nil
}

type Slot struct {
	Key   common.Hash `json:"key"`
	Value common.Hash `json:"value"`
}
type Account struct {
	Address common.Address `json:"address"`
	Exists  bool           `json:"exists"`
	Nonce   uint64         `json:"nonce"`
	// Balance is the consensus account scalar, not an automatic mapping to
	// a chain profile's visible native asset (for example Arc native USDC).
	Balance     *hexutil.Big   `json:"balance"`
	StorageRoot common.Hash    `json:"storageRoot"`
	CodeHash    common.Hash    `json:"codeHash"`
	Code        *hexutil.Bytes `json:"code,omitempty"`
	Storage     []Slot         `json:"storage"`
}

func nodeValue(root common.Hash, key []byte, nodes Nodes) ([]byte, error) {
	if len(nodes) > MaxNodes {
		return nil, ErrProof
	}
	if root == types.EmptyRootHash {
		if len(nodes) != 0 {
			return nil, ErrProof
		}
		return nil, nil
	}
	db := memorydb.New()
	defer func() { _ = db.Close() }()
	for _, b := range nodes {
		if len(b) == 0 || len(b) > MaxNodeBytes {
			return nil, ErrProof
		}
		if e := db.Put(crypto.Keccak256(b), b); e != nil {
			return nil, e
		}
	}
	v, e := trie.VerifyProof(root, key, db)
	if e != nil {
		return nil, fmt.Errorf("%w: trie path", ErrProof)
	}
	return v, nil
}
func storageKey(s string) (common.Hash, error) {
	if len(s) < 3 || len(s) > 66 || s[:2] != "0x" {
		return common.Hash{}, ErrProof
	}
	v := s[2:]
	if len(v)%2 != 0 {
		v = "0" + v
	}
	b, e := hex.DecodeString(v)
	if e != nil {
		return common.Hash{}, ErrProof
	}
	return common.BytesToHash(b), nil
}

// Verify derives values from the proof, compares every RPC field, and hashes
// supplied code. Empty/nonexistent values require a valid absence proof.
func Verify(root common.Hash, selection Selection, bundle Bundle) ([]Account, error) {
	return VerifyContext(context.Background(), root, selection, bundle)
}

func VerifyContext(ctx context.Context, root common.Hash, selection Selection, bundle Bundle) ([]Account, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	s, digest, e := selection.Normalize()
	if e != nil || root == (common.Hash{}) {
		return nil, ErrProof
	}
	_, actual, e := bundle.Selection.Normalize()
	if e != nil || digest != actual || bundle.Version != 1 || len(bundle.Evidence) != len(s.Targets) {
		return nil, ErrProof
	}
	var out []Account
	for i, t := range s.Targets {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		ev := bundle.Evidence[i]
		p := ev.Proof
		if p.Address == nil || *p.Address != t.Address || p.Balance == nil || p.Nonce == nil || p.CodeHash == nil || p.StorageHash == nil || len(p.StorageProof) != len(t.Slots) {
			return nil, ErrProof
		}
		value, e := nodeValue(root, crypto.Keccak256(t.Address[:]), p.AccountProof)
		if e != nil {
			return nil, e
		}
		a := Account{Address: t.Address, Exists: value != nil, Balance: new(hexutil.Big), StorageRoot: types.EmptyRootHash, CodeHash: types.EmptyCodeHash, Storage: []Slot{}}
		if value != nil {
			var state types.StateAccount
			if e := rlp.DecodeBytes(value, &state); e != nil || state.Balance == nil || len(state.CodeHash) != 32 {
				return nil, ErrProof
			}
			a.Nonce, a.Balance, a.StorageRoot, a.CodeHash = state.Nonce, (*hexutil.Big)(state.Balance.ToBig()), state.Root, common.BytesToHash(state.CodeHash)
		}
		// Geth reports zero roots/hashes for a proven absent account. The
		// alternative empty-trie/code hashes are also unambiguous absence.
		codeMatch := *p.CodeHash == a.CodeHash || (!a.Exists && *p.CodeHash == (common.Hash{}))
		rootMatch := *p.StorageHash == a.StorageRoot || (!a.Exists && *p.StorageHash == (common.Hash{}))
		if a.Nonce != uint64(*p.Nonce) || a.Balance.ToInt().Cmp(p.Balance.ToInt()) != 0 || !codeMatch || !rootMatch {
			return nil, ErrProof
		}
		if t.Code {
			if ev.Code == nil || len(*ev.Code) > MaxCodeBytes || crypto.Keccak256Hash(*ev.Code) != a.CodeHash {
				return nil, ErrProof
			}
			b := hexutil.Bytes(bytes.Clone(*ev.Code))
			a.Code = &b
		} else if ev.Code != nil {
			return nil, ErrProof
		}
		for j, key := range t.Slots {
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			sp := p.StorageProof[j]
			got, e := storageKey(sp.Key)
			if e != nil || got != key || sp.Value == nil {
				return nil, ErrProof
			}
			v, e := nodeValue(a.StorageRoot, crypto.Keccak256(key[:]), sp.Proof)
			if e != nil {
				return nil, e
			}
			var scalar []byte
			if v != nil {
				if e := rlp.DecodeBytes(v, &scalar); e != nil || len(scalar) == 0 || len(scalar) > 32 || scalar[0] == 0 {
					return nil, ErrProof
				}
			}
			n := new(big.Int).SetBytes(scalar)
			if n.Cmp(sp.Value.ToInt()) != 0 {
				return nil, ErrProof
			}
			a.Storage = append(a.Storage, Slot{Key: key, Value: common.BytesToHash(scalar)})
		}
		out = append(out, a)
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return out, nil
}

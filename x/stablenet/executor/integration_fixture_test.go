package executor

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// Frozen fixtures bridge mutually incompatible geth modules. Every normal
// executor test regenerates and byte-compares actual first/second executions;
// root integration tests consume the exact same checked-in wire bytes.
type frozenBaseline struct {
	Ref      execwire.Ref
	Root     execwire.Hash
	Accounts []execwire.AccountChange
	Storage  []execwire.StorageChange
	Code     []execwire.Code
}
type frozenState struct {
	Address execwire.Address
	Account execwire.Account
	Code    []byte
}
type frozenFixture struct {
	Version      int
	Profile      string
	Chain        execwire.Chain
	ParentHeader []byte
	Baseline     frozenBaseline
	Payload      []byte
	Accounts     []frozenState
	Storage      []execwire.StorageChange
}

func freezeExecution(t *testing.T, profile string, in ReplayInput) []byte {
	t.Helper()
	in.TraceProfile = profile
	out, err := Replay(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	before, err := in.Database.OpenTrie(in.Parent.Root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := in.Database.OpenTrie(in.Block.Root())
	if err != nil {
		t.Fatal(err)
	}
	initial, err := state.New(in.Parent.Root, in.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	final, err := state.New(in.Block.Root(), in.Database, nil)
	if err != nil {
		t.Fatal(err)
	}
	f := frozenFixture{Version: 1, Profile: profile, Chain: execwire.Chain{ID: in.Chain.Config().ChainID.String(), Genesis: execwire.Hash(in.Parent.Hash())}, Baseline: frozenBaseline{Ref: execwire.Ref{Generation: 1, Hash: execwire.Hash(in.Parent.Hash())}, Root: execwire.Hash(in.Parent.Root)}}
	f.ParentHeader, err = rlp.EncodeToBytes(in.Parent)
	if err != nil {
		t.Fatal(err)
	}
	f.Payload, err = execwire.MarshalResult(out)
	if err != nil {
		t.Fatal(err)
	}
	addresses := map[execwire.Address]bool{}
	slots := map[[52]byte]execwire.StorageChange{}
	codes := map[execwire.Hash]bool{}
	for _, step := range out.Steps {
		for _, a := range step.Accounts {
			addresses[a.Address] = true
		}
		for _, s := range step.Storage {
			var key [52]byte
			copy(key[:20], s.Address[:])
			copy(key[20:], s.Slot[:])
			slots[key] = s
		}
	}
	for addr := range addresses {
		a, err := before.GetAccount(common.Address(addr))
		if err != nil {
			t.Fatal(err)
		}
		b := accountValue(a)
		f.Baseline.Accounts = append(f.Baseline.Accounts, execwire.AccountChange{Address: addr, Before: b, After: b, Baseline: true})
		if b.Exists && b.CodeHash != execwire.Hash(types.EmptyCodeHash) && !codes[b.CodeHash] {
			code, err := in.Database.ContractCode(common.Address(addr), common.Hash(b.CodeHash))
			if err != nil {
				t.Fatal(err)
			}
			f.Baseline.Code = append(f.Baseline.Code, execwire.Code{Hash: b.CodeHash, Bytes: code})
			codes[b.CodeHash] = true
		}
		last, err := after.GetAccount(common.Address(addr))
		if err != nil {
			t.Fatal(err)
		}
		f.Accounts = append(f.Accounts, frozenState{Address: addr, Account: accountValue(last), Code: final.GetCode(common.Address(addr))})
	}
	for _, slot := range slots {
		v := initial.GetState(common.Address(slot.Address), common.Hash(slot.Slot))
		f.Baseline.Storage = append(f.Baseline.Storage, execwire.StorageChange{Address: slot.Address, Slot: slot.Slot, Before: execwire.Word(v), After: execwire.Word(v), Baseline: true})
		slot.Before = execwire.Word(v)
		slot.After = execwire.Word(final.GetState(common.Address(slot.Address), common.Hash(slot.Slot)))
		slot.Baseline = false
		f.Storage = append(f.Storage, slot)
	}
	sort.Slice(f.Baseline.Accounts, func(i, j int) bool {
		return bytes.Compare(f.Baseline.Accounts[i].Address[:], f.Baseline.Accounts[j].Address[:]) < 0
	})
	sort.Slice(f.Accounts, func(i, j int) bool { return bytes.Compare(f.Accounts[i].Address[:], f.Accounts[j].Address[:]) < 0 })
	sort.Slice(f.Baseline.Code, func(i, j int) bool { return bytes.Compare(f.Baseline.Code[i].Hash[:], f.Baseline.Code[j].Hash[:]) < 0 })
	less := func(a, b execwire.StorageChange) bool {
		c := bytes.Compare(a.Address[:], b.Address[:])
		return c < 0 || c == 0 && bytes.Compare(a.Slot[:], b.Slot[:]) < 0
	}
	sort.Slice(f.Baseline.Storage, func(i, j int) bool { return less(f.Baseline.Storage[i], f.Baseline.Storage[j]) })
	sort.Slice(f.Storage, func(i, j int) bool { return less(f.Storage[i], f.Storage[j]) })
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}
func TestFrozenExecutionFixturesMatchActualEVMAndWBFT(t *testing.T) {
	fixtures := map[string]func(*testing.T) (string, ReplayInput){
		"wbft-native": func(t *testing.T) (string, ReplayInput) { _, in, _, _, _ := wbftFixture(t); return "stablenet", in },
		"wbft-native-fork": func(t *testing.T) (string, ReplayInput) {
			_, in, _, _, _ := wbftFixtureMint(t, 10)
			return "stablenet", in
		},
		"evm-creations": func(t *testing.T) (string, ReplayInput) {
			_, in := evmFixtureWith(t, []byte{0}, []evmTxSpec{{input: []byte{0x60, 0, 0x60, 0, 0xf3}}, {input: append(append([]byte{0x73}, common.Address{88}.Bytes()...), 0xff)}, {input: []byte{0x60, 0, 0x60, 0, 0xfd}}})
			return "evm", in
		},
		"evm-storage": func(t *testing.T) (string, ReplayInput) { _, in := evmFixture(t); return "evm", in },
		"evm-calls": func(t *testing.T) (string, ReplayInput) {
			_, in := evmFixtureWith(t, allNativeCallsCode(), []evmTxSpec{{to: &account, value: 1}})
			return "evm", in
		},
		"evm-revert": func(t *testing.T) (string, ReplayInput) {
			_, in := evmFixtureWith(t, []byte{0x60, 3, 0x60, 0, 0x55, 0x60, 0, 0x60, 0, 0xfd}, []evmTxSpec{{to: &account, value: 1}})
			return "evm", in
		},
	}
	for name, build := range fixtures {
		t.Run(name, func(t *testing.T) {
			profile, in := build(t)
			snapshot := dump(t, in.Database.DiskDB())
			data := freezeExecution(t, profile, in)
			if !reflect.DeepEqual(snapshot, dump(t, in.Database.DiskDB())) {
				t.Fatal("fixture replay changed durable state")
			}
			path := filepath.Join("..", "..", "..", "testdata", "execution", name+".json")
			if os.Getenv("INDEXER_UPDATE_EXECUTION_FIXTURES") == "1" {
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want, data) {
				t.Fatal("frozen execution fixture differs from actual EVM/WBFT")
			}
		})
	}
}

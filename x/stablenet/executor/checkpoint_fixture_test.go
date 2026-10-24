package executor

import (
	"bytes"
	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	snapshot "github.com/0xmhha/indexer-go/x/stablenet/executor/checkpoint"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFrozenCheckpointFixturesMatchOfficialWBFTState(t *testing.T) {
	disk, in, _, _, _ := wbftFixture(t)
	before := dump(t, disk)
	for _, spec := range []struct {
		name, mode string
		header     *types.Header
		targets    []snapshot.Target
	}{{"wbft-genesis", "genesis", in.Parent, nil}, {"wbft-checkpoint", "checkpoint", in.Block.Header(), nil}, {"wbft-partial", "checkpoint", in.Block.Header(), []snapshot.Target{{Address: execwire.Address(in.Chain.Config().Anzeon.SystemContracts.NativeCoinAdapter.Address), Slots: []execwire.Word{{31: 1}, {31: 2}}}, {Address: execwire.Address{19: 0xfe}, Slots: []execwire.Word{{31: 1}, {31: 2}}}}}} {
		t.Run(spec.name, func(t *testing.T) {
			h := spec.header
			header, err := rlp.EncodeToBytes(h)
			if err != nil {
				t.Fatal(err)
			}
			m := format.Manifest{Version: 1, Mode: spec.mode, Coverage: "full", Chain: execwire.Chain{ID: in.Chain.Config().ChainID.String(), Genesis: execwire.Hash(in.Parent.Hash())}, Ref: execwire.Ref{Generation: 1, Height: h.Number.Uint64(), Hash: execwire.Hash(h.Hash())}, Root: execwire.Hash(h.Root), Header: header}
			if spec.targets != nil {
				m.Coverage = "partial"
			}
			var out bytes.Buffer
			if err = snapshot.Export(t.Context(), &out, m, in.Database, spec.targets); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("..", "..", "..", "testdata", "execution", "checkpoints", spec.name+".ndjson")
			if os.Getenv("INDEXER_UPDATE_EXECUTION_FIXTURES") == "1" {
				if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, out.Bytes(), 0644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want, out.Bytes()) {
				t.Fatal("checkpoint fixture differs from actual official state")
			}
		})
	}
	if !reflect.DeepEqual(before, dump(t, disk)) {
		t.Fatal("checkpoint export wrote durable node state")
	}
}

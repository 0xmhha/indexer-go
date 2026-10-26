package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/checkpoint"
	"github.com/ethereum/go-ethereum/common"
	"reflect"
	"testing"
)

func TestNativeStateProofExportHasInclusionAndAbsence(t *testing.T) {
	_, in := evmFixture(t)
	before := dump(t, in.Database.DiskDB())
	var buf bytes.Buffer
	targets := []execwire.StateProofTarget{{Address: execwire.Address(account), Code: true, Slots: []execwire.Word{{}, {31: 255}}}, {Address: execwire.Address(common.Address{19: 255}), Code: true, Slots: []execwire.Word{{}}}}
	if e := checkpoint.ExportStateProofs(t.Context(), &buf, in.Database, in.Block.Root(), targets); e != nil {
		t.Fatal(e)
	}
	var got struct {
		Version  uint32
		Evidence []struct {
			Proof struct {
				AccountProof []string `json:"accountProof"`
				StorageProof []struct {
					Proof []string `json:"proof"`
				} `json:"storageProof"`
			}
			Code *string `json:"code"`
		}
	}
	if e := json.Unmarshal(buf.Bytes(), &got); e != nil {
		t.Fatalf("native evidence must contain actual proof paths: %v", e)
	}
	if got.Version != 1 || len(got.Evidence) != 2 {
		t.Fatalf("incomplete evidence: %s", buf.Bytes())
	}
	if len(got.Evidence[0].Proof.AccountProof) == 0 || len(got.Evidence[0].Proof.StorageProof) != 2 || got.Evidence[0].Code == nil || len(got.Evidence[1].Proof.AccountProof) == 0 {
		t.Fatal("missing inclusion/absence/code paths")
	}
	if !reflect.DeepEqual(before, dump(t, in.Database.DiskDB())) {
		t.Fatal("native proof export mutated source DB")
	}
}

type proofCancelWriter struct{ cancel context.CancelFunc }

func (w proofCancelWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }
func TestNativeStateProofExportReturnsCanceledAfterWrite(t *testing.T) {
	_, in := evmFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e := checkpoint.ExportStateProofs(ctx, proofCancelWriter{cancel}, in.Database, in.Block.Root(), []execwire.StateProofTarget{{Address: execwire.Address(account)}})
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("canceled export reported success: %v", e)
	}
}

func TestNativeStateProofExportBoundsFailWithoutOutput(t *testing.T) {
	_, in := evmFixture(t)
	for _, targets := range [][]execwire.StateProofTarget{nil, make([]execwire.StateProofTarget, 9), {{Address: execwire.Address(account), Slots: make([]execwire.Word, 129)}}, {{Address: execwire.Address(account)}, {Address: execwire.Address(account)}}} {
		var buf bytes.Buffer
		if e := checkpoint.ExportStateProofs(t.Context(), &buf, in.Database, in.Block.Root(), targets); e == nil {
			t.Fatal("invalid selection accepted")
		}
		if buf.Len() != 0 {
			t.Fatal("invalid selection wrote output")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var buf bytes.Buffer
	if e := checkpoint.ExportStateProofs(ctx, &buf, in.Database, in.Block.Root(), []execwire.StateProofTarget{{Address: execwire.Address(account)}}); !errors.Is(e, context.Canceled) || buf.Len() != 0 {
		t.Fatal("canceled export returned/wrote success")
	}
}

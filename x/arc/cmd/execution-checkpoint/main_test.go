package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestArcCheckpointVerifiesNativeBaselineWithoutDBOrRPC(t *testing.T) {
	payload, err := os.ReadFile("../../../../testdata/execution/arc-storage.json")
	require.NoError(t, err)
	var f struct {
		Chain        execwire.Chain
		Baseline     port.ExecutionBaseline
		ParentHeader []byte
	}
	require.NoError(t, json.Unmarshal(payload, &f))
	m := format.Manifest{Version: 1, Mode: "genesis", Coverage: "full", Chain: f.Chain, Ref: f.Baseline.Ref, Root: f.Baseline.Root, Header: f.ParentHeader}
	var snapshot bytes.Buffer
	require.NoError(t, format.Write(&snapshot, format.Record{Kind: "manifest", Manifest: &m}))
	code := map[execwire.Hash][]byte{}
	for _, c := range f.Baseline.Code {
		code[c.Hash] = c.Bytes
	}
	slices.SortFunc(f.Baseline.Accounts, func(a, b execwire.AccountChange) int {
		return bytes.Compare(crypto.Keccak256(a.Address[:]), crypto.Keccak256(b.Address[:]))
	})
	for _, a := range f.Baseline.Accounts {
		require.NoError(t, format.Write(&snapshot, format.Record{Kind: "account", Address: a.Address, Account: &a.After, Code: code[a.After.CodeHash]}))
		slots := []execwire.StorageChange{}
		for _, s := range f.Baseline.Storage {
			if s.Address == a.Address && s.After != (execwire.Word{}) {
				slots = append(slots, s)
			}
		}
		slices.SortFunc(slots, func(a, b execwire.StorageChange) int {
			return bytes.Compare(crypto.Keccak256(a.Slot[:]), crypto.Keccak256(b.Slot[:]))
		})
		for _, s := range slots {
			require.NoError(t, format.Write(&snapshot, format.Record{Kind: "storage", Address: s.Address, Slot: s.Slot, Value: s.After}))
		}
	}
	require.NoError(t, format.Write(&snapshot, format.Record{Kind: "end"}))
	dir := t.TempDir()
	input, expected := filepath.Join(dir, "state.ndjson"), filepath.Join(dir, "expected.json")
	require.NoError(t, os.WriteFile(input, snapshot.Bytes(), 0600))
	manifest, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(expected, manifest, 0600))
	t.Setenv("INDEXER_DB_POSTGRES_DSN", "invalid-dsn")
	t.Setenv("INDEXER_CHECKPOINT_RPC", "not-a-rpc")
	var out bytes.Buffer
	require.NoError(t, run(t.Context(), []string{"--input", input, "--expected", expected}, &out))
	require.Contains(t, out.String(), `"Coverage":"full"`)
	require.Error(t, run(t.Context(), []string{"--action", "import", "--input", input, "--expected", expected}, &out))
	out.Reset()
	require.NoError(t, run(t.Context(), []string{"--help"}, &out))
	require.Contains(t, out.String(), "-action")
}

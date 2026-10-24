package executionops

import (
	"bytes"
	"encoding/json"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyToolDoesNotOpenDBOrRPC(t *testing.T) {
	path := "../../testdata/execution/checkpoints/wbft-genesis.ndjson"
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	first, err := format.NewDecoder(bytes.NewReader(payload)).Next()
	require.NoError(t, err)
	expected, err := json.Marshal(first.Manifest)
	require.NoError(t, err)
	identity := filepath.Join(t.TempDir(), "expected.json")
	require.NoError(t, os.WriteFile(identity, expected, 0600))
	t.Setenv("INDEXER_DB_POSTGRES_DSN", "invalid-dsn")
	t.Setenv("INDEXER_CHECKPOINT_RPC", "not-a-rpc")
	var out bytes.Buffer
	require.NoError(t, Run(t.Context(), Options{Action: "verify", Input: path, Expected: identity}, stablenet.New(), &out))
	require.Contains(t, out.String(), `"Coverage":"full"`)
	require.Error(t, Run(t.Context(), Options{Action: "import", Input: path, Expected: identity}, stablenet.New(), &out))
	require.Error(t, Run(t.Context(), Options{Action: "invalid"}, stablenet.New(), &out))
}

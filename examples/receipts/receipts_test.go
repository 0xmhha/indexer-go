package receipts_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/examples/receipts"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// TestReceiptsIndexer is the R6-2 criterion (refactoring plan): this module
// is outside the indexer and changes none of it. It builds its own binary
// (the indexer plus this package's handlers, through pkg/sdk), runs it with
// a configuration file against a test chain, and gets what its handlers
// serve: here the receipts.totals feature, kept block by block in the
// indexer's key-value store. The receipt lookup is P07's (acceptance_test.go).
func TestReceiptsIndexer(t *testing.T) {
	sc := testchain.BuildReceipts()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := t.TempDir()
	port := freePort(t)
	cfg := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(config(sc, srv.URL(), dir, port, 0)), 0o600))
	ix := start(t, cfg, port)

	m1, m2 := sc.Merchants[0].Hex(), sc.Merchants[1].Hex()
	require.Eventually(t, func() bool {
		code, body := ix.get("/merchants/" + m1 + "/totals")
		return code == http.StatusOK && body["receipts"] == float64(3)
	}, time.Minute, 20*time.Millisecond, "the first merchant's three receipts")
	code, body := ix.get("/merchants/" + m1 + "/totals")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{"receipts": float64(3), "amount": "6200"}, body, "orders 1 and 2, and order 1 again")
	code, body = ix.get("/merchants/" + m2 + "/totals")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{"receipts": float64(1), "amount": "800"}, body)
	code, _ = ix.get("/merchants/not-an-address/totals")
	assert.Equal(t, http.StatusBadRequest, code)
}

// TestShort pins the shortened hash format of the receipts.
func TestShort(t *testing.T) {
	assert.Equal(t, "0x123456…cdef", receipts.Short("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"))
	assert.Equal(t, "0x12", receipts.Short("0x12"))
}

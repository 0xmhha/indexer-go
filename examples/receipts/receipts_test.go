package receipts_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/examples/receipts"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// config writes the indexer configuration of the example: the declared
// mode over the settlement contract of the receipts scenario, its receipts
// table, the receipts.totals feature and the API on port.
func config(sc *testchain.ReceiptsScenario, rpc, dir string, port int) string {
	return fmt.Sprintf(`rpc:
  endpoint: %q
database:
  path: %q
log:
  level: warn
indexer:
  mode: declared
  poll_interval: 10ms
api:
  enabled: true
  host: 127.0.0.1
  port: %d
  enable_graphql: true
features:
  records:
    enabled: true
    sources:
      - name: settlement
        address: %q
        events:
          - %q
    tables:
      - name: receipts
        source: settlement
        event: PaymentSettled
        keys:
          - [merchant, orderId]
  receipts.totals:
    enabled: true
`, rpc, filepath.Join(dir, "db"), port, sc.Settlement.Hex(), testchain.PaymentSettledSignature)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func get(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		return 0, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return resp.StatusCode, out
}

// TestReceiptsIndexer is the R6-2 criterion (refactoring plan): this module
// is outside the indexer and changes none of it. It builds its own binary
// (the indexer plus this package's handlers, through pkg/sdk), runs it with
// a configuration file against a test chain, and gets the receipts and
// merchant totals its handlers serve.
func TestReceiptsIndexer(t *testing.T) {
	sc := testchain.BuildReceipts()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := t.TempDir()
	port := freePort(t)
	cfg := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(config(sc, srv.URL(), dir, port)), 0o600))

	bin := filepath.Join(dir, "receipts-indexer")
	build := exec.Command("go", "build", "-o", bin, "./cmd/receipts-indexer")
	out, err := build.CombinedOutput()
	require.NoError(t, err, "%s", out)

	cmd := exec.Command(bin, "--config", cfg)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	require.NoError(t, cmd.Start())
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGINT)
		_ = cmd.Wait()
	}()
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	m1, m2 := strings.ToLower(sc.Merchants[0].Hex()), sc.Merchants[1].Hex()

	// The handlers' feature has seen every payment once the totals of both
	// merchants are complete.
	require.Eventually(t, func() bool {
		code, body := get(t, base+"/merchants/"+m1+"/totals")
		return code == http.StatusOK && body["receipts"] == float64(3)
	}, time.Minute, 50*time.Millisecond, "the first merchant's three receipts")
	code, body := get(t, base+"/merchants/"+m1+"/totals")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{"receipts": float64(3), "amount": "6200"}, body, "orders 1 and 2, and order 1 again")
	code, body = get(t, base+"/merchants/"+m2+"/totals")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{"receipts": float64(1), "amount": "800"}, body)

	// The receipt of an order settled twice is its earliest log, marked.
	first := sc.Payments[0]
	code, body = get(t, base+"/receipts/"+sc.Merchants[0].Hex()+"/"+first.OrderID.Hex())
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, m1, body["merchant"])
	assert.Equal(t, first.OrderID.Hex(), body["orderId"])
	assert.Equal(t, strings.ToLower(sc.Device.Hex()), body["device"])
	assert.Equal(t, "2500", body["amount"])
	assert.Equal(t, float64(first.Block), body["blockNumber"])
	assert.NotZero(t, body["blockTime"])
	assert.Regexp(t, `^0x[0-9a-f]{6}…[0-9a-f]{4}$`, body["txHashShort"])
	assert.Equal(t, true, body["duplicate"])

	code, body = get(t, base+"/receipts/"+m1+"/"+sc.Payments[1].OrderID.Hex())
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, false, body["duplicate"], "order 2 settled once")
	assert.Equal(t, "1200", body["amount"])

	code, body = get(t, base+"/receipts/"+m2+"/0x"+strings.Repeat("ab", 32))
	assert.Equal(t, http.StatusNotFound, code)
	assert.Equal(t, map[string]any{"error": "NOT_INDEXED"}, body)
	code, _ = get(t, base+"/receipts/"+m2+"/42")
	assert.Equal(t, http.StatusBadRequest, code)

	// Declared data only: the explorer's JSON-RPC is not served.
	resp, err := http.Post(base+"/rpc", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`))
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestShort pins the shortened hash format of the receipts.
func TestShort(t *testing.T) {
	assert.Equal(t, "0x123456…cdef", receipts.Short("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"))
	assert.Equal(t, "0x12", receipts.Short("0x12"))
}

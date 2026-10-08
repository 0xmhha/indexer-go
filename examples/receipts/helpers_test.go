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
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// config writes the indexer configuration of the example: the declared
// mode over the settlement contract of the receipts scenario from
// startBlock, finalized blocks only, its receipts table, the
// receipts.totals feature and the API on port.
func config(sc *testchain.ReceiptsScenario, rpc, dir string, port int, startBlock uint64) string {
	return fmt.Sprintf(`rpc:
  endpoint: %q
  timeout: 2s
database:
  path: %q
log:
  level: error
indexer:
  mode: declared
  finality: finalized
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
        start_block: %d
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
`, rpc, filepath.Join(dir, "db"), port, sc.Settlement.Hex(), startBlock, testchain.PaymentSettledSignature)
}

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// binary builds the example's indexer once per test run.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "receipts-indexer")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "receipts-indexer")
		out, err := exec.Command("go", "build", "-o", binPath, "./cmd/receipts-indexer").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("%w: %s", err, out)
		}
	})
	require.NoError(t, buildErr)
	return binPath
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// indexer is a running receipts-indexer.
type indexer struct {
	cmd  *exec.Cmd
	base string
}

// start runs the example's indexer with cfg and waits for its API.
func start(t *testing.T, cfg string, port int) *indexer {
	t.Helper()
	cmd := exec.Command(binary(t), "--config", cfg)
	cmd.Stdout, cmd.Stderr = io.Discard, os.Stderr
	require.NoError(t, cmd.Start())
	ix := &indexer{cmd: cmd, base: "http://127.0.0.1:" + strconv.Itoa(port)}
	t.Cleanup(ix.stop)
	require.Eventually(t, func() bool { code, _ := ix.get("/health"); return code == http.StatusOK }, 30*time.Second, 20*time.Millisecond)
	return ix
}

// stop interrupts the indexer and waits for it to exit.
func (ix *indexer) stop() {
	if ix.cmd.ProcessState != nil {
		return
	}
	_ = ix.cmd.Process.Signal(syscall.SIGINT)
	_ = ix.cmd.Wait()
}

// get returns a response's status and JSON body (nil when not an object).
func (ix *indexer) get(path string) (int, map[string]any) {
	resp, err := http.Get(ix.base + path)
	if err != nil {
		return 0, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return resp.StatusCode, out
}

// waitCursor waits until the indexer reports the cursor at block n.
func (ix *indexer) waitCursor(t *testing.T, n uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		code, body := ix.get("/healthz")
		return code == http.StatusOK && body["cursor"] == float64(n)
	}, 30*time.Second, 10*time.Millisecond, "cursor at %d", n)
}

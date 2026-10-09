package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// buildIndexer builds the indexer command into dir.
func buildIndexer(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "indexer")
	out, err := exec.Command("go", "build", "-o", bin, "../../cmd/indexer").CombinedOutput()
	require.NoError(t, err, "%s", out)
	return bin
}

// latestHeight asks a running indexer for its indexed height; ok is false
// while it does not answer.
func latestHeight(base string) (uint64, bool) {
	body, _ := json.Marshal(map[string]string{"query": "{ latestHeight }"})
	client := http.Client{Timeout: time.Second}
	resp, err := client.Post(base+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, false
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Data struct {
			LatestHeight string `json:"latestHeight"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || out.Data.LatestHeight == "" {
		return 0, false
	}
	h, err := strconv.ParseUint(out.Data.LatestHeight, 10, 64)
	return h, err == nil
}

// TestKillAtRandomPointsRecovers (refactoring plan R0-2 and R2-3): the
// indexer process is killed with SIGKILL at random moments while it
// indexes a chain (in a block, during a commit, while starting up), and
// restarted each time; once it reaches the head the database equals one
// that indexed the chain without interruption. Pebble only: a PostgreSQL
// transaction is the server's to keep.
func TestKillAtRandomPointsRecovers(t *testing.T) {
	pebbleOnly(t)
	if testing.Short() {
		t.Skip("builds and kills the indexer binary")
	}
	sc := testchain.BuildLoad(600, 40, 0)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	head := sc.Chain.Head()

	work := t.TempDir()
	bin := buildIndexer(t, work)
	dir := filepath.Join(work, "db")
	port := freeAPIPort(t)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	cfgPath := filepath.Join(work, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(fmt.Sprintf(`rpc:
  endpoint: %q
database:
  path: %q
log:
  level: error
indexer:
  workers: 8
  poll_interval: 10ms
api:
  enabled: true
  host: 127.0.0.1
  port: %d
  enable_graphql: true
  enable_jsonrpc: false
  enable_websocket: false
features:
  stablenet.system_contracts:
    enabled: true
`, srv.URL(), dir, port)), 0o600))

	start := func() *exec.Cmd {
		cmd := exec.Command(bin, "--config", cfgPath)
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Start())
		return cmd
	}

	seed := time.Now().UnixNano()
	t.Logf("seed %d", seed)
	rng := rand.New(rand.NewSource(seed))
	var heights []uint64
	midway := 0
	for kill := 0; kill < 12; kill++ {
		cmd := start()
		// Every third kill counts from the process start, so some land in
		// startup recovery; the others from when the API answers, so a
		// slow start (a loaded machine) does not move them all before
		// indexing begins.
		if kill%3 != 0 {
			deadline := time.Now().Add(30 * time.Second)
			for _, ok := latestHeight(base); !ok && time.Now().Before(deadline); _, ok = latestHeight(base) {
				time.Sleep(5 * time.Millisecond)
			}
		}
		time.Sleep(time.Duration(20+rng.Intn(400)) * time.Millisecond)
		h, answered := latestHeight(base)
		require.NoError(t, cmd.Process.Signal(syscall.SIGKILL))
		_ = cmd.Wait()
		heights = append(heights, h)
		if answered && h > 0 && h < head {
			midway++
		}
		if answered && h >= head {
			break
		}
	}
	t.Logf("indexed heights seen before each kill: %v (head %d)", heights, head)
	require.GreaterOrEqual(t, midway, 3, "kills landed while the chain was being indexed")

	// The last run reaches the head and stops normally.
	cmd := start()
	require.Eventually(t, func() bool { h, ok := latestHeight(base); return ok && h == head }, 2*time.Minute, 20*time.Millisecond)
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	_ = cmd.Wait()

	// The same chain indexed without interruption, by the same wiring.
	fresh := filepath.Join(t.TempDir(), "fresh")
	app := startApp(t, srv, fresh)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	app.Shutdown()

	diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
}

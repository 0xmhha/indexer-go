package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
)

// Regenerate with: go test ./cmd/indexer -run TestGolden -update
var updateGolden = flag.Bool("update", false, "rewrite golden files")

const goldenKeyspace = "testdata/golden/keyspace.txt"

// ingestMode selects the fetcher's block write path.
type ingestMode struct {
	name   string
	atomic bool
}

var (
	legacyMode = ingestMode{name: "legacy", atomic: false}
	atomicMode = ingestMode{name: "atomic", atomic: true}
	allModes   = []ingestMode{legacyMode, atomicMode}
)

// defaultMode is the production default write path.
var defaultMode = atomicMode

// startApp builds the production single-chain wiring (NewApp) against srv
// with the database at dir, using the default write path. The caller must
// call app.Shutdown.
func startApp(t *testing.T, srv *testchain.Server, dir string) *App {
	return startAppMode(t, srv, dir, defaultMode)
}

func startAppMode(t testing.TB, srv *testchain.Server, dir string, mode ingestMode) *App {
	t.Helper()
	return startAppAt(t, srv.URL(), dir, mode)
}

// startAppAt is startAppMode for any RPC endpoint (for example a live node).
func startAppAt(t testing.TB, endpoint, dir string, mode ingestMode) *App {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = dir
	cfg.API.Enabled = false
	cfg.Indexer.StartHeight = 0
	cfg.Indexer.AtomicBlock = mode.atomic

	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	return app
}

// runSession starts the app on dir, indexes [from, to] the way the live loop
// does (FetchRange), and shuts it down cleanly.
func runSession(t *testing.T, srv *testchain.Server, dir string, from, to uint64) {
	t.Helper()
	runSessionMode(t, srv, dir, from, to, defaultMode)
}

func runSessionMode(t *testing.T, srv *testchain.Server, dir string, from, to uint64, mode ingestMode) {
	t.Helper()
	app := startAppMode(t, srv, dir, mode)
	defer app.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, from, to))
}

// indexScenario indexes every scenario block in one session and returns the
// database directory.
func indexScenario(t *testing.T, sc *testchain.Scenario) string {
	return indexScenarioMode(t, sc, defaultMode)
}

func indexScenarioMode(t *testing.T, sc *testchain.Scenario, mode ingestMode) string {
	t.Helper()
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)

	dir := filepath.Join(t.TempDir(), "db")
	runSessionMode(t, srv, dir, 0, sc.Chain.Head(), mode)

	require.Empty(t, srv.UnknownMethods(), "indexer called RPC methods the test chain does not implement")
	return dir
}

func dumpDir(t *testing.T, dir string) []testchain.Entry {
	t.Helper()
	entries, err := testchain.DumpKeyspace(dir, normalizeVolatile)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	return entries
}

// volatileJSONFields lists, per key prefix, JSON fields that hold wall-clock
// time instead of chain data. They are removed before comparison so the rest
// of the record is still pinned. Each entry is a determinism defect to fix in
// the owning feature (handlers must not read the clock).
var volatileJSONFields = map[string][]string{
	"/data/token/metadata/": {"createdAt", "updatedAt"}, // pkg/token/block_processor.go time.Now()
}

func normalizeVolatile(key, value []byte) ([]byte, bool) {
	for prefix, fields := range volatileJSONFields {
		if !bytes.HasPrefix(key, []byte(prefix)) {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(value, &m); err != nil {
			return value, true // not JSON: compare as is
		}
		for _, f := range fields {
			delete(m, f)
		}
		out, err := json.Marshal(m) // map keys are sorted
		if err != nil {
			return value, true
		}
		return out, true
	}
	return value, true
}

func dumpScenarioIndex(t *testing.T) []testchain.Entry {
	t.Helper()
	return dumpDir(t, indexScenario(t, testchain.BuildDefault()))
}

// TestGoldenKeyspace pins the complete storage contents produced by indexing
// the reference scenario. Phase 0 changes must only alter keys they intend to.
func TestGoldenKeyspace(t *testing.T) {
	entries := dumpScenarioIndex(t)

	var got bytes.Buffer
	require.NoError(t, testchain.FormatKeyspace(&got, entries))

	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenKeyspace), 0o755))
		require.NoError(t, os.WriteFile(goldenKeyspace, got.Bytes(), 0o644))
		return
	}
	want, err := os.ReadFile(goldenKeyspace)
	require.NoError(t, err, "missing golden file; run with -update")
	require.Equal(t, string(want), got.String())
}

// TestIndexIsDeterministic guards the golden test itself: two clean runs over
// the same chain must produce identical storage.
func TestIndexIsDeterministic(t *testing.T) {
	first := dumpScenarioIndex(t)
	second := dumpScenarioIndex(t)
	require.Empty(t, testchain.DiffKeyspace(first, second, 20))
}

// TestLegacyPathMatchesGolden keeps the fallback path honest until it is
// removed: on a clean run the legacy path must produce exactly the keyspace
// of the default (atomic) path.
func TestLegacyPathMatchesGolden(t *testing.T) {
	atomic := dumpScenarioIndex(t)
	legacy := dumpDir(t, indexScenarioMode(t, testchain.BuildDefault(), legacyMode))
	diff := testchain.DiffKeyspace(atomic, legacy, 0)
	require.Empty(t, diff, "legacy path differs from the default path: %v", testchain.SummarizeDiff(diff))
}

package app

import (
	"context"
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feature"
	"github.com/0xmhha/indexer-go/pkg/fetch"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// logCountName is a test feature that reads only the declared logs and
// counts them in the key-value store: a feature enabled after the
// declared table, to check its backfill.
const logCountName = "test.log_count"

var logCountKey = []byte("/x/test/logcount")

type logCountFeature struct{}

func (logCountFeature) Name() string       { return logCountName }
func (logCountFeature) Requires() []string { return nil }
func (logCountFeature) LogsOnly() bool     { return true }

func (logCountFeature) Register(r feature.Registrar) error {
	kv, ok := r.Deps().Storage.(port.KV)
	if !ok {
		return errors.New("no key-value store")
	}
	r.OnBlock(feature.BlockHandlerFunc(func(ctx context.Context, b *feature.Block) error {
		n := uint64(0)
		for _, r := range b.Receipts {
			n += uint64(len(r.Logs))
		}
		if n == 0 {
			return nil
		}
		return kv.Put(ctx, logCountKey, binary.BigEndian.AppendUint64(nil, logCount(ctx, kv)+n))
	}))
	return nil
}

func logCount(ctx context.Context, kv port.KV) uint64 {
	v, err := kv.Get(ctx, logCountKey)
	if err != nil {
		return 0
	}
	return binary.BigEndian.Uint64(v)
}

func init() {
	feature.Register(logCountFeature{})
	storage.RegisterKeyspace(logCountName, storage.ChainData, "/x/test/")
}

// longReceipts is the receipts scenario followed by plain blocks up to
// height head.
func longReceipts(head uint64) *testchain.ReceiptsScenario {
	sc := testchain.BuildReceipts()
	for sc.Chain.Head() < head {
		sc.Chain.AddBlock()
	}
	return sc
}

// sparseMode is the declared mode with finalized blocks.
func sparseMode(c *config.Config) {
	declaredMode(c)
	c.Indexer.Finality = fetch.FinalityFinalized
}

// TestSparseDeclaredIngest: with indexer.mode declared and finalized
// blocks the indexer reads a thousand blocks per eth_getLogs call and the
// headers of the blocks with declared logs only, stores just those blocks,
// keeps its cursor at the head, and continues after a restart.
func TestSparseDeclaredIngest(t *testing.T) {
	sc := longReceipts(3_500)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	ctx := context.Background()

	sc.Chain.SetHead(1_800)
	app := startRecordsApp(t, srv, dir, sc, sparseMode)
	runLiveUntil(t, app, 1_800)
	app.Shutdown()
	sc.Chain.SetHead(3_500)
	app = startRecordsApp(t, srv, dir, sc, sparseMode)
	defer app.Shutdown()
	runLiveUntil(t, app, 3_500)

	requireReceipts(t, ctx, app.storage.(port.RecordReader), sc)
	calls := srv.Calls()
	assert.LessOrEqual(t, calls["eth_getLogs"], 6, "a thousand blocks a call (and a restart): %d", calls["eth_getLogs"])
	withLogs := map[uint64]bool{}
	for _, p := range sc.Payments {
		withLogs[p.Block] = true
	}
	loads := srv.BlockLoads()
	for n := range loads {
		assert.True(t, withLogs[n], "read the header of block %d, which has no declared log", n)
	}
	for n := range withLogs {
		_, err := app.storage.GetBlock(ctx, n)
		assert.NoError(t, err, "block %d with logs is stored", n)
	}
	_, err := app.storage.GetBlock(ctx, 2_000)
	assert.ErrorIs(t, err, port.ErrNotFound, "a block without declared logs is not stored")
	latest, err := app.storage.GetLatestHeight(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(3_500), latest)
}

// TestSparseSplitsRefusedRanges: a node that refuses wide eth_getLogs
// ranges gets them in halves; nothing is missed.
func TestSparseSplitsRefusedRanges(t *testing.T) {
	sc := longReceipts(2_500)
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	srv.SetMaxLogRange(300)
	app := startRecordsApp(t, srv, filepath.Join(t.TempDir(), "db"), sc, sparseMode)
	defer app.Shutdown()
	runLiveUntil(t, app, 2_500)
	requireReceipts(t, context.Background(), app.storage.(port.RecordReader), sc)
	assert.Greater(t, srv.Calls()["eth_getLogs"], 6, "the refused thousand-block ranges were split")
}

// TestDeclaredBackfillReadsLogsFromTheNode: a feature enabled on a
// declared database is backfilled from the node's logs (the database keeps
// none), with every block's logs once, in both declared modes.
func TestDeclaredBackfillReadsLogsFromTheNode(t *testing.T) {
	for name, mode := range map[string]func(*config.Config){"per block": declaredMode, "finalized ranges": sparseMode} {
		t.Run(name, func(t *testing.T) {
			sc := longReceipts(1_500)
			srv := testchain.NewServer(sc.Chain)
			defer srv.Close()
			dir := filepath.Join(t.TempDir(), "db")
			app := startRecordsApp(t, srv, dir, sc, mode)
			runLiveUntil(t, app, 1_500)
			app.Shutdown()

			on := true
			app = startRecordsApp(t, srv, dir, sc, mode, func(c *config.Config) {
				c.Features[logCountName] = config.FeatureConfig{Enabled: &on}
			})
			defer app.Shutdown()
			app.fetcher.WaitBackground()
			assert.Equal(t, uint64(len(sc.Payments)), logCount(context.Background(), app.storage.(port.KV)),
				"every declared log counted once by the backfill")
		})
	}
}

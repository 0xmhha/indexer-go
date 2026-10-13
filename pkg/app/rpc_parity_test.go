package app

import (
	"context"
	"encoding/json"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/source/replay"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

var rpcParityBalances = flag.Bool("rpc-parity-balances", false, "also compare indexed balance snapshots with live node state")
var rpcParityArchive = flag.String("rpc-parity-archive", "", "replay a previously recorded contiguous range into both stores without a live node")

// TestLiveRPCStorageParity freezes one live range before replaying it into
// both drivers. The two drivers never query the changing live node.
// It is opt-in, bounded to 100 blocks per target, and sends no transactions.
func TestLiveRPCStorageParity(t *testing.T) {
	if *liveRPCTargets == "" {
		t.Skip("set -live-rpc-targets to a JSON file")
	}
	postgresDSN(t)
	raw, err := os.ReadFile(*liveRPCTargets)
	require.NoError(t, err)
	var targets []liveRPCTarget
	require.NoError(t, json.Unmarshal(raw, &targets))
	require.NotEmpty(t, targets)
	for _, target := range targets {
		t.Run(target.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
			defer cancel()
			c, err := gethrpc.DialContext(ctx, target.Endpoint)
			require.NoError(t, err)
			defer c.Close()
			info, err := sourcerpc.NodeInfo(ctx, c)
			require.NoError(t, err)
			require.Equal(t, target.ChainID, info.ChainID)
			src, err := sourcerpc.Detect(ctx, c)
			require.NoError(t, err)
			if target.Profile != "" {
				require.Equal(t, target.Profile, src.Profile().ID())
			}
			head, err := src.Head(ctx)
			require.NoError(t, err)
			blocks := max(target.Blocks, 1)
			require.LessOrEqual(t, blocks, uint64(100))
			require.GreaterOrEqual(t, head, blocks)
			from := head - blocks + 1
			archive := filepath.Join(t.TempDir(), "rpc")
			recorded := rpcParityIndex(t, ctx, target.Endpoint, archive, config.DriverPebble, from, head)
			verificationRPC, err := gethrpc.DialContext(ctx, recorded.config.RPC.Endpoint)
			require.NoError(t, err)
			defer verificationRPC.Close()
			// Identity checks and oracle reads share the recorder's rate limit.
			checkLiveRPCStorage(t, ctx, recorded, sourcerpc.New(verificationRPC, recorded.profile), from, head)
			want := dumpPortsFrom(t, recorded.storage, from)
			if *rpcParityBalances {
				checkRPCBalanceSnapshots(t, ctx, recorded, want)
			}
			recorded.Shutdown() // finish the archive before either replay
			compareRPCDrivers(t, ctx, archive, from, head, want)
			t.Logf("chain %d profile %s: identical RPC input at %d..%d; %d port reads matched", info.ChainID, src.Profile().ID(), from, head, len(want))
		})
	}
}

// TestRecordedRPCStorageParity exercises the same record/replay comparison
// without an external node, including starting above genesis.
func TestRecordedRPCStorageParity(t *testing.T) {
	postgresDSN(t)
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	from, head := uint64(1), sc.Chain.Head()
	archive := filepath.Join(t.TempDir(), "rpc")
	app := rpcParityIndex(t, ctx, srv.URL(), archive, config.DriverPebble, from, head)
	want := dumpPortsFrom(t, app.storage, from)
	checkRPCBalanceSnapshots(t, ctx, app, want)
	app.Shutdown()
	srv.Close() // replay must stand alone
	compareRPCDrivers(t, ctx, archive, from, head, want)
}

// TestRPCArchiveStorageParity reruns a frozen live sample without further
// external calls. Its manifest must describe the contiguous indexed range.
func TestRPCArchiveStorageParity(t *testing.T) {
	if *rpcParityArchive == "" {
		t.Skip("set -rpc-parity-archive to a recorded range")
	}
	postgresDSN(t)
	a, err := replay.Open(*rpcParityArchive)
	require.NoError(t, err)
	m := a.Manifest()
	require.Positive(t, m.Blocks)
	require.GreaterOrEqual(t, m.Last, m.First)
	require.LessOrEqual(t, m.Last-m.First, uint64(99))
	require.Equal(t, uint64(m.Blocks), m.Last-m.First+1, "a contiguous range, without contextual blocks")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	app := rpcParityIndex(t, ctx, "replay://"+*rpcParityArchive, "", config.DriverPebble, m.First, m.Last)
	want := dumpPortsFrom(t, app.storage, m.First)
	if *rpcParityBalances {
		checkRPCBalanceSnapshots(t, ctx, app, want)
	}
	require.Empty(t, app.rpcReplay.Unrecorded())
	app.Shutdown()
	compareRPCDrivers(t, ctx, *rpcParityArchive, m.First, m.Last, want)
	t.Logf("recorded blocks %d..%d: %d port reads matched", m.First, m.Last, len(want))
}

func rpcParityIndex(t *testing.T, ctx context.Context, endpoint, recordDir, driver string, from, head uint64) *App {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "db")
	cfg := config.NewConfig()
	cfg.RPC.Endpoint, cfg.RPC.RecordDir = endpoint, recordDir
	cfg.RPC.Timeout = 30 * time.Second
	if recordDir != "" {
		cfg.RPC.RateLimit = 5
	}
	cfg.Database.Path, cfg.Database.Driver = dir, driver
	if driver == config.DriverPostgres {
		dsn, schema := postgresDSN(t), testSchema(dir)
		cfg.Database.Postgres = config.PostgresConfig{DSN: dsn, Schema: schema, MaxConns: 8}
		t.Cleanup(func() { dropTestSchemas(dsn, schema) })
	}
	cfg.API.Enabled = false
	cfg.Indexer.StartHeight, cfg.Indexer.Workers = from, 1
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	t.Cleanup(app.Shutdown)
	require.NoError(t, app.fetcher.FetchRange(ctx, from, head))
	return app
}

func compareRPCDrivers(t *testing.T, ctx context.Context, archive string, from, head uint64, want map[string]string) {
	t.Helper()
	keys := make([]string, 0, len(want))
	for key := range want {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, driver := range []string{config.DriverPebble, config.DriverPostgres} {
		passed := t.Run(driver, func(t *testing.T) {
			app := rpcParityIndex(t, ctx, "replay://"+archive, "", driver, from, head)
			got := dumpPortsFrom(t, app.storage, from)
			require.Len(t, got, len(want), "the same read set")
			for _, key := range keys {
				require.Equal(t, want[key], got[key], "%s: same recorded input", key)
			}
			require.Empty(t, app.rpcReplay.Unrecorded(), "no live RPC fallback")
		})
		require.True(t, passed, "%s comparison failed", driver)
	}
}

// checkRPCBalanceSnapshots compares the last indexed entry of each
// account/block against node state. It queries through the recorder, so
// the oracle is frozen along with the blocks, receipts and startup state.
func checkRPCBalanceSnapshots(t *testing.T, ctx context.Context, app *App, reads map[string]string) {
	t.Helper()
	ec, err := ethclient.DialContext(ctx, app.config.RPC.Endpoint)
	require.NoError(t, err)
	defer ec.Close()
	keys := make([]string, 0)
	for key := range reads {
		if strings.HasPrefix(key, "balanceHistory/") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	accounts, snapshots := 0, 0
	for _, key := range keys {
		var history []port.BalanceSnapshot
		require.NoError(t, json.Unmarshal([]byte(reads[key]), &history))
		if len(history) == 0 {
			continue
		}
		accounts++
		addr := common.HexToAddress(strings.TrimPrefix(key, "balanceHistory/"))
		for i, entry := range history {
			if i+1 < len(history) && history[i+1].BlockNumber == entry.BlockNumber {
				continue
			}
			want, err := ec.BalanceAt(ctx, addr, new(big.Int).SetUint64(entry.BlockNumber))
			require.NoError(t, err, "state of %s at %d", addr, entry.BlockNumber)
			require.Zero(t, entry.Balance.Cmp(want), "balance of %s after %d: indexed %s, node %s", addr, entry.BlockNumber, entry.Balance, want)
			snapshots++
		}
	}
	require.Positive(t, accounts, "balance comparison must have an actual sample")
	require.Positive(t, snapshots)
	t.Logf("%d accounts, %d block balances matched node state", accounts, snapshots)
}

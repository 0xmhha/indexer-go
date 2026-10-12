package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/api/jsonrpc"
	sourcerpc "github.com/0xmhha/indexer-go/pkg/source/rpc"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

var liveRPCTargets = flag.String("live-rpc-targets", "", "JSON file of live RPC targets; unset makes no external calls")

type liveRPCTarget struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	ChainID  uint64 `json:"chain_id"`
	Profile  string `json:"profile"`
	Blocks   uint64 `json:"blocks"`
}

// liveRPCConfig uses production feature defaults and starts at the range
// being verified, so a full node need not retain genesis state.
func liveRPCConfig(t testing.TB, endpoint, dir string, from uint64) *config.Config {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = endpoint
	cfg.RPC.Timeout = 15 * time.Second
	setTestDatabase(t, cfg, dir)
	cfg.API.Enabled = false
	cfg.Indexer.StartHeight = from
	cfg.Indexer.Workers = 1
	return cfg
}

// TestLiveBalancesWithPrunedNode exercises the live test's entry point
// against a node retaining the recent range's state but no genesis state.
func TestLiveBalancesWithPrunedNode(t *testing.T) {
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	srv.PruneStateBefore(sc.Chain.Head() - 2)
	t.Setenv("INDEXER_LIVE_RPC", srv.URL())
	t.Setenv("INDEXER_LIVE_BALANCE_BLOCKS", "2")
	TestLiveBalances(t)
}

// TestLiveRPCFramework checks production startup, stored chain identities
// and RPC recording replay on caller-supplied chains. It is opt-in and
// sends no transactions. Chain ids and profiles are not hard-coded here.
func TestLiveRPCFramework(t *testing.T) {
	if *liveRPCTargets == "" {
		t.Skip("set -live-rpc-targets to a JSON file")
	}
	f, err := os.Open(*liveRPCTargets)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var targets []liveRPCTarget
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&targets))
	require.NotEmpty(t, targets)
	for _, target := range targets {
		require.NotEmpty(t, target.Name)
		require.NotEmpty(t, target.Endpoint)
		require.Positive(t, target.ChainID)
		t.Run(target.Name, func(t *testing.T) { verifyLiveRPCTarget(t, target) })
	}
}

func verifyLiveRPCTarget(t *testing.T, target liveRPCTarget) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
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
	require.Positive(t, head)
	blocks := target.Blocks
	if blocks == 0 {
		blocks = 1
	}
	require.LessOrEqual(t, blocks, uint64(100), "keep public RPC verification bounded")
	from := uint64(1)
	if head >= blocks {
		from = head - blocks + 1
	}
	ec := ethclient.NewClient(c)
	_, genesisErr := ec.BalanceAt(ctx, common.Address{}, big.NewInt(0))
	t.Logf("chain %d profile %s: blocks %d..%d; genesis state query error: %v", info.ChainID, src.Profile().ID(), from, head, genesisErr)
	var answered gethrpc.Error
	if errors.As(genesisErr, &answered) {
		cfg := liveRPCConfig(t, target.Endpoint, filepath.Join(t.TempDir(), "genesis"), 0)
		app, startErr := NewApp(cfg, zap.NewNop(), false, "")
		if app != nil {
			app.Shutdown()
		}
		require.ErrorContains(t, startErr, "keeps no state of block 0")
		t.Log("startup at genesis rejected")
	}
	_, err = ec.BalanceAt(ctx, common.Address{}, new(big.Int).SetUint64(from-1))
	require.NoError(t, err, "the state before the recent range must be retained")

	archive := filepath.Join(t.TempDir(), "rpc")
	dir := filepath.Join(t.TempDir(), "indexed")
	run := func(endpoint, recordDir, db string, verify bool) {
		cfg := liveRPCConfig(t, endpoint, db, from)
		cfg.RPC.RecordDir = recordDir
		app, err := NewApp(cfg, zap.NewNop(), false, "")
		require.NoError(t, err, "production startup from the recent range")
		defer app.Shutdown()
		require.Equal(t, src.Profile().ID(), app.profile.ID())
		require.NoError(t, app.fetcher.FetchRange(ctx, from, head))
		if verify {
			checkLiveRPCStorage(t, ctx, app, src, from, head)
		} else {
			require.Empty(t, app.rpcReplay.Unrecorded())
		}
	}
	run(target.Endpoint, archive, dir, true)
	replayed := filepath.Join(t.TempDir(), "replayed")
	run("replay://"+archive, "", replayed, false)
	diff := testchain.DiffKeyspace(dumpDir(t, dir), dumpDir(t, replayed), 0)
	require.Empty(t, diff, testchain.SummarizeDiff(diff))
	t.Log("RPC recording replay produced identical storage")
}

func checkLiveRPCStorage(t *testing.T, ctx context.Context, app *App, src *sourcerpc.Source, from, head uint64) {
	t.Helper()
	gql, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)
	rpcAPI := jsonrpc.NewHandler(app.storage, zap.NewNop())
	defer rpcAPI.Close()
	txs, logs := 0, 0
	for n := from; n <= head; n++ {
		b, rs, err := src.BlockWithReceipts(ctx, n)
		require.NoError(t, err)
		stored, err := app.storage.GetBlockByHash(ctx, b.Hash)
		require.NoError(t, err)
		require.Equal(t, n, stored.Number)
		require.Equal(t, b.ParentHash, stored.ParentHash)
		require.Len(t, stored.Transactions, len(b.Transactions))
		for i, tx := range b.Transactions {
			got, loc, err := app.storage.GetTransaction(ctx, tx.Hash)
			require.NoError(t, err)
			require.Equal(t, tx.Type, got.Type)
			require.Equal(t, tx.From, got.From)
			require.Equal(t, tx.To, got.To)
			require.Equal(t, tx.Value, got.Value)
			require.Equal(t, b.Hash, loc.BlockHash)
			require.Equal(t, uint64(i), loc.TxIndex)
			r, err := app.storage.GetReceipt(ctx, tx.Hash)
			require.NoError(t, err)
			require.Equal(t, rs[i].GasUsed, r.GasUsed)
			require.Equal(t, rs[i].Status, r.Status)
			require.Equal(t, tx.Hash, r.TxHash)
			require.Equal(t, b.Hash, r.BlockHash)
			txs++
		}
		wantLogs := 0
		for _, r := range rs {
			wantLogs += len(r.Logs)
		}
		indexedLogs, err := app.storage.GetLogsByBlock(ctx, n)
		require.NoError(t, err)
		require.Len(t, indexedLogs, wantLogs)
		at := 0
		for _, r := range rs {
			for _, log := range r.Logs {
				got := indexedLogs[at]
				require.Equal(t, log.Address, got.Address)
				require.Equal(t, log.Topics, got.Topics)
				require.Equal(t, log.Data, got.Data)
				require.Equal(t, log.TxHash, got.TxHash)
				require.Equal(t, log.Index, got.Index)
				at++
			}
		}
		logs += wantLogs
		query := gql.ExecuteQuery(fmt.Sprintf(`{ block(number: "%d") { hash } }`, n), nil)
		require.Empty(t, query.Errors)
		result := query.Data.(map[string]any)["block"].(map[string]any)
		require.Equal(t, b.Hash.Hex(), result["hash"])
		resultRPC, rpcErr := rpcAPI.HandleMethod(ctx, "getBlock", json.RawMessage(fmt.Sprintf(`{"number":%d}`, n)))
		require.Nil(t, rpcErr)
		require.Equal(t, b.Hash.Hex(), resultRPC.(map[string]any)["hash"])
	}
	t.Logf("verified %d blocks, %d transactions, %d logs", head-from+1, txs, logs)
}

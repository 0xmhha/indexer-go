package app

import (
	"context"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// indexedBalanceAt returns the indexed balance of addr after block n and
// whether the indexer has any record of addr up to n.
func indexedBalanceAt(t *testing.T, r port.HistoricalReader, addr common.Address, n uint64) (*big.Int, bool) {
	t.Helper()
	hist, _, err := r.GetBalanceHistory(context.Background(), addr, 0, n, port.FirstPage(1<<20))
	require.NoError(t, err)
	if len(hist) == 0 {
		return nil, false
	}
	return hist[len(hist)-1].Balance, true
}

// requireChainBalances compares the indexed native balance history with the
// chain's state after every block: every account the indexer tracks must
// match, and an account it does not track must not have changed since
// genesis.
func requireChainBalances(t *testing.T, app *App, chain *testchain.Chain) {
	t.Helper()
	r := app.storage.(port.HistoricalReader)
	head := chain.Head()
	var mismatches []string
	for _, addr := range chain.Accounts() {
		genesis := chain.BalanceAt(addr, 0)
		for n := uint64(0); n <= head; n++ {
			want := chain.BalanceAt(addr, n)
			got, tracked := indexedBalanceAt(t, r, addr, n)
			if tracked && got.Cmp(want) != 0 {
				mismatches = append(mismatches, fmt.Sprintf("%s after block %d: indexed %s, chain %s", addr.Hex(), n, got, want))
				break // the first divergence per account is enough
			}
			if !tracked && want.Cmp(genesis) != 0 {
				mismatches = append(mismatches, fmt.Sprintf("%s after block %d: not tracked, chain changed to %s", addr.Hex(), n, want))
				break
			}
		}
	}
	require.Empty(t, mismatches, "indexed native balances differ from the chain")
}

func indexAll(t *testing.T, chain *testchain.Chain) *App {
	t.Helper()
	srv := testchain.NewServer(chain)
	t.Cleanup(srv.Close)
	app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, chain.Head()))
	return app
}

// TestNativeBalancesMatchChainEVM checks the generic EVM rules: failed
// transactions move no value, the tip goes to the coinbase, the base fee is
// burned.
func TestNativeBalancesMatchChainEVM(t *testing.T) {
	sc := testchain.BuildDefault()
	requireChainBalances(t, indexAll(t, sc.Chain), sc.Chain)
}

// TestNativeBalancesMatchChainStableNet checks go-stablenet's rules: native
// moves (including internal ones, mints and burns) from NativeCoinAdapter
// Transfer logs, the governance tip, and base fee distribution to validators.
func TestNativeBalancesMatchChainStableNet(t *testing.T) {
	sc := testchain.BuildStableNet()
	requireChainBalances(t, indexAll(t, sc.Chain), sc.Chain)
}

// TestStableNetGovernanceEvents indexes governance events with go-stablenet's
// event layouts: the proposal and the deposit mint proposal must be stored.
func TestStableNetGovernanceEvents(t *testing.T) {
	sc := testchain.BuildStableNet()
	app := indexAll(t, sc.Chain)
	r := systemcontracts.NewStore(app.storage.(port.KV), nil)
	ctx := context.Background()

	p, err := r.GetProposalById(ctx, testchain.GovMinter, big.NewInt(1))
	require.NoError(t, err)
	require.Equal(t, sc.Accounts[0].Address, p.Proposer)
	require.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, p.CallData)

	deposits, err := r.GetDepositMintProposals(ctx, 0, sc.Chain.Head(), systemcontracts.ProposalStatusAll)
	require.NoError(t, err)
	require.Len(t, deposits, 1)
	require.Equal(t, sc.Accounts[3].Address, deposits[0].Beneficiary)
	require.Equal(t, "bank-ref1", deposits[0].BankReference)
}

// TestNativeTransfersAreNotTokenTransfers: on StableNet every native value
// move emits a NativeCoinAdapter Transfer event. They are native transfers
// (in balance.native), not ERC-20 transfers of a token.
func TestNativeTransfersAreNotTokenTransfers(t *testing.T) {
	sc := testchain.BuildStableNet()
	app := indexAll(t, sc.Chain)
	r := app.storage.(port.AddressIndexReader)
	got, _, err := r.GetERC20TransfersByToken(context.Background(), testchain.NativeCoinAdapterAddress, port.Page{Limit: 100, Offset: 0})
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestAddressStatsCountPaidGasAndMovedValue: value counts only for
// successful transactions and gas only for the account that paid it.
func TestAddressStatsCountPaidGasAndMovedValue(t *testing.T) {
	sc := testchain.BuildDefault()
	app := indexAll(t, sc.Chain)
	r := app.storage.(port.HistoricalReader)

	for _, acct := range sc.Accounts[:3] {
		sent, received, gas := new(big.Int), new(big.Int), new(big.Int)
		for n := uint64(0); n <= sc.Chain.Head(); n++ {
			b := sc.Chain.Block(n)
			for i, tx := range b.Block.Transactions() {
				rc := b.Receipts[i]
				from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
				require.NoError(t, err)
				ok := rc.Status == types.ReceiptStatusSuccessful
				if from == acct.Address {
					gas.Add(gas, new(big.Int).Mul(new(big.Int).SetUint64(rc.GasUsed), rc.EffectiveGasPrice))
					if ok {
						sent.Add(sent, tx.Value())
					}
				}
				if tx.To() != nil && *tx.To() == acct.Address && ok {
					received.Add(received, tx.Value())
				}
			}
		}
		stats, err := r.GetAddressStats(context.Background(), acct.Address)
		require.NoError(t, err)
		require.Zero(t, sent.Cmp(stats.TotalValueSent), "%s sent", acct.Address.Hex())
		require.Zero(t, received.Cmp(stats.TotalValueReceived), "%s received", acct.Address.Hex())
		require.Zero(t, gas.Cmp(stats.TotalGasCost), "%s gas", acct.Address.Hex())
	}
}

// TestTimeQueriesFindIndexedBlocks: ingest indexes every block's time, so
// the time queries find the indexed blocks.
func TestTimeQueriesFindIndexedBlocks(t *testing.T) {
	sc := testchain.BuildDefault()
	app := indexAll(t, sc.Chain)
	r := app.storage.(port.HistoricalReader)
	ctx := context.Background()
	head := sc.Chain.Head()
	first, last := sc.Chain.Block(0).Block.Time(), sc.Chain.Block(head).Block.Time()

	blocks, _, err := r.GetBlocksByTimeRange(ctx, first, last, port.FirstPage(int(head)+1))
	require.NoError(t, err)
	require.Len(t, blocks, int(head)+1)
	for i, b := range blocks {
		require.Equal(t, uint64(i), b.Number)
	}

	b, err := r.GetBlockByTimestamp(ctx, last)
	require.NoError(t, err)
	require.Equal(t, head, b.Number)

	m, err := r.GetNetworkMetrics(ctx, first, last)
	require.NoError(t, err)
	require.Equal(t, head+1, m.TotalBlocks)
}

// TestNativeBalancesSurviveNodeErrors: balance.native follows the
// determinism rules (docs/SDK.md). A balance read the node does not answer
// fails the block, which is retried, so the balances still match the chain
// (it used to start the account at 0). A node that answers with an error
// (as one without an old block's state does) cannot give the balance: the
// indexer does not start (TestNodeStateCheckedAtStartup) unless
// allow_pruned_state is set; the account then starts at 0 and indexing goes
// on rather than stopping for good.
func TestNativeBalancesSurviveNodeErrors(t *testing.T) {
	t.Run("node down for a while", func(t *testing.T) {
		sc := testchain.BuildDefault()
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
		defer app.Shutdown()
		// Block 0 first, so the failures meet the first accounts of block 1.
		head := sc.Chain.Head()
		sc.Chain.SetHead(0)
		runLiveUntil(t, app, 0)
		sc.Chain.SetHead(head)
		before := srv.Calls()["eth_getBalance"]
		srv.FailMethod("eth_getBalance", 2)
		runLiveUntil(t, app, head)
		require.Greater(t, srv.Calls()["eth_getBalance"], before+2, "accounts were read from the node after the failures")
		requireChainBalances(t, app, sc.Chain)
	})
	t.Run("node without the state, allowed", func(t *testing.T) {
		sc := testchain.BuildDefault()
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		srv.DisableMethod("eth_getBalance")
		app, err := startStateApp(t, srv, filepath.Join(t.TempDir(), "db"), true, nil)
		require.NoError(t, err)
		defer app.Shutdown()
		runLiveUntil(t, app, sc.Chain.Head()) // reaches the head
	})
}

// startStateApp starts the app with balance.native and token.metadata, the
// features that read the node's state, allow_pruned_state set as given;
// change changes the configuration further (may be nil).
func startStateApp(t *testing.T, srv *testchain.Server, dir string, allowPruned bool, change func(*config.Config)) (*App, error) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, dir)
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
	for _, name := range []string{"balance.native", "token.metadata"} {
		var fc config.FeatureConfig
		require.NoError(t, yaml.Unmarshal([]byte(fmt.Sprintf("enabled: true\nallow_pruned_state: %v\n", allowPruned)), &fc))
		cfg.Features[name] = fc
	}
	if change != nil {
		change(cfg)
	}
	return NewApp(cfg, zap.NewNop(), false, "")
}

// TestNodeStateCheckedAtStartup: balance.native starts an account from the
// node's balance before the block and token.metadata reads a contract as of
// its creation block, so the indexer starts only if the node keeps the state
// before the first block such a feature will process: the
// next block to index, or the start of its backfill when it is enabled on
// an indexed database. A node that is not an archive node keeps only recent
// states.
func TestNodeStateCheckedAtStartup(t *testing.T) {
	sc := testchain.BuildDefault()
	head := sc.Chain.Head()
	require.Greater(t, head, uint64(3))
	pruned := head - 2 // the node keeps the states of the last blocks only

	t.Run("from genesis", func(t *testing.T) {
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		srv.PruneStateBefore(pruned)
		_, err := startStateApp(t, srv, filepath.Join(t.TempDir(), "db"), false, nil)
		require.ErrorContains(t, err, "keeps no state of block 0")
		require.ErrorContains(t, err, "allow_pruned_state")
	})
	t.Run("from genesis, allowed", func(t *testing.T) {
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		srv.PruneStateBefore(pruned)
		app, err := startStateApp(t, srv, filepath.Join(t.TempDir(), "db"), true, nil)
		require.NoError(t, err)
		defer app.Shutdown()
		runLiveUntil(t, app, head)
	})
	t.Run("token.metadata alone", func(t *testing.T) {
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		srv.PruneStateBefore(pruned)
		balanceOff := func(c *config.Config) {
			off := false
			c.Features["balance.native"] = config.FeatureConfig{Enabled: &off}
		}
		_, err := startStateApp(t, srv, filepath.Join(t.TempDir(), "db"), false, balanceOff)
		require.ErrorContains(t, err, "[token.metadata]")
		app, err := startStateApp(t, srv, filepath.Join(t.TempDir(), "db"), true, balanceOff)
		require.NoError(t, err)
		app.Shutdown()
	})
	t.Run("from a recent start height", func(t *testing.T) {
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		srv.PruneStateBefore(pruned)
		app, err := startStateApp(t, srv, filepath.Join(t.TempDir(), "db"), false, func(c *config.Config) {
			c.Indexer.StartHeight = pruned + 1 // the state of block pruned is kept
		})
		require.NoError(t, err)
		app.Shutdown()
		_, err = startStateApp(t, srv, filepath.Join(t.TempDir(), "db"), false, func(c *config.Config) {
			c.Indexer.StartHeight = pruned
		})
		require.ErrorContains(t, err, fmt.Sprintf("keeps no state of block %d", pruned-1))
	})
	t.Run("restart near the head", func(t *testing.T) {
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		dir := filepath.Join(t.TempDir(), "db")
		app, err := startStateApp(t, srv, dir, false, nil)
		require.NoError(t, err)
		runLiveUntil(t, app, head)
		app.Shutdown()
		srv.PruneStateBefore(pruned)
		app, err = startStateApp(t, srv, dir, false, nil) // next block head+1 needs the head's state
		require.NoError(t, err)
		app.Shutdown()
	})
	t.Run("enabled on an indexed database", func(t *testing.T) {
		srv := testchain.NewServer(sc.Chain)
		defer srv.Close()
		dir := filepath.Join(t.TempDir(), "db")
		app, err := startStateApp(t, srv, dir, false, func(c *config.Config) {
			off := false
			c.Features["balance.native"] = config.FeatureConfig{Enabled: &off}
		})
		require.NoError(t, err)
		runLiveUntil(t, app, head)
		app.Shutdown()
		srv.PruneStateBefore(pruned)
		_, err = startStateApp(t, srv, dir, false, nil) // backfills from block 0
		require.ErrorContains(t, err, "keeps no state of block 0")
	})
}

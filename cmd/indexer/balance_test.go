package main

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

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/core/port"
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

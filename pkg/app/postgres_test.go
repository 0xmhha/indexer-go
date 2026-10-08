package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// The PostgreSQL store against the Pebble store (refactoring plan R4-2,
// stage 6). These tests need INDEXER_TEST_POSTGRES; the rest of the
// end-to-end suite runs on PostgreSQL with INDEXER_TEST_DRIVER=postgres
// (testdb_test.go).

// postgresDSN returns INDEXER_TEST_POSTGRES or skips the test.
func postgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("INDEXER_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("set INDEXER_TEST_POSTGRES to compare the PostgreSQL store with Pebble")
	}
	return dsn
}

// indexWithDriver indexes sc into a fresh database of the driver and returns
// the port dump of the result.
func indexWithDriver(t *testing.T, sc *testchain.Scenario, driver string) map[string]string {
	t.Helper()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	cfg.Database.Path = dir
	if driver == config.DriverPostgres {
		dsn := postgresDSN(t)
		schema := testSchema(dir)
		cfg.Database.Driver = config.DriverPostgres
		cfg.Database.Postgres = config.PostgresConfig{DSN: dsn, Schema: schema, MaxConns: 8}
		t.Cleanup(func() { dropTestSchemas(dsn, schema) })
	}
	cfg.API.Enabled = false
	cfg.Indexer.StartHeight = 0
	enableTestChainFeatures(cfg)

	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	defer app.Shutdown()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	return dumpPorts(t, app.storage)
}

// TestPostgresMatchesPebble: indexing a scenario into PostgreSQL gives the
// same answer to every read port as indexing it into Pebble.
func TestPostgresMatchesPebble(t *testing.T) {
	postgresDSN(t)
	for name, build := range map[string]func() *testchain.Scenario{
		"evm":       testchain.BuildDefault,
		"stablenet": func() *testchain.Scenario { return &testchain.BuildStableNet().Scenario },
	} {
		t.Run(name, func(t *testing.T) {
			pebble := indexWithDriver(t, build(), config.DriverPebble)
			pg := indexWithDriver(t, build(), config.DriverPostgres)
			require.NotEmpty(t, pebble)
			keys := make([]string, 0, len(pebble))
			for k := range pebble {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				assert.Equal(t, pebble[k], pg[k], "%s", k)
			}
			assert.Len(t, pg, len(pebble), "the same reads")
			t.Logf("%d reads compared", len(keys))
		})
	}
}

// dumpPorts reads everything the read ports answer about the indexed data:
// every block, transaction, receipt and log, and for every address and
// token they mention the address, balance, token, account abstraction and
// statistics reads, plus the lists and the chain packages' key-value data.
// Each read is keyed by what it asked, its result encoded as JSON.
func dumpPorts(t *testing.T, s storage.Storage) map[string]string {
	t.Helper()
	ctx := context.Background()
	out := map[string]string{}
	put := func(key string, v any, err error) {
		t.Helper()
		switch {
		case errors.Is(err, port.ErrNotFound):
			v = "not found" // the stores word the error differently
		case err != nil:
			v = "error: " + err.Error()
		}
		b, jerr := json.Marshal(modelJSON(t, v))
		require.NoError(t, jerr, key)
		if string(b) == "null" && v != nil && reflect.TypeOf(v).Kind() == reflect.Slice {
			b = []byte("[]") // a nil and an empty list are the same answer
		}
		out[key] = string(b)
	}
	all := port.Page{Limit: 1000}

	head, err := s.GetLatestHeight(ctx)
	require.NoError(t, err)
	put("latestHeight", head, nil)
	n, err := s.GetBlockCount(ctx)
	put("blockCount", n, err)
	n, err = s.GetTransactionCount(ctx)
	put("transactionCount", n, err)

	addrs := map[common.Address]bool{}
	tokens := map[common.Address]bool{}
	var txs []common.Hash
	for h := uint64(0); h <= head; h++ {
		b, err := s.GetBlock(ctx, h)
		put(fmt.Sprintf("block/%d", h), b, err)
		if err != nil {
			continue
		}
		byHash, err := s.GetBlockByHash(ctx, b.Hash)
		put(fmt.Sprintf("blockByHash/%d", h), byHash, err)
		receipts, err := s.GetReceiptsByBlockNumber(ctx, h)
		put(fmt.Sprintf("receipts/%d", h), receipts, err)
		logs, err := s.GetLogsByBlock(ctx, h)
		put(fmt.Sprintf("logs/%d", h), logs, err)
		missing, err := s.GetMissingReceipts(ctx, h)
		put(fmt.Sprintf("missingReceipts/%d", h), missing, err)
		addrs[b.Miner] = true
		for _, tx := range b.Transactions {
			txs = append(txs, tx.Hash)
			addrs[tx.From] = true
			if tx.To != nil {
				addrs[*tx.To] = true
			}
		}
		for _, r := range receipts {
			if r.ContractAddress != nil {
				addrs[*r.ContractAddress] = true
			}
			for _, l := range r.Logs {
				addrs[l.Address] = true
				tokens[l.Address] = true
				for _, topic := range l.Topics[min(1, len(l.Topics)):] {
					addrs[common.BytesToAddress(topic.Bytes())] = true
				}
			}
		}
	}
	for _, h := range txs {
		tx, loc, err := s.GetTransaction(ctx, h)
		put("tx/"+h.Hex(), []any{tx, loc}, err)
		r, err := s.GetReceipt(ctx, h)
		put("receipt/"+h.Hex(), r, err)
		if ai, ok := s.(port.AddressIndexReader); ok {
			v, err := ai.GetInternalTransactions(ctx, h)
			put("internal/"+h.Hex(), v, err)
		}
		v, err := s.GetSetCodeAuthorizationsByTx(ctx, h)
		put("setcodeByTx/"+h.Hex(), v, err)
		ops, err := s.GetUserOpsByTx(ctx, h)
		put("useropsByTx/"+h.Hex(), ops, err)
	}
	logs, err := s.GetLogs(ctx, &port.LogFilter{FromBlock: 0, ToBlock: head})
	put("logs", logs, err)

	sorted := make([]common.Address, 0, len(addrs))
	for a := range addrs {
		sorted = append(sorted, a)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Hex() < sorted[j].Hex() })
	for _, a := range sorted {
		k := a.Hex()
		hashes, _, err := s.GetTransactionsByAddress(ctx, a, all)
		put("addressTxs/"+k, hashes, err)
		filtered, _, err := s.GetTransactionsByAddressFiltered(ctx, a, nil, all)
		put("addressTxsFiltered/"+k, filtered, err)
		bal, err := s.GetAddressBalance(ctx, a, 0)
		put("balance/"+k, bal, err)
		hist, _, err := s.GetBalanceHistory(ctx, a, 0, math.MaxUint64, all)
		put("balanceHistory/"+k, hist, err)
		stats, err := s.GetAddressStats(ctx, a)
		put("addressStats/"+k, stats, err)
		gas, err := s.GetGasStatsByAddress(ctx, a, 0, head)
		put("gasByAddress/"+k, gas, err)
		tb, err := s.GetTokenBalances(ctx, a, "")
		sort.Slice(tb, func(i, j int) bool { return tb[i].ContractAddress.Hex() < tb[j].ContractAddress.Hex() })
		put("tokenBalances/"+k, tb, err)
		abi, err := s.GetABI(ctx, a)
		put("abi/"+k, abi, err)
		verified, err := s.IsContractVerified(ctx, a)
		put("verified/"+k, verified, err)
		sc, err := s.GetAddressSetCodeStats(ctx, a)
		put("setcodeStats/"+k, sc, err)
		del, err := s.GetAddressDelegationState(ctx, a)
		put("delegation/"+k, del, err)
		byTarget, _, err := s.GetSetCodeAuthorizationsByTarget(ctx, a, all)
		put("setcodeByTarget/"+k, byTarget, err)
		byAuthority, _, err := s.GetSetCodeAuthorizationsByAuthority(ctx, a, all)
		put("setcodeByAuthority/"+k, byAuthority, err)
		bySender, _, err := s.GetUserOpsBySender(ctx, a, all)
		put("useropsBySender/"+k, bySender, err)
		byBundler, _, err := s.GetUserOpsByBundler(ctx, a, all)
		put("useropsByBundler/"+k, byBundler, err)
		account, err := s.GetSmartAccount(ctx, a)
		put("smartAccount/"+k, account, err)
		md, err := s.GetTokenMetadata(ctx, a)
		put("tokenMetadata/"+k, md, err)
		search, err := s.Search(ctx, k, nil, 10)
		put("search/"+k, search, err)
		if ai, ok := s.(port.AddressIndexReader); ok {
			c, err := ai.GetContractCreation(ctx, a)
			put("creation/"+k, c, err)
			byCreator, _, err := ai.GetContractsByCreator(ctx, a, all)
			put("contractsByCreator/"+k, byCreator, err)
			for _, from := range []bool{true, false} {
				it, _, err := ai.GetInternalTransactionsByAddress(ctx, a, from, all)
				put(fmt.Sprintf("internalByAddress/%s/%v", k, from), it, err)
				e20, _, err := ai.GetERC20TransfersByAddress(ctx, a, from, all)
				put(fmt.Sprintf("erc20ByAddress/%s/%v", k, from), e20, err)
				e721, _, err := ai.GetERC721TransfersByAddress(ctx, a, from, all)
				put(fmt.Sprintf("erc721ByAddress/%s/%v", k, from), e721, err)
			}
			nfts, _, err := ai.GetNFTsByOwner(ctx, a, all)
			put("nfts/"+k, nfts, err)
		}
		if th, ok := s.(port.TokenHolderIndexReader); ok {
			held, _, err := th.GetHolderTokens(ctx, a, all)
			put("holderTokens/"+k, held, err)
		}
		if mi, ok := s.(port.ModuleIndexReader); ok {
			mods, err := mi.GetAccountModules(ctx, a)
			put("accountModules/"+k, mods, err)
			ms, err := mi.GetModuleStats(ctx, a)
			put("moduleStats/"+k, ms, err)
		}
	}
	for tok := range tokens {
		k := tok.Hex()
		if ai, ok := s.(port.AddressIndexReader); ok {
			e20, _, err := ai.GetERC20TransfersByToken(ctx, tok, all)
			put("erc20ByToken/"+k, e20, err)
			e721, _, err := ai.GetERC721TransfersByToken(ctx, tok, all)
			put("erc721ByToken/"+k, e721, err)
		}
		if th, ok := s.(port.TokenHolderIndexReader); ok {
			holders, _, err := th.GetTokenHolders(ctx, tok, all)
			put("holders/"+k, holders, err)
			count, err := th.GetTokenHolderCount(ctx, tok)
			put("holderCount/"+k, count, err)
			st, err := th.GetTokenHolderStats(ctx, tok)
			put("holderStats/"+k, st, err)
		}
	}

	// Lists and statistics over the whole index.
	if ai, ok := s.(port.AddressIndexReader); ok {
		contracts, _, err := ai.ListContracts(ctx, all)
		put("contracts", contracts, err)
	}
	listed, _, err := s.ListTokensByStandard(ctx, "", all)
	put("tokens", listed, err)
	verifiedList, _, err := s.ListVerifiedContracts(ctx, all)
	put("verifiedContracts", verifiedList, err)
	abis, err := s.ListABIs(ctx)
	put("abis", abis, err)
	bundlers, _, err := s.ListBundlers(ctx, all)
	put("bundlers", bundlers, err)
	accounts, _, err := s.ListSmartAccounts(ctx, all)
	put("smartAccounts", accounts, err)
	recentOps, err := s.GetRecentUserOps(ctx, 100)
	put("recentUserOps", recentOps, err)
	recentAuth, err := s.GetRecentSetCodeAuthorizations(ctx, 100)
	put("recentSetCode", recentAuth, err)
	if mi, ok := s.(port.ModuleIndexReader); ok {
		recent, err := mi.GetRecentModuleEvents(ctx, 100)
		put("recentModules", recent, err)
		stats, _, err := mi.ListModuleStats(ctx, all)
		put("moduleStatsList", stats, err)
	}
	gas, err := s.GetGasStatsByBlockRange(ctx, 0, head)
	put("gasStats", gas, err)
	miners, err := s.GetTopMiners(ctx, 100, 0, 0)
	put("topMiners", sortMiners(miners), err)
	topGas, err := s.GetTopAddressesByGasUsed(ctx, 100, 0, head)
	sort.SliceStable(topGas, func(i, j int) bool { return topGas[i].Address.Hex() < topGas[j].Address.Hex() })
	put("topGas", topGas, err)
	topTx, err := s.GetTopAddressesByTxCount(ctx, 100, 0, head)
	sort.SliceStable(topTx, func(i, j int) bool { return topTx[i].Address.Hex() < topTx[j].Address.Hex() })
	put("topTxCount", topTx, err)
	blocks, _, err := s.GetBlocksByTimeRange(ctx, 0, math.MaxUint64, all)
	put("blocksByTime", blockNumbers(blocks), err)
	metrics, err := s.GetNetworkMetrics(ctx, 0, math.MaxUint64)
	put("networkMetrics", metrics, err)

	// The chain packages' key-value data (consensus, system contracts, fee
	// delegation, notifications) under their registered prefixes.
	for _, ks := range storage.Keyspaces() {
		if ks.Class != storage.ChainData {
			continue
		}
		for _, p := range ks.Prefixes {
			var kv []string
			err := s.(port.KV).Scan(ctx, []byte(p), nil, false, func(key, value []byte) bool {
				if len(key) < len(p) || string(key[:len(p)]) != p {
					return false
				}
				kv = append(kv, fmt.Sprintf("%x=%x", key, value))
				return true
			})
			if len(kv) > 0 && !isStoreKeyspace(ks.Owner) {
				put("kv/"+p, kv, err)
			}
		}
	}
	return out
}

// isStoreKeyspace reports whether a keyspace is the Pebble store's own
// layout (which PostgreSQL keeps in tables), not data of a port.KV owner.
func isStoreKeyspace(owner string) bool {
	switch owner {
	case "core", "reorg", "features", "address", "balance", "contracts", "tokens", "aa", "multichain", "outbox":
		return true
	}
	return false
}

func sortMiners(m []port.MinerStats) []port.MinerStats {
	sort.SliceStable(m, func(i, j int) bool { return m[i].Address.Hex() < m[j].Address.Hex() })
	return m
}

func blockNumbers(blocks []*model.Block) []uint64 {
	out := make([]uint64, len(blocks))
	for i, b := range blocks {
		out[i] = b.Number
	}
	return out
}

// TestPostgresReindex: a reindex of a PostgreSQL database leaves the
// preserved data only, and indexing again gives the same rows as a fresh
// index.
func TestPostgresReindex(t *testing.T) {
	if !testOnPostgres() {
		t.Skip("runs with INDEXER_TEST_DRIVER=postgres")
	}
	sc := testchain.BuildDefault()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	fresh := filepath.Join(t.TempDir(), "fresh")
	runSession(t, srv, fresh, 0, sc.Chain.Head())

	dir := filepath.Join(t.TempDir(), "db")
	runSession(t, srv, dir, 0, sc.Chain.Head())
	cfg := config.NewConfig()
	setTestDatabase(t, cfg, dir)
	require.NoError(t, reindexDatabases(cfg, zap.NewNop()))
	for _, e := range dumpPostgres(t, dir) {
		assert.Contains(t, []string{"abis", "contract_verifications"}, tableOf(e.Key), "row left after reindex: %s", e.Key)
	}

	runSession(t, srv, dir, 0, sc.Chain.Head())
	diff := testchain.DiffKeyspace(dumpDir(t, fresh), dumpDir(t, dir), 20)
	assert.Empty(t, diff, "indexing again after a reindex gives a fresh index")
}

func tableOf(key []byte) string {
	for i, c := range key {
		if c == '/' {
			return string(key[:i])
		}
	}
	return string(key)
}

// modelJSON replaces the chain-neutral model values in v, which carry
// extensions JSON cannot encode, with their model codec encoding (what the
// stores keep), so dumps compare them byte for byte.
func modelJSON(t *testing.T, v any) any {
	t.Helper()
	enc := func(b []byte, err error) string {
		t.Helper()
		require.NoError(t, err)
		return fmt.Sprintf("%x", b)
	}
	switch x := v.(type) {
	case *model.Block:
		if x == nil {
			return nil
		}
		return enc(model.EncodeBlock(x))
	case *model.Transaction:
		if x == nil {
			return nil
		}
		return enc(model.EncodeTransaction(x))
	case *model.Receipt:
		if x == nil {
			return nil
		}
		return enc(model.EncodeReceipt(x))
	case []*model.Receipt:
		out := make([]any, len(x))
		for i, r := range x {
			out[i] = modelJSON(t, r)
		}
		return out
	case []*model.Log:
		// A log is encoded within a receipt.
		return enc(model.EncodeReceipt(&model.Receipt{Logs: x}))
	case []*port.TransactionWithReceipt:
		out := make([]any, len(x))
		for i, tr := range x {
			out[i] = []any{modelJSON(t, tr.Transaction), modelJSON(t, tr.Receipt), tr.Location}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = modelJSON(t, e)
		}
		return out
	}
	return v
}

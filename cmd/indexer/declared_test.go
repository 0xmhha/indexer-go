package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/chains/stablenet/features/systemcontracts"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/features/records"
)

// declaredMode configures the declared ingest mode with only the records
// feature.
func declaredMode(c *config.Config) {
	c.Indexer.Mode = config.ModeDeclared
	delete(c.Features, systemcontracts.Name)
}

// declaredKeys are the key prefixes a declared database may hold: block
// headers and their indexes, metadata, and the records. (Undo records and
// outbox entries are left out of dumps.)
var declaredKeys = []string{"/data/blocks/", "/index/blockh/", "/index/time/", "/meta/", "/rec/", "/reckey/"}

// requireOnlyDeclaredData requires every stored key to be one a declared
// database may hold: no transaction, receipt or log of any contract.
func requireOnlyDeclaredData(t *testing.T, dir string) {
	t.Helper()
	if testOnPostgres() {
		return // the PostgreSQL dump is checked by the same records and port reads
	}
	for _, e := range dumpDir(t, dir) {
		ok := false
		for _, p := range declaredKeys {
			ok = ok || strings.HasPrefix(string(e.Key), p)
		}
		require.True(t, ok, "declared mode stored %q", e.Key)
	}
}

// runLiveUntil runs the live loop until the database holds block head.
func runLiveUntil(t *testing.T, app *App, head uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.fetcher.Run(ctx) }()
	require.Eventually(t, func() bool {
		h, err := app.storage.GetLatestHeight(ctx)
		return err == nil && h >= head
	}, 30*time.Second, 10*time.Millisecond)
	cancel()
	<-done
}

// TestDeclaredModeStoresOnlyDeclaredData (refactoring plan R6-1, P07-FR-01
// to 03): with indexer.mode declared the indexer reads headers and the
// declared logs only, stores nothing but headers and the declared table's
// records, starts at the declared start block, continues after a restart
// without gaps or duplicates, and serves only the declared tables.
func TestDeclaredModeStoresOnlyDeclaredData(t *testing.T) {
	sc := testchain.BuildReceipts()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	dir := filepath.Join(t.TempDir(), "db")
	startAt := sc.Payments[0].Block // the declared start block
	declared := func(c *config.Config) {
		declaredMode(c)
		spec := receiptsSpec(sc)
		spec.Sources[0].StartBlock = startAt
		require.NoError(t, c.SetFeatureSettings(records.Name, spec))
	}
	ctx := context.Background()

	// A first run stops part way.
	app := startRecordsApp(t, srv, dir, sc, declared)
	sc.Chain.SetHead(sc.Payments[1].Block)
	runLiveUntil(t, app, sc.Payments[1].Block)
	_, err := app.storage.GetBlock(ctx, startAt-1)
	assert.ErrorIs(t, err, port.ErrNotFound, "nothing before the start block")
	app.Shutdown()

	// After a restart it continues from the cursor to the head.
	sc.Chain.SetHead(uint64(sc.Chain.Len() - 1))
	app = startRecordsApp(t, srv, dir, sc, declared)
	head := sc.Chain.Head()
	runLiveUntil(t, app, head)
	s := app.storage.(port.RecordReader)
	requireReceipts(t, ctx, s, sc)

	// Only the declared GraphQL is served.
	h, err := graphql.NewHandlerWithOptions(app.storage, zap.NewNop(), &graphql.HandlerOptions{ExtensionsOnly: true})
	require.NoError(t, err)
	res := h.ExecuteQuery(`{ records(table: "receipts") { nodes { blockNumber } } }`, nil)
	require.Empty(t, res.Errors)
	assert.Len(t, res.Data.(map[string]interface{})["records"].(map[string]interface{})["nodes"], len(sc.Payments))
	res = h.ExecuteQuery(`{ block(number: "2") { hash } }`, nil)
	assert.NotEmpty(t, res.Errors, "no explorer queries")

	// A payment in a new block is found; nothing is stored twice.
	m := sc.Merchants[1]
	order := common.HexToHash("0x5151")
	sc.Chain.AddBlock(testchain.TxSpec{From: sc.Accounts[0], Tx: &types.LegacyTx{To: &sc.Settlement, Gas: 200000, GasPrice: common.Big1},
		GasUsed: 90000, Logs: []*types.Log{{Address: sc.Settlement, Topics: []common.Hash{testchain.SigPaymentSettled, common.BytesToHash(m.Bytes()), order},
			Data: append(common.LeftPadBytes(sc.Device.Bytes(), 32), common.LeftPadBytes([]byte{0x01, 0x00}, 32)...)}}})
	sc.Payments = append(sc.Payments, testchain.Payment{Block: sc.Chain.Head(), Merchant: m, OrderID: order, Device: sc.Device, Amount: 256})
	runLiveUntil(t, app, sc.Chain.Head())
	requireReceipts(t, ctx, s, sc)
	app.Shutdown()
	requireOnlyDeclaredData(t, dir)
}

// TestDeclaredModeRejects: the declared mode needs the records feature and
// refuses features that need whole blocks.
func TestDeclaredModeRejects(t *testing.T) {
	sc := testchain.BuildReceipts()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	on, off := true, false
	for name, configure := range map[string]func(*config.Config){
		"explorer feature": func(c *config.Config) {
			c.Indexer.Mode = config.ModeDeclared
		},
		"no records": func(c *config.Config) {
			declaredMode(c)
			c.Features[records.Name] = config.FeatureConfig{Enabled: &off}
		},
		"unknown mode": func(c *config.Config) { c.Indexer.Mode = "explorer" },
	} {
		cfg := config.NewConfig()
		cfg.RPC.Endpoint = srv.URL()
		setTestDatabase(t, cfg, filepath.Join(t.TempDir(), "db"))
		cfg.API.Enabled = false
		enableTestChainFeatures(cfg)
		cfg.Features[records.Name] = config.FeatureConfig{Enabled: &on}
		require.NoError(t, cfg.SetFeatureSettings(records.Name, receiptsSpec(sc)))
		configure(cfg)
		err := cfg.Validate()
		if err == nil {
			_, err = NewApp(cfg, zap.NewNop(), false, "")
		}
		assert.Error(t, err, name)
	}
}

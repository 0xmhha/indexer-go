package main

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/declared"
	"github.com/0xmhha/indexer-go/pkg/features/records"
)

// receiptsSpec declares the receipts table of the receipts scenario.
func receiptsSpec(sc *testchain.ReceiptsScenario) declared.Spec {
	return declared.Spec{
		Sources: []declared.Source{{Name: "settlement", Address: sc.Settlement.Hex(), Events: []string{testchain.PaymentSettledSignature}}},
		Tables:  []declared.Table{{Name: "receipts", Source: "settlement", Event: "PaymentSettled", Keys: [][]string{{"merchant", "orderId"}}}},
	}
}

// startRecordsApp starts the app on dir with the records feature over the
// receipts scenario; configure changes the configuration further.
func startRecordsApp(t *testing.T, srv *testchain.Server, dir string, sc *testchain.ReceiptsScenario, configure ...func(*config.Config)) *App {
	t.Helper()
	cfg := config.NewConfig()
	cfg.RPC.Endpoint = srv.URL()
	cfg.RPC.Timeout = 5 * time.Second
	setTestDatabase(t, cfg, dir)
	cfg.API.Enabled = false
	enableTestChainFeatures(cfg)
	on := true
	if cfg.Features == nil {
		cfg.Features = map[string]config.FeatureConfig{}
	}
	cfg.Features[records.Name] = config.FeatureConfig{Enabled: &on}
	require.NoError(t, cfg.SetFeatureSettings(records.Name, receiptsSpec(sc)))
	for _, c := range configure {
		c(cfg)
	}
	app, err := NewApp(cfg, zap.NewNop(), false, "")
	require.NoError(t, err)
	return app
}

// requireReceipts checks the receipts table against the scenario's
// payments: every payment of the settlement contract, in chain order, and
// nothing else; a (merchant, orderId) lookup finds an order's logs, the
// earliest first.
func requireReceipts(t *testing.T, ctx context.Context, s port.RecordReader, sc *testchain.ReceiptsScenario) {
	t.Helper()
	got, _, err := s.ListRecords(ctx, "receipts", port.FirstPage(100))
	require.NoError(t, err)
	require.Len(t, got, len(sc.Payments), "only the settlement contract's PaymentSettled logs")
	for i, p := range sc.Payments {
		r := got[i]
		assert.Equal(t, p.Block, r.BlockNumber, "payment %d", i)
		assert.Equal(t, sc.Settlement, r.Address)
		assert.Equal(t, map[string]string{
			"merchant": strings.ToLower(p.Merchant.Hex()), "orderId": p.OrderID.Hex(),
			"device": strings.ToLower(p.Device.Hex()), "amount": strconv.FormatInt(p.Amount, 10),
		}, r.Fields, "payment %d", i)
		assert.NotZero(t, r.BlockTime)
	}
	order1 := port.RecordKey{ID: "merchant,orderId", Values: []string{strings.ToLower(sc.Merchants[0].Hex()), sc.Payments[0].OrderID.Hex()}}
	dup, _, err := s.ListRecordsByKey(ctx, "receipts", order1, port.FirstPage(10))
	require.NoError(t, err)
	require.Len(t, dup, 2, "order 1 of the first merchant settled twice")
	assert.Less(t, dup[0].BlockNumber, dup[1].BlockNumber, "the earliest first")
}

// TestRecordsFromDeclaredTables (refactoring plan R6-1): the records
// feature stores exactly the declared event of the declared contract,
// indexed by its key, serves it over GraphQL, and indexing the same range
// again changes nothing.
func TestRecordsFromDeclaredTables(t *testing.T) {
	sc := testchain.BuildReceipts()
	srv := testchain.NewServer(sc.Chain)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	app := startRecordsApp(t, srv, filepath.Join(t.TempDir(), "db"), sc)
	defer app.Shutdown()
	head := sc.Chain.Head()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	s := app.storage.(port.RecordReader)
	requireReceipts(t, ctx, s, sc)
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, head))
	requireReceipts(t, ctx, s, sc)

	// GraphQL: values are normalized (address case, a hex integer order id
	// padded to 32 bytes is the same order).
	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)
	q := `query($m: String!, $o: String!) { records(table: "receipts", where: [{field: "orderId", value: $o}, {field: "merchant", value: $m}]) {
		nodes { blockNumber logIndex address transactionHash fields { name value } amount: field(name: "amount") } } }`
	res := h.ExecuteQuery(q, map[string]interface{}{"m": "0x" + strings.ToUpper(sc.Merchants[0].Hex()[2:]), "o": sc.Payments[0].OrderID.Hex()})
	require.Empty(t, res.Errors)
	nodes := res.Data.(map[string]interface{})["records"].(map[string]interface{})["nodes"].([]interface{})
	require.Len(t, nodes, 2)
	first := nodes[0].(map[string]interface{})
	assert.Equal(t, "2500", first["amount"])
	assert.Equal(t, []interface{}{
		map[string]interface{}{"name": "merchant", "value": strings.ToLower(sc.Merchants[0].Hex())},
		map[string]interface{}{"name": "orderId", "value": sc.Payments[0].OrderID.Hex()},
		map[string]interface{}{"name": "device", "value": strings.ToLower(sc.Device.Hex())},
		map[string]interface{}{"name": "amount", "value": "2500"},
	}, first["fields"], "the event's arguments in order")

	for name, query := range map[string]string{
		"unknown table": `{ records(table: "payments") { nodes { blockNumber } } }`,
		"not a key":     `{ records(table: "receipts", where: [{field: "device", value: "0x00"}]) { nodes { blockNumber } } }`,
		"bad value":     `{ records(table: "receipts", where: [{field: "merchant", value: "x"}, {field: "orderId", value: "0x01"}]) { nodes { blockNumber } } }`,
	} {
		res := h.ExecuteQuery(query, nil)
		assert.NotEmpty(t, res.Errors, name)
	}
	res = h.ExecuteQuery(`{ records(table: "receipts", pagination: {limit: 3}) { nodes { blockNumber } pageInfo { hasNextPage } } }`, nil)
	require.Empty(t, res.Errors)
	page := res.Data.(map[string]interface{})["records"].(map[string]interface{})
	assert.Len(t, page["nodes"], 3)
	assert.Equal(t, true, page["pageInfo"].(map[string]interface{})["hasNextPage"])
}

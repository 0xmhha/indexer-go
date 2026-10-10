package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// sparseTail is how many empty blocks follow the default scenario, more
// than one scan limit (10,000 blocks), so matches in the scenario are rare
// as seen from the head.
const sparseTail = 10050

// newSparseFixture indexes the default scenario followed by sparseTail
// empty blocks.
func newSparseFixture(t *testing.T) *pagingFixture {
	t.Helper()
	// The storage side (the newest-first address index) is checked on both
	// drivers by the port contracts; this indexes 10,000 blocks.
	pebbleOnly(t)
	if testing.Short() {
		t.Skip("indexes more than 10,000 blocks")
	}
	sc := testchain.BuildDefault()
	for range sparseTail {
		sc.Chain.AddBlock()
	}
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	store := &countingStorage{Storage: app.storage}
	h, err := graphql.NewHandler(store, zap.NewNop())
	require.NoError(t, err)
	return &pagingFixture{h: h, store: store, sc: sc, head: sc.Chain.Head()}
}

// TestUnboundedQueriesUseTheIndexes: without a block range, an address
// filter on transactions and an address or topic filter on logs are served
// from the indexes, with the bounded path's results, instead of reading the
// chain block by block; a filter no index serves stops at the scan limit
// and says where.
func TestUnboundedQueriesUseTheIndexes(t *testing.T) {
	f := newSparseFixture(t)
	scenarioEnd := f.head - sparseTail
	reset := func() {
		f.store.blockReads.Store(0)
		f.store.receiptReads.Store(0)
	}

	t.Run("TransactionsByAddress", func(t *testing.T) {
		const fields = `nodes { hash blockNumber blockTimestamp } totalCount pageInfo { hasNextPage } scannedThrough`
		for _, filter := range []string{
			fmt.Sprintf(`from: "%s"`, f.sc.Accounts[1].Address.Hex()),
			fmt.Sprintf(`to: "%s"`, f.sc.ERC20.Hex()),
		} {
			reset()
			unbounded := f.query(t, fmt.Sprintf(`{ transactions(pagination: {limit: 3}, filter: {%s}) { %s } }`, filter, fields))
			reads := f.store.blockReads.Load()
			bounded := f.query(t, fmt.Sprintf(`{ transactions(pagination: {limit: 3}, filter: {%s, blockNumberFrom: "0", blockNumberTo: "%d"}) { %s } }`, filter, scenarioEnd, fields))
			un, _, uh := connection(unbounded, "transactions")
			bn, _, bh := connection(bounded, "transactions")
			require.NotEmpty(t, bn, filter)
			assert.Equal(t, bn, un, filter)
			assert.Equal(t, bh, uh, filter)
			assert.Nil(t, unbounded["transactions"].(map[string]any)["scannedThrough"])
			assert.LessOrEqual(t, reads, int64(len(un)+1), "%s: one block read per match (page plus one), not a scan", filter)
		}
	})

	t.Run("LogsByAddressAndTopic", func(t *testing.T) {
		transfer := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")).Hex()
		const fields = `nodes { transactionHash logIndex blockNumber } totalCount pageInfo { hasNextPage } scannedThrough`
		for _, filter := range []string{
			fmt.Sprintf(`address: "%s"`, f.sc.ERC20.Hex()),
			fmt.Sprintf(`topics: ["%s"]`, transfer),
		} {
			reset()
			unbounded := f.query(t, fmt.Sprintf(`{ logs(filter: {%s}, pagination: {limit: 50}) { %s } }`, filter, fields))
			reads := f.store.receiptReads.Load()
			bounded := f.query(t, fmt.Sprintf(`{ logs(filter: {%s, blockNumberFrom: "0", blockNumberTo: "%d"}, pagination: {limit: 50}) { %s } }`, filter, scenarioEnd, fields))
			un, _, _ := connection(unbounded, "logs")
			bn, _, _ := connection(bounded, "logs")
			require.NotEmpty(t, bn, filter)
			assert.Equal(t, bn, un, filter)
			assert.Nil(t, unbounded["logs"].(map[string]any)["scannedThrough"])
			assert.Zero(t, reads, "%s: no receipts read block by block", filter)
		}
	})

	t.Run("LongRangesAreCutAndReported", func(t *testing.T) {
		res := f.query(t, fmt.Sprintf(`{ transactions(pagination: {limit: 3}, filter: {blockNumberFrom: "0", blockNumberTo: "%d"}) { nodes { hash } scannedThrough } }`, f.head))
		conn := res["transactions"].(map[string]any)
		assert.Empty(t, conn["nodes"], "the newest 10,001 blocks are empty")
		assert.Equal(t, fmt.Sprint(f.head-10000), conn["scannedThrough"], "a newest-first list keeps the newest blocks")

		filter := fmt.Sprintf(`address: "%s"`, f.sc.ERC20.Hex())
		long := f.query(t, fmt.Sprintf(`{ logs(filter: {%s, blockNumberFrom: "0", blockNumberTo: "%d"}, pagination: {limit: 50}) { nodes { transactionHash logIndex } scannedThrough } }`, filter, f.head))
		short := f.query(t, fmt.Sprintf(`{ logs(filter: {%s, blockNumberFrom: "0", blockNumberTo: "%d"}, pagination: {limit: 50}) { nodes { transactionHash logIndex } scannedThrough } }`, filter, scenarioEnd))
		assert.Equal(t, "10000", long["logs"].(map[string]any)["scannedThrough"], "an oldest-first list keeps the oldest blocks")
		assert.Nil(t, short["logs"].(map[string]any)["scannedThrough"], "a range within the limit is read whole")
		assert.Equal(t, short["logs"].(map[string]any)["nodes"], long["logs"].(map[string]any)["nodes"])
	})

	t.Run("UnindexedFilterStopsAtTheScanLimit", func(t *testing.T) {
		reset()
		res := f.query(t, `{ transactions(pagination: {limit: 3}, filter: {type: 2}) { nodes { hash } scannedThrough } }`)
		conn := res["transactions"].(map[string]any)
		assert.Empty(t, conn["nodes"], "the scenario's dynamic fee transactions are below the scan limit")
		assert.Equal(t, fmt.Sprint(f.head-10000+1), conn["scannedThrough"])
		assert.Equal(t, int64(10000), f.store.blockReads.Load())

		reset()
		res = f.query(t, `{ logs(filter: {}, pagination: {limit: 1000}) { nodes { logIndex } scannedThrough } }`)
		conn = res["logs"].(map[string]any)
		assert.NotEmpty(t, conn["nodes"])
		assert.Equal(t, "9999", conn["scannedThrough"])
		assert.Equal(t, int64(10000), f.store.receiptReads.Load())
	})
}

// TestUnboundedTransactionsWithAnIncompleteIndexScan: while the address
// index has a gap (online backfill) an address filter reads blocks, with
// the same results.
func TestUnboundedTransactionsWithAnIncompleteIndexScan(t *testing.T) {
	f := newPagingFixture(t)
	gapped, err := graphql.NewHandler(gappedAddressIndex{f.store}, zap.NewNop())
	require.NoError(t, err)
	q := fmt.Sprintf(`{ transactions(pagination: {limit: 7}, filter: {from: "%s"}) { nodes { hash } pageInfo { hasNextPage } } }`, f.sc.Accounts[1].Address.Hex())

	f.store.indexReads.Store(0)
	indexed := f.query(t, q)
	require.Positive(t, f.store.indexReads.Load())

	f.store.indexReads.Store(0)
	res := gapped.ExecuteQuery(q, nil)
	require.Empty(t, res.Errors)
	assert.Zero(t, f.store.indexReads.Load(), "the incomplete index is not read")
	raw, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var scanned map[string]any
	require.NoError(t, json.Unmarshal(raw, &scanned))
	assert.Equal(t, indexed, scanned)
}

// gappedAddressIndex reports the address index as still backfilling.
type gappedAddressIndex struct{ *countingStorage }

func (g gappedAddressIndex) FeatureStates(ctx context.Context) (map[string]port.FeatureState, error) {
	states, err := g.countingStorage.FeatureStates(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]port.FeatureState{}
	for k, v := range states {
		out[k] = v
	}
	out["address.index"] = port.FeatureState{Active: true, Gap: &port.BlockRange{From: 0, To: 10}}
	return out, nil
}

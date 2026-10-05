package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/internal/testchain"
	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/storage"
)

// countingStorage counts block and receipt reads made by the API.
type countingStorage struct {
	storage.Storage
	blockReads atomic.Int64
}

func (c *countingStorage) GetBlock(ctx context.Context, h uint64) (*types.Block, error) {
	c.blockReads.Add(1)
	return c.Storage.GetBlock(ctx, h)
}

func (c *countingStorage) GetBlocks(ctx context.Context, from, to uint64) ([]*types.Block, error) {
	c.blockReads.Add(int64(to - from + 1))
	return c.Storage.GetBlocks(ctx, from, to)
}

type pagingFixture struct {
	h     *graphql.Handler
	store *countingStorage
	sc    *testchain.Scenario
	head  uint64
}

func newPagingFixture(t *testing.T) *pagingFixture {
	t.Helper()
	sc := testchain.BuildLoad(60, 20, 0) // 61 blocks, 1201 transactions
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))

	store := &countingStorage{Storage: app.storage}
	h, err := graphql.NewHandler(store, zap.NewNop())
	require.NoError(t, err)
	return &pagingFixture{h: h, store: store, sc: sc, head: sc.Chain.Head()}
}

func (f *pagingFixture) query(t *testing.T, q string) map[string]any {
	t.Helper()
	res := f.h.ExecuteQuery(q, nil)
	require.Empty(t, res.Errors, "%s: %v", q, res.Errors)
	raw, err := json.Marshal(res.Data)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func connection(m map[string]any, field string) (nodes []any, total float64, hasNext bool) {
	c := m[field].(map[string]any)
	return c["nodes"].([]any), c["totalCount"].(float64), c["pageInfo"].(map[string]any)["hasNextPage"].(bool)
}

// TestUnboundedTransactionsReadOnlyWhatThePageNeeds covers P1: without a
// block range the resolver used to load every block. Results must equal the
// bounded path over the whole chain while reading only a few blocks.
func TestUnboundedTransactionsReadOnlyWhatThePageNeeds(t *testing.T) {
	f := newPagingFixture(t)
	const fields = `nodes { hash blockNumber } totalCount pageInfo { hasNextPage }`

	for _, offset := range []int{0, 25, 500} {
		f.store.blockReads.Store(0)
		unbounded := f.query(t, fmt.Sprintf(`{ transactions(pagination: {limit: 10, offset: %d}) { %s } }`, offset, fields))
		reads := f.store.blockReads.Load()
		bounded := f.query(t, fmt.Sprintf(`{ transactions(pagination: {limit: 10, offset: %d}, filter: {blockNumberFrom: "0", blockNumberTo: "%d"}) { %s } }`, offset, f.head, fields))

		un, ut, uh := connection(unbounded, "transactions")
		bn, bt, bh := connection(bounded, "transactions")
		require.Equal(t, bn, un, "offset %d: same page as the bounded path", offset)
		require.Equal(t, bt, ut, "offset %d: totalCount without filters stays exact", offset)
		require.Equal(t, bh, uh)
		// 20 transactions per block: offset+limit+1 needs only a few blocks.
		require.LessOrEqual(t, reads, int64((offset+11)/20+2), "offset %d read %d blocks", offset, reads)
	}
}

func TestUnboundedFilteredTransactionsMatchBoundedPage(t *testing.T) {
	f := newPagingFixture(t)
	from := f.sc.Accounts[1].Address.Hex()
	const fields = `nodes { hash } totalCount pageInfo { hasNextPage }`

	unbounded := f.query(t, fmt.Sprintf(`{ transactions(pagination: {limit: 7}, filter: {from: "%s"}) { %s } }`, from, fields))
	bounded := f.query(t, fmt.Sprintf(`{ transactions(pagination: {limit: 7}, filter: {from: "%s", blockNumberFrom: "0", blockNumberTo: "%d"}) { %s } }`, from, f.head, fields))
	un, ut, uh := connection(unbounded, "transactions")
	bn, bt, bh := connection(bounded, "transactions")
	require.Equal(t, bn, un)
	require.Equal(t, bh, uh)
	// With an address filter the count is a lower bound unless the scan
	// reached genesis.
	require.LessOrEqual(t, ut, bt)
	require.Greater(t, ut, float64(len(un)), "a lower bound above the page size signals more results")
}

func TestUnboundedLogsMatchBoundedPage(t *testing.T) {
	f := newPagingFixture(t)
	token := f.sc.ERC20.Hex()
	const fields = `nodes { transactionHash logIndex blockNumber } totalCount pageInfo { hasNextPage }`

	for _, offset := range []int{0, 30} {
		unbounded := f.query(t, fmt.Sprintf(`{ logs(filter: {address: "%s"}, pagination: {limit: 10, offset: %d}) { %s } }`, token, offset, fields))
		bounded := f.query(t, fmt.Sprintf(`{ logs(filter: {address: "%s", blockNumberFrom: "0", blockNumberTo: "%d"}, pagination: {limit: 10, offset: %d}) { %s } }`, token, f.head, offset, fields))
		un, _, uh := connection(unbounded, "logs")
		bn, _, bh := connection(bounded, "logs")
		require.Equal(t, bn, un, "offset %d", offset)
		require.Equal(t, bh, uh)
	}
}

func TestUnboundedQueriesRejectDeepOffsets(t *testing.T) {
	f := newPagingFixture(t)
	for _, q := range []string{
		`{ transactions(pagination: {limit: 10, offset: 20000}) { totalCount } }`,
		`{ logs(filter: {}, pagination: {limit: 10, offset: 20000}) { totalCount } }`,
	} {
		res := f.h.ExecuteQuery(q, nil)
		require.NotEmpty(t, res.Errors, q)
		require.Contains(t, res.Errors[0].Message, "requires a block range")
	}
}

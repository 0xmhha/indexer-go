package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/api/graphql"
	"github.com/0xmhha/indexer-go/pkg/testchain"
)

// indexLoad indexes a load chain of blocks blocks with txs transactions
// each and returns a GraphQL handler over it and its head.
func indexLoad(t *testing.T, blocks, txs int) (*graphql.Handler, uint64) {
	t.Helper()
	sc := testchain.BuildLoad(blocks, txs, 0)
	srv := testchain.NewServer(sc.Chain)
	t.Cleanup(srv.Close)
	app := startApp(t, srv, filepath.Join(t.TempDir(), "db"))
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	start := time.Now()
	require.NoError(t, app.fetcher.FetchRange(ctx, 0, sc.Chain.Head()))
	t.Logf("indexed %d blocks x %d transactions in %v", blocks, txs, time.Since(start).Round(time.Millisecond))
	h, err := graphql.NewHandler(app.storage, zap.NewNop())
	require.NoError(t, err)
	return h, sc.Chain.Head()
}

// queryTime is the median time of a query over runs runs.
func queryTime(t *testing.T, h *graphql.Handler, query string, runs int) time.Duration {
	t.Helper()
	times := make([]time.Duration, runs)
	for i := range times {
		start := time.Now()
		res := h.ExecuteQuery(query, nil)
		times[i] = time.Since(start)
		require.Empty(t, res.Errors, query)
	}
	slices.Sort(times)
	return times[runs/2]
}

// nothingIn scans the blocks [from, to] for transactions to an address no
// transaction is sent to: the whole range is read.
func nothingIn(from, to uint64) string {
	return `{ transactions(filter: {blockNumberFrom: "` + strconv.FormatUint(from, 10) + `", blockNumberTo: "` + strconv.FormatUint(to, 10) +
		`", to: "0x000000000000000000000000000000000000dead"}, pagination: {limit: 20}) { nodes { hash } } }`
}

// TestRangeQueryTimeScalesWithRange (refactoring plan R0-8, P1) on a large
// database: a bounded query costs in proportion to its range, and a query
// without a range costs what its page needs, not the database's size.
// Run it with INDEXER_BENCH_LARGE=1 (it indexes about 26,000 blocks).
func TestRangeQueryTimeScalesWithRange(t *testing.T) {
	if os.Getenv("INDEXER_BENCH_LARGE") == "" {
		t.Skip("set INDEXER_BENCH_LARGE=1")
	}
	pebbleOnly(t)
	const txs = 4
	large, head := indexLoad(t, 24_000, txs)
	small, smallHead := indexLoad(t, 2_000, txs)

	ranges := []uint64{100, 1_000, 10_000}
	times := map[uint64]time.Duration{}
	for _, r := range ranges {
		times[r] = queryTime(t, large, nothingIn(head-r+1, head), 5)
		t.Logf("bounded scan of %6d blocks: %v (%v per block)", r, times[r], times[r]/time.Duration(r))
	}
	for i := 1; i < len(ranges); i++ {
		ratio := float64(times[ranges[i]]) / float64(times[ranges[i-1]])
		t.Logf("x%d range: x%.1f time", ranges[i]/ranges[i-1], ratio)
		require.Greater(t, ratio, 3.0, "a 10x range costs clearly more")
		require.Less(t, ratio, 30.0, "a 10x range costs about 10x, not more")
	}

	page := `{ transactions(pagination: {limit: 20}) { nodes { hash } } }`
	onLarge, onSmall := queryTime(t, large, page, 9), queryTime(t, small, page, 9)
	t.Logf("first page without a range: %v on %d blocks, %v on %d blocks", onLarge, head, onSmall, smallHead)
	require.Less(t, float64(onLarge), 3*float64(onSmall)+float64(time.Millisecond), "a page without a range does not grow with the database")
}

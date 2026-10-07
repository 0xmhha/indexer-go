package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// indexAddressTxs indexes n transactions for addr.
func indexAddressTxs(t testing.TB, s *PebbleStorage, addr common.Address, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		require.NoError(t, s.AddTransactionToAddressIndex(ctx, addr, common.BigToHash(common.Big1.SetInt64(int64(i+1)))))
	}
}

// TestCursorPageDoesNotWalkEarlierEntries: a page read with a cursor visits
// only its own entries (and one to know whether more follow), however deep
// it is; an offset page visits every entry before it.
func TestCursorPageDoesNotWalkEarlierEntries(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	addr := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	const total, limit = 3000, 10
	indexAddressTxs(t, s, addr, total)

	// The cursor of the page that ends at entry total-2*limit.
	_, cursor, err := s.GetTransactionsByAddress(ctx, addr, port.Page{Limit: total - 2*limit})
	require.NoError(t, err)
	require.NotEmpty(t, cursor)

	before := s.pageSteps.Load()
	page, _, err := s.GetTransactionsByAddress(ctx, addr, port.Page{After: cursor, Limit: limit})
	require.NoError(t, err)
	require.Len(t, page, limit)
	require.LessOrEqual(t, s.pageSteps.Load()-before, int64(limit+1), "a cursor page visits only its own entries")

	before = s.pageSteps.Load()
	_, _, err = s.GetTransactionsByAddress(ctx, addr, port.Page{Offset: total - limit, Limit: limit})
	require.NoError(t, err)
	require.GreaterOrEqual(t, s.pageSteps.Load()-before, int64(total-limit), "an offset page walks the entries before it")
}

// BenchmarkAddressTxPage compares reading a page deep in a long list by
// offset and by cursor.
func BenchmarkAddressTxPage(b *testing.B) {
	s, err := NewPebbleStorage(DefaultConfig(b.TempDir()))
	require.NoError(b, err)
	b.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	addr := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	const total, limit = 50_000, 20
	indexAddressTxs(b, s, addr, total)

	for _, depth := range []int{0, total / 2, total - limit} {
		var cursor string
		if depth > 0 {
			_, cursor, err = s.GetTransactionsByAddress(ctx, addr, port.Page{Limit: depth})
			require.NoError(b, err)
		}
		b.Run(fmt.Sprintf("Offset/%d", depth), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, _, err := s.GetTransactionsByAddress(ctx, addr, port.Page{Offset: depth, Limit: limit}); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("Cursor/%d", depth), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, _, err := s.GetTransactionsByAddress(ctx, addr, port.Page{After: cursor, Limit: limit}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

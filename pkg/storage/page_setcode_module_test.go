package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// TestSetCodeCursorPageDoesNotWalkEarlierEntries: a page of a target's
// SetCode authorizations read with a cursor visits only its own index
// entries (and one to know whether more follow), however deep it is; an
// offset page visits every entry before it.
func TestSetCodeCursorPageDoesNotWalkEarlierEntries(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	target := common.HexToAddress("0x00000000000000000000000000000000000000b1")
	authority := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	const total, limit = 1000, 10

	records := make([]*port.SetCodeAuthorizationRecord, total)
	for i := range records {
		records[i] = &port.SetCodeAuthorizationRecord{
			TxHash:           common.BigToHash(big.NewInt(int64(i + 1))),
			BlockNumber:      uint64(i + 1),
			TargetAddress:    target,
			AuthorityAddress: authority,
			ChainID:          big.NewInt(1),
			R:                big.NewInt(1),
			S:                big.NewInt(1),
			Applied:          true,
		}
	}
	require.NoError(t, s.SaveSetCodeAuthorizations(ctx, records))

	// Follow cursors (a page holds at most the maximum page size) to the
	// page that starts total-limit entries deep.
	var cursor string
	for read := 0; read < total-limit; {
		size := min(100, total-limit-read)
		page, next, err := s.GetSetCodeAuthorizationsByTarget(ctx, target, port.Page{After: cursor, Limit: size})
		require.NoError(t, err)
		require.Len(t, page, size)
		require.NotEmpty(t, next)
		read += size
		cursor = next
	}

	before := s.pageSteps.Load()
	page, next, err := s.GetSetCodeAuthorizationsByTarget(ctx, target, port.Page{After: cursor, Limit: limit})
	require.NoError(t, err)
	require.Len(t, page, limit)
	require.Empty(t, next)
	require.Equal(t, uint64(limit), page[0].BlockNumber, "newest first: the deepest page holds the oldest entries")
	require.LessOrEqual(t, s.pageSteps.Load()-before, int64(limit+1), "a cursor page visits only its own entries")

	before = s.pageSteps.Load()
	_, _, err = s.GetSetCodeAuthorizationsByTarget(ctx, target, port.Page{Offset: total - limit, Limit: limit})
	require.NoError(t, err)
	require.GreaterOrEqual(t, s.pageSteps.Load()-before, int64(total-limit), "an offset page walks the entries before it")
}

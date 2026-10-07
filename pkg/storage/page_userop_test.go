package storage

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/userop"
)

// TestUserOpCursorPageDoesNotWalkEarlierEntries: a cursor page of a
// newest-first UserOperation list visits only its own index entries, however
// deep it is; an offset page visits every entry before it.
func TestUserOpCursorPageDoesNotWalkEarlierEntries(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	sender := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	const total, limit = 1000, 10
	ops := make([]*userop.UserOperation, total)
	for i := range ops {
		n := int64(i + 1)
		ops[i] = &userop.UserOperation{
			Hash:            common.BigToHash(common.Big1.SetInt64(n)),
			Sender:          sender,
			TransactionHash: common.BigToHash(common.Big1.SetInt64(n + total)),
			BlockNumber:     uint64(n),
		}
	}
	require.NoError(t, s.SaveUserOps(ctx, ops))

	// The cursor of the page that ends at entry total-2*limit; the default
	// maximum limit bounds a page, so walk there page by page.
	var cursor string
	for read := 0; read < total-2*limit; read += limit {
		_, next, err := s.GetUserOpsBySender(ctx, sender, port.Page{After: cursor, Limit: limit})
		require.NoError(t, err)
		require.NotEmpty(t, next)
		cursor = next
	}

	before := s.pageSteps.Load()
	page, _, err := s.GetUserOpsBySender(ctx, sender, port.Page{After: cursor, Limit: limit})
	require.NoError(t, err)
	require.Len(t, page, limit)
	require.Equal(t, uint64(2*limit), page[0].BlockNumber, "newest first")
	require.LessOrEqual(t, s.pageSteps.Load()-before, int64(limit+1), "a cursor page visits only its own entries")

	before = s.pageSteps.Load()
	_, _, err = s.GetUserOpsBySender(ctx, sender, port.Page{Offset: total - limit, Limit: limit})
	require.NoError(t, err)
	require.GreaterOrEqual(t, s.pageSteps.Load()-before, int64(total-limit), "an offset page walks the entries before it")
}

package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// TestTokenHoldersCursorPageDoesNotWalkEarlierHolders: the holders list is
// ordered by balance through its index key, so a cursor page deep in the
// list visits only its own holders (and one to know whether more follow),
// while an offset page visits every holder before it.
func TestTokenHoldersCursorPageDoesNotWalkEarlierHolders(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	token := common.HexToAddress("0x00000000000000000000000000000000000000b1")
	const total, limit = 2000, 10

	// One block transaction, so the holders are written in one commit.
	txCtx, tx, err := s.BeginBlock(ctx)
	require.NoError(t, err)
	for i := 0; i < total; i++ {
		require.NoError(t, s.UpdateTokenHolder(txCtx, &port.TokenHolder{
			TokenAddress:  token,
			HolderAddress: common.BigToAddress(big.NewInt(int64(i + 1))),
			Balance:       big.NewInt(int64(i + 1)),
			LastUpdatedAt: 1,
		}))
	}
	require.NoError(t, tx.Commit())

	// The cursor of the page that ends at holder total-2*limit.
	_, cursor, err := s.GetTokenHolders(ctx, token, port.Page{Limit: total - 2*limit})
	require.NoError(t, err)
	require.NotEmpty(t, cursor)

	before := s.pageSteps.Load()
	page, _, err := s.GetTokenHolders(ctx, token, port.Page{After: cursor, Limit: limit})
	require.NoError(t, err)
	require.Len(t, page, limit)
	require.Zero(t, big.NewInt(2*limit).Cmp(page[0].Balance), "largest balance first: the page starts at balance %d", 2*limit)
	require.LessOrEqual(t, s.pageSteps.Load()-before, int64(limit+1), "a cursor page visits only its own holders")

	before = s.pageSteps.Load()
	_, _, err = s.GetTokenHolders(ctx, token, port.Page{Offset: total - limit, Limit: limit})
	require.NoError(t, err)
	require.GreaterOrEqual(t, s.pageSteps.Load()-before, int64(total-limit), "an offset page walks the holders before it")
}

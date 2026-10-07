package storage

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// TestERC20TransferCursorPageDoesNotWalkEarlierEntries: a cursor page of a
// token's transfers visits only its own index entries, however deep it is.
func TestERC20TransferCursorPageDoesNotWalkEarlierEntries(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	token := common.HexToAddress("0x00000000000000000000000000000000000020a1")
	const total, limit = 1000, 10
	for i := 0; i < total; i++ {
		require.NoError(t, s.SaveERC20Transfer(ctx, &port.ERC20Transfer{
			ContractAddress: token,
			From:            common.HexToAddress("0x01"),
			To:              common.HexToAddress("0x02"),
			Value:           big.NewInt(1),
			TransactionHash: common.BigToHash(big.NewInt(int64(i + 1))),
			BlockNumber:     uint64(i + 1),
		}))
	}

	// Walk to the page that ends at entry total-2*limit (pages are capped
	// at the maximum page size).
	var cursor string
	for read := 0; read < total-2*limit; read += limit {
		_, next, err := s.GetERC20TransfersByToken(ctx, token, port.Page{After: cursor, Limit: limit})
		require.NoError(t, err)
		require.NotEmpty(t, next)
		cursor = next
	}

	before := s.pageSteps.Load()
	page, _, err := s.GetERC20TransfersByToken(ctx, token, port.Page{After: cursor, Limit: limit})
	require.NoError(t, err)
	require.Len(t, page, limit)
	require.Equal(t, uint64(total-2*limit+1), page[0].BlockNumber)
	require.LessOrEqual(t, s.pageSteps.Load()-before, int64(limit+1), "a cursor page visits only its own entries")

	before = s.pageSteps.Load()
	_, _, err = s.GetERC20TransfersByToken(ctx, token, port.Page{Offset: total - limit, Limit: limit})
	require.NoError(t, err)
	require.GreaterOrEqual(t, s.pageSteps.Load()-before, int64(total-limit), "an offset page walks the entries before it")
}

// TestInternalTxCursorPageDoesNotWalkEarlierEntries: one index key holds
// several calls of a transaction, and the cursor addresses a call inside a
// key. A cursor page deep in the list, ending and starting inside
// transactions, visits only the keys of its own calls.
func TestInternalTxCursorPageDoesNotWalkEarlierEntries(t *testing.T) {
	s := newTestPebble(t)
	ctx := context.Background()
	from := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	to := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	// Each transaction makes three calls from `from`.
	const txs, callsPerTx, limit = 500, 3, 10
	for i := 0; i < txs; i++ {
		txHash := common.BigToHash(big.NewInt(int64(i + 1)))
		calls := make([]*port.InternalTransaction, callsPerTx)
		for j := range calls {
			calls[j] = &port.InternalTransaction{
				TransactionHash: txHash, BlockNumber: uint64(i + 1), Index: j,
				Type: port.InternalTxTypeCall, From: from, To: to, Value: big.NewInt(1),
			}
		}
		require.NoError(t, s.SaveInternalTransactions(ctx, txHash, calls))
	}

	// limit is not a multiple of callsPerTx: pages end inside transactions,
	// and the page read below starts at the last call of one.
	var cursor string
	read := 0
	for ; read < txs*callsPerTx-4*limit; read += limit {
		_, next, err := s.GetInternalTransactionsByAddress(ctx, from, true, port.Page{After: cursor, Limit: limit})
		require.NoError(t, err)
		require.NotEmpty(t, next)
		cursor = next
	}

	before := s.pageSteps.Load()
	page, _, err := s.GetInternalTransactionsByAddress(ctx, from, true, port.Page{After: cursor, Limit: limit})
	require.NoError(t, err)
	require.Len(t, page, limit)
	require.Equal(t, uint64(read/callsPerTx+1), page[0].BlockNumber)
	require.Equal(t, callsPerTx-1, read%callsPerTx)
	require.Equal(t, read%callsPerTx, page[0].Index)
	require.LessOrEqual(t, s.pageSteps.Load()-before, int64(limit/callsPerTx+2), "a cursor page visits only the keys of its own calls")

	before = s.pageSteps.Load()
	_, _, err = s.GetInternalTransactionsByAddress(ctx, from, true, port.Page{Offset: read, Limit: limit})
	require.NoError(t, err)
	require.GreaterOrEqual(t, s.pageSteps.Load()-before, int64(read/callsPerTx), "an offset page walks the keys before it")
}

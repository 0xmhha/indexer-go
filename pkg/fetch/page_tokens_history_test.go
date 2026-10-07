package fetch

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Page methods of the test doubles: they read by offset and return no cursor.

func (m *mockStorage) GetBalanceHistory(ctx context.Context, addr common.Address, fromBlock, toBlock uint64, page port.Page) ([]port.BalanceSnapshot, string, error) {
	items, err := m.offsetGetBalanceHistory(ctx, addr, fromBlock, toBlock, page.Limit, page.Offset)
	return items, "", err
}

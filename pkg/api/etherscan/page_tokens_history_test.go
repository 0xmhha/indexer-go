package etherscan

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Page methods of the test doubles: they read by offset and return no cursor.

func (m *mockStorage) ListVerifiedContracts(a0 context.Context, page port.Page) ([]common.Address, string, error) {
	items, err := m.offsetListVerifiedContracts(a0, page.Limit, page.Offset)
	return items, "", err
}

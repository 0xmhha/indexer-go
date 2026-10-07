package systemcontracts

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Page methods of the test doubles: they read by offset and return no cursor.

func (m *mockContractVerificationReader) ListVerifiedContracts(ctx context.Context, page port.Page) ([]common.Address, string, error) {
	items, err := m.offsetListVerifiedContracts(ctx, page.Limit, page.Offset)
	return items, "", err
}

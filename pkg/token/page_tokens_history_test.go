package token

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// Page methods of the test doubles: they read by offset and return no cursor.

func (m *mockTokenStorage) ListTokensByStandard(a0 context.Context, standard TokenStandard, page port.Page) ([]*TokenMetadata, string, error) {
	items, err := m.offsetListTokensByStandard(a0, standard, page.Limit, page.Offset)
	return items, "", err
}

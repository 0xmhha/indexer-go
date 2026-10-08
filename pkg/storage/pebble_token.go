package storage

import (
	"context"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/history"
)

// ============================================================================
// Token Balance Methods
// ============================================================================

// GetTokenBalances returns token balances for an address by scanning Transfer events
func (s *PebbleStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]port.TokenBalance, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	return history.TokenBalances(ctx, s, addr, tokenType, func(ctx context.Context, tb *port.TokenBalance) {
		s.applyTokenMetadata(ctx, tb, tb.ContractAddress)
	})
}

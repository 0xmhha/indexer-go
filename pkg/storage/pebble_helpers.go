package storage

import (
	"context"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage/history"
	"github.com/ethereum/go-ethereum/common"
)

// ============================================================================
// Token Balance Helpers
// ============================================================================

// applyTokenMetadata fills in a token balance's metadata (history.DescribeToken).
func (s *PebbleStorage) applyTokenMetadata(ctx context.Context, tb *port.TokenBalance, contract common.Address) {
	tb.ContractAddress = contract
	history.DescribeToken(ctx, tb, s, s.tokenMetadataFetcher, s.logger)
}

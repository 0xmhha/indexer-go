package storage

import (
	"context"
	"encoding/json"
	"time"

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

// buildTokenMetadataJSON creates a JSON string with additional token metadata
func buildTokenMetadataJSON(metadata *port.TokenMetadata) string {
	if metadata == nil {
		return ""
	}

	// Build metadata map with available fields
	metaMap := make(map[string]interface{})

	if metadata.BaseURI != "" {
		metaMap["baseURI"] = metadata.BaseURI
	}
	if metadata.TotalSupply != nil && metadata.TotalSupply.Sign() > 0 {
		metaMap["totalSupply"] = metadata.TotalSupply.String()
	}
	if metadata.SupportsERC165 {
		metaMap["supportsERC165"] = true
	}
	if metadata.SupportsMetadata {
		metaMap["supportsMetadata"] = true
	}
	if metadata.SupportsEnumerable {
		metaMap["supportsEnumerable"] = true
	}
	if !metadata.CreatedAt.IsZero() {
		metaMap["createdAt"] = metadata.CreatedAt.Format(time.RFC3339)
	}

	// Return empty string if no additional metadata
	if len(metaMap) == 0 {
		return ""
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(metaMap)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

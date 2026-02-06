package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ============================================================================
// Token Balance Methods
// ============================================================================

// GetTokenBalances returns token balances for an address by scanning Transfer events
func (s *PebbleStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]TokenBalance, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// Get the latest height
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		if err == ErrNotFound {
			return []TokenBalance{}, nil
		}
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Scan all blocks for Transfer events
	balanceMap, err := s.scanTransferEvents(ctx, addr, latestHeight)
	if err != nil {
		return nil, err
	}

	// Build result with metadata and filtering
	return s.buildTokenBalanceResult(ctx, balanceMap, tokenType), nil
}

// buildTokenMetadataJSON creates a JSON string with additional token metadata
func buildTokenMetadataJSON(metadata *TokenMetadata) string {
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

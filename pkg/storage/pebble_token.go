package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ============================================================================
// Token Balance Methods
// ============================================================================

// GetTokenBalances returns token balances for an address by scanning Transfer events
func (s *PebbleStorage) GetTokenBalances(ctx context.Context, addr common.Address, tokenType string) ([]TokenBalance, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	// ERC-20 Transfer event signature: Transfer(address indexed from, address indexed to, uint256 value)
	transferTopic := common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

	// Get the latest height
	latestHeight, err := s.GetLatestHeight(ctx)
	if err != nil {
		if err == ErrNotFound {
			return []TokenBalance{}, nil
		}
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Map to track balances by contract address
	balanceMap := make(map[common.Address]*big.Int)

	// Scan all blocks for receipts
	for height := uint64(0); height <= latestHeight; height++ {
		receipts, err := s.GetReceiptsByBlockNumber(ctx, height)
		if err != nil {
			continue
		}

		for _, receipt := range receipts {
			for _, log := range receipt.Logs {
				// Check if this is a Transfer event
				if len(log.Topics) < 3 || log.Topics[0] != transferTopic {
					continue
				}

				// Extract from and to addresses from topics
				from := common.BytesToAddress(log.Topics[1].Bytes())
				to := common.BytesToAddress(log.Topics[2].Bytes())

				// Check if this transfer involves our address
				if from != addr && to != addr {
					continue
				}

				// Extract value from data
				if len(log.Data) < 32 {
					continue
				}
				value := new(big.Int).SetBytes(log.Data[:32])

				// Get or create balance entry for this contract
				contract := log.Address
				if _, exists := balanceMap[contract]; !exists {
					balanceMap[contract] = big.NewInt(0)
				}

				// Update balance
				if to == addr {
					// Receiving tokens
					balanceMap[contract].Add(balanceMap[contract], value)
				} else if from == addr {
					// Sending tokens
					balanceMap[contract].Sub(balanceMap[contract], value)
				}
			}
		}
	}

	// Convert map to slice
	result := make([]TokenBalance, 0, len(balanceMap))
	for contract, balance := range balanceMap {
		// Only include non-zero balances
		if balance.Sign() > 0 {
			tb := TokenBalance{
				ContractAddress: contract,
				TokenType:       string(TokenStandardERC20), // Default to ERC20, updated from metadata below
				Balance:         balance,
				TokenID:         "", // Empty for ERC20
				Name:            "", // Default empty
				Symbol:          "", // Default empty
				Decimals:        nil,
				Metadata:        "",
			}

			// Apply token metadata if available
			// Priority: 1) System contract metadata, 2) Database, 3) On-demand fetch from chain
			if metadata := GetSystemContractTokenMetadata(contract); metadata != nil {
				// 1. System contract token metadata (hardcoded)
				tb.Name = metadata.Name
				tb.Symbol = metadata.Symbol
				decimals := metadata.Decimals
				tb.Decimals = &decimals
			} else if dbMetadata, err := s.GetTokenMetadata(ctx, contract); err == nil && dbMetadata != nil {
				// 2. Database token metadata
				tb.Name = dbMetadata.Name
				tb.Symbol = dbMetadata.Symbol
				decimals := int(dbMetadata.Decimals)
				tb.Decimals = &decimals
				// Set token type from stored standard
				if dbMetadata.Standard != "" {
					tb.TokenType = string(dbMetadata.Standard)
				}
				// Build metadata JSON with additional info
				tb.Metadata = buildTokenMetadataJSON(dbMetadata)
			} else if s.tokenMetadataFetcher != nil {
				// 3. On-demand fetch from chain and cache
				if fetchedMetadata, err := s.tokenMetadataFetcher.FetchTokenMetadata(ctx, contract); err == nil && fetchedMetadata != nil {
					tb.Name = fetchedMetadata.Name
					tb.Symbol = fetchedMetadata.Symbol
					decimals := int(fetchedMetadata.Decimals)
					tb.Decimals = &decimals
					// Set token type from fetched standard
					if fetchedMetadata.Standard != "" {
						tb.TokenType = string(fetchedMetadata.Standard)
					}
					// Build metadata JSON with additional info
					tb.Metadata = buildTokenMetadataJSON(fetchedMetadata)

					// Cache the fetched metadata for future queries
					if saveErr := s.SaveTokenMetadata(ctx, fetchedMetadata); saveErr != nil {
						s.logger.Warn("Failed to cache fetched token metadata",
							zap.String("contract", contract.Hex()),
							zap.Error(saveErr),
						)
					} else {
						s.logger.Info("Cached on-demand fetched token metadata",
							zap.String("contract", contract.Hex()),
							zap.String("name", fetchedMetadata.Name),
							zap.String("symbol", fetchedMetadata.Symbol),
							zap.Uint8("decimals", fetchedMetadata.Decimals),
						)
					}
				}
			}

			// Apply tokenType filter if specified
			if tokenType == "" || tokenType == tb.TokenType {
				result = append(result, tb)
			}
		}
	}

	return result, nil
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

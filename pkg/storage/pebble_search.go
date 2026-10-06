package storage

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
)

// Ensure PebbleStorage implements SearchReader
var _ port.SearchReader = (*PebbleStorage)(nil)

// Search performs a unified search across blocks, transactions, and addresses
func (s *PebbleStorage) Search(ctx context.Context, query string, resultTypes []string, limit int) ([]port.SearchResult, error) {
	if err := s.ensureNotClosed(); err != nil {
		return nil, err
	}

	if query == "" {
		return []port.SearchResult{}, nil
	}

	if limit <= 0 {
		limit = constants.DefaultPaginationLimit
	}

	var results []port.SearchResult
	queryType := detectQueryType(query)

	// Create a type filter map for quick lookup
	typeFilter := make(map[string]bool)
	if len(resultTypes) > 0 {
		for _, t := range resultTypes {
			typeFilter[t] = true
		}
	}

	// Helper function to check if type is allowed
	isTypeAllowed := func(t string) bool {
		if len(typeFilter) == 0 {
			return true
		}
		return typeFilter[t]
	}

	switch queryType {
	case "blockNumber":
		// Search by block number
		if isTypeAllowed("block") {
			blockNum, _ := strconv.ParseUint(query, 10, 64)
			block, err := s.GetBlock(ctx, blockNum)
			if err == nil && block != nil {
				metadata := map[string]interface{}{
					"number":           block.Number,
					"hash":             block.Hash.Hex(),
					"timestamp":        block.Time,
					"transactionCount": len(block.Transactions),
					"miner":            block.Miner.Hex(),
				}
				results = append(results, port.SearchResult{
					Type:     "block",
					Value:    fmt.Sprintf("%d", blockNum),
					Label:    fmt.Sprintf("Block #%d", blockNum),
					Metadata: metadata,
				})
			}
		}

	case "hash":
		// Try as block hash
		if isTypeAllowed("block") && len(results) < limit {
			hash := common.HexToHash(query)
			block, err := s.GetBlockByHash(ctx, hash)
			if err == nil && block != nil {
				metadata := map[string]interface{}{
					"number":           block.Number,
					"hash":             block.Hash.Hex(),
					"timestamp":        block.Time,
					"transactionCount": len(block.Transactions),
					"miner":            block.Miner.Hex(),
				}
				results = append(results, port.SearchResult{
					Type:     "block",
					Value:    block.Hash.Hex(),
					Label:    fmt.Sprintf("Block #%d", block.Number),
					Metadata: metadata,
				})
			}
		}

		// Try as transaction hash
		if isTypeAllowed("transaction") && len(results) < limit {
			hash := common.HexToHash(query)
			// The model keeps the hash, sender and type the chain reports.
			tx, location, err := s.GetTransaction(ctx, hash)
			if err == nil && tx != nil && location != nil {
				value := "0"
				if tx.Value != nil {
					value = tx.Value.String()
				}
				metadata := map[string]interface{}{
					"hash":        tx.Hash.Hex(),
					"from":        tx.From.Hex(),
					"to":          "",
					"blockNumber": location.BlockHeight,
					"blockHash":   location.BlockHash.Hex(),
					"value":       value,
					"gas":         tx.Gas,
				}
				if tx.To != nil {
					metadata["to"] = tx.To.Hex()
				} else {
					// Contract creation transaction - get contract address from receipt
					receipt, err := s.GetReceipt(ctx, tx.Hash)
					if err == nil && receipt != nil && receipt.ContractAddress != nil {
						metadata["contractAddress"] = receipt.ContractAddress.Hex()
					}
				}
				results = append(results, port.SearchResult{
					Type:     "transaction",
					Value:    tx.Hash.Hex(),
					Label:    fmt.Sprintf("Transaction %s", tx.Hash.Hex()[:10]+"..."),
					Metadata: metadata,
				})
			}
		}

	case "address":
		// Search by address
		addr := common.HexToAddress(query)

		// Check if it's a contract
		if isTypeAllowed("contract") && len(results) < limit {
			// Check if address has an ABI (indicating it's a contract)
			hasABI, _ := s.HasABI(ctx, addr)
			if hasABI {
				metadata := map[string]interface{}{
					"address":    addr.Hex(),
					"isContract": true,
				}

				// Try to get transaction count for this address
				txHashes, err := s.GetTransactionsByAddress(ctx, addr, 1, 0)
				if err == nil {
					metadata["transactionCount"] = len(txHashes)
				}

				results = append(results, port.SearchResult{
					Type:     "contract",
					Value:    addr.Hex(),
					Label:    fmt.Sprintf("Contract %s", addr.Hex()[:10]+"..."),
					Metadata: metadata,
				})
			}
		}

		// Always include as address if not found as contract or if both types allowed
		if isTypeAllowed("address") && len(results) < limit {
			metadata := map[string]interface{}{
				"address": addr.Hex(),
			}

			// Try to get transaction count
			txHashes, err := s.GetTransactionsByAddress(ctx, addr, 1, 0)
			if err == nil && len(txHashes) > 0 {
				metadata["transactionCount"] = len(txHashes)
			}

			results = append(results, port.SearchResult{
				Type:     "address",
				Value:    addr.Hex(),
				Label:    fmt.Sprintf("Address %s", addr.Hex()[:10]+"..."),
				Metadata: metadata,
			})
		}
	}

	// Apply limit
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// detectQueryType determines the type of search query
func detectQueryType(query string) string {
	// Remove 0x prefix if present
	query = strings.TrimPrefix(query, "0x")

	// Check if it's a number (block number)
	if _, err := strconv.ParseUint(query, 10, 64); err == nil {
		return "blockNumber"
	}

	// Check if it's a valid hex hash (64 characters for block/tx hash, 40 for address)
	if len(query) == 64 {
		// Could be block hash or transaction hash
		return "hash"
	} else if len(query) == 40 {
		// Address
		return "address"
	}

	// Default to address for shorter queries (partial address search could be implemented)
	return "address"
}

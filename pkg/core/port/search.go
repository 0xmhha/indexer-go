package port

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// SearchResult represents a unified search result across different entity types
type SearchResult struct {
	Type     string                 `json:"type"`     // "block", "transaction", "address", "contract"
	Value    string                 `json:"value"`    // The matched value (hash, address, block number)
	Label    string                 `json:"label"`    // Human-readable display label
	Metadata map[string]interface{} `json:"metadata"` // Additional metadata as map
}

// SearchReader provides search capabilities across indexed blockchain data
type SearchReader interface {
	// Search performs a unified search across blocks, transactions, and addresses
	// query: search string (block number, hash, or address)
	// resultTypes: optional filter for result types (nil = all types)
	// limit: maximum number of results to return
	Search(ctx context.Context, query string, resultTypes []string, limit int) ([]SearchResult, error)
}

// DefaultSearchLimit is the number of results a search returns when it is
// given no limit.
const DefaultSearchLimit = 10

// SearchSource is what RunSearch reads: blocks, transactions and receipts,
// whether an address has an ABI (which makes it a contract), and how many
// transactions an address's index holds.
type SearchSource interface {
	BlockReader
	HasABI(ctx context.Context, address common.Address) (bool, error)
	CountAddressTransactions(ctx context.Context, address common.Address) (int, error)
}

// RunSearch implements SearchReader.Search on any store: a decimal or
// 0x-prefixed hex number finds the block at that height, a 32-byte hash the
// block or transaction with that hash, and a 20-byte address yields an
// address result, preceded by a contract result when the address has an
// ABI. resultTypes restricts the result types (empty: all), limit caps the
// results (DefaultSearchLimit when not positive).
func RunSearch(ctx context.Context, s SearchSource, query string, resultTypes []string, limit int) ([]SearchResult, error) {
	if query == "" {
		return []SearchResult{}, nil
	}

	if limit <= 0 {
		limit = DefaultSearchLimit
	}

	var results []SearchResult
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
			blockNum, _ := parseBlockNumberQuery(query)
			block, err := s.GetBlock(ctx, blockNum)
			if err == nil && block != nil {
				metadata := map[string]interface{}{
					"number":           block.Number,
					"hash":             block.Hash.Hex(),
					"timestamp":        block.Time,
					"transactionCount": len(block.Transactions),
					"miner":            block.Miner.Hex(),
				}
				results = append(results, SearchResult{
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
				results = append(results, SearchResult{
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
				results = append(results, SearchResult{
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
				txCount, err := s.CountAddressTransactions(ctx, addr)
				if err == nil {
					metadata["transactionCount"] = txCount
				}

				results = append(results, SearchResult{
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
			txCount, err := s.CountAddressTransactions(ctx, addr)
			if err == nil && txCount > 0 {
				metadata["transactionCount"] = txCount
			}

			results = append(results, SearchResult{
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

// parseBlockNumberQuery parses a block number given in decimal or as a
// 0x-prefixed hex number.
func parseBlockNumberQuery(query string) (uint64, bool) {
	if hex, ok := strings.CutPrefix(query, "0x"); ok {
		n, err := strconv.ParseUint(hex, 16, 64)
		return n, err == nil
	}
	n, err := strconv.ParseUint(query, 10, 64)
	return n, err == nil
}

// isHexDigits reports whether s is a non-empty run of hex digits.
func isHexDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// detectQueryType determines the type of search query. It returns "" for a
// query that is not a block number, hash or address.
func detectQueryType(query string) string {
	// Remove 0x prefix if present
	hex := strings.TrimPrefix(query, "0x")

	// Check if it's a valid hex hash (64 characters for block/tx hash, 40 for
	// address). These are checked first so that a hash or address made of
	// digits is not read as a block number.
	if isHexDigits(hex) {
		if len(hex) == 64 {
			// Could be block hash or transaction hash
			return "hash"
		} else if len(hex) == 40 {
			// Address
			return "address"
		}
	}

	// Check if it's a number (block number)
	if _, ok := parseBlockNumberQuery(query); ok {
		return "blockNumber"
	}

	return ""
}

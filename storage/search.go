package storage

import (
	"context"
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

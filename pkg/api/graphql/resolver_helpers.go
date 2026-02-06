package graphql

import (
	"context"
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/graphql-go/graphql"
)

// extractContext safely extracts context.Context from interface{}
func extractContext(ctx interface{}) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if c, ok := ctx.(context.Context); ok {
		return c
	}
	return context.Background()
}

// ============================================================================
// Pagination Helpers
// ============================================================================

// PaginationParams holds pagination parameters
type PaginationParams struct {
	Limit  int
	Offset int
}

// parsePaginationParams extracts pagination parameters from GraphQL args
func parsePaginationParams(p graphql.ResolveParams, maxLimit int) PaginationParams {
	params := PaginationParams{
		Limit:  constants.DefaultPaginationLimit,
		Offset: 0,
	}

	if maxLimit == 0 {
		maxLimit = constants.DefaultMaxPaginationLimit
	}

	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > maxLimit {
				params.Limit = maxLimit
			} else {
				params.Limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			params.Offset = o
		}
	}

	return params
}

// ============================================================================
// Block Filter Helpers
// ============================================================================

// BlockFilter holds block filter parameters
type BlockFilter struct {
	NumberFrom    uint64
	NumberTo      uint64
	TimestampFrom uint64
	TimestampTo   uint64
	Miner         *common.Address
}

// parseBlockFilter extracts block filter parameters from GraphQL args
func parseBlockFilter(p graphql.ResolveParams) BlockFilter {
	filter := BlockFilter{}

	if f, ok := p.Args["filter"].(map[string]interface{}); ok {
		if nf, ok := f["numberFrom"].(string); ok {
			if n, err := strconv.ParseUint(nf, 10, 64); err == nil {
				filter.NumberFrom = n
			}
		}
		if nt, ok := f["numberTo"].(string); ok {
			if n, err := strconv.ParseUint(nt, 10, 64); err == nil {
				filter.NumberTo = n
			}
		}
		if tf, ok := f["timestampFrom"].(string); ok {
			if t, err := strconv.ParseUint(tf, 10, 64); err == nil {
				filter.TimestampFrom = t
			}
		}
		if tt, ok := f["timestampTo"].(string); ok {
			if t, err := strconv.ParseUint(tt, 10, 64); err == nil {
				filter.TimestampTo = t
			}
		}
		if m, ok := f["miner"].(string); ok {
			addr := common.HexToAddress(m)
			filter.Miner = &addr
		}
	}

	return filter
}

// hasNumberFilter returns true if number range filter is specified
func (f *BlockFilter) hasNumberFilter() bool {
	return f.NumberFrom != 0 || f.NumberTo != 0
}

// hasTimestampFilter returns true if timestamp filter is specified
func (f *BlockFilter) hasTimestampFilter() bool {
	return f.TimestampFrom > 0 || f.TimestampTo > 0
}

// hasMinerFilter returns true if miner filter is specified
func (f *BlockFilter) hasMinerFilter() bool {
	return f.Miner != nil
}

// filterBlocks filters blocks by timestamp and miner conditions
func filterBlocks(blocks []*types.Block, filter BlockFilter) []*types.Block {
	filtered := make([]*types.Block, 0, len(blocks))

	for _, block := range blocks {
		if block == nil {
			continue
		}

		// Filter by timestamp
		if filter.TimestampFrom > 0 && block.Time() < filter.TimestampFrom {
			continue
		}
		if filter.TimestampTo > 0 && block.Time() > filter.TimestampTo {
			continue
		}

		// Filter by miner
		if filter.Miner != nil && block.Coinbase() != *filter.Miner {
			continue
		}

		filtered = append(filtered, block)
	}

	return filtered
}

// ============================================================================
// Transaction Filter Helpers
// ============================================================================

// TransactionFilter holds transaction filter parameters
type TransactionFilter struct {
	BlockNumberFrom uint64
	BlockNumberTo   uint64
	From            *common.Address
	To              *common.Address
	TxType          *int
}

// parseTransactionFilter extracts transaction filter parameters from GraphQL args
func parseTransactionFilter(p graphql.ResolveParams) TransactionFilter {
	filter := TransactionFilter{}

	if f, ok := p.Args["filter"].(map[string]interface{}); ok {
		if bnf, ok := f["blockNumberFrom"].(string); ok {
			if bn, err := strconv.ParseUint(bnf, 10, 64); err == nil {
				filter.BlockNumberFrom = bn
			}
		}
		if bnt, ok := f["blockNumberTo"].(string); ok {
			if bn, err := strconv.ParseUint(bnt, 10, 64); err == nil {
				filter.BlockNumberTo = bn
			}
		}
		if from, ok := f["from"].(string); ok {
			addr := common.HexToAddress(from)
			filter.From = &addr
		}
		if to, ok := f["to"].(string); ok {
			addr := common.HexToAddress(to)
			filter.To = &addr
		}
		if t, ok := f["type"].(int); ok {
			filter.TxType = &t
		}
	}

	return filter
}

// hasAddressFilter returns true if address filter is specified
func (f *TransactionFilter) hasAddressFilter() bool {
	return f.From != nil || f.To != nil || f.TxType != nil
}

// ============================================================================
// Log Filter Helpers
// ============================================================================

// LogFilter holds log filter parameters
type LogFilter struct {
	Address         *common.Address
	Topics          []common.Hash
	BlockNumberFrom uint64
	BlockNumberTo   uint64
}

// parseLogFilter extracts log filter parameters from GraphQL args
func parseLogFilter(p graphql.ResolveParams) (LogFilter, error) {
	filter := LogFilter{}

	f, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return filter, fmt.Errorf("filter is required")
	}

	if addr, ok := f["address"].(string); ok {
		address := common.HexToAddress(addr)
		filter.Address = &address
	}

	if topicsInterface, ok := f["topics"].([]interface{}); ok {
		filter.Topics = make([]common.Hash, 0, len(topicsInterface))
		for _, t := range topicsInterface {
			if topicStr, ok := t.(string); ok {
				filter.Topics = append(filter.Topics, common.HexToHash(topicStr))
			}
		}
	}

	if bnf, ok := f["blockNumberFrom"].(string); ok {
		if bn, err := strconv.ParseUint(bnf, 10, 64); err == nil {
			filter.BlockNumberFrom = bn
		}
	}

	if bnt, ok := f["blockNumberTo"].(string); ok {
		if bn, err := strconv.ParseUint(bnt, 10, 64); err == nil {
			filter.BlockNumberTo = bn
		}
	}

	return filter, nil
}

// matchesLog checks if a log matches the filter criteria
func (f *LogFilter) matchesLog(log *types.Log) bool {
	if log == nil {
		return false
	}

	// Apply address filter
	if f.Address != nil && log.Address != *f.Address {
		return false
	}

	// Apply topics filter
	if len(f.Topics) > 0 {
		matchesTopics := false
		for _, filterTopic := range f.Topics {
			for _, logTopic := range log.Topics {
				if filterTopic == logTopic {
					matchesTopics = true
					break
				}
			}
			if matchesTopics {
				break
			}
		}
		if !matchesTopics {
			return false
		}
	}

	return true
}

// ============================================================================
// Connection Response Helpers
// ============================================================================

// ConnectionResponse represents a paginated GraphQL connection response
type ConnectionResponse struct {
	Nodes           []interface{}
	TotalCount      int
	HasNextPage     bool
	HasPreviousPage bool
	StartCursor     interface{}
	EndCursor       interface{}
}

// emptyConnection returns an empty connection response
func emptyConnection(hasPreviousPage bool) map[string]interface{} {
	return map[string]interface{}{
		"nodes":      []interface{}{},
		"totalCount": 0,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     false,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}
}

// buildConnectionResponse builds a connection response from ConnectionResponse struct
func buildConnectionResponse(resp ConnectionResponse) map[string]interface{} {
	return map[string]interface{}{
		"nodes":      resp.Nodes,
		"totalCount": resp.TotalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     resp.HasNextPage,
			"hasPreviousPage": resp.HasPreviousPage,
			"startCursor":     resp.StartCursor,
			"endCursor":       resp.EndCursor,
		},
	}
}

// ============================================================================
// Block Range Calculation Helpers
// ============================================================================

// BlockRange represents calculated block range for pagination
type BlockRange struct {
	StartBlock uint64
	EndBlock   uint64
}

// calculateBlockRangeReverse calculates block range for reverse order pagination (latest first)
func calculateBlockRangeReverse(latestHeight uint64, offset, limit int) (BlockRange, bool) {
	if latestHeight < uint64(offset) {
		return BlockRange{}, false
	}

	endBlock := latestHeight - uint64(offset)
	var startBlock uint64
	if endBlock >= uint64(limit-1) {
		startBlock = endBlock - uint64(limit) + 1
	} else {
		startBlock = 0
	}

	return BlockRange{StartBlock: startBlock, EndBlock: endBlock}, true
}

// calculateBlockRangeForward calculates block range for forward order pagination
func calculateBlockRangeForward(numberFrom, numberTo uint64, offset, limit int) (BlockRange, bool) {
	rangeSize := numberTo - numberFrom + 1
	if uint64(offset) >= rangeSize {
		return BlockRange{}, false
	}

	startBlock := numberFrom + uint64(offset)
	endBlock := startBlock + uint64(limit) - 1
	if endBlock > numberTo {
		endBlock = numberTo
	}

	return BlockRange{StartBlock: startBlock, EndBlock: endBlock}, true
}

// ============================================================================
// Utility Helpers
// ============================================================================

// reverseSlice reverses a slice in place
func reverseSlice[T any](slice []T) {
	for i, j := 0, len(slice)-1; i < j; i, j = i+1, j-1 {
		slice[i], slice[j] = slice[j], slice[i]
	}
}

// applyPagination applies offset and limit to a slice
func applyPagination[T any](items []T, offset, limit int) []T {
	start := offset
	end := offset + limit

	if start > len(items) {
		start = len(items)
	}
	if end > len(items) {
		end = len(items)
	}

	return items[start:end]
}

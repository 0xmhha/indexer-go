package graphql

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// maxUnboundedOffset caps pagination depth for list queries without a block
// range. Deeper pages must give blockNumberFrom/blockNumberTo.
const maxUnboundedOffset = 10000

// errOffsetTooDeep is returned when an unbounded list query asks for a page
// beyond maxUnboundedOffset.
var errOffsetTooDeep = fmt.Errorf("offset above %d requires a block range (blockNumberFrom/blockNumberTo)", maxUnboundedOffset)

// recentTransactions serves a transactions query without a block range.
// It reads blocks newest first, one at a time, and stops once it has
// offset+limit+1 matches, so cost follows the page asked for instead of the
// chain length. Ordering matches the bounded path (newest block first, then
// highest transaction index first).
//
// totalCount is exact when there is no address filter (global counter, as
// before) or when the scan reached genesis; otherwise it is a lower bound
// (at least one more match exists when hasNextPage is true).
func (s *Schema) recentTransactions(ctx context.Context, filter TransactionFilter, pagination PaginationParams, latestHeight uint64) (interface{}, error) {
	if pagination.Offset > maxUnboundedOffset {
		return nil, errOffsetTooDeep
	}
	need := pagination.Offset + pagination.Limit + 1

	var collected []map[string]interface{}
	for h := int64(latestHeight); h >= 0 && len(collected) < need; h-- {
		block, err := s.storage.GetBlock(ctx, uint64(h))
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("failed to get block %d: %w", h, err)
		}
		matches := s.filterTransactionsFromBlocks([]*model.Block{block}, filter)
		reverseSlice(matches)
		collected = append(collected, matches...)
	}

	totalCount := s.calculateTxTotalCount(ctx, filter, collected)
	page := applyPagination(collected, pagination.Offset, pagination.Limit)
	return s.buildTxConnectionResponse(page, totalCount, len(collected), pagination), nil
}

// earliestLogs serves a logs query without a block range. It reads blocks
// from genesis upward (the order the bounded path returns) and stops once it
// has offset+limit+1 matches. totalCount is exact only when the scan reached
// the latest block; otherwise it is a lower bound.
func (s *Schema) earliestLogs(ctx context.Context, filter LogFilter, pagination PaginationParams, latestHeight uint64, decode bool) (interface{}, error) {
	if pagination.Offset > maxUnboundedOffset {
		return nil, errOffsetTooDeep
	}
	need := pagination.Offset + pagination.Limit + 1

	var collected []map[string]interface{}
	for h := uint64(0); h <= latestHeight && len(collected) < need; h++ {
		collected = append(collected, s.collectLogsFromBlockRange(ctx, h, h, filter, decode)...)
	}
	if len(collected) >= need {
		s.logger.Debug("unbounded logs query stopped early", zap.Int("collected", len(collected)))
	}

	page := applyPagination(collected, pagination.Offset, pagination.Limit)
	return s.buildLogConnectionResponse(page, len(collected), pagination), nil
}

package graphql

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common"

	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
)

// maxUnboundedOffset caps pagination depth for list queries without a block
// range. Deeper pages must give blockNumberFrom/blockNumberTo.
const maxUnboundedOffset = 10000

// maxUnboundedScan is how many blocks a query without a block range reads
// when no index serves its filter. A query that reaches it before filling
// its page returns what it found with scannedThrough set to the last block
// it read, so one rare match cannot make a request read the whole chain.
const maxUnboundedScan = 10000

// logIndexWindow is how many blocks one read of a log index covers; it
// bounds the logs held at once for a busy contract or topic.
const logIndexWindow = 10000

// addressIndexChunk is how many address index entries are read at a time.
const addressIndexChunk = 256

// addressIndexFeature is the feature that writes the address index
// (pkg/features/address).
const addressIndexFeature = "address.index"

// errOffsetTooDeep is returned when an unbounded list query asks for a page
// beyond maxUnboundedOffset.
var errOffsetTooDeep = fmt.Errorf("offset above %d requires a block range (blockNumberFrom/blockNumberTo)", maxUnboundedOffset)

// recentTransactions serves a transactions query without a block range,
// newest block first, then highest transaction index first (the order of
// the bounded path), and stops once it has offset+limit+1 matches.
//
// A from or to filter is served from the address index when it is complete
// (address.index active without a gap), so cost follows the address's
// transactions instead of the chain length. Other filters read blocks
// newest first, at most maxUnboundedScan of them.
//
// totalCount is exact when there is no address filter (global counter, as
// before) or when every candidate was read; otherwise it is a lower bound
// (at least one more match exists when hasNextPage is true).
func (s *Schema) recentTransactions(ctx context.Context, filter TransactionFilter, pagination PaginationParams, latestHeight uint64) (interface{}, error) {
	if pagination.Offset > maxUnboundedOffset {
		return nil, errOffsetTooDeep
	}
	need := pagination.Offset + pagination.Limit + 1

	var collected []map[string]interface{}
	var scannedThrough *uint64
	if addr, reader, ok := s.addressIndexFor(ctx, filter); ok {
		var err error
		if collected, err = s.indexedTransactions(ctx, reader, addr, filter, need); err != nil {
			return nil, err
		}
	} else {
		lowest := uint64(0)
		if latestHeight >= maxUnboundedScan {
			lowest = latestHeight - maxUnboundedScan + 1
		}
		h := latestHeight
		for ; len(collected) < need; h-- {
			block, err := s.storage.GetBlock(ctx, h)
			switch {
			case errors.Is(err, port.ErrNotFound):
			case err != nil:
				return nil, fmt.Errorf("failed to get block %d: %w", h, err)
			default:
				matches := s.filterTransactionsFromBlocks([]*model.Block{block}, filter)
				reverseSlice(matches)
				collected = append(collected, matches...)
			}
			if h == lowest {
				if len(collected) < need && lowest > 0 {
					scannedThrough = &lowest
				}
				break
			}
		}
	}

	totalCount := s.calculateTxTotalCount(ctx, filter, collected)
	page := applyPagination(collected, pagination.Offset, pagination.Limit)
	resp := s.buildTxConnectionResponse(page, totalCount, len(collected), pagination)
	if scannedThrough != nil {
		markScanStopped(resp, *scannedThrough)
	}
	return resp, nil
}

// addressIndexFor returns the address whose index serves filter (from,
// else to) when the store keeps a complete address index.
func (s *Schema) addressIndexFor(ctx context.Context, filter TransactionFilter) (common.Address, port.AddressTransactionsNewestFirst, bool) {
	var addr common.Address
	switch {
	case filter.From != nil:
		addr = *filter.From
	case filter.To != nil:
		addr = *filter.To
	default:
		return addr, nil, false
	}
	reader, ok := s.storage.(port.AddressTransactionsNewestFirst)
	if !ok {
		return addr, nil, false
	}
	states, ok := s.storage.(interface {
		FeatureStates(ctx context.Context) (map[string]port.FeatureState, error)
	})
	if !ok {
		return addr, nil, false
	}
	all, err := states.FeatureStates(ctx)
	if err != nil {
		return addr, nil, false
	}
	st, ok := all[addressIndexFeature]
	if !ok || !st.Active || st.Gap != nil {
		return addr, nil, false // the index misses blocks
	}
	return addr, reader, true
}

// indexedTransactions reads addr's index newest first and keeps the
// transactions matching filter until it has need of them. The index also
// holds transactions where addr is the other party or the fee payer, so
// every one is checked against the filter.
func (s *Schema) indexedTransactions(ctx context.Context, reader port.AddressTransactionsNewestFirst, addr common.Address, filter TransactionFilter, need int) ([]map[string]interface{}, error) {
	var collected []map[string]interface{}
	seen := map[common.Hash]bool{}
	blockTimes := map[uint64]uint64{}
	page := port.Page{Limit: addressIndexChunk}
	for len(collected) < need {
		hashes, next, err := reader.GetTransactionsByAddressNewestFirst(ctx, addr, page)
		if err != nil {
			return nil, fmt.Errorf("failed to read the address index: %w", err)
		}
		txs, locations, err := s.storage.GetTransactions(ctx, hashes)
		if err != nil {
			return nil, fmt.Errorf("failed to get transactions: %w", err)
		}
		for i, tx := range txs {
			if tx == nil || locations[i] == nil || seen[tx.Hash] || !s.matchesTransactionFilter(tx, filter) {
				continue
			}
			seen[tx.Hash] = true
			height := locations[i].BlockHeight
			blockTime, ok := blockTimes[height]
			if !ok {
				block, err := s.storage.GetBlock(ctx, height)
				if err != nil {
					return nil, fmt.Errorf("failed to get block %d: %w", height, err)
				}
				blockTime = block.Time
				blockTimes[height] = blockTime
			}
			txMap := s.transactionToMap(tx, locations[i])
			txMap["blockTimestamp"] = fmt.Sprintf("%d", blockTime)
			collected = append(collected, txMap)
			if len(collected) == need {
				break
			}
		}
		if next == "" {
			break
		}
		page = port.Page{After: next, Limit: addressIndexChunk}
	}
	return collected, nil
}

// earliestLogs serves a logs query without a block range, from genesis
// upward (the order of the bounded path), and stops once it has
// offset+limit+1 matches. An address or topic filter is served from the
// log indexes a window at a time, so cost follows the matching logs; a
// query without either reads blocks, at most maxUnboundedScan of them.
// totalCount is exact only when every candidate was read; otherwise it is
// a lower bound.
func (s *Schema) earliestLogs(ctx context.Context, filter LogFilter, pagination PaginationParams, latestHeight uint64, decode bool) (interface{}, error) {
	if pagination.Offset > maxUnboundedOffset {
		return nil, errOffsetTooDeep
	}
	need := pagination.Offset + pagination.Limit + 1

	var collected []map[string]interface{}
	var scannedThrough *uint64
	if filter.Address != nil || len(filter.Topics) > 0 {
		for from := uint64(0); from <= latestHeight && len(collected) < need; from += logIndexWindow {
			to := min(from+logIndexWindow-1, latestHeight)
			logs, err := s.indexedLogs(ctx, filter, from, to)
			if err != nil {
				return nil, err
			}
			for _, l := range logs {
				if len(collected) == need {
					break
				}
				collected = append(collected, s.logToMapWithDecode(l, decode))
			}
		}
	} else {
		last := min(latestHeight, maxUnboundedScan-1)
		for h := uint64(0); h <= last && len(collected) < need; h++ {
			collected = append(collected, s.collectLogsFromBlockRange(ctx, h, h, filter, decode)...)
		}
		if len(collected) < need && last < latestHeight {
			scannedThrough = &last
		}
	}

	page := applyPagination(collected, pagination.Offset, pagination.Limit)
	resp := s.buildLogConnectionResponse(page, len(collected), pagination)
	if scannedThrough != nil {
		markScanStopped(resp, *scannedThrough)
	}
	return resp, nil
}

// indexedLogs returns the logs in [from, to] matching filter, in chain
// order, read from the address index or, without an address, from the
// topic indexes (a filter topic may be at any position).
func (s *Schema) indexedLogs(ctx context.Context, filter LogFilter, from, to uint64) ([]*model.Log, error) {
	var candidates []*model.Log
	if filter.Address != nil {
		logs, err := s.storage.GetLogsByAddress(ctx, *filter.Address, from, to)
		if err != nil {
			return nil, fmt.Errorf("failed to read the log address index: %w", err)
		}
		candidates = logs
	} else {
		type logID struct {
			block uint64
			index uint
		}
		seen := map[logID]bool{}
		for _, topic := range filter.Topics {
			for position := 0; position < 4; position++ {
				logs, err := s.storage.GetLogsByTopic(ctx, topic, position, from, to)
				if err != nil {
					return nil, fmt.Errorf("failed to read the log topic index: %w", err)
				}
				for _, l := range logs {
					if id := (logID{l.BlockNumber, l.Index}); !seen[id] {
						seen[id] = true
						candidates = append(candidates, l)
					}
				}
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].BlockNumber != candidates[j].BlockNumber {
			return candidates[i].BlockNumber < candidates[j].BlockNumber
		}
		return candidates[i].Index < candidates[j].Index
	})
	matching := candidates[:0]
	for _, l := range candidates {
		if filter.matchesLog(l) {
			matching = append(matching, l)
		}
	}
	return matching, nil
}

// markScanStopped records on a connection that its query stopped at the
// scan limit after reading through block (the lowest for transactions, the
// highest for logs) before filling its page.
func markScanStopped(resp map[string]interface{}, block uint64) {
	resp["scannedThrough"] = fmt.Sprintf("%d", block)
}

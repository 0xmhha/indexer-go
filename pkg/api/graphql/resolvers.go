package graphql

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveLatestHeight resolves the latest indexed block height
func (s *Schema) resolveLatestHeight(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	height, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return "0", nil
		}
		s.logger.Error("failed to get latest height",
			zap.Error(err))
		return nil, err
	}

	return fmt.Sprintf("%d", height), nil
}

// resolveBlock resolves a block by number
func (s *Schema) resolveBlock(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	numberStr, ok := p.Args["number"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block number")
	}

	number, err := strconv.ParseUint(numberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid block number format: %w", err)
	}

	block, err := s.models().GetModelBlock(ctx, number)
	if err != nil {
		s.logger.Error("failed to get block",
			zap.Uint64("number", number),
			zap.Error(err))
		return nil, err
	}

	return s.blockToMap(block), nil
}

// resolveBlockByHash resolves a block by hash
func (s *Schema) resolveBlockByHash(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	hashStr, ok := p.Args["hash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block hash")
	}

	hash := common.HexToHash(hashStr)
	block, err := s.models().GetModelBlockByHash(ctx, hash)
	if err != nil {
		s.logger.Error("failed to get block by hash",
			zap.String("hash", hashStr),
			zap.Error(err))
		return nil, err
	}

	return s.blockToMap(block), nil
}

// resolveBlocks resolves blocks with filtering and pagination
func (s *Schema) resolveBlocks(p graphql.ResolveParams) (interface{}, error) {
	ctx := extractContext(p.Context)
	pagination := parsePaginationParams(p, 0)
	filter := parseBlockFilter(p)

	// Get latest height
	latestHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return emptyConnection(false), nil
		}
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Track whether user explicitly specified a number range filter
	// Must be checked BEFORE mutating filter.NumberTo below
	userRequestedRange := filter.hasNumberFilter()

	// Set default range if not specified
	if !userRequestedRange {
		filter.NumberTo = latestHeight
	} else if filter.NumberTo == 0 {
		filter.NumberTo = latestHeight
	}

	// Validate range
	if filter.NumberFrom > filter.NumberTo {
		return nil, fmt.Errorf("invalid block range: numberFrom (%d) > numberTo (%d)", filter.NumberFrom, filter.NumberTo)
	}

	// Calculate block range based on pagination mode
	// Default queries (no user filter) use reverse order (latest blocks first)
	// User-filtered queries use forward order
	blockRange, ok := s.calculateBlockRange(filter, latestHeight, pagination, userRequestedRange)
	if !ok {
		return emptyConnection(pagination.Offset > 0), nil
	}

	// Fetch and filter blocks
	blocks, err := s.models().GetModelBlocks(ctx, blockRange.StartBlock, blockRange.EndBlock)
	if err != nil {
		s.logger.Error("failed to get blocks",
			zap.Uint64("startBlock", blockRange.StartBlock),
			zap.Uint64("endBlock", blockRange.EndBlock),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get blocks: %w", err)
	}

	filteredBlocks := filterBlocks(blocks, filter)
	nodes := s.blocksToNodes(filteredBlocks)
	totalCount := s.calculateBlockTotalCount(ctx, filter, filteredBlocks, latestHeight)

	reverseOrder := !userRequestedRange
	return s.buildBlockConnectionResponse(filteredBlocks, nodes, totalCount, filter, blockRange, pagination, reverseOrder), nil
}

// calculateBlockRange calculates the block range for pagination
// userRequestedRange indicates whether the user explicitly specified a number range filter
func (s *Schema) calculateBlockRange(filter BlockFilter, latestHeight uint64, pagination PaginationParams, userRequestedRange bool) (BlockRange, bool) {
	if !userRequestedRange {
		return calculateBlockRangeReverse(latestHeight, pagination.Offset, pagination.Limit)
	}
	return calculateBlockRangeForward(filter.NumberFrom, filter.NumberTo, pagination.Offset, pagination.Limit)
}

// blocksToNodes converts blocks to GraphQL nodes
func (s *Schema) blocksToNodes(blocks []*model.Block) []interface{} {
	nodes := make([]interface{}, len(blocks))
	for i, block := range blocks {
		nodes[i] = s.blockToMap(block)
	}
	return nodes
}

// calculateBlockTotalCount calculates total count based on filters
func (s *Schema) calculateBlockTotalCount(ctx context.Context, filter BlockFilter, filteredBlocks []*model.Block, latestHeight uint64) int {
	if filter.hasTimestampFilter() || filter.hasMinerFilter() {
		return len(filteredBlocks)
	}

	if count, err := s.storage.GetBlockCount(ctx); err == nil {
		return int(count)
	}
	return int(latestHeight + 1)
}

// buildBlockConnectionResponse builds the GraphQL connection response for blocks
// reverseOrder indicates default (no filter) pagination where latest blocks come first
func (s *Schema) buildBlockConnectionResponse(blocks []*model.Block, nodes []interface{}, totalCount int, filter BlockFilter, blockRange BlockRange, pagination PaginationParams, reverseOrder bool) map[string]interface{} {
	var hasNextPage, hasPreviousPage bool
	if reverseOrder {
		hasNextPage = blockRange.StartBlock > 0
		hasPreviousPage = pagination.Offset > 0
	} else {
		hasNextPage = blockRange.EndBlock < filter.NumberTo
		hasPreviousPage = pagination.Offset > 0
	}

	var startCursor, endCursor interface{}
	if len(blocks) > 0 {
		startCursor = fmt.Sprintf("%d", blocks[0].Number)
		endCursor = fmt.Sprintf("%d", blocks[len(blocks)-1].Number)
	}

	return buildConnectionResponse(ConnectionResponse{
		Nodes:           nodes,
		TotalCount:      totalCount,
		HasNextPage:     hasNextPage,
		HasPreviousPage: hasPreviousPage,
		StartCursor:     startCursor,
		EndCursor:       endCursor,
	})
}

// resolveBlocksRange resolves blocks in a specific range (optimized for frontend catch-up)
// Returns blocks from startNumber to endNumber (inclusive) with a maximum of 100 blocks
func (s *Schema) resolveBlocksRange(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse start and end block numbers
	startNumberStr, ok := p.Args["startNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid startNumber")
	}
	endNumberStr, ok := p.Args["endNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid endNumber")
	}

	startNumber, err := strconv.ParseUint(startNumberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid startNumber format: %w", err)
	}
	endNumber, err := strconv.ParseUint(endNumberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid endNumber format: %w", err)
	}

	// Validate range
	if startNumber > endNumber {
		return nil, fmt.Errorf("startNumber (%d) cannot be greater than endNumber (%d)", startNumber, endNumber)
	}

	// Limit range to 100 blocks for performance
	const maxRange = 100
	if endNumber-startNumber+1 > maxRange {
		endNumber = startNumber + maxRange - 1
		s.logger.Warn("blocksRange request limited to 100 blocks",
			zap.Uint64("requestedStart", startNumber),
			zap.Uint64("adjustedEnd", endNumber))
	}

	// Get latest height for sync status
	latestHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Adjust endNumber if it exceeds latest height
	if endNumber > latestHeight {
		endNumber = latestHeight
	}

	// Check if there are no blocks to return
	if startNumber > latestHeight {
		return map[string]interface{}{
			"blocks":       []interface{}{},
			"startNumber":  fmt.Sprintf("%d", startNumber),
			"endNumber":    fmt.Sprintf("%d", startNumber),
			"count":        0,
			"hasMore":      false,
			"latestHeight": fmt.Sprintf("%d", latestHeight),
		}, nil
	}

	// Parse optional flags
	includeTransactions := true // default to include
	if it, ok := p.Args["includeTransactions"].(bool); ok {
		includeTransactions = it
	}
	includeReceipts := false // default to not include for performance
	if ir, ok := p.Args["includeReceipts"].(bool); ok {
		includeReceipts = ir
	}

	// Fetch blocks in range
	blocks := make([]interface{}, 0, endNumber-startNumber+1)
	for blockNum := startNumber; blockNum <= endNumber; blockNum++ {
		block, err := s.models().GetModelBlock(ctx, blockNum)
		if err != nil {
			s.logger.Warn("failed to get block in range",
				zap.Uint64("blockNumber", blockNum),
				zap.Error(err))
			continue // Skip missing blocks
		}

		blockMap := s.blockToMap(block)

		// Optionally exclude transactions for lighter response
		if !includeTransactions {
			blockMap["transactions"] = []interface{}{}
		} else if includeReceipts {
			// If receipts are requested, enhance transactions with receipt data
			blockTs := fmt.Sprintf("%d", block.Time)
			enhancedTxs := make([]interface{}, 0, len(block.Transactions))
			for i, tx := range block.Transactions {
				txMap := s.transactionToMap(tx, &port.TxLocation{
					BlockHeight: blockNum,
					BlockHash:   block.Hash,
					TxIndex:     uint64(i),
				})
				txMap["blockTimestamp"] = blockTs
				// Get receipt for this transaction
				receipt, err := s.storage.GetReceipt(ctx, tx.Hash)
				if err == nil && receipt != nil {
					txMap["receipt"] = s.receiptToMap(receipt)
				}
				enhancedTxs = append(enhancedTxs, txMap)
			}
			blockMap["transactions"] = enhancedTxs
		}

		blocks = append(blocks, blockMap)
	}

	// Determine if there are more blocks available
	hasMore := endNumber < latestHeight

	return map[string]interface{}{
		"blocks":       blocks,
		"startNumber":  fmt.Sprintf("%d", startNumber),
		"endNumber":    fmt.Sprintf("%d", endNumber),
		"count":        len(blocks),
		"hasMore":      hasMore,
		"latestHeight": fmt.Sprintf("%d", latestHeight),
	}, nil
}

// resolveTransaction resolves a transaction by hash
func (s *Schema) resolveTransaction(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	hashStr, ok := p.Args["hash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid transaction hash")
	}

	hash := common.HexToHash(hashStr)
	tx, location, err := s.models().GetModelTransaction(ctx, hash)
	if err != nil {
		s.logger.Error("failed to get transaction",
			zap.String("hash", hashStr),
			zap.Error(err))
		return nil, err
	}

	result := s.transactionToMap(tx, location)
	if location != nil {
		if block, err := s.models().GetModelBlock(ctx, location.BlockHeight); err == nil && block != nil {
			result["blockTimestamp"] = fmt.Sprintf("%d", block.Time)
		}
	}

	// Include the stored receipt for status determination
	receipt, err := s.storage.GetReceipt(ctx, hash)
	if err == nil && receipt != nil {
		result["receipt"] = s.receiptToMap(receipt)
	}

	return result, nil
}

// resolveTransactions resolves transactions with filtering and pagination
func (s *Schema) resolveTransactions(p graphql.ResolveParams) (interface{}, error) {
	ctx := extractContext(p.Context)
	pagination := parsePaginationParams(p, 0)
	filter := parseTransactionFilter(p)

	// Get latest height
	latestHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return emptyConnection(false), nil
		}
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Without a block range, read only as many recent blocks as the page
	// needs instead of loading the whole chain.
	if filter.BlockNumberFrom == 0 && filter.BlockNumberTo == 0 {
		return s.recentTransactions(ctx, filter, pagination, latestHeight)
	}

	// Set default and validate block range
	blockFrom, blockTo := s.normalizeBlockRange(filter.BlockNumberFrom, filter.BlockNumberTo, latestHeight)
	if blockFrom > blockTo {
		return nil, fmt.Errorf("invalid block range: blockNumberFrom (%d) > blockNumberTo (%d)", blockFrom, blockTo)
	}

	// Fetch blocks and filter transactions
	blocks, err := s.models().GetModelBlocks(ctx, blockFrom, blockTo)
	if err != nil {
		s.logger.Error("failed to get blocks",
			zap.Uint64("blockNumberFrom", blockFrom),
			zap.Uint64("blockNumberTo", blockTo),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get blocks: %w", err)
	}

	filteredTxs := s.filterTransactionsFromBlocks(blocks, filter)
	reverseSlice(filteredTxs) // DESC order (newest first)

	totalCount := s.calculateTxTotalCount(ctx, filter, filteredTxs)
	paginatedTxs := applyPagination(filteredTxs, pagination.Offset, pagination.Limit)

	return s.buildTxConnectionResponse(paginatedTxs, totalCount, len(filteredTxs), pagination), nil
}

// normalizeBlockRange sets default block range and applies safety limits.
// For explicit ranges, it caps at maxRange to prevent excessive queries.
// For default queries (no range specified), it scans all blocks to ensure
// all transactions are discoverable regardless of which blocks contain them.
func (s *Schema) normalizeBlockRange(from, to, latestHeight uint64) (uint64, uint64) {
	if from == 0 && to == 0 {
		// Default: scan entire chain so all transactions are reachable
		to = latestHeight
		return from, to
	}

	if to == 0 {
		to = latestHeight
	}

	// Only apply maxRange limit when user specifies an explicit range
	const maxRange = uint64(10000)
	if to-from > maxRange {
		to = from + maxRange
	}

	return from, to
}

// filterTransactionsFromBlocks filters transactions from blocks based on filter criteria
func (s *Schema) filterTransactionsFromBlocks(blocks []*model.Block, filter TransactionFilter) []map[string]interface{} {
	var filteredTxs []map[string]interface{}

	for _, block := range blocks {
		if block == nil {
			continue
		}

		for i, tx := range block.Transactions {
			if !s.matchesTransactionFilter(tx, filter) {
				continue
			}

			location := &port.TxLocation{
				BlockHeight: block.Number,
				BlockHash:   block.Hash,
				TxIndex:     uint64(i),
			}
			txMap := s.transactionToMap(tx, location)
			txMap["blockTimestamp"] = fmt.Sprintf("%d", block.Time)
			filteredTxs = append(filteredTxs, txMap)
		}
	}

	return filteredTxs
}

// matchesTransactionFilter checks if a transaction matches the filter criteria
func (s *Schema) matchesTransactionFilter(tx *model.Transaction, filter TransactionFilter) bool {
	if filter.TxType != nil && int(tx.Type) != *filter.TxType {
		return false
	}
	if filter.From != nil && tx.From != *filter.From {
		return false
	}
	if filter.To != nil {
		if tx.To == nil || *tx.To != *filter.To {
			return false
		}
	}
	return true
}

// calculateTxTotalCount calculates total transaction count based on filters
func (s *Schema) calculateTxTotalCount(ctx context.Context, filter TransactionFilter, filteredTxs []map[string]interface{}) int {
	if filter.hasAddressFilter() {
		return len(filteredTxs)
	}

	if count, err := s.storage.GetTransactionCount(ctx); err == nil {
		return int(count)
	}
	return len(filteredTxs)
}

// buildTxConnectionResponse builds the GraphQL connection response for transactions
func (s *Schema) buildTxConnectionResponse(txs []map[string]interface{}, totalCount, totalFiltered int, pagination PaginationParams) map[string]interface{} {
	end := pagination.Offset + pagination.Limit
	if end > totalFiltered {
		end = totalFiltered
	}

	var startCursor, endCursor interface{}
	if len(txs) > 0 {
		if hash, ok := txs[0]["hash"].(string); ok {
			startCursor = hash
		}
		if hash, ok := txs[len(txs)-1]["hash"].(string); ok {
			endCursor = hash
		}
	}

	return buildConnectionResponse(ConnectionResponse{
		Nodes:           toInterfaceSlice(txs),
		TotalCount:      totalCount,
		HasNextPage:     end < totalFiltered,
		HasPreviousPage: pagination.Offset > 0,
		StartCursor:     startCursor,
		EndCursor:       endCursor,
	})
}

// toInterfaceSlice converts []map[string]interface{} to []interface{}
func toInterfaceSlice(maps []map[string]interface{}) []interface{} {
	result := make([]interface{}, len(maps))
	for i, m := range maps {
		result[i] = m
	}
	return result
}

// resolveTransactionsByAddress resolves transactions by address
func (s *Schema) resolveTransactionsByAddress(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)

	// Get pagination parameters with validation
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > 100 {
				limit = 100 // Maximum limit to prevent resource exhaustion
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Fetch transaction hashes from storage
	txHashes, err := s.storage.GetTransactionsByAddress(ctx, address, limit+1, offset)
	if err != nil {
		s.logger.Error("failed to get transactions by address",
			zap.String("address", addressStr),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get transactions by address: %w", err)
	}

	// Determine if there are more results
	hasMore := len(txHashes) > limit
	if hasMore {
		txHashes = txHashes[:limit]
	}

	// Convert transaction results to full transaction objects
	nodes := make([]interface{}, 0, len(txHashes))
	blockTimestamps := make(map[uint64]string) // cache block timestamps
	for _, txHash := range txHashes {
		tx, location, err := s.models().GetModelTransaction(ctx, txHash)
		if err != nil {
			if !errors.Is(err, port.ErrNotFound) {
				s.logger.Error("failed to get transaction",
					zap.String("txHash", txHash.Hex()),
					zap.String("address", addressStr),
					zap.Error(err))
			} else {
				s.logger.Warn("transaction not found",
					zap.String("txHash", txHash.Hex()),
					zap.String("address", addressStr))
			}
			continue
		}

		txMap := s.transactionToMap(tx, location)
		if location != nil {
			if ts, ok := blockTimestamps[location.BlockHeight]; ok {
				txMap["blockTimestamp"] = ts
			} else {
				block, blockErr := s.models().GetModelBlock(ctx, location.BlockHeight)
				if blockErr == nil && block != nil {
					ts = fmt.Sprintf("%d", block.Time)
					blockTimestamps[location.BlockHeight] = ts
					txMap["blockTimestamp"] = ts
				}
			}
		}
		nodes = append(nodes, txMap)
	}

	// Calculate pagination info
	totalCount := len(nodes)
	hasNextPage := hasMore
	hasPreviousPage := offset > 0

	var startCursor, endCursor interface{}
	if len(nodes) > 0 {
		if txMap, ok := nodes[0].(map[string]interface{}); ok {
			if hash, ok := txMap["hash"].(string); ok {
				startCursor = hash
			}
		}
		if txMap, ok := nodes[len(nodes)-1].(map[string]interface{}); ok {
			if hash, ok := txMap["hash"].(string); ok {
				endCursor = hash
			}
		}
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     hasNextPage,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     startCursor,
			"endCursor":       endCursor,
		},
	}, nil
}

// resolveReceipt resolves a receipt by transaction hash. Stored receipts
// are complete (storage schema v2), so they are returned as stored.
func (s *Schema) resolveReceipt(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	hashStr, ok := p.Args["transactionHash"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid transaction hash")
	}

	hash := common.HexToHash(hashStr)
	receipt, err := s.storage.GetReceipt(ctx, hash)
	if err != nil {
		s.logger.Error("failed to get receipt",
			zap.String("hash", hashStr),
			zap.Error(err))
		return nil, err
	}

	return s.receiptToMap(receipt), nil
}

// resolveReceiptsByBlock resolves receipts by block number
func (s *Schema) resolveReceiptsByBlock(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context
	numberStr, ok := p.Args["blockNumber"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid block number")
	}

	number, err := strconv.ParseUint(numberStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid block number format: %w", err)
	}

	receipts, err := s.storage.GetReceiptsByBlockNumber(ctx, number)
	if err != nil {
		s.logger.Error("failed to get receipts by block",
			zap.Uint64("number", number),
			zap.Error(err))
		return nil, err
	}

	result := make([]interface{}, len(receipts))
	for i, receipt := range receipts {
		result[i] = s.receiptToMap(receipt)
	}

	return result, nil
}

// resolveLogs resolves logs with filtering and pagination
func (s *Schema) resolveLogs(p graphql.ResolveParams) (interface{}, error) {
	ctx := extractContext(p.Context)
	decode := s.getDecodeParam(p)
	pagination := parsePaginationParams(p, 100)

	filter, err := parseLogFilter(p)
	if err != nil {
		return nil, err
	}

	// Get latest height
	latestHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return emptyConnection(false), nil
		}
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Without a block range, read only as many blocks as the page needs.
	if filter.BlockNumberFrom == 0 && filter.BlockNumberTo == 0 {
		return s.earliestLogs(ctx, filter, pagination, latestHeight, decode)
	}

	// Set default and validate block range
	blockFrom, blockTo := s.normalizeBlockRange(filter.BlockNumberFrom, filter.BlockNumberTo, latestHeight)
	if blockFrom > blockTo {
		return nil, fmt.Errorf("invalid block range: blockNumberFrom (%d) > blockNumberTo (%d)", blockFrom, blockTo)
	}

	// Collect and filter logs
	filteredLogs := s.collectLogsFromBlockRange(ctx, blockFrom, blockTo, filter, decode)

	totalCount := len(filteredLogs)
	paginatedLogs := applyPagination(filteredLogs, pagination.Offset, pagination.Limit)

	return s.buildLogConnectionResponse(paginatedLogs, totalCount, pagination), nil
}

// getDecodeParam extracts the decode parameter from GraphQL args
func (s *Schema) getDecodeParam(p graphql.ResolveParams) bool {
	if d, ok := p.Args["decode"].(bool); ok {
		return d
	}
	return false
}

// collectLogsFromBlockRange collects logs from receipts in the block range
func (s *Schema) collectLogsFromBlockRange(ctx context.Context, blockFrom, blockTo uint64, filter LogFilter, decode bool) []map[string]interface{} {
	var filteredLogs []map[string]interface{}

	for blockNum := blockFrom; blockNum <= blockTo; blockNum++ {
		receipts, err := s.storage.GetReceiptsByBlockNumber(ctx, blockNum)
		if err != nil {
			if !errors.Is(err, port.ErrNotFound) {
				s.logger.Error("failed to get receipts for block",
					zap.Uint64("blockNumber", blockNum),
					zap.Error(err))
			}
			continue
		}

		for _, receipt := range receipts {
			if receipt == nil {
				continue
			}

			for _, log := range receipt.Logs {
				if filter.matchesLog(log) {
					filteredLogs = append(filteredLogs, s.logToMapWithDecode(log, decode))
				}
			}
		}
	}

	return filteredLogs
}

// buildLogConnectionResponse builds the GraphQL connection response for logs
func (s *Schema) buildLogConnectionResponse(logs []map[string]interface{}, totalCount int, pagination PaginationParams) map[string]interface{} {
	end := pagination.Offset + pagination.Limit
	if end > totalCount {
		end = totalCount
	}

	var startCursor, endCursor interface{}
	if len(logs) > 0 {
		startCursor = s.buildLogCursor(logs[0])
		endCursor = s.buildLogCursor(logs[len(logs)-1])
	}

	return buildConnectionResponse(ConnectionResponse{
		Nodes:           toInterfaceSlice(logs),
		TotalCount:      totalCount,
		HasNextPage:     end < totalCount,
		HasPreviousPage: pagination.Offset > 0,
		StartCursor:     startCursor,
		EndCursor:       endCursor,
	})
}

// buildLogCursor builds a cursor string for a log entry
func (s *Schema) buildLogCursor(log map[string]interface{}) interface{} {
	if txHash, ok := log["transactionHash"].(string); ok {
		if logIndex, ok := log["logIndex"].(int); ok {
			return fmt.Sprintf("%s:%d", txHash, logIndex)
		}
	}
	return nil
}

// models reads blocks and transactions as the chain-neutral model, so hashes,
// transaction types and senders are the ones the chain reports.
func (s *Schema) models() port.ModelReader {
	return storage.AsModelReader(s.storage)
}

// modelBlockOf returns the stored model of a block another reader returned
// as a go-ethereum block, falling back to converting it.
func (s *Schema) modelBlockOf(ctx context.Context, b *types.Block) *model.Block {
	if b == nil {
		return nil
	}
	if m, err := s.models().GetModelBlock(ctx, b.NumberU64()); err == nil {
		return m
	}
	m, _ := gethconv.BlockFromGeth(b)
	return m
}

// modelTxAt returns the stored model of a transaction another reader
// returned as a go-ethereum transaction. It is looked up by position: the
// go-ethereum hash of a chain-specific type differs from the chain's.
func (s *Schema) modelTxAt(ctx context.Context, tx *types.Transaction, loc *port.TxLocation) *model.Transaction {
	if loc != nil {
		if b, err := s.models().GetModelBlock(ctx, loc.BlockHeight); err == nil && int(loc.TxIndex) < len(b.Transactions) {
			return b.Transactions[loc.TxIndex]
		}
	}
	if tx == nil {
		return nil
	}
	m, _ := gethconv.TxFromGeth(tx)
	return m
}

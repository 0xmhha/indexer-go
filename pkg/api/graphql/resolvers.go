package graphql

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
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
		if errors.Is(err, storage.ErrNotFound) {
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

	block, err := s.storage.GetBlock(ctx, number)
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
	block, err := s.storage.GetBlockByHash(ctx, hash)
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
		if errors.Is(err, storage.ErrNotFound) {
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
	blocks, err := s.storage.GetBlocks(ctx, blockRange.StartBlock, blockRange.EndBlock)
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
func (s *Schema) blocksToNodes(blocks []*types.Block) []interface{} {
	nodes := make([]interface{}, len(blocks))
	for i, block := range blocks {
		nodes[i] = s.blockToMap(block)
	}
	return nodes
}

// calculateBlockTotalCount calculates total count based on filters
func (s *Schema) calculateBlockTotalCount(ctx context.Context, filter BlockFilter, filteredBlocks []*types.Block, latestHeight uint64) int {
	if filter.hasTimestampFilter() || filter.hasMinerFilter() {
		return len(filteredBlocks)
	}

	if histStorage, ok := s.storage.(storage.HistoricalReader); ok {
		if count, err := histStorage.GetBlockCount(ctx); err == nil {
			return int(count)
		}
	}
	return int(latestHeight + 1)
}

// buildBlockConnectionResponse builds the GraphQL connection response for blocks
// reverseOrder indicates default (no filter) pagination where latest blocks come first
func (s *Schema) buildBlockConnectionResponse(blocks []*types.Block, nodes []interface{}, totalCount int, filter BlockFilter, blockRange BlockRange, pagination PaginationParams, reverseOrder bool) map[string]interface{} {
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
		startCursor = fmt.Sprintf("%d", blocks[0].NumberU64())
		endCursor = fmt.Sprintf("%d", blocks[len(blocks)-1].NumberU64())
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
		block, err := s.storage.GetBlock(ctx, blockNum)
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
			txs := block.Transactions()
			blockTs := fmt.Sprintf("%d", block.Header().Time)
			enhancedTxs := make([]interface{}, 0, len(txs))
			for i, tx := range txs {
				txMap := s.transactionToMap(tx, &storage.TxLocation{
					BlockHeight: blockNum,
					TxIndex:     uint64(i),
				})
				txMap["blockTimestamp"] = blockTs
				// Get receipt for this transaction
				receipt, err := s.storage.GetReceipt(ctx, tx.Hash())
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
	tx, location, err := s.storage.GetTransaction(ctx, hash)
	if err != nil {
		s.logger.Error("failed to get transaction",
			zap.String("hash", hashStr),
			zap.Error(err))
		return nil, err
	}

	result := s.transactionToMap(tx, location)

	// Fetch and include receipt data for status determination
	receipt, err := s.storage.GetReceipt(ctx, hash)
	if err == nil && receipt != nil {
		// Derive missing receipt fields from block and transaction context
		if location != nil {
			block, blockErr := s.storage.GetBlock(ctx, location.BlockHeight)
			if blockErr == nil && block != nil {
				result["blockTimestamp"] = fmt.Sprintf("%d", block.Header().Time)
				// Set receipt block info
				receipt.BlockNumber = big.NewInt(int64(location.BlockHeight))
				receipt.BlockHash = location.BlockHash
				receipt.TransactionIndex = uint(location.TxIndex)

				// Calculate GasUsed
				if location.TxIndex == 0 {
					receipt.GasUsed = receipt.CumulativeGasUsed
				} else {
					txs := block.Transactions()
					if int(location.TxIndex) > 0 && int(location.TxIndex) <= len(txs) {
						prevTxHash := txs[location.TxIndex-1].Hash()
						prevReceipt, prevErr := s.storage.GetReceipt(ctx, prevTxHash)
						if prevErr == nil && prevReceipt != nil {
							receipt.GasUsed = receipt.CumulativeGasUsed - prevReceipt.CumulativeGasUsed
						}
					}
				}

				// Calculate effective gas price
				baseFee := block.BaseFee()
				if baseFee != nil && tx.Type() == types.DynamicFeeTxType {
					tipCap := tx.GasTipCap()
					feeCap := tx.GasFeeCap()
					if tipCap != nil && feeCap != nil {
						effectiveGasPrice := new(big.Int).Add(baseFee, tipCap)
						if effectiveGasPrice.Cmp(feeCap) > 0 {
							effectiveGasPrice = feeCap
						}
						receipt.EffectiveGasPrice = effectiveGasPrice
					}
				} else if tx.GasPrice() != nil {
					receipt.EffectiveGasPrice = tx.GasPrice()
				}
			}
		}
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
		if errors.Is(err, storage.ErrNotFound) {
			return emptyConnection(false), nil
		}
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Set default and validate block range
	blockFrom, blockTo := s.normalizeBlockRange(filter.BlockNumberFrom, filter.BlockNumberTo, latestHeight)
	if blockFrom > blockTo {
		return nil, fmt.Errorf("invalid block range: blockNumberFrom (%d) > blockNumberTo (%d)", blockFrom, blockTo)
	}

	// Fetch blocks and filter transactions
	blocks, err := s.storage.GetBlocks(ctx, blockFrom, blockTo)
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

// normalizeBlockRange sets default block range and limits to prevent excessive queries
func (s *Schema) normalizeBlockRange(from, to, latestHeight uint64) (uint64, uint64) {
	if from == 0 && to == 0 {
		to = latestHeight
	} else if to == 0 {
		to = latestHeight
	}

	// Limit range to prevent excessive queries
	const maxRange = uint64(1000)
	if to-from > maxRange {
		to = from + maxRange
	}

	return from, to
}

// filterTransactionsFromBlocks filters transactions from blocks based on filter criteria
func (s *Schema) filterTransactionsFromBlocks(blocks []*types.Block, filter TransactionFilter) []map[string]interface{} {
	var filteredTxs []map[string]interface{}

	for _, block := range blocks {
		if block == nil {
			continue
		}

		for i, tx := range block.Transactions() {
			if !s.matchesTransactionFilter(tx, filter) {
				continue
			}

			location := &storage.TxLocation{
				BlockHeight: block.NumberU64(),
				BlockHash:   block.Hash(),
				TxIndex:     uint64(i),
			}
			txMap := s.transactionToMap(tx, location)
			txMap["blockTimestamp"] = fmt.Sprintf("%d", block.Header().Time)
			filteredTxs = append(filteredTxs, txMap)
		}
	}

	return filteredTxs
}

// matchesTransactionFilter checks if a transaction matches the filter criteria
func (s *Schema) matchesTransactionFilter(tx *types.Transaction, filter TransactionFilter) bool {
	if filter.TxType != nil && int(tx.Type()) != *filter.TxType {
		return false
	}

	// Get signer and from address
	var signer types.Signer
	if tx.ChainId() != nil {
		signer = types.LatestSignerForChainID(tx.ChainId())
	} else {
		signer = types.HomesteadSigner{}
	}

	from, err := types.Sender(signer, tx)
	if err != nil {
		s.logger.Warn("failed to get transaction sender",
			zap.String("txHash", tx.Hash().Hex()),
			zap.Error(err))
		return false
	}

	if filter.From != nil && from != *filter.From {
		return false
	}

	if filter.To != nil {
		txTo := tx.To()
		if txTo == nil || *txTo != *filter.To {
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

	if histStorage, ok := s.storage.(storage.HistoricalReader); ok {
		if count, err := histStorage.GetTransactionCount(ctx); err == nil {
			return int(count)
		}
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

	// Batch fetch all transactions
	txs, locs, batchErr := s.storage.GetTransactions(ctx, txHashes)
	if batchErr != nil && !errors.Is(batchErr, storage.ErrNotFound) {
		s.logger.Error("failed to batch get transactions",
			zap.String("address", addressStr),
			zap.Error(batchErr))
	}

	// Convert transaction results to full transaction objects
	nodes := make([]interface{}, 0, len(txHashes))
	blockTimestamps := make(map[uint64]string) // cache block timestamps
	for i, tx := range txs {
		if tx == nil {
			if batchErr != nil {
				s.logger.Warn("transaction not found in batch",
					zap.String("txHash", txHashes[i].Hex()),
					zap.String("address", addressStr))
			}
			continue
		}
		location := locs[i]

		txMap := s.transactionToMap(tx, location)
		if location != nil {
			if ts, ok := blockTimestamps[location.BlockHeight]; ok {
				txMap["blockTimestamp"] = ts
			} else {
				block, blockErr := s.storage.GetBlock(ctx, location.BlockHeight)
				if blockErr == nil && block != nil {
					ts = fmt.Sprintf("%d", block.Header().Time)
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

// resolveReceipt resolves a receipt by transaction hash
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

	// Get transaction to find block info for deriving missing receipt fields
	tx, location, err := s.storage.GetTransaction(ctx, hash)
	if err != nil {
		s.logger.Warn("failed to get transaction for receipt context",
			zap.String("hash", hashStr),
			zap.Error(err))
		// Still return basic receipt data
		return s.receiptToMap(receipt), nil
	}

	// Get block to derive additional fields
	var baseFee *big.Int
	if location != nil {
		block, err := s.storage.GetBlock(ctx, location.BlockHeight)
		if err == nil && block != nil {
			baseFee = block.BaseFee()
			// Set receipt block info from location
			receipt.BlockNumber = big.NewInt(int64(location.BlockHeight))
			receipt.BlockHash = location.BlockHash
			receipt.TransactionIndex = uint(location.TxIndex)

			// Calculate GasUsed from CumulativeGasUsed
			// GasUsed = current.CumulativeGasUsed - previous.CumulativeGasUsed
			if location.TxIndex == 0 {
				// First transaction in block: gasUsed = cumulativeGasUsed
				receipt.GasUsed = receipt.CumulativeGasUsed
			} else {
				// Get previous transaction's receipt to calculate gas used
				txs := block.Transactions()
				if int(location.TxIndex) > 0 && int(location.TxIndex) <= len(txs) {
					prevTxHash := txs[location.TxIndex-1].Hash()
					prevReceipt, err := s.storage.GetReceipt(ctx, prevTxHash)
					if err == nil && prevReceipt != nil {
						receipt.GasUsed = receipt.CumulativeGasUsed - prevReceipt.CumulativeGasUsed
					}
				}
			}
		}
	}

	// Calculate effective gas price
	if tx != nil {
		if baseFee != nil && tx.Type() == types.DynamicFeeTxType {
			// EIP-1559: effectiveGasPrice = min(baseFee + tipCap, feeCap)
			tipCap := tx.GasTipCap()
			feeCap := tx.GasFeeCap()
			if tipCap != nil && feeCap != nil {
				effectiveGasPrice := new(big.Int).Add(baseFee, tipCap)
				if effectiveGasPrice.Cmp(feeCap) > 0 {
					effectiveGasPrice = feeCap
				}
				receipt.EffectiveGasPrice = effectiveGasPrice
			}
		} else if tx.GasPrice() != nil {
			// Legacy/AccessList tx: effectiveGasPrice = gasPrice
			receipt.EffectiveGasPrice = tx.GasPrice()
		}
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

	// Get block for deriving receipt fields
	block, err := s.storage.GetBlock(ctx, number)
	if err != nil {
		s.logger.Error("failed to get block",
			zap.Uint64("number", number),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	receipts, err := s.storage.GetReceiptsByBlockNumber(ctx, number)
	if err != nil {
		s.logger.Error("failed to get receipts by block",
			zap.Uint64("number", number),
			zap.Error(err))
		return nil, err
	}

	// Derive missing fields for each receipt
	baseFee := block.BaseFee()
	txs := block.Transactions()

	result := make([]interface{}, len(receipts))
	for i, receipt := range receipts {
		// Set block info
		receipt.BlockNumber = big.NewInt(int64(number))
		receipt.BlockHash = block.Hash()

		// Calculate GasUsed
		if i == 0 {
			receipt.GasUsed = receipt.CumulativeGasUsed
		} else if i > 0 && receipts[i-1] != nil {
			receipt.GasUsed = receipt.CumulativeGasUsed - receipts[i-1].CumulativeGasUsed
		}

		// Calculate effective gas price from transaction
		if i < len(txs) {
			tx := txs[i]
			if baseFee != nil && tx.Type() == types.DynamicFeeTxType {
				tipCap := tx.GasTipCap()
				feeCap := tx.GasFeeCap()
				if tipCap != nil && feeCap != nil {
					effectiveGasPrice := new(big.Int).Add(baseFee, tipCap)
					if effectiveGasPrice.Cmp(feeCap) > 0 {
						effectiveGasPrice = feeCap
					}
					receipt.EffectiveGasPrice = effectiveGasPrice
				}
			} else if tx.GasPrice() != nil {
				receipt.EffectiveGasPrice = tx.GasPrice()
			}
		}

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
		if errors.Is(err, storage.ErrNotFound) {
			return emptyConnection(false), nil
		}
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
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
			if !errors.Is(err, storage.ErrNotFound) {
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

// ========== System Contract Resolvers ==========

// resolveTotalSupply resolves the current total supply
func (s *Schema) resolveTotalSupply(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to SystemContractReader
	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	supply, err := reader.GetTotalSupply(ctx)
	if err != nil {
		s.logger.Error("failed to get total supply", zap.Error(err))
		return nil, err
	}

	return supply.String(), nil
}

// resolveActiveMinters resolves the list of active minters
func (s *Schema) resolveActiveMinters(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	minters, err := reader.GetActiveMinters(ctx)
	if err != nil {
		s.logger.Error("failed to get active minters", zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, minter := range minters {
		allowance, err := reader.GetMinterAllowance(ctx, minter)
		if err != nil {
			s.logger.Warn("failed to get minter allowance", zap.String("minter", minter.Hex()), zap.Error(err))
			continue
		}

		result = append(result, map[string]interface{}{
			"address":   minter.Hex(),
			"allowance": allowance.String(),
			"isActive":  true,
		})
	}

	return result, nil
}

// resolveActiveMinterAddresses resolves the list of active minter addresses only
func (s *Schema) resolveActiveMinterAddresses(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	minters, err := reader.GetActiveMinters(ctx)
	if err != nil {
		s.logger.Error("failed to get active minter addresses", zap.Error(err))
		return nil, err
	}

	// Convert to hex string addresses
	var result []string
	for _, minter := range minters {
		result = append(result, minter.Hex())
	}

	return result, nil
}

// resolveMinterAllowance resolves the allowance for a specific minter
func (s *Schema) resolveMinterAllowance(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	minterStr, ok := p.Args["minter"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid minter address")
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	minter := common.HexToAddress(minterStr)
	allowance, err := reader.GetMinterAllowance(ctx, minter)
	if err != nil {
		s.logger.Error("failed to get minter allowance",
			zap.String("minter", minterStr),
			zap.Error(err))
		return nil, err
	}

	return allowance.String(), nil
}

// resolveActiveValidators resolves the list of active validators
func (s *Schema) resolveActiveValidators(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	validators, err := reader.GetActiveValidators(ctx)
	if err != nil {
		s.logger.Error("failed to get active validators", zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, validator := range validators {
		result = append(result, map[string]interface{}{
			"address":  validator.Hex(),
			"isActive": true,
		})
	}

	return result, nil
}

// resolveActiveValidatorAddresses resolves the list of active validator addresses only
func (s *Schema) resolveActiveValidatorAddresses(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	validators, err := reader.GetActiveValidators(ctx)
	if err != nil {
		s.logger.Error("failed to get active validator addresses", zap.Error(err))
		return nil, err
	}

	// Convert to hex string addresses
	var result []string
	for _, validator := range validators {
		result = append(result, validator.Hex())
	}

	return result, nil
}

// resolveBlacklistedAddresses resolves the list of blacklisted addresses
func (s *Schema) resolveBlacklistedAddresses(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	addresses, err := reader.GetBlacklistedAddresses(ctx)
	if err != nil {
		s.logger.Error("failed to get blacklisted addresses", zap.Error(err))
		return nil, err
	}

	var result []string
	for _, addr := range addresses {
		result = append(result, addr.Hex())
	}

	return result, nil
}

// resolveProposals resolves governance proposals with filtering
func (s *Schema) resolveProposals(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse filter (optional - nil means no filter)
	filter, _ := p.Args["filter"].(map[string]interface{})
	if filter == nil {
		filter = map[string]interface{}{}
	}

	// Parse contract (optional - if not provided, queries all contracts)
	contract := common.Address{} // Zero address queries all contracts
	contractStr := ""
	if contractVal, ok := filter["contract"].(string); ok {
		contractStr = contractVal
		contract = common.HexToAddress(contractStr)
	}

	// Parse status (optional)
	status := storage.ProposalStatusNone
	if statusStr, ok := filter["status"].(string); ok {
		status = parseProposalStatus(statusStr)
	}

	// Parse proposer (optional) - will be filtered client-side
	var proposer common.Address
	var hasProposerFilter bool
	if proposerStr, ok := filter["proposer"].(string); ok && proposerStr != "" {
		proposer = common.HexToAddress(proposerStr)
		hasProposerFilter = true
	}

	// Get pagination parameters
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	proposals, err := reader.GetProposals(ctx, contract, status, limit, offset)
	if err != nil {
		logContract := contractStr
		if logContract == "" {
			logContract = "all"
		}
		s.logger.Error("failed to get proposals",
			zap.String("contract", logContract),
			zap.Error(err))
		return nil, err
	}

	var nodes []map[string]interface{}
	for _, proposal := range proposals {
		// Apply proposer filter if specified
		if hasProposerFilter && proposal.Proposer != proposer {
			continue
		}
		nodes = append(nodes, s.proposalToMap(proposal))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) >= limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveProposal resolves a specific proposal by ID
func (s *Schema) resolveProposal(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid contract address")
	}

	proposalIdStr, ok := p.Args["proposalId"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid proposal ID")
	}

	contract := common.HexToAddress(contractStr)
	proposalId, success := new(big.Int).SetString(proposalIdStr, 10)
	if !success {
		return nil, fmt.Errorf("invalid proposal ID format")
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	proposal, err := reader.GetProposalById(ctx, contract, proposalId)
	if err != nil {
		s.logger.Error("failed to get proposal",
			zap.String("contract", contractStr),
			zap.String("proposalId", proposalIdStr),
			zap.Error(err))
		return nil, err
	}

	if proposal == nil {
		return nil, nil
	}

	return s.proposalToMap(proposal), nil
}

// resolveProposalVotes resolves votes for a specific proposal
func (s *Schema) resolveProposalVotes(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid contract address")
	}

	proposalIdStr, ok := p.Args["proposalId"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid proposal ID")
	}

	contract := common.HexToAddress(contractStr)
	proposalId, success := new(big.Int).SetString(proposalIdStr, 10)
	if !success {
		return nil, fmt.Errorf("invalid proposal ID format")
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	votes, err := reader.GetProposalVotes(ctx, contract, proposalId)
	if err != nil {
		s.logger.Error("failed to get proposal votes",
			zap.String("contract", contractStr),
			zap.String("proposalId", proposalIdStr),
			zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, vote := range votes {
		result = append(result, s.proposalVoteToMap(vote))
	}

	return result, nil
}

// Helper function to convert Proposal to map
func (s *Schema) proposalToMap(proposal *storage.Proposal) map[string]interface{} {
	m := map[string]interface{}{
		"contract":          proposal.Contract.Hex(),
		"proposalId":        proposal.ProposalID.String(),
		"proposer":          proposal.Proposer.Hex(),
		"actionType":        common.Bytes2Hex(proposal.ActionType[:]),
		"callData":          common.Bytes2Hex(proposal.CallData),
		"memberVersion":     proposal.MemberVersion.String(),
		"requiredApprovals": int(proposal.RequiredApprovals),
		"approved":          int(proposal.Approved),
		"rejected":          int(proposal.Rejected),
		"status":            proposalStatusToString(proposal.Status),
		"createdAt":         fmt.Sprintf("%d", proposal.CreatedAt),
		"blockNumber":       fmt.Sprintf("%d", proposal.BlockNumber),
		"transactionHash":   proposal.TxHash.Hex(),
	}

	if proposal.ExecutedAt != nil {
		m["executedAt"] = fmt.Sprintf("%d", *proposal.ExecutedAt)
	} else {
		m["executedAt"] = nil
	}

	return m
}

// Helper function to convert ProposalVote to map
func (s *Schema) proposalVoteToMap(vote *storage.ProposalVote) map[string]interface{} {
	return map[string]interface{}{
		"contract":        vote.Contract.Hex(),
		"proposalId":      vote.ProposalID.String(),
		"voter":           vote.Voter.Hex(),
		"approval":        vote.Approval,
		"blockNumber":     fmt.Sprintf("%d", vote.BlockNumber),
		"transactionHash": vote.TxHash.Hex(),
		"timestamp":       fmt.Sprintf("%d", vote.Timestamp),
	}
}

// Helper function to parse ProposalStatus from string
func parseProposalStatus(statusStr string) storage.ProposalStatus {
	switch statusStr {
	case "NONE":
		return storage.ProposalStatusNone
	case "VOTING":
		return storage.ProposalStatusVoting
	case "APPROVED":
		return storage.ProposalStatusApproved
	case "EXECUTED":
		return storage.ProposalStatusExecuted
	case "CANCELLED":
		return storage.ProposalStatusCancelled
	case "EXPIRED":
		return storage.ProposalStatusExpired
	case "FAILED":
		return storage.ProposalStatusFailed
	case "REJECTED":
		return storage.ProposalStatusRejected
	default:
		return storage.ProposalStatusNone
	}
}

// Helper function to convert ProposalStatus to string
func proposalStatusToString(status storage.ProposalStatus) string {
	switch status {
	case storage.ProposalStatusNone:
		return "NONE"
	case storage.ProposalStatusVoting:
		return "VOTING"
	case storage.ProposalStatusApproved:
		return "APPROVED"
	case storage.ProposalStatusExecuted:
		return "EXECUTED"
	case storage.ProposalStatusCancelled:
		return "CANCELLED"
	case storage.ProposalStatusExpired:
		return "EXPIRED"
	case storage.ProposalStatusFailed:
		return "FAILED"
	case storage.ProposalStatusRejected:
		return "REJECTED"
	default:
		return "NONE"
	}
}

// resolveMintEvents resolves mint events with filtering and pagination
func (s *Schema) resolveMintEvents(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	// Parse optional minter address (support both 'minter' and 'address' fields)
	var minter common.Address
	if minterStr, ok := filter["minter"].(string); ok && minterStr != "" {
		minter = common.HexToAddress(minterStr)
	} else if addressStr, ok := filter["address"].(string); ok && addressStr != "" {
		// Support 'address' as alias for 'minter'
		minter = common.HexToAddress(addressStr)
	}

	// Pagination
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > 100 {
				limit = 100
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMintEvents(ctx, fromBlock, toBlock, minter, limit, offset)
	if err != nil {
		s.logger.Error("failed to get mint events", zap.Error(err))
		return nil, err
	}

	var nodes []map[string]interface{}
	for _, event := range events {
		nodes = append(nodes, s.mintEventToMap(event))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) >= limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveBurnEvents resolves burn events with filtering and pagination
func (s *Schema) resolveBurnEvents(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	// Parse optional burner address (support both 'burner' and 'address' fields)
	var burner common.Address
	if burnerStr, ok := filter["burner"].(string); ok && burnerStr != "" {
		burner = common.HexToAddress(burnerStr)
	} else if addressStr, ok := filter["address"].(string); ok && addressStr != "" {
		// Support 'address' as alias for 'burner'
		burner = common.HexToAddress(addressStr)
	}

	// Pagination
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > 100 {
				limit = 100
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetBurnEvents(ctx, fromBlock, toBlock, burner, limit, offset)
	if err != nil {
		s.logger.Error("failed to get burn events", zap.Error(err))
		return nil, err
	}

	var nodes []map[string]interface{}
	for _, event := range events {
		nodes = append(nodes, s.burnEventToMap(event))
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(nodes),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(nodes) >= limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveMinterHistory resolves minter configuration history
func (s *Schema) resolveMinterHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	minterStr, ok := p.Args["minter"].(string)
	if !ok {
		return nil, fmt.Errorf("minter address is required")
	}
	minter := common.HexToAddress(minterStr)

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMinterHistory(ctx, minter)
	if err != nil {
		s.logger.Error("failed to get minter history", zap.String("minter", minterStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.minterConfigEventToMap(event))
	}

	return result, nil
}

// resolveValidatorHistory resolves validator change history
func (s *Schema) resolveValidatorHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	validatorStr, ok := p.Args["validator"].(string)
	if !ok {
		return nil, fmt.Errorf("validator address is required")
	}
	validator := common.HexToAddress(validatorStr)

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetValidatorHistory(ctx, validator)
	if err != nil {
		s.logger.Error("failed to get validator history", zap.String("validator", validatorStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.validatorChangeEventToMap(event))
	}

	return result, nil
}

// resolveGasTipHistory resolves gas tip update history
func (s *Schema) resolveGasTipHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetGasTipHistory(ctx, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get gas tip history", zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.gasTipUpdateEventToMap(event))
	}

	return result, nil
}

// resolveBlacklistHistory resolves blacklist change history
func (s *Schema) resolveBlacklistHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("address is required")
	}
	address := common.HexToAddress(addressStr)

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetBlacklistHistory(ctx, address)
	if err != nil {
		s.logger.Error("failed to get blacklist history", zap.String("address", addressStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.blacklistEventToMap(event))
	}

	return result, nil
}

// resolveMemberHistory resolves member change history
func (s *Schema) resolveMemberHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("contract address is required")
	}
	contract := common.HexToAddress(contractStr)

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMemberHistory(ctx, contract)
	if err != nil {
		s.logger.Error("failed to get member history", zap.String("contract", contractStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.memberChangeEventToMap(event))
	}

	return result, nil
}

// resolveEmergencyPauseHistory resolves emergency pause history
func (s *Schema) resolveEmergencyPauseHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("contract address is required")
	}
	contract := common.HexToAddress(contractStr)

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetEmergencyPauseHistory(ctx, contract)
	if err != nil {
		s.logger.Error("failed to get emergency pause history", zap.String("contract", contractStr), zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.emergencyPauseEventToMap(event))
	}

	return result, nil
}

// resolveDepositMintProposals resolves deposit mint proposals
func (s *Schema) resolveDepositMintProposals(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	// Parse optional status filter
	status := storage.ProposalStatusNone
	if statusStr, ok := filter["status"].(string); ok && statusStr != "" {
		status = parseProposalStatus(statusStr)
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	proposals, err := reader.GetDepositMintProposals(ctx, fromBlock, toBlock, status)
	if err != nil {
		s.logger.Error("failed to get deposit mint proposals", zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, proposal := range proposals {
		result = append(result, s.depositMintProposalToMap(proposal))
	}

	return result, nil
}

// Phase 2.3: Add missing system contract query resolvers

// resolveMinterConfigHistory resolves minter configuration change history across all minters
func (s *Schema) resolveMinterConfigHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	// Parse block range
	var fromBlock, toBlock uint64
	if fb, ok := filter["fromBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(fb, 10, 64)
		fromBlock = parsed
	}
	if tb, ok := filter["toBlock"].(string); ok {
		parsed, _ := strconv.ParseUint(tb, 10, 64)
		toBlock = parsed
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMinterConfigHistory(ctx, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get minter config history",
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, s.minterConfigEventToMap(event))
	}

	return result, nil
}

// resolveAuthorizedAccounts resolves list of authorized accounts from GovCouncil
func (s *Schema) resolveAuthorizedAccounts(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	accounts, err := reader.GetAuthorizedAccounts(ctx)
	if err != nil {
		s.logger.Error("failed to get authorized accounts", zap.Error(err))
		return nil, err
	}

	// Convert addresses to hex strings
	var result []string
	for _, account := range accounts {
		result = append(result, account.Hex())
	}

	return result, nil
}

// Helper function to convert MintEvent to map
func (s *Schema) mintEventToMap(event *storage.MintEvent) map[string]interface{} {
	return map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"minter":          event.Minter.Hex(),
		"to":              event.To.Hex(),
		"amount":          event.Amount.String(),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert BurnEvent to map
func (s *Schema) burnEventToMap(event *storage.BurnEvent) map[string]interface{} {
	m := map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"burner":          event.Burner.Hex(),
		"amount":          event.Amount.String(),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
	if event.WithdrawalID != "" {
		m["withdrawalId"] = event.WithdrawalID
	}
	return m
}

// Helper function to convert MinterConfigEvent to map
func (s *Schema) minterConfigEventToMap(event *storage.MinterConfigEvent) map[string]interface{} {
	return map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"minter":          event.Minter.Hex(),
		"allowance":       event.Allowance.String(),
		"action":          event.Action,
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert ValidatorChangeEvent to map
func (s *Schema) validatorChangeEventToMap(event *storage.ValidatorChangeEvent) map[string]interface{} {
	m := map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"validator":       event.Validator.Hex(),
		"action":          event.Action,
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
	if event.OldValidator != nil {
		m["oldValidator"] = event.OldValidator.Hex()
	}
	return m
}

// Helper function to convert GasTipUpdateEvent to map
func (s *Schema) gasTipUpdateEventToMap(event *storage.GasTipUpdateEvent) map[string]interface{} {
	return map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"oldTip":          event.OldTip.String(),
		"newTip":          event.NewTip.String(),
		"updater":         event.Updater.Hex(),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert BlacklistEvent to map
func (s *Schema) blacklistEventToMap(event *storage.BlacklistEvent) map[string]interface{} {
	return map[string]interface{}{
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"account":         event.Account.Hex(),
		"action":          event.Action,
		"proposalId":      event.ProposalID.String(),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert MemberChangeEvent to map
func (s *Schema) memberChangeEventToMap(event *storage.MemberChangeEvent) map[string]interface{} {
	m := map[string]interface{}{
		"contract":        event.Contract.Hex(),
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"member":          event.Member.Hex(),
		"action":          event.Action,
		"totalMembers":    fmt.Sprintf("%d", event.TotalMembers),
		"newQuorum":       int(event.NewQuorum),
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
	if event.OldMember != nil {
		m["oldMember"] = event.OldMember.Hex()
	}
	return m
}

// Helper function to convert EmergencyPauseEvent to map
func (s *Schema) emergencyPauseEventToMap(event *storage.EmergencyPauseEvent) map[string]interface{} {
	return map[string]interface{}{
		"contract":        event.Contract.Hex(),
		"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
		"transactionHash": event.TxHash.Hex(),
		"proposalId":      event.ProposalID.String(),
		"action":          event.Action,
		"timestamp":       fmt.Sprintf("%d", event.Timestamp),
	}
}

// Helper function to convert DepositMintProposal to map
func (s *Schema) depositMintProposalToMap(proposal *storage.DepositMintProposal) map[string]interface{} {
	return map[string]interface{}{
		"proposalId":      proposal.ProposalID.String(),
		"requester":       proposal.Requester.Hex(),
		"beneficiary":     proposal.Beneficiary.Hex(),
		"amount":          proposal.Amount.String(),
		"depositId":       proposal.DepositID,
		"bankReference":   proposal.BankReference,
		"status":          proposalStatusToString(proposal.Status),
		"blockNumber":     fmt.Sprintf("%d", proposal.BlockNumber),
		"transactionHash": proposal.TxHash.Hex(),
		"timestamp":       fmt.Sprintf("%d", proposal.Timestamp),
	}
}

// resolveMaxProposalsUpdateHistory resolves max proposals per member update history
func (s *Schema) resolveMaxProposalsUpdateHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid contract address")
	}

	contract := common.HexToAddress(contractStr)

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetMaxProposalsUpdateHistory(ctx, contract)
	if err != nil {
		s.logger.Error("failed to get max proposals update history",
			zap.String("contract", contractStr),
			zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		result = append(result, map[string]interface{}{
			"contract":        event.Contract.Hex(),
			"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
			"transactionHash": event.TxHash.Hex(),
			"oldMax":          int(event.OldMax),
			"newMax":          int(event.NewMax),
			"timestamp":       fmt.Sprintf("%d", event.Timestamp),
		})
	}

	if result == nil {
		result = []map[string]interface{}{}
	}

	return result, nil
}

// resolveProposalExecutionSkippedEvents resolves proposal execution skipped events
func (s *Schema) resolveProposalExecutionSkippedEvents(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	contractStr, ok := p.Args["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid contract address")
	}

	contract := common.HexToAddress(contractStr)

	var proposalID *big.Int
	if pidStr, ok := p.Args["proposalId"].(string); ok && pidStr != "" {
		proposalID = new(big.Int)
		proposalID.SetString(pidStr, 10)
	}

	reader, ok := s.storage.(storage.SystemContractReader)
	if !ok {
		return nil, fmt.Errorf("storage does not implement SystemContractReader")
	}

	events, err := reader.GetProposalExecutionSkippedEvents(ctx, contract, proposalID)
	if err != nil {
		s.logger.Error("failed to get proposal execution skipped events",
			zap.String("contract", contractStr),
			zap.Error(err))
		return nil, err
	}

	var result []map[string]interface{}
	for _, event := range events {
		pidStr := "0"
		if event.ProposalID != nil {
			pidStr = event.ProposalID.String()
		}
		result = append(result, map[string]interface{}{
			"contract":        event.Contract.Hex(),
			"blockNumber":     fmt.Sprintf("%d", event.BlockNumber),
			"transactionHash": event.TxHash.Hex(),
			"account":         event.Account.Hex(),
			"proposalId":      pidStr,
			"reason":          event.Reason,
			"timestamp":       fmt.Sprintf("%d", event.Timestamp),
		})
	}

	if result == nil {
		result = []map[string]interface{}{}
	}

	return result, nil
}

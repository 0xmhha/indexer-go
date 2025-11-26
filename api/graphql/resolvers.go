package graphql

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/storage"
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
	ctx := p.Context

	// Get pagination parameters with defaults
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit // Maximum limit to prevent resource exhaustion
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Parse filter parameters
	var numberFrom, numberTo uint64
	var timestampFrom, timestampTo uint64
	var miner *common.Address

	if filter, ok := p.Args["filter"].(map[string]interface{}); ok {
		if nf, ok := filter["numberFrom"].(string); ok {
			if n, err := strconv.ParseUint(nf, 10, 64); err == nil {
				numberFrom = n
			}
		}
		if nt, ok := filter["numberTo"].(string); ok {
			if n, err := strconv.ParseUint(nt, 10, 64); err == nil {
				numberTo = n
			}
		}
		if tf, ok := filter["timestampFrom"].(string); ok {
			if t, err := strconv.ParseUint(tf, 10, 64); err == nil {
				timestampFrom = t
			}
		}
		if tt, ok := filter["timestampTo"].(string); ok {
			if t, err := strconv.ParseUint(tt, 10, 64); err == nil {
				timestampTo = t
			}
		}
		if m, ok := filter["miner"].(string); ok {
			addr := common.HexToAddress(m)
			miner = &addr
		}
	}

	// Determine block range
	latestHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// No blocks indexed yet
			return map[string]interface{}{
				"nodes":      []interface{}{},
				"totalCount": 0,
				"pageInfo": map[string]interface{}{
					"hasNextPage":     false,
					"hasPreviousPage": false,
					"startCursor":     nil,
					"endCursor":       nil,
				},
			}, nil
		}
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Set default range if not specified
	if numberFrom == 0 && numberTo == 0 {
		if latestHeight >= uint64(offset+limit-1) {
			numberFrom = latestHeight - uint64(offset+limit-1)
			numberTo = latestHeight - uint64(offset)
		} else {
			numberFrom = 0
			numberTo = latestHeight
		}
	} else if numberTo == 0 {
		numberTo = latestHeight
	}

	// Validate range
	if numberFrom > numberTo {
		return nil, fmt.Errorf("invalid block range: numberFrom (%d) > numberTo (%d)", numberFrom, numberTo)
	}

	// Adjust range based on offset and limit
	rangeSize := numberTo - numberFrom + 1
	if uint64(offset) >= rangeSize {
		// Offset beyond available blocks
		return map[string]interface{}{
			"nodes":      []interface{}{},
			"totalCount": 0,
			"pageInfo": map[string]interface{}{
				"hasNextPage":     false,
				"hasPreviousPage": offset > 0,
				"startCursor":     nil,
				"endCursor":       nil,
			},
		}, nil
	}

	startBlock := numberFrom + uint64(offset)
	endBlock := startBlock + uint64(limit) - 1
	if endBlock > numberTo {
		endBlock = numberTo
	}

	// Fetch blocks
	blocks, err := s.storage.GetBlocks(ctx, startBlock, endBlock)
	if err != nil {
		s.logger.Error("failed to get blocks",
			zap.Uint64("startBlock", startBlock),
			zap.Uint64("endBlock", endBlock),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get blocks: %w", err)
	}

	// Apply filtering
	filteredBlocks := make([]*types.Block, 0, len(blocks))
	for _, block := range blocks {
		if block == nil {
			continue
		}

		// Filter by timestamp
		if timestampFrom > 0 && block.Time() < timestampFrom {
			continue
		}
		if timestampTo > 0 && block.Time() > timestampTo {
			continue
		}

		// Filter by miner
		if miner != nil && block.Coinbase() != *miner {
			continue
		}

		filteredBlocks = append(filteredBlocks, block)
	}

	// Convert to maps
	nodes := make([]interface{}, len(filteredBlocks))
	for i, block := range filteredBlocks {
		nodes[i] = s.blockToMap(block)
	}

	// Calculate total count based on filters
	var totalCount int
	hasTimestampFilter := timestampFrom > 0 || timestampTo > 0
	hasMinerFilter := miner != nil

	if !hasTimestampFilter && !hasMinerFilter {
		// No filters applied (or only block number range) - use actual total count
		if histStorage, ok := s.storage.(storage.HistoricalReader); ok {
			// Use storage method for accurate total block count
			count, err := histStorage.GetBlockCount(ctx)
			if err == nil {
				totalCount = int(count)
			} else {
				// Fallback: use latest height + 1
				totalCount = int(latestHeight + 1)
			}
		} else {
			// Fallback: use latest height + 1
			totalCount = int(latestHeight + 1)
		}
	} else {
		// Filters applied - totalCount represents filtered results in current range
		// Note: This is the count within the queried range, not the total across all blocks
		totalCount = len(filteredBlocks)
	}

	hasNextPage := endBlock < numberTo
	hasPreviousPage := offset > 0

	var startCursor, endCursor interface{}
	if len(filteredBlocks) > 0 {
		startCursor = fmt.Sprintf("%d", filteredBlocks[0].NumberU64())
		endCursor = fmt.Sprintf("%d", filteredBlocks[len(filteredBlocks)-1].NumberU64())
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

	return s.transactionToMap(tx, location), nil
}

// resolveTransactions resolves transactions with filtering and pagination
func (s *Schema) resolveTransactions(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Get pagination parameters with defaults
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			if l > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit // Maximum limit to prevent resource exhaustion
			} else {
				limit = l
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Parse filter parameters
	var blockNumberFrom, blockNumberTo uint64
	var fromAddr, toAddr *common.Address
	var txType *int

	if filter, ok := p.Args["filter"].(map[string]interface{}); ok {
		if bnf, ok := filter["blockNumberFrom"].(string); ok {
			if bn, err := strconv.ParseUint(bnf, 10, 64); err == nil {
				blockNumberFrom = bn
			}
		}
		if bnt, ok := filter["blockNumberTo"].(string); ok {
			if bn, err := strconv.ParseUint(bnt, 10, 64); err == nil {
				blockNumberTo = bn
			}
		}
		if from, ok := filter["from"].(string); ok {
			addr := common.HexToAddress(from)
			fromAddr = &addr
		}
		if to, ok := filter["to"].(string); ok {
			addr := common.HexToAddress(to)
			toAddr = &addr
		}
		if t, ok := filter["type"].(int); ok {
			txType = &t
		}
	}

	// Determine block range
	latestHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// No blocks indexed yet
			return map[string]interface{}{
				"nodes":      []interface{}{},
				"totalCount": 0,
				"pageInfo": map[string]interface{}{
					"hasNextPage":     false,
					"hasPreviousPage": false,
					"startCursor":     nil,
					"endCursor":       nil,
				},
			}, nil
		}
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Set default range if not specified
	if blockNumberFrom == 0 && blockNumberTo == 0 {
		if latestHeight >= uint64(limit-1) {
			blockNumberFrom = latestHeight - uint64(limit-1)
			blockNumberTo = latestHeight
		} else {
			blockNumberFrom = 0
			blockNumberTo = latestHeight
		}
	} else if blockNumberTo == 0 {
		blockNumberTo = latestHeight
	}

	// Validate range
	if blockNumberFrom > blockNumberTo {
		return nil, fmt.Errorf("invalid block range: blockNumberFrom (%d) > blockNumberTo (%d)", blockNumberFrom, blockNumberTo)
	}

	// Limit range to prevent excessive queries
	maxRange := uint64(1000)
	if blockNumberTo-blockNumberFrom > maxRange {
		blockNumberTo = blockNumberFrom + maxRange
	}

	// Fetch blocks in range
	blocks, err := s.storage.GetBlocks(ctx, blockNumberFrom, blockNumberTo)
	if err != nil {
		s.logger.Error("failed to get blocks",
			zap.Uint64("blockNumberFrom", blockNumberFrom),
			zap.Uint64("blockNumberTo", blockNumberTo),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get blocks: %w", err)
	}

	// Collect and filter transactions
	var filteredTxs []map[string]interface{}

	for _, block := range blocks {
		if block == nil {
			continue
		}

		txs := block.Transactions()
		for i, tx := range txs {
			// Apply filters
			if txType != nil && int(tx.Type()) != *txType {
				continue
			}

			// Get signer for this transaction's chain
			var signer types.Signer
			if tx.ChainId() != nil {
				signer = types.LatestSignerForChainID(tx.ChainId())
			} else {
				// Fallback for legacy transactions without chain ID
				signer = types.HomesteadSigner{}
			}

			// Get from address
			from, err := types.Sender(signer, tx)
			if err != nil {
				s.logger.Warn("failed to get transaction sender",
					zap.String("txHash", tx.Hash().Hex()),
					zap.Error(err))
				continue
			}

			if fromAddr != nil && from != *fromAddr {
				continue
			}

			if toAddr != nil {
				txTo := tx.To()
				if txTo == nil || *txTo != *toAddr {
					continue
				}
			}

			location := &storage.TxLocation{
				BlockHeight: block.NumberU64(),
				BlockHash:   block.Hash(),
				TxIndex:     uint64(i),
			}

			filteredTxs = append(filteredTxs, s.transactionToMap(tx, location))
		}
	}

	// Calculate total count based on filters
	var totalCount int
	hasFilters := fromAddr != nil || toAddr != nil || txType != nil

	if !hasFilters {
		// No filters applied - use actual total transaction count
		if histStorage, ok := s.storage.(storage.HistoricalReader); ok {
			count, err := histStorage.GetTransactionCount(ctx)
			if err == nil {
				totalCount = int(count)
			} else {
				// Fallback: use filtered count from queried range
				totalCount = len(filteredTxs)
			}
		} else {
			// Fallback: use filtered count from queried range
			totalCount = len(filteredTxs)
		}
	} else {
		// Filters applied - totalCount represents filtered results in current range
		// Note: Due to block range limitation (max 1000), this may not reflect total across all blocks
		totalCount = len(filteredTxs)
	}

	// Apply pagination
	start := offset
	end := offset + limit

	if start > len(filteredTxs) {
		start = len(filteredTxs)
	}
	if end > len(filteredTxs) {
		end = len(filteredTxs)
	}

	paginatedTxs := filteredTxs[start:end]

	// Calculate pagination info
	hasNextPage := end < len(filteredTxs)
	hasPreviousPage := offset > 0

	var startCursor, endCursor interface{}
	if len(paginatedTxs) > 0 {
		if txHash, ok := paginatedTxs[0]["hash"].(string); ok {
			startCursor = txHash
		}
		if txHash, ok := paginatedTxs[len(paginatedTxs)-1]["hash"].(string); ok {
			endCursor = txHash
		}
	}

	return map[string]interface{}{
		"nodes":      paginatedTxs,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     hasNextPage,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     startCursor,
			"endCursor":       endCursor,
		},
	}, nil
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
	limit := 10
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

	// Convert transaction hashes to full transaction objects
	nodes := make([]interface{}, 0, len(txHashes))
	for _, txHash := range txHashes {
		tx, location, err := s.storage.GetTransaction(ctx, txHash)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				s.logger.Warn("transaction hash not found in storage",
					zap.String("txHash", txHash.Hex()),
					zap.String("address", addressStr))
				continue
			}
			s.logger.Error("failed to get transaction",
				zap.String("txHash", txHash.Hex()),
				zap.Error(err))
			// Continue with other transactions instead of failing completely
			continue
		}

		if tx == nil {
			s.logger.Warn("transaction is nil",
				zap.String("txHash", txHash.Hex()))
			continue
		}

		nodes = append(nodes, s.transactionToMap(tx, location))
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
	ctx := p.Context

	// Get decode parameter
	decode := false
	if d, ok := p.Args["decode"].(bool); ok {
		decode = d
	}

	// Get pagination parameters with validation
	limit := 10
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

	// Parse required filter parameters
	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("filter is required")
	}

	var logAddress *common.Address
	var topics []common.Hash
	var blockNumberFrom, blockNumberTo uint64

	if addr, ok := filter["address"].(string); ok {
		address := common.HexToAddress(addr)
		logAddress = &address
	}

	if topicsInterface, ok := filter["topics"].([]interface{}); ok {
		topics = make([]common.Hash, 0, len(topicsInterface))
		for _, t := range topicsInterface {
			if topicStr, ok := t.(string); ok {
				topics = append(topics, common.HexToHash(topicStr))
			}
		}
	}

	if bnf, ok := filter["blockNumberFrom"].(string); ok {
		if bn, err := strconv.ParseUint(bnf, 10, 64); err == nil {
			blockNumberFrom = bn
		}
	}

	if bnt, ok := filter["blockNumberTo"].(string); ok {
		if bn, err := strconv.ParseUint(bnt, 10, 64); err == nil {
			blockNumberTo = bn
		}
	}

	// Determine block range
	latestHeight, err := s.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// No blocks indexed yet
			return map[string]interface{}{
				"nodes":      []interface{}{},
				"totalCount": 0,
				"pageInfo": map[string]interface{}{
					"hasNextPage":     false,
					"hasPreviousPage": false,
					"startCursor":     nil,
					"endCursor":       nil,
				},
			}, nil
		}
		s.logger.Error("failed to get latest height", zap.Error(err))
		return nil, fmt.Errorf("failed to get latest height: %w", err)
	}

	// Set default range if not specified
	if blockNumberFrom == 0 && blockNumberTo == 0 {
		if latestHeight >= uint64(limit-1) {
			blockNumberFrom = latestHeight - uint64(limit-1)
			blockNumberTo = latestHeight
		} else {
			blockNumberFrom = 0
			blockNumberTo = latestHeight
		}
	} else if blockNumberTo == 0 {
		blockNumberTo = latestHeight
	}

	// Validate range
	if blockNumberFrom > blockNumberTo {
		return nil, fmt.Errorf("invalid block range: blockNumberFrom (%d) > blockNumberTo (%d)", blockNumberFrom, blockNumberTo)
	}

	// Limit range to prevent excessive queries
	maxRange := uint64(1000)
	if blockNumberTo-blockNumberFrom > maxRange {
		blockNumberTo = blockNumberFrom + maxRange
	}

	// Collect logs from receipts in the block range
	var filteredLogs []map[string]interface{}

	for blockNum := blockNumberFrom; blockNum <= blockNumberTo; blockNum++ {
		receipts, err := s.storage.GetReceiptsByBlockNumber(ctx, blockNum)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				// Block has no receipts, continue
				continue
			}
			s.logger.Error("failed to get receipts for block",
				zap.Uint64("blockNumber", blockNum),
				zap.Error(err))
			// Continue with other blocks instead of failing completely
			continue
		}

		for _, receipt := range receipts {
			if receipt == nil {
				continue
			}

			for _, log := range receipt.Logs {
				if log == nil {
					continue
				}

				// Apply address filter
				if logAddress != nil && log.Address != *logAddress {
					continue
				}

				// Apply topics filter
				if len(topics) > 0 {
					matchesTopics := false
					for _, filterTopic := range topics {
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
						continue
					}
				}

				filteredLogs = append(filteredLogs, s.logToMapWithDecode(log, decode))
			}
		}
	}

	// Apply pagination
	totalCount := len(filteredLogs)
	start := offset
	end := offset + limit

	if start > totalCount {
		start = totalCount
	}
	if end > totalCount {
		end = totalCount
	}

	paginatedLogs := filteredLogs[start:end]

	// Calculate pagination info
	hasNextPage := end < totalCount
	hasPreviousPage := offset > 0

	var startCursor, endCursor interface{}
	if len(paginatedLogs) > 0 {
		firstLog := paginatedLogs[0]
		if txHash, ok := firstLog["transactionHash"].(string); ok {
			if logIndex, ok := firstLog["logIndex"].(int); ok {
				startCursor = fmt.Sprintf("%s:%d", txHash, logIndex)
			}
		}

		lastLog := paginatedLogs[len(paginatedLogs)-1]
		if txHash, ok := lastLog["transactionHash"].(string); ok {
			if logIndex, ok := lastLog["logIndex"].(int); ok {
				endCursor = fmt.Sprintf("%s:%d", txHash, logIndex)
			}
		}
	}

	return map[string]interface{}{
		"nodes":      paginatedLogs,
		"totalCount": totalCount,
		"pageInfo": map[string]interface{}{
			"hasNextPage":     hasNextPage,
			"hasPreviousPage": hasPreviousPage,
			"startCursor":     startCursor,
			"endCursor":       endCursor,
		},
	}, nil
}

// Helper function to convert context (placeholder for proper context usage)
func getContext(p graphql.ResolveParams) context.Context {
	if ctx, ok := p.Context.(context.Context); ok {
		return ctx
	}
	return context.Background()
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

	// Parse filter
	filter, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	contractStr, ok := filter["contract"].(string)
	if !ok {
		return nil, fmt.Errorf("contract address is required")
	}
	contract := common.HexToAddress(contractStr)

	// Parse status (optional)
	status := storage.ProposalStatusNone
	if statusStr, ok := filter["status"].(string); ok {
		status = parseProposalStatus(statusStr)
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
		s.logger.Error("failed to get proposals",
			zap.String("contract", contractStr),
			zap.Error(err))
		return nil, err
	}

	var nodes []map[string]interface{}
	for _, proposal := range proposals {
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

	// Parse optional minter address
	var minter common.Address
	if minterStr, ok := filter["minter"].(string); ok && minterStr != "" {
		minter = common.HexToAddress(minterStr)
	}

	// Pagination
	limit := 10
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

	// Parse optional burner address
	var burner common.Address
	if burnerStr, ok := filter["burner"].(string); ok && burnerStr != "" {
		burner = common.HexToAddress(burnerStr)
	}

	// Pagination
	limit := 10
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
		"to":              proposal.To.Hex(),
		"amount":          proposal.Amount.String(),
		"depositId":       proposal.DepositID,
		"status":          proposalStatusToString(proposal.Status),
		"blockNumber":     fmt.Sprintf("%d", proposal.BlockNumber),
		"transactionHash": proposal.TxHash.Hex(),
		"timestamp":       fmt.Sprintf("%d", proposal.Timestamp),
	}
}

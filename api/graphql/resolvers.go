package graphql

import (
	"context"
	"errors"
	"fmt"
	"strconv"

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

	// Calculate pagination info
	totalCount := len(filteredBlocks)
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

	// Apply pagination
	totalCount := len(filteredTxs)
	start := offset
	end := offset + limit

	if start > totalCount {
		start = totalCount
	}
	if end > totalCount {
		end = totalCount
	}

	paginatedTxs := filteredTxs[start:end]

	// Calculate pagination info
	hasNextPage := end < totalCount
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

				filteredLogs = append(filteredLogs, s.logToMap(log))
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

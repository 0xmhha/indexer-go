package graphql

import (
	"fmt"
	"math/big"
	"strconv"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/graphql-go/graphql"
	"go.uber.org/zap"
)

// resolveBlocksByTimeRange resolves blocks within a time range
func (s *Schema) resolveBlocksByTimeRange(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	fromTimeStr, ok := p.Args["fromTime"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromTime")
	}

	toTimeStr, ok := p.Args["toTime"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toTime")
	}

	fromTime, err := strconv.ParseUint(fromTimeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromTime format: %w", err)
	}

	toTime, err := strconv.ParseUint(toTimeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toTime format: %w", err)
	}

	// Get pagination parameters
	limit := 10
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > 100 {
				limit = 100
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	blocks, err := histStorage.GetBlocksByTimeRange(ctx, fromTime, toTime, limit, offset)
	if err != nil {
		s.logger.Error("failed to get blocks by time range",
			zap.Uint64("fromTime", fromTime),
			zap.Uint64("toTime", toTime),
			zap.Error(err))
		return nil, err
	}

	// Convert blocks to maps
	nodes := make([]interface{}, len(blocks))
	for i, block := range blocks {
		nodes[i] = s.blockToMap(block)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(blocks),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(blocks) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveBlockByTimestamp resolves the block closest to a timestamp
func (s *Schema) resolveBlockByTimestamp(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	timestampStr, ok := p.Args["timestamp"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid timestamp")
	}

	timestamp, err := strconv.ParseUint(timestampStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp format: %w", err)
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	block, err := histStorage.GetBlockByTimestamp(ctx, timestamp)
	if err != nil {
		s.logger.Error("failed to get block by timestamp",
			zap.Uint64("timestamp", timestamp),
			zap.Error(err))
		return nil, err
	}

	return s.blockToMap(block), nil
}

// resolveTransactionsByAddressFiltered resolves filtered transactions for an address
func (s *Schema) resolveTransactionsByAddressFiltered(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)

	// Parse filter
	filterArgs, ok := p.Args["filter"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid filter")
	}

	filter, err := parseHistoricalTransactionFilter(filterArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse filter: %w", err)
	}

	// Get pagination parameters
	limit := 10
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > 100 {
				limit = 100
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	txsWithReceipts, err := histStorage.GetTransactionsByAddressFiltered(ctx, address, filter, limit, offset)
	if err != nil {
		s.logger.Error("failed to get filtered transactions",
			zap.String("address", addressStr),
			zap.Error(err))
		return nil, err
	}

	// Convert to maps
	nodes := make([]interface{}, len(txsWithReceipts))
	for i, txr := range txsWithReceipts {
		txMap := s.transactionToMap(txr.Transaction, txr.Location)
		if txr.Receipt != nil {
			txMap["receipt"] = s.receiptToMap(txr.Receipt)
		}
		nodes[i] = txMap
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(txsWithReceipts),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(txsWithReceipts) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveAddressBalance resolves address balance at a specific block
func (s *Schema) resolveAddressBalance(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)

	// Parse block number (optional, defaults to 0 for latest)
	var blockNumber uint64 = 0
	if blockNumberStr, ok := p.Args["blockNumber"].(string); ok {
		bn, err := strconv.ParseUint(blockNumberStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid blockNumber format: %w", err)
		}
		blockNumber = bn
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	balance, err := histStorage.GetAddressBalance(ctx, address, blockNumber)
	if err != nil {
		s.logger.Error("failed to get address balance",
			zap.String("address", addressStr),
			zap.Uint64("blockNumber", blockNumber),
			zap.Error(err))
		return nil, err
	}

	return balance.String(), nil
}

// resolveBalanceHistory resolves balance history for an address
func (s *Schema) resolveBalanceHistory(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	addressStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	address := common.HexToAddress(addressStr)

	fromBlockStr, ok := p.Args["fromBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromBlock")
	}

	toBlockStr, ok := p.Args["toBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toBlock")
	}

	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	// Get pagination parameters
	limit := 10
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > 100 {
				limit = 100
			}
		}
		if o, ok := pagination["offset"].(int); ok && o >= 0 {
			offset = o
		}
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	snapshots, err := histStorage.GetBalanceHistory(ctx, address, fromBlock, toBlock, limit, offset)
	if err != nil {
		s.logger.Error("failed to get balance history",
			zap.String("address", addressStr),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	// Convert snapshots to maps
	nodes := make([]interface{}, len(snapshots))
	for i, snapshot := range snapshots {
		nodes[i] = map[string]interface{}{
			"blockNumber": fmt.Sprintf("%d", snapshot.BlockNumber),
			"balance":     snapshot.Balance.String(),
			"delta":       snapshot.Delta.String(),
			"transactionHash": func() interface{} {
				if snapshot.TxHash == (common.Hash{}) {
					return nil
				}
				return snapshot.TxHash.Hex()
			}(),
		}
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(snapshots),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(snapshots) == limit,
			"hasPreviousPage": offset > 0,
			"startCursor":     nil,
			"endCursor":       nil,
		},
	}, nil
}

// resolveBlockCount resolves total block count
func (s *Schema) resolveBlockCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	count, err := histStorage.GetBlockCount(ctx)
	if err != nil {
		s.logger.Error("failed to get block count", zap.Error(err))
		return nil, err
	}

	return fmt.Sprintf("%d", count), nil
}

// resolveTransactionCount resolves total transaction count
func (s *Schema) resolveTransactionCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	count, err := histStorage.GetTransactionCount(ctx)
	if err != nil {
		s.logger.Error("failed to get transaction count", zap.Error(err))
		return nil, err
	}

	return fmt.Sprintf("%d", count), nil
}

// parseHistoricalTransactionFilter parses GraphQL filter arguments to storage.TransactionFilter
func parseHistoricalTransactionFilter(args map[string]interface{}) (*storage.TransactionFilter, error) {
	filter := &storage.TransactionFilter{
		TxType:      storage.TxTypeAll,
		SuccessOnly: false,
	}

	// Parse fromBlock
	if fromBlockStr, ok := args["fromBlock"].(string); ok {
		fb, err := strconv.ParseUint(fromBlockStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid fromBlock: %w", err)
		}
		filter.FromBlock = fb
	} else {
		return nil, fmt.Errorf("fromBlock is required")
	}

	// Parse toBlock
	if toBlockStr, ok := args["toBlock"].(string); ok {
		tb, err := strconv.ParseUint(toBlockStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid toBlock: %w", err)
		}
		filter.ToBlock = tb
	} else {
		return nil, fmt.Errorf("toBlock is required")
	}

	// Parse optional minValue
	if minValueStr, ok := args["minValue"].(string); ok {
		minValue, success := new(big.Int).SetString(minValueStr, 10)
		if !success {
			return nil, fmt.Errorf("invalid minValue format")
		}
		filter.MinValue = minValue
	}

	// Parse optional maxValue
	if maxValueStr, ok := args["maxValue"].(string); ok {
		maxValue, success := new(big.Int).SetString(maxValueStr, 10)
		if !success {
			return nil, fmt.Errorf("invalid maxValue format")
		}
		filter.MaxValue = maxValue
	}

	// Parse optional txType
	if txType, ok := args["txType"].(int); ok {
		filter.TxType = storage.TransactionType(txType)
	}

	// Parse optional successOnly
	if successOnly, ok := args["successOnly"].(bool); ok {
		filter.SuccessOnly = successOnly
	}

	return filter, nil
}

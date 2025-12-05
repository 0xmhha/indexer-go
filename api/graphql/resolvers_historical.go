package graphql

import (
	"fmt"
	"math/big"
	"strconv"

	"github.com/0xmhha/indexer-go/internal/constants"
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
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
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

	// Note: totalCount represents blocks returned in this page
	// To get the true total count of blocks in the time range, we would need
	// a separate CountBlocksByTimeRange method in storage (performance optimization for future)
	totalCount := len(blocks)

	// For more accurate totalCount when no pagination is applied
	if limit >= constants.DefaultMaxPaginationLimit && offset == 0 {
		// User requested maximum limit - totalCount is likely accurate for the range
		totalCount = len(blocks)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": totalCount,
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
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
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
	limit := constants.DefaultPaginationLimit
	offset := 0
	if pagination, ok := p.Args["pagination"].(map[string]interface{}); ok {
		if l, ok := pagination["limit"].(int); ok && l > 0 {
			limit = l
			if limit > constants.DefaultMaxPaginationLimit {
				limit = constants.DefaultMaxPaginationLimit
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

// resolveTopMiners resolves the top miners by block count
func (s *Schema) resolveTopMiners(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Get limit parameter
	limit := constants.DefaultPaginationLimit
	if l, ok := p.Args["limit"].(int); ok && l > 0 {
		limit = l
		if limit > 100 {
			limit = 100
		}
	}

	// Get block range parameters (optional)
	var fromBlock, toBlock uint64
	if fb, ok := p.Args["fromBlock"].(string); ok {
		if fbInt, err := strconv.ParseUint(fb, 10, 64); err == nil {
			fromBlock = fbInt
		}
	}
	if tb, ok := p.Args["toBlock"].(string); ok {
		if tbInt, err := strconv.ParseUint(tb, 10, 64); err == nil {
			toBlock = tbInt
		}
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	stats, err := histStorage.GetTopMiners(ctx, limit, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get top miners",
			zap.Int("limit", limit),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	// Convert to response format
	result := make([]map[string]interface{}, len(stats))
	for i, stat := range stats {
		result[i] = map[string]interface{}{
			"address":         stat.Address.Hex(),
			"blockCount":      fmt.Sprintf("%d", stat.BlockCount),
			"lastBlockNumber": fmt.Sprintf("%d", stat.LastBlockNumber),
			"lastBlockTime":   fmt.Sprintf("%d", stat.LastBlockTime),
			"percentage":      stat.Percentage,
			"totalRewards":    stat.TotalRewards.String(),
		}
	}

	return result, nil
}

// resolveTokenBalances resolves token balances for an address
func (s *Schema) resolveTokenBalances(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Get address parameter
	addrStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}

	if !common.IsHexAddress(addrStr) {
		return nil, fmt.Errorf("invalid address format")
	}
	addr := common.HexToAddress(addrStr)

	// Get tokenType parameter (optional)
	tokenType := ""
	if tt, ok := p.Args["tokenType"].(string); ok {
		tokenType = tt
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	balances, err := histStorage.GetTokenBalances(ctx, addr, tokenType)
	if err != nil {
		s.logger.Error("failed to get token balances",
			zap.String("address", addrStr),
			zap.String("tokenType", tokenType),
			zap.Error(err))
		return nil, err
	}

	// Convert to response format
	result := make([]map[string]interface{}, len(balances))
	for i, balance := range balances {
		r := map[string]interface{}{
			"contractAddress": balance.ContractAddress.Hex(),
			"tokenType":       balance.TokenType,
			"balance":         balance.Balance.String(),
		}

		// Add tokenId if not empty
		if balance.TokenID != "" {
			r["tokenId"] = balance.TokenID
		}

		// Add name if not empty
		if balance.Name != "" {
			r["name"] = balance.Name
		}

		// Add symbol if not empty
		if balance.Symbol != "" {
			r["symbol"] = balance.Symbol
		}

		// Add decimals if not nil
		if balance.Decimals != nil {
			r["decimals"] = *balance.Decimals
		}

		// Add metadata if not empty
		if balance.Metadata != "" {
			r["metadata"] = balance.Metadata
		}

		result[i] = r
	}

	return result, nil
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

// resolveGasStats resolves gas usage statistics for a block range
func (s *Schema) resolveGasStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse fromBlock
	fromBlockStr, ok := p.Args["fromBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromBlock")
	}
	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	// Parse toBlock
	toBlockStr, ok := p.Args["toBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toBlock")
	}
	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	stats, err := histStorage.GetGasStatsByBlockRange(ctx, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get gas stats",
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	return map[string]interface{}{
		"totalGasUsed":     fmt.Sprintf("%d", stats.TotalGasUsed),
		"totalGasLimit":    fmt.Sprintf("%d", stats.TotalGasLimit),
		"averageGasUsed":   fmt.Sprintf("%d", stats.AverageGasUsed),
		"averageGasPrice":  stats.AverageGasPrice.String(),
		"blockCount":       fmt.Sprintf("%d", stats.BlockCount),
		"transactionCount": fmt.Sprintf("%d", stats.TransactionCount),
	}, nil
}

// resolveAddressGasStats resolves gas usage statistics for a specific address
func (s *Schema) resolveAddressGasStats(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse address
	addrStr, ok := p.Args["address"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid address")
	}
	address := common.HexToAddress(addrStr)

	// Parse fromBlock
	fromBlockStr, ok := p.Args["fromBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromBlock")
	}
	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	// Parse toBlock
	toBlockStr, ok := p.Args["toBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toBlock")
	}
	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	stats, err := histStorage.GetGasStatsByAddress(ctx, address, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get address gas stats",
			zap.String("address", address.Hex()),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	return map[string]interface{}{
		"address":          stats.Address.Hex(),
		"totalGasUsed":     fmt.Sprintf("%d", stats.TotalGasUsed),
		"transactionCount": fmt.Sprintf("%d", stats.TransactionCount),
		"averageGasPerTx":  fmt.Sprintf("%d", stats.AverageGasPerTx),
		"totalFeesPaid":    stats.TotalFeesPaid.String(),
	}, nil
}

// resolveTopAddressesByGasUsed resolves top addresses by total gas used
func (s *Schema) resolveTopAddressesByGasUsed(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse limit
	limit := constants.DefaultPaginationLimit
	if l, ok := p.Args["limit"].(int); ok && l > 0 {
		limit = l
		if limit > 100 {
			limit = 100
		}
	}

	// Parse fromBlock
	fromBlockStr, ok := p.Args["fromBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromBlock")
	}
	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	// Parse toBlock
	toBlockStr, ok := p.Args["toBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toBlock")
	}
	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	statsList, err := histStorage.GetTopAddressesByGasUsed(ctx, limit, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get top addresses by gas used",
			zap.Int("limit", limit),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	// Convert to response format
	result := make([]map[string]interface{}, len(statsList))
	for i, stats := range statsList {
		result[i] = map[string]interface{}{
			"address":          stats.Address.Hex(),
			"totalGasUsed":     fmt.Sprintf("%d", stats.TotalGasUsed),
			"transactionCount": fmt.Sprintf("%d", stats.TransactionCount),
			"averageGasPerTx":  fmt.Sprintf("%d", stats.AverageGasPerTx),
			"totalFeesPaid":    stats.TotalFeesPaid.String(),
		}
	}

	return result, nil
}

// resolveTopAddressesByTxCount resolves top addresses by transaction count
func (s *Schema) resolveTopAddressesByTxCount(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse limit
	limit := constants.DefaultPaginationLimit
	if l, ok := p.Args["limit"].(int); ok && l > 0 {
		limit = l
		if limit > 100 {
			limit = 100
		}
	}

	// Parse fromBlock
	fromBlockStr, ok := p.Args["fromBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromBlock")
	}
	fromBlock, err := strconv.ParseUint(fromBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromBlock format: %w", err)
	}

	// Parse toBlock
	toBlockStr, ok := p.Args["toBlock"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toBlock")
	}
	toBlock, err := strconv.ParseUint(toBlockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toBlock format: %w", err)
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	statsList, err := histStorage.GetTopAddressesByTxCount(ctx, limit, fromBlock, toBlock)
	if err != nil {
		s.logger.Error("failed to get top addresses by tx count",
			zap.Int("limit", limit),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, err
	}

	// Convert to response format
	result := make([]map[string]interface{}, len(statsList))
	for i, stats := range statsList {
		result[i] = map[string]interface{}{
			"address":            stats.Address.Hex(),
			"transactionCount":   fmt.Sprintf("%d", stats.TransactionCount),
			"totalGasUsed":       fmt.Sprintf("%d", stats.TotalGasUsed),
			"lastActivityBlock":  fmt.Sprintf("%d", stats.LastActivityBlock),
			"firstActivityBlock": fmt.Sprintf("%d", stats.FirstActivityBlock),
		}
	}

	return result, nil
}

// resolveNetworkMetrics resolves network activity metrics for a time range
func (s *Schema) resolveNetworkMetrics(p graphql.ResolveParams) (interface{}, error) {
	ctx := p.Context

	// Parse fromTime
	fromTimeStr, ok := p.Args["fromTime"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid fromTime")
	}
	fromTime, err := strconv.ParseUint(fromTimeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromTime format: %w", err)
	}

	// Parse toTime
	toTimeStr, ok := p.Args["toTime"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid toTime")
	}
	toTime, err := strconv.ParseUint(toTimeStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toTime format: %w", err)
	}

	// Cast storage to HistoricalReader
	histStorage, ok := s.storage.(storage.HistoricalReader)
	if !ok {
		return nil, fmt.Errorf("storage does not support historical queries")
	}

	metrics, err := histStorage.GetNetworkMetrics(ctx, fromTime, toTime)
	if err != nil {
		s.logger.Error("failed to get network metrics",
			zap.Uint64("fromTime", fromTime),
			zap.Uint64("toTime", toTime),
			zap.Error(err))
		return nil, err
	}

	return map[string]interface{}{
		"tps":               metrics.TPS,
		"blockTime":         metrics.BlockTime,
		"totalBlocks":       fmt.Sprintf("%d", metrics.TotalBlocks),
		"totalTransactions": fmt.Sprintf("%d", metrics.TotalTransactions),
		"averageBlockSize":  fmt.Sprintf("%d", metrics.AverageBlockSize),
		"timePeriod":        fmt.Sprintf("%d", metrics.TimePeriod),
	}, nil
}

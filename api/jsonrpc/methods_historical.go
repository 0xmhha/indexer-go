package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// getBlocksByTimeRange returns blocks within a time range
func (h *Handler) getBlocksByTimeRange(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		FromTime interface{} `json:"fromTime"`
		ToTime   interface{} `json:"toTime"`
		Limit    *int        `json:"limit,omitempty"`
		Offset   *int        `json:"offset,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.FromTime == nil {
		return nil, NewError(InvalidParams, "missing required parameter: fromTime", nil)
	}
	if p.ToTime == nil {
		return nil, NewError(InvalidParams, "missing required parameter: toTime", nil)
	}

	// Parse fromTime
	var fromTime uint64
	switch v := p.FromTime.(type) {
	case float64:
		fromTime = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid fromTime format", err.Error())
		}
		fromTime = num
	default:
		return nil, NewError(InvalidParams, "fromTime must be a string or number", nil)
	}

	// Parse toTime
	var toTime uint64
	switch v := p.ToTime.(type) {
	case float64:
		toTime = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid toTime format", err.Error())
		}
		toTime = num
	default:
		return nil, NewError(InvalidParams, "toTime must be a string or number", nil)
	}

	// Parse pagination
	limit := 10
	offset := 0
	if p.Limit != nil && *p.Limit > 0 {
		limit = *p.Limit
		if limit > 100 {
			limit = 100
		}
	}
	if p.Offset != nil && *p.Offset >= 0 {
		offset = *p.Offset
	}

	// Cast storage to HistoricalReader
	histStorage, ok := h.storage.(storage.HistoricalReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support historical queries", nil)
	}

	blocks, err := histStorage.GetBlocksByTimeRange(ctx, fromTime, toTime, limit, offset)
	if err != nil {
		h.logger.Error("failed to get blocks by time range",
			zap.Uint64("fromTime", fromTime),
			zap.Uint64("toTime", toTime),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get blocks", err.Error())
	}

	// Convert blocks to JSON
	nodes := make([]interface{}, len(blocks))
	for i, block := range blocks {
		nodes[i] = h.blockToJSON(block)
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(blocks),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(blocks) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// getBlockByTimestamp returns the block closest to a timestamp
func (h *Handler) getBlockByTimestamp(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Timestamp interface{} `json:"timestamp"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Timestamp == nil {
		return nil, NewError(InvalidParams, "missing required parameter: timestamp", nil)
	}

	// Parse timestamp
	var timestamp uint64
	switch v := p.Timestamp.(type) {
	case float64:
		timestamp = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid timestamp format", err.Error())
		}
		timestamp = num
	default:
		return nil, NewError(InvalidParams, "timestamp must be a string or number", nil)
	}

	// Cast storage to HistoricalReader
	histStorage, ok := h.storage.(storage.HistoricalReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support historical queries", nil)
	}

	block, err := histStorage.GetBlockByTimestamp(ctx, timestamp)
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, NewError(InternalError, "block not found", nil)
		}
		h.logger.Error("failed to get block by timestamp",
			zap.Uint64("timestamp", timestamp),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get block", err.Error())
	}

	return h.blockToJSON(block), nil
}

// getTransactionsByAddressFiltered returns filtered transactions for an address
func (h *Handler) getTransactionsByAddressFiltered(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address  string                 `json:"address"`
		Filter   map[string]interface{} `json:"filter"`
		Limit    *int                   `json:"limit,omitempty"`
		Offset   *int                   `json:"offset,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Address == "" {
		return nil, NewError(InvalidParams, "missing required parameter: address", nil)
	}
	if p.Filter == nil {
		return nil, NewError(InvalidParams, "missing required parameter: filter", nil)
	}

	address := common.HexToAddress(p.Address)

	// Parse filter
	filter, parseErr := parseTransactionFilter(p.Filter)
	if parseErr != nil {
		return nil, NewError(InvalidParams, "invalid filter", parseErr.Error())
	}

	// Parse pagination
	limit := 10
	offset := 0
	if p.Limit != nil && *p.Limit > 0 {
		limit = *p.Limit
		if limit > 100 {
			limit = 100
		}
	}
	if p.Offset != nil && *p.Offset >= 0 {
		offset = *p.Offset
	}

	// Cast storage to HistoricalReader
	histStorage, ok := h.storage.(storage.HistoricalReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support historical queries", nil)
	}

	txsWithReceipts, err := histStorage.GetTransactionsByAddressFiltered(ctx, address, filter, limit, offset)
	if err != nil {
		h.logger.Error("failed to get filtered transactions",
			zap.String("address", p.Address),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get transactions", err.Error())
	}

	// Convert to JSON
	nodes := make([]interface{}, len(txsWithReceipts))
	for i, txr := range txsWithReceipts {
		txJSON := h.transactionToJSON(txr.Transaction, txr.Location)
		if txr.Receipt != nil {
			txJSON["receipt"] = h.receiptToJSON(txr.Receipt)
		}
		nodes[i] = txJSON
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(txsWithReceipts),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(txsWithReceipts) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// getAddressBalance returns address balance at a specific block
func (h *Handler) getAddressBalance(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address     string      `json:"address"`
		BlockNumber interface{} `json:"blockNumber,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Address == "" {
		return nil, NewError(InvalidParams, "missing required parameter: address", nil)
	}

	address := common.HexToAddress(p.Address)

	// Parse block number (optional, defaults to 0 for latest)
	var blockNumber uint64 = 0
	if p.BlockNumber != nil {
		switch v := p.BlockNumber.(type) {
		case float64:
			blockNumber = uint64(v)
		case string:
			num, err := strconv.ParseUint(v, 0, 64)
			if err != nil {
				return nil, NewError(InvalidParams, "invalid blockNumber format", err.Error())
			}
			blockNumber = num
		default:
			return nil, NewError(InvalidParams, "blockNumber must be a string or number", nil)
		}
	}

	// Cast storage to HistoricalReader
	histStorage, ok := h.storage.(storage.HistoricalReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support historical queries", nil)
	}

	balance, err := histStorage.GetAddressBalance(ctx, address, blockNumber)
	if err != nil {
		h.logger.Error("failed to get address balance",
			zap.String("address", p.Address),
			zap.Uint64("blockNumber", blockNumber),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get balance", err.Error())
	}

	return map[string]interface{}{
		"balance": fmt.Sprintf("0x%x", balance),
	}, nil
}

// getBalanceHistory returns balance history for an address
func (h *Handler) getBalanceHistory(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address   string      `json:"address"`
		FromBlock interface{} `json:"fromBlock"`
		ToBlock   interface{} `json:"toBlock"`
		Limit     *int        `json:"limit,omitempty"`
		Offset    *int        `json:"offset,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Address == "" {
		return nil, NewError(InvalidParams, "missing required parameter: address", nil)
	}
	if p.FromBlock == nil {
		return nil, NewError(InvalidParams, "missing required parameter: fromBlock", nil)
	}
	if p.ToBlock == nil {
		return nil, NewError(InvalidParams, "missing required parameter: toBlock", nil)
	}

	address := common.HexToAddress(p.Address)

	// Parse fromBlock
	var fromBlock uint64
	switch v := p.FromBlock.(type) {
	case float64:
		fromBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid fromBlock format", err.Error())
		}
		fromBlock = num
	default:
		return nil, NewError(InvalidParams, "fromBlock must be a string or number", nil)
	}

	// Parse toBlock
	var toBlock uint64
	switch v := p.ToBlock.(type) {
	case float64:
		toBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid toBlock format", err.Error())
		}
		toBlock = num
	default:
		return nil, NewError(InvalidParams, "toBlock must be a string or number", nil)
	}

	// Parse pagination
	limit := 10
	offset := 0
	if p.Limit != nil && *p.Limit > 0 {
		limit = *p.Limit
		if limit > 100 {
			limit = 100
		}
	}
	if p.Offset != nil && *p.Offset >= 0 {
		offset = *p.Offset
	}

	// Cast storage to HistoricalReader
	histStorage, ok := h.storage.(storage.HistoricalReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support historical queries", nil)
	}

	snapshots, err := histStorage.GetBalanceHistory(ctx, address, fromBlock, toBlock, limit, offset)
	if err != nil {
		h.logger.Error("failed to get balance history",
			zap.String("address", p.Address),
			zap.Uint64("fromBlock", fromBlock),
			zap.Uint64("toBlock", toBlock),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get balance history", err.Error())
	}

	// Convert snapshots to JSON
	nodes := make([]interface{}, len(snapshots))
	for i, snapshot := range snapshots {
		node := map[string]interface{}{
			"blockNumber": fmt.Sprintf("0x%x", snapshot.BlockNumber),
			"balance":     fmt.Sprintf("0x%x", snapshot.Balance),
		}

		// Handle signed delta
		if snapshot.Delta.Sign() < 0 {
			node["delta"] = fmt.Sprintf("-0x%x", new(big.Int).Abs(snapshot.Delta))
		} else {
			node["delta"] = fmt.Sprintf("0x%x", snapshot.Delta)
		}

		// Only include transactionHash if non-zero
		if snapshot.TxHash != (common.Hash{}) {
			node["transactionHash"] = snapshot.TxHash.Hex()
		} else {
			node["transactionHash"] = nil
		}

		nodes[i] = node
	}

	return map[string]interface{}{
		"nodes":      nodes,
		"totalCount": len(snapshots),
		"pageInfo": map[string]interface{}{
			"hasNextPage":     len(snapshots) == limit,
			"hasPreviousPage": offset > 0,
		},
	}, nil
}

// getBlockCount returns total block count
func (h *Handler) getBlockCount(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	// Cast storage to HistoricalReader
	histStorage, ok := h.storage.(storage.HistoricalReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support historical queries", nil)
	}

	count, err := histStorage.GetBlockCount(ctx)
	if err != nil {
		h.logger.Error("failed to get block count", zap.Error(err))
		return nil, NewError(InternalError, "failed to get block count", err.Error())
	}

	return map[string]interface{}{
		"count": fmt.Sprintf("0x%x", count),
	}, nil
}

// getTransactionCount returns total transaction count
func (h *Handler) getTransactionCount(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	// Cast storage to HistoricalReader
	histStorage, ok := h.storage.(storage.HistoricalReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support historical queries", nil)
	}

	count, err := histStorage.GetTransactionCount(ctx)
	if err != nil {
		h.logger.Error("failed to get transaction count", zap.Error(err))
		return nil, NewError(InternalError, "failed to get transaction count", err.Error())
	}

	return map[string]interface{}{
		"count": fmt.Sprintf("0x%x", count),
	}, nil
}

// parseTransactionFilter parses filter parameters to storage.TransactionFilter
func parseTransactionFilter(filter map[string]interface{}) (*storage.TransactionFilter, error) {
	result := &storage.TransactionFilter{
		TxType:      storage.TxTypeAll,
		SuccessOnly: false,
	}

	// Parse fromBlock (required)
	fromBlockVal, ok := filter["fromBlock"]
	if !ok {
		return nil, fmt.Errorf("fromBlock is required")
	}
	switch v := fromBlockVal.(type) {
	case float64:
		result.FromBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid fromBlock: %w", err)
		}
		result.FromBlock = num
	default:
		return nil, fmt.Errorf("fromBlock must be a string or number")
	}

	// Parse toBlock (required)
	toBlockVal, ok := filter["toBlock"]
	if !ok {
		return nil, fmt.Errorf("toBlock is required")
	}
	switch v := toBlockVal.(type) {
	case float64:
		result.ToBlock = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid toBlock: %w", err)
		}
		result.ToBlock = num
	default:
		return nil, fmt.Errorf("toBlock must be a string or number")
	}

	// Parse optional minValue
	if minValueVal, ok := filter["minValue"]; ok {
		switch v := minValueVal.(type) {
		case string:
			minValue, success := new(big.Int).SetString(v, 0)
			if !success {
				return nil, fmt.Errorf("invalid minValue format")
			}
			result.MinValue = minValue
		default:
			return nil, fmt.Errorf("minValue must be a string")
		}
	}

	// Parse optional maxValue
	if maxValueVal, ok := filter["maxValue"]; ok {
		switch v := maxValueVal.(type) {
		case string:
			maxValue, success := new(big.Int).SetString(v, 0)
			if !success {
				return nil, fmt.Errorf("invalid maxValue format")
			}
			result.MaxValue = maxValue
		default:
			return nil, fmt.Errorf("maxValue must be a string")
		}
	}

	// Parse optional txType
	if txTypeVal, ok := filter["txType"]; ok {
		switch v := txTypeVal.(type) {
		case float64:
			result.TxType = storage.TransactionType(int(v))
		default:
			return nil, fmt.Errorf("txType must be a number")
		}
	}

	// Parse optional successOnly
	if successOnlyVal, ok := filter["successOnly"]; ok {
		if b, ok := successOnlyVal.(bool); ok {
			result.SuccessOnly = b
		} else {
			return nil, fmt.Errorf("successOnly must be a boolean")
		}
	}

	return result, nil
}

package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// eth_newFilter creates a new log filter
// https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_newfilter
func (h *Handler) ethNewFilter(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []struct {
		FromBlock interface{}   `json:"fromBlock,omitempty"`
		ToBlock   interface{}   `json:"toBlock,omitempty"`
		Address   interface{}   `json:"address,omitempty"`
		Topics    []interface{} `json:"topics,omitempty"`
		Decode    *bool         `json:"decode,omitempty"` // Optional: decode logs using ABI
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing filter parameter", nil)
	}

	filterParam := p[0]

	// Build filter
	filter := &storage.LogFilter{}

	// Parse fromBlock
	if filterParam.FromBlock != nil {
		fromBlock, err := h.parseBlockNumber(filterParam.FromBlock)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid fromBlock", err.Error())
		}
		filter.FromBlock = fromBlock
	} else {
		// Default to latest
		latestHeight, err := h.storage.GetLatestHeight(ctx)
		if err != nil && err != storage.ErrNotFound {
			h.logger.Error("failed to get latest height", zap.Error(err))
			return nil, NewError(InternalError, "failed to get latest height", err.Error())
		}
		filter.FromBlock = latestHeight
	}

	// Parse toBlock (default to "latest" for filters)
	if filterParam.ToBlock != nil {
		toBlock, err := h.parseBlockNumber(filterParam.ToBlock)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid toBlock", err.Error())
		}
		filter.ToBlock = toBlock
	} else {
		// Default to latest
		latestHeight, err := h.storage.GetLatestHeight(ctx)
		if err != nil && err != storage.ErrNotFound {
			h.logger.Error("failed to get latest height", zap.Error(err))
			return nil, NewError(InternalError, "failed to get latest height", err.Error())
		}
		filter.ToBlock = latestHeight
	}

	// Parse address filter
	if filterParam.Address != nil {
		switch addr := filterParam.Address.(type) {
		case string:
			// Single address
			filter.Addresses = []common.Address{common.HexToAddress(addr)}
		case []interface{}:
			// Multiple addresses
			filter.Addresses = make([]common.Address, len(addr))
			for i, a := range addr {
				addrStr, ok := a.(string)
				if !ok {
					return nil, NewError(InvalidParams, "invalid address format", nil)
				}
				filter.Addresses[i] = common.HexToAddress(addrStr)
			}
		default:
			return nil, NewError(InvalidParams, "invalid address parameter type", nil)
		}
	}

	// Parse topics filter
	if len(filterParam.Topics) > 0 {
		filter.Topics = make([][]common.Hash, len(filterParam.Topics))
		for i, topicParam := range filterParam.Topics {
			if topicParam == nil {
				// nil means "any value" for this position
				filter.Topics[i] = nil
				continue
			}

			switch topic := topicParam.(type) {
			case string:
				// Single topic
				filter.Topics[i] = []common.Hash{common.HexToHash(topic)}
			case []interface{}:
				// Multiple topic options (OR)
				filter.Topics[i] = make([]common.Hash, len(topic))
				for j, t := range topic {
					topicStr, ok := t.(string)
					if !ok {
						return nil, NewError(InvalidParams, "invalid topic format", nil)
					}
					filter.Topics[i][j] = common.HexToHash(topicStr)
				}
			default:
				return nil, NewError(InvalidParams, "invalid topic parameter type", nil)
			}
		}
	}

	// Get current block height for the filter's starting point
	latestHeight, err := h.storage.GetLatestHeight(ctx)
	if err != nil && err != storage.ErrNotFound {
		h.logger.Error("failed to get latest height", zap.Error(err))
		return nil, NewError(InternalError, "failed to get latest height", err.Error())
	}

	// Check if decoding is requested
	decode := false
	if filterParam.Decode != nil {
		decode = *filterParam.Decode
	}

	// Create filter
	filterID := h.filterManager.NewFilter(LogFilterType, filter, latestHeight, decode)

	h.logger.Debug("created new log filter",
		zap.String("id", filterID),
		zap.Uint64("fromBlock", filter.FromBlock),
		zap.Uint64("toBlock", filter.ToBlock),
		zap.Int("addresses", len(filter.Addresses)),
		zap.Int("topics", len(filter.Topics)),
		zap.Bool("decode", decode),
	)

	return filterID, nil
}

// eth_newBlockFilter creates a filter for new block notifications
// https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_newblockfilter
func (h *Handler) ethNewBlockFilter(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	// Get current block height
	latestHeight, err := h.storage.GetLatestHeight(ctx)
	if err != nil && err != storage.ErrNotFound {
		h.logger.Error("failed to get latest height", zap.Error(err))
		return nil, NewError(InternalError, "failed to get latest height", err.Error())
	}

	// Create block filter (decode=false since block filters don't return logs)
	filterID := h.filterManager.NewFilter(BlockFilterType, nil, latestHeight, false)

	h.logger.Debug("created new block filter",
		zap.String("id", filterID),
		zap.Uint64("lastBlock", latestHeight),
	)

	return filterID, nil
}

// eth_newPendingTransactionFilter creates a filter for pending transactions
// https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_newpendingtransactionfilter
func (h *Handler) ethNewPendingTransactionFilter(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	// Get current block height
	latestHeight, err := h.storage.GetLatestHeight(ctx)
	if err != nil && err != storage.ErrNotFound {
		h.logger.Error("failed to get latest height", zap.Error(err))
		return nil, NewError(InternalError, "failed to get latest height", err.Error())
	}

	// Create pending transaction filter (decode=false since tx filters don't return logs)
	filterID := h.filterManager.NewFilter(PendingTxFilterType, nil, latestHeight, false)

	h.logger.Debug("created new pending transaction filter",
		zap.String("id", filterID),
	)

	return filterID, nil
}

// eth_uninstallFilter removes a filter
// https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_uninstallfilter
func (h *Handler) ethUninstallFilter(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []string
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing filter ID", nil)
	}

	filterID := p[0]
	removed := h.filterManager.RemoveFilter(filterID)

	h.logger.Debug("uninstalled filter",
		zap.String("id", filterID),
		zap.Bool("existed", removed),
	)

	return removed, nil
}

// eth_getFilterChanges returns changes since last poll for a filter
// https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_getfilterchanges
func (h *Handler) ethGetFilterChanges(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []string
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing filter ID", nil)
	}

	filterID := p[0]

	// Get filter
	filter, exists := h.filterManager.GetFilter(filterID)
	if !exists {
		return nil, NewError(FilterNotFound, fmt.Sprintf("filter %s not found", filterID), nil)
	}

	switch filter.Type {
	case LogFilterType:
		// Get new logs since last poll
		logs, currentHeight, err := h.filterManager.GetLogsSinceLastPoll(ctx, h.storage, filterID)
		if err != nil {
			h.logger.Error("failed to get filter changes",
				zap.String("filterID", filterID),
				zap.Error(err),
			)
			return nil, NewError(InternalError, "failed to get logs", err.Error())
		}

		// Update last poll block
		h.filterManager.UpdateLastPollBlock(filterID, currentHeight)

		// Convert logs to JSON format with decode setting from filter
		result := make([]interface{}, len(logs))
		for i, log := range logs {
			result[i] = h.logToJSONWithDecode(log, filter.Decode)
		}

		h.logger.Debug("got filter changes",
			zap.String("filterID", filterID),
			zap.String("type", "log"),
			zap.Int("count", len(result)),
			zap.Bool("decode", filter.Decode),
		)

		return result, nil

	case BlockFilterType:
		// Get new block hashes since last poll
		hashes, currentHeight, err := h.filterManager.GetBlockHashesSinceLastPoll(ctx, h.storage, filterID)
		if err != nil {
			h.logger.Error("failed to get filter changes",
				zap.String("filterID", filterID),
				zap.Error(err),
			)
			return nil, NewError(InternalError, "failed to get block hashes", err.Error())
		}

		// Update last poll block
		h.filterManager.UpdateLastPollBlock(filterID, currentHeight)

		// Convert to hex strings
		result := make([]string, len(hashes))
		for i, hash := range hashes {
			result[i] = hash.Hex()
		}

		h.logger.Debug("got filter changes",
			zap.String("filterID", filterID),
			zap.String("type", "block"),
			zap.Int("count", len(result)),
		)

		return result, nil

	case PendingTxFilterType:
		// Get new pending transactions since last poll
		txHashes, err := h.filterManager.GetPendingTransactionsSinceLastPoll(ctx, h.storage, filterID)
		if err != nil {
			h.logger.Error("failed to get filter changes",
				zap.String("filterID", filterID),
				zap.Error(err),
			)
			return nil, NewError(InternalError, "failed to get pending transactions", err.Error())
		}

		// Convert to hex strings
		result := make([]string, len(txHashes))
		for i, hash := range txHashes {
			result[i] = hash.Hex()
		}

		h.logger.Debug("got filter changes",
			zap.String("filterID", filterID),
			zap.String("type", "pendingTx"),
			zap.Int("count", len(result)),
		)

		return result, nil

	default:
		return nil, NewError(InternalError, "unknown filter type", nil)
	}
}

// eth_getFilterLogs returns all logs matching a filter
// https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_getfilterlogs
func (h *Handler) ethGetFilterLogs(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []string
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing filter ID", nil)
	}

	filterID := p[0]

	// Get filter
	filter, exists := h.filterManager.GetFilter(filterID)
	if !exists {
		return nil, NewError(FilterNotFound, fmt.Sprintf("filter %s not found", filterID), nil)
	}

	if filter.Type != LogFilterType {
		return nil, NewError(InvalidParams, "filter is not a log filter", nil)
	}

	// Get all logs matching the filter (not just changes)
	logs, err := h.storage.GetLogs(ctx, filter.LogFilter)
	if err != nil {
		h.logger.Error("failed to get filter logs",
			zap.String("filterID", filterID),
			zap.Error(err),
		)
		return nil, NewError(InternalError, "failed to get logs", err.Error())
	}

	// Convert logs to JSON format with decode setting from filter
	result := make([]interface{}, len(logs))
	for i, log := range logs {
		result[i] = h.logToJSONWithDecode(log, filter.Decode)
	}

	h.logger.Debug("got filter logs",
		zap.String("filterID", filterID),
		zap.Int("count", len(result)),
		zap.Bool("decode", filter.Decode),
	)

	return result, nil
}

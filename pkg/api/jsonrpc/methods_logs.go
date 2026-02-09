package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// eth_getLogs implements the Ethereum JSON-RPC eth_getLogs method
// https://ethereum.org/en/developers/docs/apis/json-rpc/#eth_getlogs
func (h *Handler) ethGetLogs(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []struct {
		FromBlock interface{}   `json:"fromBlock,omitempty"`
		ToBlock   interface{}   `json:"toBlock,omitempty"`
		Address   interface{}   `json:"address,omitempty"`
		Topics    []interface{} `json:"topics,omitempty"`
		BlockHash *string       `json:"blockHash,omitempty"`
		Decode    *bool         `json:"decode,omitempty"` // Optional: decode logs using ABI
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing filter parameter", nil)
	}

	filterParam := p[0]

	// blockHash and block range are mutually exclusive
	if filterParam.BlockHash != nil && (filterParam.FromBlock != nil || filterParam.ToBlock != nil) {
		return nil, NewError(InvalidParams, "cannot specify both blockHash and block range", nil)
	}

	// Build filter
	filter := &storage.LogFilter{}

	// Handle blockHash case
	if filterParam.BlockHash != nil {
		blockHash := common.HexToHash(*filterParam.BlockHash)
		block, err := h.storage.GetBlockByHash(ctx, blockHash)
		if err != nil {
			if err == storage.ErrNotFound {
				return nil, NewError(InternalError, "block not found", nil)
			}
			h.logger.Error("failed to get block by hash", zap.String("hash", *filterParam.BlockHash), zap.Error(err))
			return nil, NewError(InternalError, "failed to get block", err.Error())
		}
		blockNum := block.Number().Uint64()
		filter.FromBlock = blockNum
		filter.ToBlock = blockNum
	} else {
		// Parse fromBlock
		if filterParam.FromBlock != nil {
			fromBlock, err := h.parseBlockNumber(filterParam.FromBlock)
			if err != nil {
				return nil, NewError(InvalidParams, "invalid fromBlock", err.Error())
			}
			filter.FromBlock = fromBlock
		} else {
			filter.FromBlock = 0 // Default to genesis
		}

		// Parse toBlock
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

	// Get logs from storage
	logs, err := h.storage.GetLogs(ctx, filter)
	if err != nil {
		h.logger.Error("failed to get logs", zap.Error(err))
		return nil, NewError(InternalError, "failed to get logs", err.Error())
	}

	// Check if decoding is requested
	decode := false
	if filterParam.Decode != nil {
		decode = *filterParam.Decode
	}

	// Convert logs to JSON format
	result := make([]interface{}, len(logs))
	for i, log := range logs {
		result[i] = h.logToJSONWithDecode(log, decode)
	}

	return result, nil
}

// parseBlockNumber parses a block number parameter
// Supports: "earliest", "latest", "pending", hex number, decimal number
func (h *Handler) parseBlockNumber(blockParam interface{}) (uint64, error) {
	switch v := blockParam.(type) {
	case string:
		switch v {
		case "earliest":
			return 0, nil
		case "latest":
			latestHeight, err := h.storage.GetLatestHeight(context.Background())
			if err != nil && err != storage.ErrNotFound {
				return 0, fmt.Errorf("failed to get latest height: %w", err)
			}
			return latestHeight, nil
		case "pending":
			// For now, treat pending as latest
			latestHeight, err := h.storage.GetLatestHeight(context.Background())
			if err != nil && err != storage.ErrNotFound {
				return 0, fmt.Errorf("failed to get latest height: %w", err)
			}
			return latestHeight, nil
		default:
			// Try parsing as hex
			if len(v) > 2 && v[:2] == "0x" {
				var blockNum uint64
				if _, err := fmt.Sscanf(v, "0x%x", &blockNum); err != nil {
					return 0, fmt.Errorf("invalid block number format: %s", v)
				}
				return blockNum, nil
			}
			// Try parsing as decimal
			var blockNum uint64
			if _, err := fmt.Sscanf(v, "%d", &blockNum); err != nil {
				return 0, fmt.Errorf("invalid block number format: %s", v)
			}
			return blockNum, nil
		}
	case float64:
		return uint64(v), nil
	default:
		return 0, fmt.Errorf("invalid block number type: %T", blockParam)
	}
}

// logToJSONWithDecode converts a log to JSON-friendly format with optional decoding
func (h *Handler) logToJSONWithDecode(log *types.Log, decode bool) map[string]interface{} {
	topics := make([]interface{}, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.Hex()
	}

	result := map[string]interface{}{
		"address":          log.Address.Hex(),
		"topics":           topics,
		"data":             fmt.Sprintf("0x%x", log.Data),
		"blockNumber":      fmt.Sprintf("0x%x", log.BlockNumber),
		"transactionHash":  log.TxHash.Hex(),
		"transactionIndex": fmt.Sprintf("0x%x", log.TxIndex),
		"blockHash":        log.BlockHash.Hex(),
		"logIndex":         fmt.Sprintf("0x%x", log.Index),
		"removed":          log.Removed,
	}

	// Optionally decode the log if ABI is available
	if decode && h.abiDecoder.HasABI(log.Address) {
		decoded, err := h.abiDecoder.DecodeLog(log)
		if err == nil {
			result["decoded"] = map[string]interface{}{
				"eventName": decoded.EventName,
				"args":      decoded.Args,
			}
		}
		// Silently ignore decode errors - not all logs may be decodable
	}

	return result
}

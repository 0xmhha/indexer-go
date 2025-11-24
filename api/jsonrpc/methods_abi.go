package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"

	abiDecoder "github.com/0xmhha/indexer-go/abi"
	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// setContractABI stores an ABI for a contract
func (h *Handler) setContractABI(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []struct {
		Address common.Address `json:"address"`
		Name    string         `json:"name"`
		ABI     string         `json:"abi"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing parameters", nil)
	}

	param := p[0]

	// Validate required fields
	if param.Address == (common.Address{}) {
		return nil, NewError(InvalidParams, "address is required", nil)
	}

	if param.ABI == "" {
		return nil, NewError(InvalidParams, "ABI is required", nil)
	}

	// Validate ABI JSON
	if err := abiDecoder.ValidateABI(param.ABI); err != nil {
		return nil, NewError(InvalidParams, "invalid ABI", err.Error())
	}

	// Store ABI
	if err := h.storage.SetABI(ctx, param.Address, []byte(param.ABI)); err != nil {
		h.logger.Error("failed to set ABI",
			zap.String("address", param.Address.Hex()),
			zap.Error(err),
		)
		return nil, NewError(InternalError, "failed to store ABI", err.Error())
	}

	// Load ABI into decoder
	if err := h.abiDecoder.LoadABI(param.Address, param.Name, param.ABI); err != nil {
		h.logger.Warn("failed to load ABI into decoder",
			zap.String("address", param.Address.Hex()),
			zap.Error(err),
		)
		// Don't fail the request, ABI is already stored
	}

	h.logger.Info("ABI stored successfully",
		zap.String("address", param.Address.Hex()),
		zap.String("name", param.Name),
	)

	return map[string]interface{}{
		"success": true,
		"address": param.Address.Hex(),
	}, nil
}

// getContractABI retrieves an ABI for a contract
func (h *Handler) getContractABI(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []struct {
		Address common.Address `json:"address"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing address parameter", nil)
	}

	address := p[0].Address

	// Get ABI from storage
	abiJSON, err := h.storage.GetABI(ctx, address)
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, NewError(InternalError, fmt.Sprintf("ABI not found for contract %s", address.Hex()), nil)
		}
		h.logger.Error("failed to get ABI",
			zap.String("address", address.Hex()),
			zap.Error(err),
		)
		return nil, NewError(InternalError, "failed to get ABI", err.Error())
	}

	// Parse ABI JSON to get contract name and events/methods info
	var abiArray []interface{}
	if err := json.Unmarshal(abiJSON, &abiArray); err != nil {
		h.logger.Error("failed to parse ABI JSON",
			zap.String("address", address.Hex()),
			zap.Error(err),
		)
		return nil, NewError(InternalError, "failed to parse ABI", err.Error())
	}

	// Extract events and methods
	events, err := abiDecoder.ExtractEventsFromABI(string(abiJSON))
	if err != nil {
		h.logger.Warn("failed to extract events from ABI",
			zap.String("address", address.Hex()),
			zap.Error(err),
		)
	}

	methods, err := abiDecoder.ExtractMethodsFromABI(string(abiJSON))
	if err != nil {
		h.logger.Warn("failed to extract methods from ABI",
			zap.String("address", address.Hex()),
			zap.Error(err),
		)
	}

	// Convert event signatures to hex strings
	eventsMap := make(map[string]string)
	for name, sig := range events {
		eventsMap[name] = sig.Hex()
	}

	// Convert method selectors to hex strings
	methodsMap := make(map[string]string)
	for name, selector := range methods {
		methodsMap[name] = common.Bytes2Hex(selector)
	}

	return map[string]interface{}{
		"address": address.Hex(),
		"abi":     abiArray,
		"events":  eventsMap,
		"methods": methodsMap,
	}, nil
}

// deleteContractABI removes an ABI for a contract
func (h *Handler) deleteContractABI(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []struct {
		Address common.Address `json:"address"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing address parameter", nil)
	}

	address := p[0].Address

	// Delete ABI from storage
	if err := h.storage.DeleteABI(ctx, address); err != nil {
		h.logger.Error("failed to delete ABI",
			zap.String("address", address.Hex()),
			zap.Error(err),
		)
		return nil, NewError(InternalError, "failed to delete ABI", err.Error())
	}

	// Unload ABI from decoder
	h.abiDecoder.UnloadABI(address)

	h.logger.Info("ABI deleted successfully",
		zap.String("address", address.Hex()),
	)

	return map[string]interface{}{
		"success": true,
		"address": address.Hex(),
	}, nil
}

// listContractABIs returns all contracts that have ABIs
func (h *Handler) listContractABIs(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	// Get all contracts with ABIs
	addresses, err := h.storage.ListABIs(ctx)
	if err != nil {
		h.logger.Error("failed to list ABIs", zap.Error(err))
		return nil, NewError(InternalError, "failed to list ABIs", err.Error())
	}

	// Convert addresses to hex strings
	result := make([]string, len(addresses))
	for i, addr := range addresses {
		result[i] = addr.Hex()
	}

	return map[string]interface{}{
		"contracts": result,
		"count":     len(result),
	}, nil
}

// decodeLog decodes a single log using the contract's ABI
func (h *Handler) decodeLog(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p []struct {
		Address     common.Address `json:"address"`
		Topics      []string       `json:"topics"`
		Data        string         `json:"data"`
		BlockNumber uint64         `json:"blockNumber"`
		TxHash      string         `json:"txHash"`
		TxIndex     uint           `json:"txIndex"`
		BlockHash   string         `json:"blockHash"`
		LogIndex    uint           `json:"logIndex"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if len(p) == 0 {
		return nil, NewError(InvalidParams, "missing log parameter", nil)
	}

	param := p[0]

	// Convert topics from hex strings
	topics := make([]common.Hash, len(param.Topics))
	for i, topic := range param.Topics {
		topics[i] = common.HexToHash(topic)
	}

	// Convert data from hex string
	data := common.FromHex(param.Data)

	// Create log object
	log := &types.Log{
		Address:     param.Address,
		Topics:      topics,
		Data:        data,
		BlockNumber: param.BlockNumber,
		TxHash:      common.HexToHash(param.TxHash),
		TxIndex:     param.TxIndex,
		BlockHash:   common.HexToHash(param.BlockHash),
		Index:       param.LogIndex,
	}

	// Decode log
	decoded, err := h.abiDecoder.DecodeLog(log)
	if err != nil {
		return nil, NewError(InternalError, "failed to decode log", err.Error())
	}

	return decoded, nil
}

package jsonrpc

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// getSetCodeAuthorization returns a specific SetCode authorization by tx hash and auth index
func (h *Handler) getSetCodeAuthorization(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		TxHash    string `json:"txHash"`
		AuthIndex int    `json:"authIndex"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.TxHash == "" {
		return nil, NewError(InvalidParams, "missing required parameter: txHash", nil)
	}

	txHash := common.HexToHash(p.TxHash)

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := h.storage.(storage.SetCodeIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support SetCode queries", nil)
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByTx(ctx, txHash)
	if err != nil {
		h.logger.Error("failed to get SetCode authorization",
			zap.String("txHash", p.TxHash),
			zap.Int("authIndex", p.AuthIndex),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get SetCode authorization", err.Error())
	}

	for _, record := range records {
		if record.AuthIndex == p.AuthIndex {
			return h.setCodeAuthorizationToJSON(record), nil
		}
	}

	return nil, nil // Not found
}

// getSetCodeAuthorizationsByTx returns all SetCode authorizations in a transaction
func (h *Handler) getSetCodeAuthorizationsByTx(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		TxHash string `json:"txHash"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.TxHash == "" {
		return nil, NewError(InvalidParams, "missing required parameter: txHash", nil)
	}

	txHash := common.HexToHash(p.TxHash)

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := h.storage.(storage.SetCodeIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support SetCode queries", nil)
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByTx(ctx, txHash)
	if err != nil {
		h.logger.Error("failed to get SetCode authorizations by tx",
			zap.String("txHash", p.TxHash),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get SetCode authorizations", err.Error())
	}

	result := make([]interface{}, len(records))
	for i, record := range records {
		result[i] = h.setCodeAuthorizationToJSON(record)
	}

	return result, nil
}

// getSetCodeAuthorizationsByTarget returns SetCode authorizations where address is the target
func (h *Handler) getSetCodeAuthorizationsByTarget(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Target string `json:"target"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Target == "" {
		return nil, NewError(InvalidParams, "missing required parameter: target", nil)
	}

	target := common.HexToAddress(p.Target)

	// Default pagination
	limit := 100
	offset := 0
	if p.Limit > 0 && p.Limit <= 1000 {
		limit = p.Limit
	}
	if p.Offset >= 0 {
		offset = p.Offset
	}

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := h.storage.(storage.SetCodeIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support SetCode queries", nil)
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByTarget(ctx, target, limit, offset)
	if err != nil {
		h.logger.Error("failed to get SetCode authorizations by target",
			zap.String("target", p.Target),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get SetCode authorizations", err.Error())
	}

	result := make([]interface{}, len(records))
	for i, record := range records {
		result[i] = h.setCodeAuthorizationToJSON(record)
	}

	return map[string]interface{}{
		"authorizations": result,
		"count":          len(result),
		"limit":          limit,
		"offset":         offset,
	}, nil
}

// getSetCodeAuthorizationsByAuthority returns SetCode authorizations where address is the authority
func (h *Handler) getSetCodeAuthorizationsByAuthority(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Authority string `json:"authority"`
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Authority == "" {
		return nil, NewError(InvalidParams, "missing required parameter: authority", nil)
	}

	authority := common.HexToAddress(p.Authority)

	// Default pagination
	limit := 100
	offset := 0
	if p.Limit > 0 && p.Limit <= 1000 {
		limit = p.Limit
	}
	if p.Offset >= 0 {
		offset = p.Offset
	}

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := h.storage.(storage.SetCodeIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support SetCode queries", nil)
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByAuthority(ctx, authority, limit, offset)
	if err != nil {
		h.logger.Error("failed to get SetCode authorizations by authority",
			zap.String("authority", p.Authority),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get SetCode authorizations", err.Error())
	}

	result := make([]interface{}, len(records))
	for i, record := range records {
		result[i] = h.setCodeAuthorizationToJSON(record)
	}

	return map[string]interface{}{
		"authorizations": result,
		"count":          len(result),
		"limit":          limit,
		"offset":         offset,
	}, nil
}

// getAddressSetCodeInfo returns SetCode information for an address
func (h *Handler) getAddressSetCodeInfo(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address string `json:"address"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Address == "" {
		return nil, NewError(InvalidParams, "missing required parameter: address", nil)
	}

	address := common.HexToAddress(p.Address)

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := h.storage.(storage.SetCodeIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support SetCode queries", nil)
	}

	// Get delegation state
	delegationState, err := setCodeReader.GetAddressDelegationState(ctx, address)
	if err != nil {
		h.logger.Warn("failed to get delegation state",
			zap.String("address", p.Address),
			zap.Error(err))
		// Continue with default values
	}

	// Get stats
	stats, err := setCodeReader.GetAddressSetCodeStats(ctx, address)
	if err != nil {
		h.logger.Warn("failed to get SetCode stats",
			zap.String("address", p.Address),
			zap.Error(err))
		// Continue with default values
	}

	result := map[string]interface{}{
		"address":               p.Address,
		"hasDelegation":         false,
		"delegationTarget":      nil,
		"asTargetCount":         0,
		"asAuthorityCount":      0,
		"lastActivityBlock":     nil,
		"lastActivityTimestamp": nil,
	}

	if delegationState != nil {
		result["hasDelegation"] = delegationState.HasDelegation
		if delegationState.DelegationTarget != nil {
			result["delegationTarget"] = delegationState.DelegationTarget.Hex()
		}
	}

	if stats != nil {
		result["asTargetCount"] = stats.AsTargetCount
		result["asAuthorityCount"] = stats.AsAuthorityCount
		if stats.LastActivityBlock > 0 {
			result["lastActivityBlock"] = strconv.FormatUint(stats.LastActivityBlock, 10)
		}
	}

	return result, nil
}

// getSetCodeTransactionsInBlock returns SetCode transactions in a specific block
func (h *Handler) getSetCodeTransactionsInBlock(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		BlockNumber interface{} `json:"blockNumber"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.BlockNumber == nil {
		return nil, NewError(InvalidParams, "missing required parameter: blockNumber", nil)
	}

	// Parse block number (can be string or number)
	var blockNumber uint64
	switch v := p.BlockNumber.(type) {
	case float64:
		blockNumber = uint64(v)
	case string:
		num, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, NewError(InvalidParams, "invalid block number format", err.Error())
		}
		blockNumber = num
	default:
		return nil, NewError(InvalidParams, "block number must be a string or number", nil)
	}

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := h.storage.(storage.SetCodeIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support SetCode queries", nil)
	}

	records, err := setCodeReader.GetSetCodeAuthorizationsByBlock(ctx, blockNumber)
	if err != nil {
		h.logger.Error("failed to get SetCode authorizations by block",
			zap.Uint64("blockNumber", blockNumber),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get SetCode transactions", err.Error())
	}

	// Collect unique transaction hashes
	txHashes := make(map[common.Hash]bool)
	for _, record := range records {
		txHashes[record.TxHash] = true
	}

	// Fetch transactions
	result := make([]interface{}, 0, len(txHashes))
	for txHash := range txHashes {
		tx, location, err := h.storage.GetTransaction(ctx, txHash)
		if err != nil {
			h.logger.Warn("failed to get transaction",
				zap.String("txHash", txHash.Hex()),
				zap.Error(err))
			continue
		}
		if tx != nil {
			result = append(result, h.transactionToJSON(tx, location))
		}
	}

	return result, nil
}

// getRecentSetCodeTransactions returns recent SetCode transactions
func (h *Handler) getRecentSetCodeTransactions(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Limit int `json:"limit"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	limit := 10
	if p.Limit > 0 && p.Limit <= 100 {
		limit = p.Limit
	}

	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := h.storage.(storage.SetCodeIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support SetCode queries", nil)
	}

	records, err := setCodeReader.GetRecentSetCodeAuthorizations(ctx, limit*2)
	if err != nil {
		h.logger.Error("failed to get recent SetCode authorizations",
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get recent SetCode transactions", err.Error())
	}

	// Collect unique transaction hashes (up to limit)
	txHashes := make([]common.Hash, 0, limit)
	seen := make(map[common.Hash]bool)
	for _, record := range records {
		if !seen[record.TxHash] {
			seen[record.TxHash] = true
			txHashes = append(txHashes, record.TxHash)
			if len(txHashes) >= limit {
				break
			}
		}
	}

	// Fetch transactions
	result := make([]interface{}, 0, len(txHashes))
	for _, txHash := range txHashes {
		tx, location, err := h.storage.GetTransaction(ctx, txHash)
		if err != nil {
			h.logger.Warn("failed to get transaction",
				zap.String("txHash", txHash.Hex()),
				zap.Error(err))
			continue
		}
		if tx != nil {
			result = append(result, h.transactionToJSON(tx, location))
		}
	}

	return result, nil
}

// getSetCodeTransactionCount returns the total count of SetCode transactions
func (h *Handler) getSetCodeTransactionCount(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	// Cast storage to SetCodeIndexReader
	setCodeReader, ok := h.storage.(storage.SetCodeIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support SetCode queries", nil)
	}

	count, err := setCodeReader.GetSetCodeTransactionCount(ctx)
	if err != nil {
		h.logger.Error("failed to get SetCode transaction count",
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get SetCode transaction count", err.Error())
	}

	return map[string]interface{}{
		"count": count,
	}, nil
}

// setCodeAuthorizationToJSON converts a SetCodeAuthorizationRecord to JSON format
func (h *Handler) setCodeAuthorizationToJSON(record *storage.SetCodeAuthorizationRecord) map[string]interface{} {
	result := map[string]interface{}{
		"txHash":             record.TxHash.Hex(),
		"blockNumber":        strconv.FormatUint(record.BlockNumber, 10),
		"blockHash":          record.BlockHash.Hex(),
		"transactionIndex":   record.TxIndex,
		"authorizationIndex": record.AuthIndex,
		"chainId":            record.ChainID.String(),
		"address":            record.TargetAddress.Hex(),
		"nonce":              strconv.FormatUint(record.Nonce, 10),
		"yParity":            record.YParity,
		"r":                  "0x" + record.R.Text(16),
		"s":                  "0x" + record.S.Text(16),
		"applied":            record.Applied,
		"timestamp":          strconv.FormatInt(record.Timestamp.Unix(), 10),
	}

	if record.AuthorityAddress != (common.Address{}) {
		result["authority"] = record.AuthorityAddress.Hex()
	}

	if record.Error != "" && record.Error != storage.SetCodeErrNone {
		result["error"] = record.Error
	}

	return result
}

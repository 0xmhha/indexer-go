package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/internal/constants"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// ========== Contract Creation Methods ==========

// getContractCreation returns contract creation information by contract address
func (h *Handler) getContractCreation(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address string `json:"address"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.Address == "" {
		return nil, NewError(InvalidParams, "address is required", nil)
	}

	address := common.HexToAddress(p.Address)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	creation, err := addressReader.GetContractCreation(ctx, address)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get contract creation", zap.String("address", p.Address), zap.Error(err))
		return nil, NewError(InternalError, "failed to get contract creation", err.Error())
	}

	return contractCreationToMap(creation), nil
}

// getContractsByCreator returns contracts created by a specific address
func (h *Handler) getContractsByCreator(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Creator string `json:"creator"`
		Limit   *int   `json:"limit,omitempty"`
		Offset  *int   `json:"offset,omitempty"`
		After   string `json:"after,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.Creator == "" {
		return nil, NewError(InvalidParams, "creator is required", nil)
	}

	creator := common.HexToAddress(p.Creator)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	contracts, next, err := addressReader.GetContractsByCreator(ctx, creator, addressListPage(p.Limit, p.Offset, p.After))
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, NewError(InvalidParams, "invalid pagination cursor", nil)
		}
		h.logger.Error("failed to get contracts by creator", zap.String("creator", p.Creator), zap.Error(err))
		return nil, NewError(InternalError, "failed to get contracts by creator", err.Error())
	}

	// Get full contract creation info for each contract
	results := make([]interface{}, 0, len(contracts))
	for _, contractAddr := range contracts {
		creation, err := addressReader.GetContractCreation(ctx, contractAddr)
		if err != nil {
			h.logger.Warn("failed to get contract creation details",
				zap.String("contract", contractAddr.Hex()),
				zap.Error(err))
			continue
		}
		results = append(results, contractCreationToMap(creation))
	}

	return map[string]interface{}{
		"contracts":  results,
		"total":      len(results),
		"nextCursor": addressNextCursor(next),
	}, nil
}

// ========== Internal Transaction Methods ==========

// getInternalTransactions returns internal transactions for a transaction hash
func (h *Handler) getInternalTransactions(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		TxHash string `json:"txHash"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.TxHash == "" {
		return nil, NewError(InvalidParams, "txHash is required", nil)
	}

	txHash := common.HexToHash(p.TxHash)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	internals, err := addressReader.GetInternalTransactions(ctx, txHash)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return map[string]interface{}{
				"internals": []interface{}{},
			}, nil
		}
		h.logger.Error("failed to get internal transactions", zap.String("txHash", p.TxHash), zap.Error(err))
		return nil, NewError(InternalError, "failed to get internal transactions", err.Error())
	}

	results := make([]interface{}, len(internals))
	for i, internal := range internals {
		results[i] = internalTransactionToMap(internal)
	}

	return map[string]interface{}{
		"internals": results,
	}, nil
}

// getInternalTransactionsByAddress returns internal transactions involving a specific address
func (h *Handler) getInternalTransactionsByAddress(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address string `json:"address"`
		IsFrom  bool   `json:"isFrom"`
		Limit   *int   `json:"limit,omitempty"`
		Offset  *int   `json:"offset,omitempty"`
		After   string `json:"after,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.Address == "" {
		return nil, NewError(InvalidParams, "address is required", nil)
	}

	address := common.HexToAddress(p.Address)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	internals, next, err := addressReader.GetInternalTransactionsByAddress(ctx, address, p.IsFrom, addressListPage(p.Limit, p.Offset, p.After))
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, NewError(InvalidParams, "invalid pagination cursor", nil)
		}
		h.logger.Error("failed to get internal transactions by address",
			zap.String("address", p.Address),
			zap.Bool("isFrom", p.IsFrom),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get internal transactions by address", err.Error())
	}

	results := make([]interface{}, len(internals))
	for i, internal := range internals {
		results[i] = internalTransactionToMap(internal)
	}

	return map[string]interface{}{
		"internals":  results,
		"total":      len(results),
		"nextCursor": addressNextCursor(next),
	}, nil
}

// ========== ERC20 Transfer Methods ==========

// getERC20Transfer returns ERC20 transfer by transaction hash and log index
func (h *Handler) getERC20Transfer(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		TxHash   string `json:"txHash"`
		LogIndex uint   `json:"logIndex"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.TxHash == "" {
		return nil, NewError(InvalidParams, "txHash is required", nil)
	}

	txHash := common.HexToHash(p.TxHash)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	transfer, err := addressReader.GetERC20Transfer(ctx, txHash, p.LogIndex)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get ERC20 transfer",
			zap.String("txHash", p.TxHash),
			zap.Uint("logIndex", p.LogIndex),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get ERC20 transfer", err.Error())
	}

	return erc20TransferToMap(transfer), nil
}

// getERC20TransfersByToken returns ERC20 transfers for a specific token contract
func (h *Handler) getERC20TransfersByToken(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Token  string `json:"token"`
		Limit  *int   `json:"limit,omitempty"`
		Offset *int   `json:"offset,omitempty"`
		After  string `json:"after,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.Token == "" {
		return nil, NewError(InvalidParams, "token is required", nil)
	}

	token := common.HexToAddress(p.Token)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	transfers, next, err := addressReader.GetERC20TransfersByToken(ctx, token, addressListPage(p.Limit, p.Offset, p.After))
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, NewError(InvalidParams, "invalid pagination cursor", nil)
		}
		h.logger.Error("failed to get ERC20 transfers by token", zap.String("token", p.Token), zap.Error(err))
		return nil, NewError(InternalError, "failed to get ERC20 transfers by token", err.Error())
	}

	results := make([]interface{}, len(transfers))
	for i, transfer := range transfers {
		results[i] = erc20TransferToMap(transfer)
	}

	return map[string]interface{}{
		"transfers":  results,
		"total":      len(results),
		"nextCursor": addressNextCursor(next),
	}, nil
}

// getERC20TransfersByAddress returns ERC20 transfers involving a specific address
func (h *Handler) getERC20TransfersByAddress(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address string `json:"address"`
		IsFrom  bool   `json:"isFrom"`
		Limit   *int   `json:"limit,omitempty"`
		Offset  *int   `json:"offset,omitempty"`
		After   string `json:"after,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.Address == "" {
		return nil, NewError(InvalidParams, "address is required", nil)
	}

	address := common.HexToAddress(p.Address)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	transfers, next, err := addressReader.GetERC20TransfersByAddress(ctx, address, p.IsFrom, addressListPage(p.Limit, p.Offset, p.After))
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, NewError(InvalidParams, "invalid pagination cursor", nil)
		}
		h.logger.Error("failed to get ERC20 transfers by address",
			zap.String("address", p.Address),
			zap.Bool("isFrom", p.IsFrom),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get ERC20 transfers by address", err.Error())
	}

	results := make([]interface{}, len(transfers))
	for i, transfer := range transfers {
		results[i] = erc20TransferToMap(transfer)
	}

	return map[string]interface{}{
		"transfers":  results,
		"total":      len(results),
		"nextCursor": addressNextCursor(next),
	}, nil
}

// ========== ERC721 Transfer Methods ==========

// getERC721Transfer returns ERC721 transfer by transaction hash and log index
func (h *Handler) getERC721Transfer(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		TxHash   string `json:"txHash"`
		LogIndex uint   `json:"logIndex"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.TxHash == "" {
		return nil, NewError(InvalidParams, "txHash is required", nil)
	}

	txHash := common.HexToHash(p.TxHash)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	transfer, err := addressReader.GetERC721Transfer(ctx, txHash, p.LogIndex)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get ERC721 transfer",
			zap.String("txHash", p.TxHash),
			zap.Uint("logIndex", p.LogIndex),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get ERC721 transfer", err.Error())
	}

	return erc721TransferToMap(transfer), nil
}

// getERC721TransfersByToken returns ERC721 transfers for a specific NFT contract
func (h *Handler) getERC721TransfersByToken(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Token  string `json:"token"`
		Limit  *int   `json:"limit,omitempty"`
		Offset *int   `json:"offset,omitempty"`
		After  string `json:"after,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.Token == "" {
		return nil, NewError(InvalidParams, "token is required", nil)
	}

	token := common.HexToAddress(p.Token)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	transfers, next, err := addressReader.GetERC721TransfersByToken(ctx, token, addressListPage(p.Limit, p.Offset, p.After))
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, NewError(InvalidParams, "invalid pagination cursor", nil)
		}
		h.logger.Error("failed to get ERC721 transfers by token", zap.String("token", p.Token), zap.Error(err))
		return nil, NewError(InternalError, "failed to get ERC721 transfers by token", err.Error())
	}

	results := make([]interface{}, len(transfers))
	for i, transfer := range transfers {
		results[i] = erc721TransferToMap(transfer)
	}

	return map[string]interface{}{
		"transfers":  results,
		"total":      len(results),
		"nextCursor": addressNextCursor(next),
	}, nil
}

// getERC721TransfersByAddress returns ERC721 transfers involving a specific address
func (h *Handler) getERC721TransfersByAddress(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Address string `json:"address"`
		IsFrom  bool   `json:"isFrom"`
		Limit   *int   `json:"limit,omitempty"`
		Offset  *int   `json:"offset,omitempty"`
		After   string `json:"after,omitempty"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.Address == "" {
		return nil, NewError(InvalidParams, "address is required", nil)
	}

	address := common.HexToAddress(p.Address)

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	transfers, next, err := addressReader.GetERC721TransfersByAddress(ctx, address, p.IsFrom, addressListPage(p.Limit, p.Offset, p.After))
	if err != nil {
		if errors.Is(err, port.ErrInvalidCursor) {
			return nil, NewError(InvalidParams, "invalid pagination cursor", nil)
		}
		h.logger.Error("failed to get ERC721 transfers by address",
			zap.String("address", p.Address),
			zap.Bool("isFrom", p.IsFrom),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get ERC721 transfers by address", err.Error())
	}

	results := make([]interface{}, len(transfers))
	for i, transfer := range transfers {
		results[i] = erc721TransferToMap(transfer)
	}

	return map[string]interface{}{
		"transfers":  results,
		"total":      len(results),
		"nextCursor": addressNextCursor(next),
	}, nil
}

// getERC721Owner returns current owner of an NFT token
func (h *Handler) getERC721Owner(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Token   string `json:"token"`
		TokenID string `json:"tokenId"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid parameters", err.Error())
	}

	if p.Token == "" {
		return nil, NewError(InvalidParams, "token is required", nil)
	}
	if p.TokenID == "" {
		return nil, NewError(InvalidParams, "tokenId is required", nil)
	}

	token := common.HexToAddress(p.Token)
	tokenId, ok := new(big.Int).SetString(p.TokenID, 10)
	if !ok {
		return nil, NewError(InvalidParams, "invalid tokenId format", nil)
	}

	// Check if storage implements AddressIndexReader
	addressReader, ok := h.storage.(port.AddressIndexReader)
	if !ok {
		return nil, NewError(InternalError, "storage does not support address indexing", nil)
	}

	owner, err := addressReader.GetERC721Owner(ctx, token, tokenId)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, nil
		}
		h.logger.Error("failed to get ERC721 owner",
			zap.String("token", p.Token),
			zap.String("tokenId", p.TokenID),
			zap.Error(err))
		return nil, NewError(InternalError, "failed to get ERC721 owner", err.Error())
	}

	return map[string]interface{}{
		"owner": owner.Hex(),
	}, nil
}

// ========== Helper mapper functions ==========

// contractCreationToMap converts ContractCreation to a map
func contractCreationToMap(creation *port.ContractCreation) map[string]interface{} {
	return map[string]interface{}{
		"contractAddress": creation.ContractAddress.Hex(),
		"creator":         creation.Creator.Hex(),
		"transactionHash": creation.TransactionHash.Hex(),
		"blockNumber":     fmt.Sprintf("%d", creation.BlockNumber),
		"timestamp":       fmt.Sprintf("%d", creation.Timestamp),
		"bytecodeSize":    creation.BytecodeSize,
	}
}

// internalTransactionToMap converts InternalTransaction to a map
func internalTransactionToMap(internal *port.InternalTransaction) map[string]interface{} {
	m := map[string]interface{}{
		"transactionHash": internal.TransactionHash.Hex(),
		"blockNumber":     fmt.Sprintf("%d", internal.BlockNumber),
		"index":           internal.Index,
		"type":            internal.Type,
		"from":            internal.From.Hex(),
		"to":              internal.To.Hex(),
		"value":           internal.Value.String(),
		"gas":             fmt.Sprintf("%d", internal.Gas),
		"gasUsed":         fmt.Sprintf("%d", internal.GasUsed),
		"input":           fmt.Sprintf("0x%x", internal.Input),
		"output":          fmt.Sprintf("0x%x", internal.Output),
		"depth":           internal.Depth,
	}

	if internal.Error != "" {
		m["error"] = internal.Error
	}

	return m
}

// erc20TransferToMap converts ERC20Transfer to a map
func erc20TransferToMap(transfer *port.ERC20Transfer) map[string]interface{} {
	return map[string]interface{}{
		"contractAddress": transfer.ContractAddress.Hex(),
		"from":            transfer.From.Hex(),
		"to":              transfer.To.Hex(),
		"value":           "0x" + transfer.Value.Text(16), // Hex encoding per JSON-RPC standard
		"transactionHash": transfer.TransactionHash.Hex(),
		"blockNumber":     fmt.Sprintf("%d", transfer.BlockNumber),
		"logIndex":        transfer.LogIndex,
		"timestamp":       fmt.Sprintf("%d", transfer.Timestamp),
	}
}

// erc721TransferToMap converts ERC721Transfer to a map
func erc721TransferToMap(transfer *port.ERC721Transfer) map[string]interface{} {
	return map[string]interface{}{
		"contractAddress": transfer.ContractAddress.Hex(),
		"from":            transfer.From.Hex(),
		"to":              transfer.To.Hex(),
		"tokenId":         "0x" + transfer.TokenId.Text(16), // Hex encoding per JSON-RPC standard
		"transactionHash": transfer.TransactionHash.Hex(),
		"blockNumber":     fmt.Sprintf("%d", transfer.BlockNumber),
		"logIndex":        transfer.LogIndex,
		"timestamp":       fmt.Sprintf("%d", transfer.Timestamp),
	}
}

// addressListPage returns the page request of an address index list
// method from its limit, offset and after parameters: limit defaults to
// constants.DefaultPaginationLimit and is at most
// constants.DefaultMaxPaginationLimit, and after (the nextCursor of the
// previous page) takes precedence over offset.
func addressListPage(limit, offset *int, after string) port.Page {
	page := port.Page{Limit: constants.DefaultPaginationLimit, After: after}
	if limit != nil && *limit > 0 {
		page.Limit = min(*limit, constants.DefaultMaxPaginationLimit)
	}
	if offset != nil && *offset >= 0 {
		page.Offset = *offset
	}
	return page
}

// addressNextCursor returns the nextCursor result field: the cursor of the
// next page, or null when the list has no more items.
func addressNextCursor(next string) interface{} {
	if next == "" {
		return nil
	}
	return next
}

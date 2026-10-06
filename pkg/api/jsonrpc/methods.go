package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	abiDecoder "github.com/0xmhha/indexer-go/pkg/abi"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// Handler handles JSON-RPC method calls
type Handler struct {
	storage       port.QueryStore
	logger        *zap.Logger
	filterManager *FilterManager
	abiDecoder    *abiDecoder.Decoder
}

// NewHandler creates a new JSON-RPC handler
func NewHandler(store port.QueryStore, logger *zap.Logger) *Handler {
	h := &Handler{
		storage:       store,
		logger:        logger,
		filterManager: NewFilterManager(context.Background(), 5*time.Minute), // 5 minute filter timeout
		abiDecoder:    abiDecoder.NewDecoder(),
	}

	// Load all stored ABIs into the decoder
	if err := h.loadStoredABIs(context.Background()); err != nil {
		logger.Warn("failed to load stored ABIs", zap.Error(err))
		// Don't fail initialization, ABIs can be loaded later
	}

	return h
}

// loadStoredABIs loads all ABIs from storage into the decoder
func (h *Handler) loadStoredABIs(ctx context.Context) error {
	addresses, err := h.storage.ListABIs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list ABIs: %w", err)
	}

	loaded := 0
	for _, addr := range addresses {
		abiJSON, err := h.storage.GetABI(ctx, addr)
		if err != nil {
			h.logger.Warn("failed to get ABI",
				zap.String("address", addr.Hex()),
				zap.Error(err),
			)
			continue
		}

		// Load into decoder
		if err := h.abiDecoder.LoadABI(addr, "", string(abiJSON)); err != nil {
			h.logger.Warn("failed to load ABI into decoder",
				zap.String("address", addr.Hex()),
				zap.Error(err),
			)
			continue
		}

		loaded++
	}

	h.logger.Info("loaded ABIs from storage",
		zap.Int("total", len(addresses)),
		zap.Int("loaded", loaded),
	)

	return nil
}

// Close cleans up handler resources
func (h *Handler) Close() {
	if h.filterManager != nil {
		h.filterManager.Close()
	}
}

// HandleMethod handles a JSON-RPC method call
func (h *Handler) HandleMethod(ctx context.Context, method string, params json.RawMessage) (interface{}, *Error) {
	switch method {
	case "getLatestHeight":
		return h.getLatestHeight(ctx, params)
	case "getBlock":
		return h.getBlock(ctx, params)
	case "getBlockByHash":
		return h.getBlockByHash(ctx, params)
	case "getTxResult":
		return h.getTxResult(ctx, params)
	case "getTxReceipt":
		return h.getTxReceipt(ctx, params)
	// Historical data methods
	case "getBlocksByTimeRange":
		return h.getBlocksByTimeRange(ctx, params)
	case "getBlockByTimestamp":
		return h.getBlockByTimestamp(ctx, params)
	case "getTransactionsByAddressFiltered":
		return h.getTransactionsByAddressFiltered(ctx, params)
	case "getAddressBalance":
		return h.getAddressBalance(ctx, params)
	case "getBalanceHistory":
		return h.getBalanceHistory(ctx, params)
	case "getBlockCount":
		return h.getBlockCount(ctx, params)
	case "getTransactionCount":
		return h.getTransactionCount(ctx, params)
	// Address indexing methods
	case "getContractCreation":
		return h.getContractCreation(ctx, params)
	case "getContractsByCreator":
		return h.getContractsByCreator(ctx, params)
	case "getInternalTransactions":
		return h.getInternalTransactions(ctx, params)
	case "getInternalTransactionsByAddress":
		return h.getInternalTransactionsByAddress(ctx, params)
	case "getERC20Transfer":
		return h.getERC20Transfer(ctx, params)
	case "getERC20TransfersByToken":
		return h.getERC20TransfersByToken(ctx, params)
	case "getERC20TransfersByAddress":
		return h.getERC20TransfersByAddress(ctx, params)
	case "getERC721Transfer":
		return h.getERC721Transfer(ctx, params)
	case "getERC721TransfersByToken":
		return h.getERC721TransfersByToken(ctx, params)
	case "getERC721TransfersByAddress":
		return h.getERC721TransfersByAddress(ctx, params)
	case "getERC721Owner":
		return h.getERC721Owner(ctx, params)
	// Ethereum-compatible log filtering methods
	case "eth_getLogs":
		return h.ethGetLogs(ctx, params)
	// Ethereum-compatible filter methods
	case "eth_newFilter":
		return h.ethNewFilter(ctx, params)
	case "eth_newBlockFilter":
		return h.ethNewBlockFilter(ctx, params)
	case "eth_newPendingTransactionFilter":
		return h.ethNewPendingTransactionFilter(ctx, params)
	case "eth_uninstallFilter":
		return h.ethUninstallFilter(ctx, params)
	case "eth_getFilterChanges":
		return h.ethGetFilterChanges(ctx, params)
	case "eth_getFilterLogs":
		return h.ethGetFilterLogs(ctx, params)
	// ABI management methods
	case "setContractABI":
		return h.setContractABI(ctx, params)
	case "getContractABI":
		return h.getContractABI(ctx, params)
	case "deleteContractABI":
		return h.deleteContractABI(ctx, params)
	case "listContractABIs":
		return h.listContractABIs(ctx, params)
	case "decodeLog":
		return h.decodeLog(ctx, params)
	// EIP-7702 SetCode methods
	case "getSetCodeAuthorization":
		return h.getSetCodeAuthorization(ctx, params)
	case "getSetCodeAuthorizationsByTx":
		return h.getSetCodeAuthorizationsByTx(ctx, params)
	case "getSetCodeAuthorizationsByTarget":
		return h.getSetCodeAuthorizationsByTarget(ctx, params)
	case "getSetCodeAuthorizationsByAuthority":
		return h.getSetCodeAuthorizationsByAuthority(ctx, params)
	case "getAddressSetCodeInfo":
		return h.getAddressSetCodeInfo(ctx, params)
	case "getSetCodeTransactionsInBlock":
		return h.getSetCodeTransactionsInBlock(ctx, params)
	case "getRecentSetCodeTransactions":
		return h.getRecentSetCodeTransactions(ctx, params)
	case "getSetCodeTransactionCount":
		return h.getSetCodeTransactionCount(ctx, params)
	// Notification methods
	case "notification_getSettings":
		return h.getNotificationSettings(ctx, params)
	case "notification_getSetting":
		return h.getNotificationSetting(ctx, params)
	case "notification_createSetting":
		return h.createNotificationSetting(ctx, params)
	case "notification_updateSetting":
		return h.updateNotificationSetting(ctx, params)
	case "notification_deleteSetting":
		return h.deleteNotificationSetting(ctx, params)
	case "notification_list":
		return h.getNotifications(ctx, params)
	case "notification_get":
		return h.getNotification(ctx, params)
	case "notification_getStats":
		return h.getNotificationStats(ctx, params)
	case "notification_getHistory":
		return h.getDeliveryHistory(ctx, params)
	case "notification_test":
		return h.testNotificationSetting(ctx, params)
	case "notification_retry":
		return h.retryNotification(ctx, params)
	case "notification_cancel":
		return h.cancelNotification(ctx, params)
	default:
		if fn, ok := registeredMethod(method); ok {
			return fn(ctx, MethodDeps{Storage: h.storage, Logger: h.logger}, params)
		}
		return nil, NewError(MethodNotFound, fmt.Sprintf("method '%s' not found", method), nil)
	}
}

// getLatestHeight returns the latest indexed block height
func (h *Handler) getLatestHeight(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	height, err := h.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return map[string]interface{}{
				"height": uint64(0),
			}, nil
		}
		h.logger.Error("failed to get latest height", zap.Error(err))
		return nil, NewError(InternalError, "failed to get latest height", err.Error())
	}

	return map[string]interface{}{
		"height": height,
	}, nil
}

// getBlock returns a block by number
func (h *Handler) getBlock(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Number interface{} `json:"number"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Number == nil {
		return nil, NewError(InvalidParams, "missing required parameter: number", nil)
	}

	// Parse block number (can be string or number)
	var blockNumber uint64
	switch v := p.Number.(type) {
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

	block, err := h.storage.GetBlock(ctx, blockNumber)
	if err != nil {
		if err == port.ErrNotFound {
			return nil, NewError(InternalError, "block not found", nil)
		}
		h.logger.Error("failed to get block", zap.Uint64("number", blockNumber), zap.Error(err))
		return nil, NewError(InternalError, "failed to get block", err.Error())
	}

	return h.blockToJSON(block), nil
}

// getBlockByHash returns a block by hash
func (h *Handler) getBlockByHash(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Hash string `json:"hash"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Hash == "" {
		return nil, NewError(InvalidParams, "missing required parameter: hash", nil)
	}

	hash := common.HexToHash(p.Hash)
	block, err := h.storage.GetBlockByHash(ctx, hash)
	if err != nil {
		if err == port.ErrNotFound {
			return nil, NewError(InternalError, "block not found", nil)
		}
		h.logger.Error("failed to get block by hash", zap.String("hash", p.Hash), zap.Error(err))
		return nil, NewError(InternalError, "failed to get block", err.Error())
	}

	return h.blockToJSON(block), nil
}

// getTxResult returns a transaction by hash
func (h *Handler) getTxResult(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Hash string `json:"hash"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Hash == "" {
		return nil, NewError(InvalidParams, "missing required parameter: hash", nil)
	}

	hash := common.HexToHash(p.Hash)
	tx, location, err := h.storage.GetTransaction(ctx, hash)
	if err != nil {
		if err == port.ErrNotFound {
			return nil, NewError(InternalError, "transaction not found", nil)
		}
		h.logger.Error("failed to get transaction", zap.String("hash", p.Hash), zap.Error(err))
		return nil, NewError(InternalError, "failed to get transaction", err.Error())
	}

	return h.transactionToJSON(tx, location), nil
}

// getTxReceipt returns a transaction receipt by hash
func (h *Handler) getTxReceipt(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	var p struct {
		Hash string `json:"hash"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, NewError(InvalidParams, "invalid params", err.Error())
	}

	if p.Hash == "" {
		return nil, NewError(InvalidParams, "missing required parameter: hash", nil)
	}

	hash := common.HexToHash(p.Hash)
	receipt, err := h.storage.GetReceipt(ctx, hash)
	if err != nil {
		if err == port.ErrNotFound {
			return nil, NewError(InternalError, "receipt not found", nil)
		}
		h.logger.Error("failed to get receipt", zap.String("hash", p.Hash), zap.Error(err))
		return nil, NewError(InternalError, "failed to get receipt", err.Error())
	}

	return h.receiptToJSON(gethconv.ReceiptToGeth(receipt)), nil
}

// blockToJSON converts a block to JSON-friendly format. Hashes are the ones
// the chain reports.
func (h *Handler) blockToJSON(block *model.Block) map[string]interface{} {
	transactions := make([]interface{}, len(block.Transactions))
	for i, tx := range block.Transactions {
		transactions[i] = tx.Hash.Hex()
	}

	uncleHashes := make([]interface{}, len(block.Uncles))
	for i, uncle := range block.Uncles {
		uncleHashes[i] = uncle.Hex()
	}

	difficulty := block.Difficulty
	if difficulty == nil {
		difficulty = new(big.Int)
	}

	result := map[string]interface{}{
		"number":           fmt.Sprintf("0x%x", block.Number),
		"hash":             block.Hash.Hex(),
		"parentHash":       block.ParentHash.Hex(),
		"nonce":            fmt.Sprintf("0x%x", block.Nonce),
		"sha3Uncles":       block.UncleHash.Hex(),
		"logsBloom":        fmt.Sprintf("0x%x", types.BytesToBloom(block.Bloom).Bytes()),
		"transactionsRoot": block.TxRoot.Hex(),
		"stateRoot":        block.StateRoot.Hex(),
		"receiptsRoot":     block.ReceiptRoot.Hex(),
		"miner":            block.Miner.Hex(),
		"difficulty":       fmt.Sprintf("0x%x", difficulty),
		"totalDifficulty":  nil, // not tracked
		"extraData":        fmt.Sprintf("0x%x", block.Extra),
		"size":             fmt.Sprintf("0x%x", block.Size),
		"gasLimit":         fmt.Sprintf("0x%x", block.GasLimit),
		"gasUsed":          fmt.Sprintf("0x%x", block.GasUsed),
		"timestamp":        fmt.Sprintf("0x%x", block.Time),
		"transactions":     transactions,
		"uncles":           uncleHashes,
	}

	if block.BaseFee != nil {
		result["baseFeePerGas"] = fmt.Sprintf("0x%x", block.BaseFee)
	}
	if block.WithdrawalsRoot != nil {
		result["withdrawalsRoot"] = block.WithdrawalsRoot.Hex()
	}
	if block.BlobGasUsed != nil {
		result["blobGasUsed"] = fmt.Sprintf("0x%x", *block.BlobGasUsed)
	}
	if block.ExcessBlobGas != nil {
		result["excessBlobGas"] = fmt.Sprintf("0x%x", *block.ExcessBlobGas)
	}

	return result
}

// transactionToJSON converts a transaction to JSON-friendly format. The
// hash, type and sender are the ones the chain reports.
func (h *Handler) transactionToJSON(tx *model.Transaction, location *port.TxLocation) map[string]interface{} {
	if location == nil {
		location = &port.TxLocation{}
	}
	result := map[string]interface{}{
		"blockHash":        location.BlockHash.Hex(),
		"blockNumber":      fmt.Sprintf("0x%x", location.BlockHeight),
		"from":             tx.From.Hex(),
		"gas":              fmt.Sprintf("0x%x", tx.Gas),
		"gasPrice":         fmt.Sprintf("0x%x", orZero(tx.GasPrice)),
		"hash":             tx.Hash.Hex(),
		"input":            fmt.Sprintf("0x%x", tx.Input),
		"nonce":            fmt.Sprintf("0x%x", tx.Nonce),
		"to":               nil,
		"contractAddress":  nil,
		"transactionIndex": fmt.Sprintf("0x%x", location.TxIndex),
		"value":            fmt.Sprintf("0x%x", orZero(tx.Value)),
		"type":             fmt.Sprintf("0x%x", tx.Type),
		"v":                fmt.Sprintf("0x%x", orZero(tx.Signature.V)),
		"r":                fmt.Sprintf("0x%x", orZero(tx.Signature.R)),
		"s":                fmt.Sprintf("0x%x", orZero(tx.Signature.S)),
	}

	if tx.To != nil {
		result["to"] = tx.To.Hex()
	} else {
		// Contract creation transaction - look up the receipt to get the contract address
		if h.storage != nil {
			receipt, err := h.storage.GetReceipt(context.Background(), tx.Hash)
			if err == nil && receipt != nil && receipt.ContractAddress != nil {
				result["contractAddress"] = receipt.ContractAddress.Hex()
			}
		}
	}

	// EIP-1559 fields (and later fee-market types). Like nodes, a mined
	// transaction reports the price it paid as gasPrice (its receipt's
	// effective gas price), not its fee cap.
	if tx.Type >= types.DynamicFeeTxType {
		result["maxFeePerGas"] = fmt.Sprintf("0x%x", orZero(tx.GasFeeCap))
		result["maxPriorityFeePerGas"] = fmt.Sprintf("0x%x", orZero(tx.GasTipCap))
		if h.storage != nil && location.BlockHash != (common.Hash{}) {
			if receipt, err := h.storage.GetReceipt(context.Background(), tx.Hash); err == nil && receipt != nil && receipt.EffectiveGasPrice != nil {
				result["gasPrice"] = fmt.Sprintf("0x%x", receipt.EffectiveGasPrice)
			}
		}
	}

	if tx.ChainID != nil {
		result["chainId"] = fmt.Sprintf("0x%x", tx.ChainID)
	}

	// Access list for EIP-2930 and later typed transactions
	if tx.Type >= types.AccessListTxType {
		accessListJSON := make([]interface{}, len(tx.AccessList))
		for i, entry := range tx.AccessList {
			storageKeys := make([]interface{}, len(entry.StorageKeys))
			for j, key := range entry.StorageKeys {
				storageKeys[j] = key.Hex()
			}
			accessListJSON[i] = map[string]interface{}{
				"address":     entry.Address.Hex(),
				"storageKeys": storageKeys,
			}
		}
		result["accessList"] = accessListJSON
	}

	// EIP-7702 SetCode transaction (type 0x04 = 4)
	if tx.Type == types.SetCodeTxType && len(tx.AuthList) > 0 {
		authListJSON := make([]interface{}, len(tx.AuthList))
		for i, auth := range tx.AuthList {
			authListJSON[i] = map[string]interface{}{
				"chainId": fmt.Sprintf("0x%x", orZero(auth.ChainID).Bytes()),
				"address": auth.Address.Hex(),
				"nonce":   fmt.Sprintf("0x%x", auth.Nonce),
				"yParity": fmt.Sprintf("0x%x", auth.V),
				"r":       fmt.Sprintf("0x%x", orZero(auth.R).Bytes()),
				"s":       fmt.Sprintf("0x%x", orZero(auth.S).Bytes()),
			}
		}
		result["authorizationList"] = authListJSON
	}

	// Fee Delegation transaction (type 0x16 = 22). fv/fr/fs are the node's
	// field names; feePayerSignatures is kept for existing clients.
	if feePayer, v, r, s, ok := h.feeDelegation(tx); ok {
		result["feePayer"] = feePayer.Hex()
		result["fv"] = fmt.Sprintf("0x%x", orZero(v))
		result["fr"] = fmt.Sprintf("0x%x", orZero(r))
		result["fs"] = fmt.Sprintf("0x%x", orZero(s))
		result["feePayerSignatures"] = []interface{}{map[string]interface{}{
			"v": v.String(),
			"r": fmt.Sprintf("0x%x", r),
			"s": fmt.Sprintf("0x%x", s),
		}}
	}

	return result
}

// feeDelegation returns the fee payer and its signature of a fee delegation
// transaction, as the chain profile decoded it.
func (h *Handler) feeDelegation(tx *model.Transaction) (common.Address, *big.Int, *big.Int, *big.Int, bool) {
	fd, ok := chains.FeeDelegationOf(tx)
	if !ok {
		return common.Address{}, nil, nil, nil, false
	}
	return fd.Payer, fd.V, fd.R, fd.S, true
}

// models reads blocks and transactions as the chain-neutral model.

func orZero(x *big.Int) *big.Int {
	if x == nil {
		return new(big.Int)
	}
	return x
}

// receiptToJSON converts a receipt to JSON-friendly format
func (h *Handler) receiptToJSON(receipt *types.Receipt) map[string]interface{} {
	logs := make([]interface{}, len(receipt.Logs))
	for i, log := range receipt.Logs {
		topics := make([]interface{}, len(log.Topics))
		for j, topic := range log.Topics {
			topics[j] = topic.Hex()
		}

		logs[i] = map[string]interface{}{
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
	}

	result := map[string]interface{}{
		"transactionHash":   receipt.TxHash.Hex(),
		"transactionIndex":  fmt.Sprintf("0x%x", receipt.TransactionIndex),
		"blockHash":         receipt.BlockHash.Hex(),
		"blockNumber":       fmt.Sprintf("0x%x", receipt.BlockNumber),
		"from":              nil, // Not available in receipt
		"to":                nil, // Not available in receipt
		"cumulativeGasUsed": fmt.Sprintf("0x%x", receipt.CumulativeGasUsed),
		"effectiveGasPrice": fmt.Sprintf("0x%x", receipt.EffectiveGasPrice),
		"gasUsed":           fmt.Sprintf("0x%x", receipt.GasUsed),
		"contractAddress":   nil,
		"logs":              logs,
		"logsBloom":         fmt.Sprintf("0x%x", receipt.Bloom[:]),
		"type":              fmt.Sprintf("0x%x", receipt.Type),
		"status":            fmt.Sprintf("0x%x", receipt.Status),
	}

	if receipt.ContractAddress != (common.Address{}) {
		result["contractAddress"] = receipt.ContractAddress.Hex()
	}

	return result
}

package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	abiDecoder "github.com/0xmhha/indexer-go/abi"
	"github.com/0xmhha/indexer-go/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// Handler handles JSON-RPC method calls
type Handler struct {
	storage       storage.Storage
	logger        *zap.Logger
	filterManager *FilterManager
	abiDecoder    *abiDecoder.Decoder
}

// NewHandler creates a new JSON-RPC handler
func NewHandler(store storage.Storage, logger *zap.Logger) *Handler {
	return &Handler{
		storage:       store,
		logger:        logger,
		filterManager: NewFilterManager(5 * time.Minute), // 5 minute filter timeout
		abiDecoder:    abiDecoder.NewDecoder(),
	}
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
	// System contract methods
	case "getTotalSupply":
		return h.getTotalSupply(ctx, params)
	case "getActiveMinters":
		return h.getActiveMinters(ctx, params)
	case "getMinterAllowance":
		return h.getMinterAllowance(ctx, params)
	case "getActiveValidators":
		return h.getActiveValidators(ctx, params)
	case "getBlacklistedAddresses":
		return h.getBlacklistedAddresses(ctx, params)
	case "getProposal":
		return h.getProposal(ctx, params)
	case "getProposals":
		return h.getProposals(ctx, params)
	case "getProposalVotes":
		return h.getProposalVotes(ctx, params)
	case "getMintEvents":
		return h.getMintEvents(ctx, params)
	case "getBurnEvents":
		return h.getBurnEvents(ctx, params)
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
	// WBFT consensus methods
	case "getWBFTBlockExtra":
		return h.getWBFTBlockExtra(ctx, params)
	case "getWBFTBlockExtraByHash":
		return h.getWBFTBlockExtraByHash(ctx, params)
	case "getEpochInfo":
		return h.getEpochInfo(ctx, params)
	case "getLatestEpochInfo":
		return h.getLatestEpochInfo(ctx, params)
	case "getValidatorSigningStats":
		return h.getValidatorSigningStats(ctx, params)
	case "getAllValidatorsSigningStats":
		return h.getAllValidatorsSigningStats(ctx, params)
	case "getValidatorSigningActivity":
		return h.getValidatorSigningActivity(ctx, params)
	case "getBlockSigners":
		return h.getBlockSigners(ctx, params)
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
	default:
		return nil, NewError(MethodNotFound, fmt.Sprintf("method '%s' not found", method), nil)
	}
}

// getLatestHeight returns the latest indexed block height
func (h *Handler) getLatestHeight(ctx context.Context, params json.RawMessage) (interface{}, *Error) {
	height, err := h.storage.GetLatestHeight(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
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
		if err == storage.ErrNotFound {
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
		if err == storage.ErrNotFound {
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
		if err == storage.ErrNotFound {
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
		if err == storage.ErrNotFound {
			return nil, NewError(InternalError, "receipt not found", nil)
		}
		h.logger.Error("failed to get receipt", zap.String("hash", p.Hash), zap.Error(err))
		return nil, NewError(InternalError, "failed to get receipt", err.Error())
	}

	return h.receiptToJSON(receipt), nil
}

// blockToJSON converts a block to JSON-friendly format
func (h *Handler) blockToJSON(block *types.Block) map[string]interface{} {
	header := block.Header()

	txs := block.Transactions()
	transactions := make([]interface{}, len(txs))
	for i, tx := range txs {
		transactions[i] = tx.Hash().Hex()
	}

	uncles := block.Uncles()
	uncleHashes := make([]interface{}, len(uncles))
	for i, uncle := range uncles {
		uncleHashes[i] = uncle.Hash().Hex()
	}

	result := map[string]interface{}{
		"number":           fmt.Sprintf("0x%x", block.NumberU64()),
		"hash":             block.Hash().Hex(),
		"parentHash":       header.ParentHash.Hex(),
		"nonce":            fmt.Sprintf("0x%x", header.Nonce.Uint64()),
		"sha3Uncles":       header.UncleHash.Hex(),
		"logsBloom":        fmt.Sprintf("0x%x", header.Bloom[:]),
		"transactionsRoot": header.TxHash.Hex(),
		"stateRoot":        header.Root.Hex(),
		"receiptsRoot":     header.ReceiptHash.Hex(),
		"miner":            header.Coinbase.Hex(),
		"difficulty":       fmt.Sprintf("0x%x", header.Difficulty),
		"totalDifficulty":  nil, // Not available in types.Block
		"extraData":        fmt.Sprintf("0x%x", header.Extra),
		"size":             fmt.Sprintf("0x%x", block.Size()),
		"gasLimit":         fmt.Sprintf("0x%x", header.GasLimit),
		"gasUsed":          fmt.Sprintf("0x%x", header.GasUsed),
		"timestamp":        fmt.Sprintf("0x%x", header.Time),
		"transactions":     transactions,
		"uncles":           uncleHashes,
	}

	// EIP-1559: Base fee per gas
	if header.BaseFee != nil {
		result["baseFeePerGas"] = fmt.Sprintf("0x%x", header.BaseFee)
	}

	// Post-Shanghai: Withdrawals root
	if header.WithdrawalsHash != nil {
		result["withdrawalsRoot"] = header.WithdrawalsHash.Hex()
	}

	// EIP-4844: Blob gas fields
	if header.BlobGasUsed != nil {
		result["blobGasUsed"] = fmt.Sprintf("0x%x", *header.BlobGasUsed)
	}
	if header.ExcessBlobGas != nil {
		result["excessBlobGas"] = fmt.Sprintf("0x%x", *header.ExcessBlobGas)
	}

	return result
}

// transactionToJSON converts a transaction to JSON-friendly format
func (h *Handler) transactionToJSON(tx *types.Transaction, location *storage.TxLocation) map[string]interface{} {
	v, r, s := tx.RawSignatureValues()

	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		h.logger.Warn("failed to get transaction sender", zap.Error(err))
	}

	result := map[string]interface{}{
		"blockHash":        location.BlockHash.Hex(),
		"blockNumber":      fmt.Sprintf("0x%x", location.BlockHeight),
		"from":             from.Hex(),
		"gas":              fmt.Sprintf("0x%x", tx.Gas()),
		"gasPrice":         fmt.Sprintf("0x%x", tx.GasPrice()),
		"hash":             tx.Hash().Hex(),
		"input":            fmt.Sprintf("0x%x", tx.Data()),
		"nonce":            fmt.Sprintf("0x%x", tx.Nonce()),
		"to":               nil,
		"transactionIndex": fmt.Sprintf("0x%x", location.TxIndex),
		"value":            fmt.Sprintf("0x%x", tx.Value()),
		"type":             fmt.Sprintf("0x%x", tx.Type()),
		"v":                fmt.Sprintf("0x%x", v),
		"r":                fmt.Sprintf("0x%x", r),
		"s":                fmt.Sprintf("0x%x", s),
	}

	if tx.To() != nil {
		result["to"] = tx.To().Hex()
	}

	// EIP-1559 fields
	if tx.Type() >= types.DynamicFeeTxType {
		result["maxFeePerGas"] = fmt.Sprintf("0x%x", tx.GasFeeCap())
		result["maxPriorityFeePerGas"] = fmt.Sprintf("0x%x", tx.GasTipCap())
	}

	// Chain ID
	if tx.ChainId() != nil {
		result["chainId"] = fmt.Sprintf("0x%x", tx.ChainId())
	}

	// Access list for EIP-2930 and EIP-1559
	if tx.Type() >= types.AccessListTxType {
		accessList := tx.AccessList()
		accessListJSON := make([]interface{}, len(accessList))
		for i, entry := range accessList {
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

	// Fee Delegation transaction (type 0x16 = 22)
	// Using go-stablenet's extended Transaction type with FeePayer methods
	const FeeDelegateDynamicFeeTxType = 22
	if tx.Type() == FeeDelegateDynamicFeeTxType {
		// Extract fee payer address
		if feePayer := tx.FeePayer(); feePayer != nil {
			result["feePayer"] = feePayer.Hex()
		}

		// Extract fee payer signature values
		fv, fr, fs := tx.RawFeePayerSignatureValues()
		if fv != nil && fr != nil && fs != nil {
			result["feePayerSignatures"] = []interface{}{
				map[string]interface{}{
					"v": fmt.Sprintf("0x%x", fv),
					"r": fmt.Sprintf("0x%x", fr),
					"s": fmt.Sprintf("0x%x", fs),
				},
			}
		}

		h.logger.Debug("Fee Delegation transaction processed",
			zap.String("hash", tx.Hash().Hex()),
			zap.Uint8("type", uint8(tx.Type())))
	}

	return result
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

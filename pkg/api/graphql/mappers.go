package graphql

import (
	"context"
	"fmt"

	"github.com/0xmhha/indexer-go/pkg/abi"
	"github.com/0xmhha/indexer-go/pkg/storage"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"go.uber.org/zap"
)

// blockToMap converts a block to a GraphQL-friendly map
func (s *Schema) blockToMap(block *types.Block) map[string]interface{} {
	if block == nil {
		return nil
	}

	txs := block.Transactions()
	blockTimestamp := fmt.Sprintf("%d", block.Header().Time)
	transactions := make([]interface{}, len(txs))
	for i, tx := range txs {
		txMap := s.transactionToMap(tx, &storage.TxLocation{
			BlockHeight: block.NumberU64(),
			BlockHash:   block.Hash(),
			TxIndex:     uint64(i),
		})
		txMap["blockTimestamp"] = blockTimestamp
		transactions[i] = txMap
	}

	uncles := block.Uncles()
	uncleHashes := make([]interface{}, len(uncles))
	for i, uncle := range uncles {
		uncleHashes[i] = uncle.Hash().Hex()
	}

	result := map[string]interface{}{
		"number":           fmt.Sprintf("%d", block.NumberU64()),
		"hash":             block.Hash().Hex(),
		"parentHash":       block.ParentHash().Hex(),
		"timestamp":        fmt.Sprintf("%d", block.Time()),
		"nonce":            fmt.Sprintf("0x%x", block.Nonce()),
		"miner":            block.Coinbase().Hex(),
		"difficulty":       block.Difficulty().String(),
		"totalDifficulty":  nil, // Not available in types.Block
		"gasLimit":         fmt.Sprintf("%d", block.GasLimit()),
		"gasUsed":          fmt.Sprintf("%d", block.GasUsed()),
		"baseFeePerGas":    nil, // EIP-1559
		"extraData":        fmt.Sprintf("0x%x", block.Extra()),
		"size":             fmt.Sprintf("%d", block.Size()),
		"transactions":     transactions,
		"transactionCount": len(transactions),
		"uncles":           uncleHashes,
		"withdrawalsRoot":  nil, // Post-Shanghai
		"blobGasUsed":      nil, // EIP-4844
		"excessBlobGas":    nil, // EIP-4844
	}

	// EIP-1559: Base fee per gas
	if baseFee := block.BaseFee(); baseFee != nil {
		result["baseFeePerGas"] = baseFee.String()
	}

	// Post-Shanghai: Withdrawals root
	header := block.Header()
	if header.WithdrawalsHash != nil {
		result["withdrawalsRoot"] = header.WithdrawalsHash.Hex()
	}

	// EIP-4844: Blob gas fields
	if header.BlobGasUsed != nil {
		result["blobGasUsed"] = fmt.Sprintf("%d", *header.BlobGasUsed)
	}
	if header.ExcessBlobGas != nil {
		result["excessBlobGas"] = fmt.Sprintf("%d", *header.ExcessBlobGas)
	}

	return result
}

// transactionToMap converts a transaction to a GraphQL-friendly map
func (s *Schema) transactionToMap(tx *types.Transaction, location *storage.TxLocation) map[string]interface{} {
	if tx == nil {
		return nil
	}

	// Handle nil location
	var blockNumber string
	var blockHash string
	var txIndex int
	if location != nil {
		blockNumber = fmt.Sprintf("%d", location.BlockHeight)
		blockHash = location.BlockHash.Hex()
		txIndex = int(location.TxIndex)
	} else {
		blockNumber = "0"
		blockHash = "0x0000000000000000000000000000000000000000000000000000000000000000"
		txIndex = 0
	}

	// Handle signature values (can be nil for some tx types)
	v, r, sigS := tx.RawSignatureValues()
	var vStr, rStr, sStr string
	if v != nil {
		vStr = v.String()
	} else {
		vStr = "0"
	}
	if r != nil {
		rStr = fmt.Sprintf("0x%x", r.Bytes())
	} else {
		rStr = "0x0"
	}
	if sigS != nil {
		sStr = fmt.Sprintf("0x%x", sigS.Bytes())
	} else {
		sStr = "0x0"
	}

	// Handle potentially nil ChainId for sender derivation
	var from common.Address
	chainId := tx.ChainId()
	if chainId != nil {
		var err error
		from, err = types.Sender(types.LatestSignerForChainID(chainId), tx)
		if err != nil {
			s.logger.Warn("failed to get transaction sender", zap.Error(err))
		}
	}

	// Handle potentially nil Value
	var valueStr string
	if tx.Value() != nil {
		valueStr = tx.Value().String()
	} else {
		valueStr = "0"
	}

	result := map[string]interface{}{
		"hash":                 tx.Hash().Hex(),
		"blockNumber":          blockNumber,
		"blockHash":            blockHash,
		"transactionIndex":     txIndex,
		"from":                 from.Hex(),
		"to":                   nil,
		"contractAddress":      nil,
		"value":                valueStr,
		"gas":                  fmt.Sprintf("%d", tx.Gas()),
		"gasPrice":             nil,
		"maxFeePerGas":         nil,
		"maxPriorityFeePerGas": nil,
		"type":                 int(tx.Type()),
		"input":                fmt.Sprintf("0x%x", tx.Data()),
		"nonce":                fmt.Sprintf("%d", tx.Nonce()),
		"v":                    vStr,
		"r":                    rStr,
		"s":                    sStr,
		"chainId":              nil,
		"accessList":           nil,
		"receipt":              nil,
		"blockTimestamp":       nil,
		// Fee Delegation fields (type 0x16 = 22)
		"feePayer":           nil,
		"feePayerSignatures": nil,
		// EIP-7702 SetCode fields (type 0x04 = 4)
		"authorizationList": nil,
	}

	if tx.To() != nil {
		result["to"] = tx.To().Hex()
	} else {
		// Contract creation transaction - look up the receipt to get the contract address
		if s.storage != nil {
			receipt, err := s.storage.GetReceipt(context.Background(), tx.Hash())
			if err == nil && receipt != nil && receipt.ContractAddress != (common.Address{}) {
				result["contractAddress"] = receipt.ContractAddress.Hex()
			}
		}
	}

	if tx.GasPrice() != nil {
		result["gasPrice"] = tx.GasPrice().String()
	}

	if tx.GasFeeCap() != nil {
		result["maxFeePerGas"] = tx.GasFeeCap().String()
	}

	if tx.GasTipCap() != nil {
		result["maxPriorityFeePerGas"] = tx.GasTipCap().String()
	}

	if tx.ChainId() != nil {
		result["chainId"] = tx.ChainId().String()
	}

	// Access list for EIP-2930 and EIP-1559 transactions
	if tx.Type() >= types.AccessListTxType {
		accessList := tx.AccessList()
		accessListMap := make([]interface{}, len(accessList))
		for i, entry := range accessList {
			storageKeys := make([]interface{}, len(entry.StorageKeys))
			for j, key := range entry.StorageKeys {
				storageKeys[j] = key.Hex()
			}
			accessListMap[i] = map[string]interface{}{
				"address":     entry.Address.Hex(),
				"storageKeys": storageKeys,
			}
		}
		result["accessList"] = accessListMap
	}

	// EIP-7702 SetCode transaction (type 0x04 = 4)
	if tx.Type() == types.SetCodeTxType {
		authList := tx.SetCodeAuthorizations()
		if len(authList) > 0 {
			authListMap := make([]interface{}, len(authList))
			for i, auth := range authList {
				authEntry := map[string]interface{}{
					"chainId":   auth.ChainID.String(),
					"address":   auth.Address.Hex(),
					"nonce":     fmt.Sprintf("%d", auth.Nonce),
					"yParity":   int(auth.V),
					"r":         fmt.Sprintf("0x%x", auth.R.Bytes()),
					"s":         fmt.Sprintf("0x%x", auth.S.Bytes()),
					"authority": nil,
				}
				// Try to derive authority address
				if authority, err := auth.Authority(); err == nil {
					authEntry["authority"] = authority.Hex()
				}
				authListMap[i] = authEntry
			}
			result["authorizationList"] = authListMap
		}
	}

	// Fee Delegation transaction (type 0x16 = 22)
	// NOTE: Fee Delegation is a StableNet-specific feature
	// tx.FeePayer() and tx.RawFeePayerSignatureValues() are only available with go-stablenet
	// TODO: Implement proper extraction when using go-stablenet client
	const FeeDelegateDynamicFeeTxType = 22
	if tx.Type() == FeeDelegateDynamicFeeTxType {
		// Fee Delegation fields are StableNet-specific, not available in standard go-ethereum
		s.logger.Debug("Fee Delegation transaction detected (StableNet-specific)",
			zap.String("hash", tx.Hash().Hex()),
			zap.Uint8("type", uint8(tx.Type())))
	}

	return result
}

// receiptToMap converts a receipt to a GraphQL-friendly map
func (s *Schema) receiptToMap(receipt *types.Receipt) map[string]interface{} {
	if receipt == nil {
		return nil
	}

	logs := make([]interface{}, len(receipt.Logs))
	for i, log := range receipt.Logs {
		logs[i] = s.logToMap(log)
	}

	// Handle potentially nil fields
	var blockNumber string
	if receipt.BlockNumber != nil {
		blockNumber = fmt.Sprintf("%d", receipt.BlockNumber.Uint64())
	} else {
		blockNumber = "0"
	}

	var effectiveGasPrice string
	if receipt.EffectiveGasPrice != nil {
		effectiveGasPrice = receipt.EffectiveGasPrice.String()
	} else {
		effectiveGasPrice = "0"
	}

	result := map[string]interface{}{
		"transactionHash":   receipt.TxHash.Hex(),
		"blockNumber":       blockNumber,
		"blockHash":         receipt.BlockHash.Hex(),
		"transactionIndex":  int(receipt.TransactionIndex),
		"contractAddress":   nil,
		"gasUsed":           fmt.Sprintf("%d", receipt.GasUsed),
		"cumulativeGasUsed": fmt.Sprintf("%d", receipt.CumulativeGasUsed),
		"effectiveGasPrice": effectiveGasPrice,
		"status":            int(receipt.Status),
		"logs":              logs,
		"logsBloom":         fmt.Sprintf("0x%x", receipt.Bloom[:]),
	}

	if receipt.ContractAddress != (common.Address{}) {
		result["contractAddress"] = receipt.ContractAddress.Hex()
	}

	return result
}

// logToMap converts a log to a GraphQL-friendly map
// Always attempts to decode using known event signatures
func (s *Schema) logToMap(log *types.Log) map[string]interface{} {
	if log == nil {
		return nil
	}

	topics := make([]interface{}, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.Hex()
	}

	result := map[string]interface{}{
		"address":          log.Address.Hex(),
		"topics":           topics,
		"data":             fmt.Sprintf("0x%x", log.Data),
		"blockNumber":      fmt.Sprintf("%d", log.BlockNumber),
		"blockHash":        log.BlockHash.Hex(),
		"transactionHash":  log.TxHash.Hex(),
		"transactionIndex": int(log.TxIndex),
		"logIndex":         int(log.Index),
		"removed":          log.Removed,
		"decoded":          nil,
	}

	// Try to decode using known event signatures
	if decoded := abi.DecodeKnownEvent(log); decoded != nil {
		result["decoded"] = decodedEventLogToMap(decoded)
	}

	return result
}

// decodedEventLogToMap converts a DecodedEventLog to a GraphQL-friendly map
func decodedEventLogToMap(decoded *abi.DecodedEventLog) map[string]interface{} {
	if decoded == nil {
		return nil
	}

	params := make([]interface{}, len(decoded.Params))
	for i, param := range decoded.Params {
		params[i] = map[string]interface{}{
			"name":    param.Name,
			"type":    param.Type,
			"value":   param.Value,
			"indexed": param.Indexed,
		}
	}

	return map[string]interface{}{
		"eventName":      decoded.EventName,
		"eventSignature": decoded.EventSignature,
		"params":         params,
	}
}

// logToMapWithDecode converts a log to a GraphQL-friendly map with optional decoding
// Uses contract ABI if available, otherwise falls back to known event signatures
func (s *Schema) logToMapWithDecode(log *types.Log, decode bool) map[string]interface{} {
	if log == nil {
		return nil
	}

	topics := make([]interface{}, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.Hex()
	}

	result := map[string]interface{}{
		"address":          log.Address.Hex(),
		"topics":           topics,
		"data":             fmt.Sprintf("0x%x", log.Data),
		"blockNumber":      fmt.Sprintf("%d", log.BlockNumber),
		"blockHash":        log.BlockHash.Hex(),
		"transactionHash":  log.TxHash.Hex(),
		"transactionIndex": int(log.TxIndex),
		"logIndex":         int(log.Index),
		"removed":          log.Removed,
		"decoded":          nil,
	}

	if decode {
		// 1. Try contract-specific ABI first (if available)
		if s.abiDecoder != nil && s.abiDecoder.HasABI(log.Address) {
			decoded, err := s.abiDecoder.DecodeLog(log)
			if err == nil {
				// Convert to new format with structured params
				params := make([]interface{}, 0)
				for name, value := range decoded.Args {
					params = append(params, map[string]interface{}{
						"name":    name,
						"type":    "unknown", // ABI decoder doesn't provide type info in args
						"value":   fmt.Sprintf("%v", value),
						"indexed": false, // Would need to track this separately
					})
				}
				result["decoded"] = map[string]interface{}{
					"eventName":      decoded.EventName,
					"eventSignature": decoded.EventName, // Full signature not available from basic decoder
					"params":         params,
				}
				return result
			}
		}

		// 2. Fall back to known event signatures
		if knownDecoded := abi.DecodeKnownEvent(log); knownDecoded != nil {
			result["decoded"] = decodedEventLogToMap(knownDecoded)
		}
	}

	return result
}

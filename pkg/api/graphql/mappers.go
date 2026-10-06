package graphql

import (
	"context"
	"fmt"
	"math/big"

	"github.com/0xmhha/indexer-go/pkg/abi"
	"github.com/0xmhha/indexer-go/pkg/core/gethconv"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// blockToMap converts a block to a GraphQL-friendly map. Hashes are the ones
// the chain reports.
func (s *Schema) blockToMap(block *model.Block) map[string]interface{} {
	if block == nil {
		return nil
	}

	blockTimestamp := fmt.Sprintf("%d", block.Time)
	transactions := make([]interface{}, len(block.Transactions))
	for i, tx := range block.Transactions {
		txMap := s.transactionToMap(tx, &port.TxLocation{
			BlockHeight: block.Number,
			BlockHash:   block.Hash,
			TxIndex:     uint64(i),
		})
		txMap["blockTimestamp"] = blockTimestamp
		transactions[i] = txMap
	}

	uncleHashes := make([]interface{}, len(block.Uncles))
	for i, uncle := range block.Uncles {
		uncleHashes[i] = uncle.Hex()
	}

	difficulty := "0"
	if block.Difficulty != nil {
		difficulty = block.Difficulty.String()
	}

	result := map[string]interface{}{
		"number":           fmt.Sprintf("%d", block.Number),
		"hash":             block.Hash.Hex(),
		"parentHash":       block.ParentHash.Hex(),
		"timestamp":        blockTimestamp,
		"nonce":            fmt.Sprintf("0x%x", block.Nonce),
		"miner":            block.Miner.Hex(),
		"difficulty":       difficulty,
		"totalDifficulty":  nil, // not tracked
		"gasLimit":         fmt.Sprintf("%d", block.GasLimit),
		"gasUsed":          fmt.Sprintf("%d", block.GasUsed),
		"baseFeePerGas":    nil, // EIP-1559
		"extraData":        fmt.Sprintf("0x%x", block.Extra),
		"size":             fmt.Sprintf("%d", block.Size),
		"transactions":     transactions,
		"transactionCount": len(transactions),
		"uncles":           uncleHashes,
		"withdrawalsRoot":  nil, // Post-Shanghai
		"blobGasUsed":      nil, // EIP-4844
		"excessBlobGas":    nil, // EIP-4844
	}

	if block.BaseFee != nil {
		result["baseFeePerGas"] = block.BaseFee.String()
	}
	if block.WithdrawalsRoot != nil {
		result["withdrawalsRoot"] = block.WithdrawalsRoot.Hex()
	}
	if block.BlobGasUsed != nil {
		result["blobGasUsed"] = fmt.Sprintf("%d", *block.BlobGasUsed)
	}
	if block.ExcessBlobGas != nil {
		result["excessBlobGas"] = fmt.Sprintf("%d", *block.ExcessBlobGas)
	}

	return result
}

// transactionToMap converts a transaction to a GraphQL-friendly map. The
// hash, type and sender are the ones the chain reports, so a StableNet fee
// delegation transaction appears as type 22 under its own hash.
func (s *Schema) transactionToMap(tx *model.Transaction, location *port.TxLocation) map[string]interface{} {
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
	vStr, rStr, sStr := "0", "0x0", "0x0"
	if v := tx.Signature.V; v != nil {
		vStr = v.String()
	}
	if r := tx.Signature.R; r != nil {
		rStr = fmt.Sprintf("0x%x", r.Bytes())
	}
	if sigS := tx.Signature.S; sigS != nil {
		sStr = fmt.Sprintf("0x%x", sigS.Bytes())
	}

	valueStr := "0"
	if tx.Value != nil {
		valueStr = tx.Value.String()
	}

	result := map[string]interface{}{
		"hash":                 tx.Hash.Hex(),
		"blockNumber":          blockNumber,
		"blockHash":            blockHash,
		"transactionIndex":     txIndex,
		"from":                 tx.From.Hex(),
		"to":                   nil,
		"contractAddress":      nil,
		"value":                valueStr,
		"gas":                  fmt.Sprintf("%d", tx.Gas),
		"gasPrice":             nil,
		"maxFeePerGas":         nil,
		"maxPriorityFeePerGas": nil,
		"type":                 int(tx.Type),
		"input":                fmt.Sprintf("0x%x", tx.Input),
		"nonce":                fmt.Sprintf("%d", tx.Nonce),
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

	if tx.To != nil {
		result["to"] = tx.To.Hex()
	} else {
		// Contract creation transaction - look up the receipt to get the contract address
		if s.storage != nil {
			receipt, err := s.storage.GetReceipt(context.Background(), tx.Hash)
			if err == nil && receipt != nil && receipt.ContractAddress != (common.Address{}) {
				result["contractAddress"] = receipt.ContractAddress.Hex()
			}
		}
	}

	if tx.GasPrice != nil {
		result["gasPrice"] = tx.GasPrice.String()
	}
	if tx.GasFeeCap != nil {
		result["maxFeePerGas"] = tx.GasFeeCap.String()
	}
	if tx.GasTipCap != nil {
		result["maxPriorityFeePerGas"] = tx.GasTipCap.String()
	}
	if tx.ChainID != nil {
		result["chainId"] = tx.ChainID.String()
	}

	// Access list for EIP-2930 and later typed transactions
	if tx.Type >= types.AccessListTxType {
		accessListMap := make([]interface{}, len(tx.AccessList))
		for i, entry := range tx.AccessList {
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
	if tx.Type == types.SetCodeTxType && len(tx.AuthList) > 0 {
		authListMap := make([]interface{}, len(tx.AuthList))
		for i, auth := range tx.AuthList {
			authEntry := map[string]interface{}{
				"chainId":   bigString(auth.ChainID),
				"address":   auth.Address.Hex(),
				"nonce":     fmt.Sprintf("%d", auth.Nonce),
				"yParity":   int(auth.V),
				"r":         fmt.Sprintf("0x%x", bigBytes(auth.R)),
				"s":         fmt.Sprintf("0x%x", bigBytes(auth.S)),
				"authority": nil,
			}
			// Try to derive authority address
			gauth := gethconv.AuthToGeth(auth)
			if authority, err := gauth.Authority(); err == nil {
				authEntry["authority"] = authority.Hex()
			}
			authListMap[i] = authEntry
		}
		result["authorizationList"] = authListMap
	}

	// Fee Delegation transaction (type 0x16 = 22)
	if feePayer, v, r, sig, ok := s.feeDelegation(tx); ok {
		result["feePayer"] = feePayer.Hex()
		result["feePayerSignatures"] = []interface{}{map[string]interface{}{
			"v": v.String(),
			"r": fmt.Sprintf("0x%x", r),
			"s": fmt.Sprintf("0x%x", sig),
		}}
	}

	return result
}

// feeDelegation returns the fee payer and its signature of a fee delegation
// transaction, as the chain profile decoded it.
func (s *Schema) feeDelegation(tx *model.Transaction) (common.Address, *big.Int, *big.Int, *big.Int, bool) {
	fd, ok := chains.FeeDelegationOf(tx)
	if !ok {
		return common.Address{}, nil, nil, nil, false
	}
	return fd.Payer, fd.V, fd.R, fd.S, true
}

func bigString(x *big.Int) string {
	if x == nil {
		return "0"
	}
	return x.String()
}

func bigBytes(x *big.Int) []byte {
	if x == nil {
		return nil
	}
	return x.Bytes()
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

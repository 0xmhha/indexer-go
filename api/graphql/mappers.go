package graphql

import (
	"fmt"

	"github.com/0xmhha/indexer-go/storage"
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
	transactions := make([]interface{}, len(txs))
	for i, tx := range txs {
		transactions[i] = s.transactionToMap(tx, &storage.TxLocation{
			BlockHeight: block.NumberU64(),
			BlockHash:   block.Hash(),
			TxIndex:     uint64(i),
		})
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

	v, r, sigS := tx.RawSignatureValues()

	from, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), tx)
	if err != nil {
		s.logger.Warn("failed to get transaction sender", zap.Error(err))
	}

	result := map[string]interface{}{
		"hash":                 tx.Hash().Hex(),
		"blockNumber":          fmt.Sprintf("%d", location.BlockHeight),
		"blockHash":            location.BlockHash.Hex(),
		"transactionIndex":     int(location.TxIndex),
		"from":                 from.Hex(),
		"to":                   nil,
		"value":                tx.Value().String(),
		"gas":                  fmt.Sprintf("%d", tx.Gas()),
		"gasPrice":             nil,
		"maxFeePerGas":         nil,
		"maxPriorityFeePerGas": nil,
		"type":                 int(tx.Type()),
		"input":                fmt.Sprintf("0x%x", tx.Data()),
		"nonce":                fmt.Sprintf("%d", tx.Nonce()),
		"v":                    v.String(),
		"r":                    fmt.Sprintf("0x%x", r.Bytes()),
		"s":                    fmt.Sprintf("0x%x", sigS.Bytes()),
		"chainId":              nil,
		"accessList":           nil,
		"receipt":              nil,
		// Fee Delegation fields (type 0x16 = 22)
		"feePayer":           nil,
		"feePayerSignatures": nil,
	}

	if tx.To() != nil {
		result["to"] = tx.To().Hex()
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
					"v": fv.String(),
					"r": fmt.Sprintf("0x%x", fr.Bytes()),
					"s": fmt.Sprintf("0x%x", fs.Bytes()),
				},
			}
		}

		s.logger.Debug("Fee Delegation transaction processed",
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

	result := map[string]interface{}{
		"transactionHash":   receipt.TxHash.Hex(),
		"blockNumber":       fmt.Sprintf("%d", receipt.BlockNumber.Uint64()),
		"blockHash":         receipt.BlockHash.Hex(),
		"transactionIndex":  int(receipt.TransactionIndex),
		"contractAddress":   nil,
		"gasUsed":           fmt.Sprintf("%d", receipt.GasUsed),
		"cumulativeGasUsed": fmt.Sprintf("%d", receipt.CumulativeGasUsed),
		"effectiveGasPrice": receipt.EffectiveGasPrice.String(),
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
func (s *Schema) logToMap(log *types.Log) map[string]interface{} {
	if log == nil {
		return nil
	}

	topics := make([]interface{}, len(log.Topics))
	for i, topic := range log.Topics {
		topics[i] = topic.Hex()
	}

	return map[string]interface{}{
		"address":          log.Address.Hex(),
		"topics":           topics,
		"data":             fmt.Sprintf("0x%x", log.Data),
		"blockNumber":      fmt.Sprintf("%d", log.BlockNumber),
		"blockHash":        log.BlockHash.Hex(),
		"transactionHash":  log.TxHash.Hex(),
		"transactionIndex": int(log.TxIndex),
		"logIndex":         int(log.Index),
		"removed":          log.Removed,
	}
}

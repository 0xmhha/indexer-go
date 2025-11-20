package graphql

import (
	"github.com/graphql-go/graphql"
)

var (
	// Scalar types
	bytesType   = graphql.String
	bigIntType  = graphql.String
	addressType = graphql.String
	hashType    = graphql.String

	// Block type
	blockType *graphql.Object

	// Transaction type
	transactionType *graphql.Object

	// Receipt type
	receiptType *graphql.Object

	// Log type
	logType *graphql.Object

	// AccessListEntry type
	accessListEntryType *graphql.Object

	// FeePayerSignature type for Fee Delegation
	feePayerSignatureType *graphql.Object

	// PageInfo type
	pageInfoType *graphql.Object

	// BlockConnection type
	blockConnectionType *graphql.Object

	// TransactionConnection type
	transactionConnectionType *graphql.Object

	// LogConnection type
	logConnectionType *graphql.Object

	// Input types
	blockFilterType                 *graphql.InputObject
	transactionFilterType           *graphql.InputObject
	logFilterType                   *graphql.InputObject
	paginationInputType             *graphql.InputObject
	historicalTransactionFilterType *graphql.InputObject

	// Historical data types
	balanceSnapshotType          *graphql.Object
	balanceHistoryConnectionType *graphql.Object

	// Analytics types
	minerStatsType   *graphql.Object
	tokenBalanceType *graphql.Object
)

func init() {
	initTypes()
}

func initTypes() {
	// AccessListEntry type
	accessListEntryType = graphql.NewObject(graphql.ObjectConfig{
		Name: "AccessListEntry",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"storageKeys": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(hashType)),
			},
		},
	})

	// FeePayerSignature type for Fee Delegation transactions
	feePayerSignatureType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "FeePayerSignature",
		Description: "Signature from fee payer in Fee Delegation transactions",
		Fields: graphql.Fields{
			"v": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"r": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"s": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
		},
	})

	// Log type
	logType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Log",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"topics": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(hashType)),
			},
			"data": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"transactionIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"logIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"removed": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
		},
	})

	// Receipt type
	receiptType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Receipt",
		Fields: graphql.Fields{
			"transactionHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"transactionIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"contractAddress": &graphql.Field{
				Type: addressType,
			},
			"gasUsed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"cumulativeGasUsed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"effectiveGasPrice": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"status": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"logs": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(logType)),
			},
			"logsBloom": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
		},
	})

	// Transaction type
	transactionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Transaction",
		Fields: graphql.Fields{
			"hash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"blockHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"transactionIndex": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"from": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"to": &graphql.Field{
				Type: addressType,
			},
			"value": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"gas": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"gasPrice": &graphql.Field{
				Type: bigIntType,
			},
			"maxFeePerGas": &graphql.Field{
				Type: bigIntType,
			},
			"maxPriorityFeePerGas": &graphql.Field{
				Type: bigIntType,
			},
			"type": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"input": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"nonce": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"v": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"r": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"s": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"chainId": &graphql.Field{
				Type: bigIntType,
			},
			"accessList": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(accessListEntryType)),
			},
			"receipt": &graphql.Field{
				Type: receiptType,
			},
			// Fee Delegation fields (type 0x16)
			"feePayer": &graphql.Field{
				Type:        addressType,
				Description: "Fee payer address for Fee Delegation transactions (type 0x16)",
			},
			"feePayerSignatures": &graphql.Field{
				Type:        graphql.NewList(graphql.NewNonNull(feePayerSignatureType)),
				Description: "Fee payer signatures for Fee Delegation transactions",
			},
		},
	})

	// Block type
	blockType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Block",
		Fields: graphql.Fields{
			"number": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"hash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"parentHash": &graphql.Field{
				Type: graphql.NewNonNull(hashType),
			},
			"timestamp": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"nonce": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"miner": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"difficulty": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"totalDifficulty": &graphql.Field{
				Type: bigIntType,
			},
			"gasLimit": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"gasUsed": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			// EIP-1559 fields
			"baseFeePerGas": &graphql.Field{
				Type:        bigIntType,
				Description: "Base fee per gas for EIP-1559 blocks (post-London)",
			},
			"extraData": &graphql.Field{
				Type: graphql.NewNonNull(bytesType),
			},
			"size": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactions": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(transactionType)),
			},
			"transactionCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"uncles": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(hashType)),
			},
			// Post-merge fields
			"withdrawalsRoot": &graphql.Field{
				Type:        hashType,
				Description: "Withdrawals merkle root (post-Shanghai)",
			},
			// EIP-4844 blob fields
			"blobGasUsed": &graphql.Field{
				Type:        bigIntType,
				Description: "Total blob gas used in this block (EIP-4844)",
			},
			"excessBlobGas": &graphql.Field{
				Type:        bigIntType,
				Description: "Excess blob gas (EIP-4844)",
			},
		},
	})

	// PageInfo type
	pageInfoType = graphql.NewObject(graphql.ObjectConfig{
		Name: "PageInfo",
		Fields: graphql.Fields{
			"hasNextPage": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"hasPreviousPage": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Boolean),
			},
			"startCursor": &graphql.Field{
				Type: graphql.String,
			},
			"endCursor": &graphql.Field{
				Type: graphql.String,
			},
		},
	})

	// BlockConnection type
	blockConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BlockConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(blockType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// TransactionConnection type
	transactionConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "TransactionConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(transactionType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// LogConnection type
	logConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "LogConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(logType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// Input types
	blockFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "BlockFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"numberFrom": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"numberTo": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"timestampFrom": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"timestampTo": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"miner": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
		},
	})

	transactionFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "TransactionFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"blockNumberFrom": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"blockNumberTo": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"from": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
			"to": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
			"type": &graphql.InputObjectFieldConfig{
				Type: graphql.Int,
			},
		},
	})

	logFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "LogFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"address": &graphql.InputObjectFieldConfig{
				Type: addressType,
			},
			"topics": &graphql.InputObjectFieldConfig{
				Type: graphql.NewList(hashType),
			},
			"blockNumberFrom": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"blockNumberTo": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
		},
	})

	paginationInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "PaginationInput",
		Fields: graphql.InputObjectConfigFieldMap{
			"limit": &graphql.InputObjectFieldConfig{
				Type: graphql.Int,
			},
			"offset": &graphql.InputObjectFieldConfig{
				Type: graphql.Int,
			},
		},
	})

	historicalTransactionFilterType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "HistoricalTransactionFilter",
		Fields: graphql.InputObjectConfigFieldMap{
			"fromBlock": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"toBlock": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(bigIntType),
			},
			"minValue": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"maxValue": &graphql.InputObjectFieldConfig{
				Type: bigIntType,
			},
			"txType": &graphql.InputObjectFieldConfig{
				Type: graphql.Int,
			},
			"successOnly": &graphql.InputObjectFieldConfig{
				Type: graphql.Boolean,
			},
		},
	})

	// BalanceSnapshot type
	balanceSnapshotType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BalanceSnapshot",
		Fields: graphql.Fields{
			"blockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"balance": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"delta": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"transactionHash": &graphql.Field{
				Type: hashType,
			},
		},
	})

	// BalanceHistoryConnection type
	balanceHistoryConnectionType = graphql.NewObject(graphql.ObjectConfig{
		Name: "BalanceHistoryConnection",
		Fields: graphql.Fields{
			"nodes": &graphql.Field{
				Type: graphql.NewList(graphql.NewNonNull(balanceSnapshotType)),
			},
			"totalCount": &graphql.Field{
				Type: graphql.NewNonNull(graphql.Int),
			},
			"pageInfo": &graphql.Field{
				Type: graphql.NewNonNull(pageInfoType),
			},
		},
	})

	// MinerStats type
	minerStatsType = graphql.NewObject(graphql.ObjectConfig{
		Name: "MinerStats",
		Fields: graphql.Fields{
			"address": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"blockCount": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"lastBlockNumber": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
		},
	})

	// TokenBalance type
	tokenBalanceType = graphql.NewObject(graphql.ObjectConfig{
		Name: "TokenBalance",
		Fields: graphql.Fields{
			"contractAddress": &graphql.Field{
				Type: graphql.NewNonNull(addressType),
			},
			"tokenType": &graphql.Field{
				Type: graphql.NewNonNull(graphql.String),
			},
			"balance": &graphql.Field{
				Type: graphql.NewNonNull(bigIntType),
			},
			"tokenId": &graphql.Field{
				Type: bigIntType,
			},
		},
	})
}
